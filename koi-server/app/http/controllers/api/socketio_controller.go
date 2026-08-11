package api

import (
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/log"
	socketiolib "github.com/zishang520/socket.io/servers/socket/v3"

	"koi-server/app/broadcasting"
	contractsaudio "koi-server/app/contracts/audio"
	"koi-server/app/services"
	"koi-server/packages/socketio/contracts"
)

// 出站消息文案，保持与既有客户端协议一致。
const (
	welcomeMessage        = "欢迎连接到Socket.IO服务器"
	helloResponseMessage  = "Hello from server"
	binaryResponseMessage = "Audio data processed successfully"
)

// socketHandler 单个 Socket.IO 事件的处理函数。
// 返回 error 即代表处理失败，由统一的包装层负责记录日志并通知客户端。
type socketHandler func(socket *socketiolib.Socket, args ...any) error

// SocketioController 负责 Socket.IO 的连接管理与事件分发。
//
// 控制器只做协议适配：解析入站参数、调用领域服务、回写响应；
// 转写结果的下行由 events/listeners 完成，控制器不参与。
type SocketioController struct {
	socketio          contracts.Socketio
	audio             contractsaudio.Transcriber
	log               log.Log
	meetingService    *services.MeetingService
	sessionMgr        *services.MeetingSessionManager
	voiceprintSvc     *services.SpeakerVoiceprintService
	hotWordService    *services.HotWordService
	hotWordLibService *services.HotWordLibraryService
}

// NewSocketioController 通过依赖注入构造控制器。
func NewSocketioController(
	socketio contracts.Socketio,
	transcriber contractsaudio.Transcriber,
	logger log.Log,
	meetingService *services.MeetingService,
	sessionMgr *services.MeetingSessionManager,
	voiceprintSvc *services.SpeakerVoiceprintService,
	hotWordService *services.HotWordService,
	hotWordLibService *services.HotWordLibraryService,
) *SocketioController {
	return &SocketioController{
		socketio:          socketio,
		audio:             transcriber,
		log:               logger,
		meetingService:    meetingService,
		sessionMgr:        sessionMgr,
		voiceprintSvc:     voiceprintSvc,
		hotWordService:    hotWordService,
		hotWordLibService: hotWordLibService,
	}
}

// ServeSocketio 将 HTTP 请求交给 Socket.IO 引擎处理。
func (r *SocketioController) ServeSocketio(ctx http.Context) http.Response {
	r.socketio.ServeHTTP(ctx.Response().Writer(), ctx.Request().Origin())
	return nil
}

// RegisterHandlers 注册 Socket.IO 事件处理器。
func (r *SocketioController) RegisterHandlers() {
	r.socketio.On(broadcasting.EventConnection, r.handleConnection)
	r.log.Info("socketio: event handlers registered")
}

// handleConnection 处理新连接：完成频道授权、绑定事件、下发欢迎消息。
func (r *SocketioController) handleConnection(socket *socketiolib.Socket, _ ...any) error {
	clientID := string(socket.Id())

	// 频道授权：连接只能加入以自身连接 ID 命名的私有频道，
	// 转写结果通过该频道下行，杜绝跨客户端串音。
	channel := broadcasting.PrivateChannel(clientID)
	if err := broadcasting.Authorize(clientID, channel); err != nil {
		r.log.Warning(fmt.Sprintf("socketio: channel authorization denied for client %s: %v", clientID, err))
		r.emit(socket, broadcasting.EventError, "channel authorization denied")
		return err
	}
	if err := r.socketio.JoinRoom(socket, channel); err != nil {
		return fmt.Errorf("join channel %s: %w", channel, err)
	}

	r.on(socket, broadcasting.EventHello, r.handleHello)
	r.on(socket, broadcasting.EventWithBinary, r.handleAudioFrame)
	r.on(socket, broadcasting.EventMessage, r.handleMessage)
	r.on(socket, broadcasting.EventSetHotwords, r.handleSetHotwords)
	r.on(socket, broadcasting.EventGetHotwords, r.handleGetHotwords)
	r.on(socket, broadcasting.EventJoinMeeting, r.handleJoinMeeting)
	r.on(socket, broadcasting.EventLeaveMeeting, r.handleLeaveMeeting)
	r.onDisconnect(socket, clientID, channel)

	r.log.Info(fmt.Sprintf("socketio: client %s joined channel %s", clientID, channel))
	r.emit(socket, broadcasting.EventWelcome, welcomeMessage)

	return nil
}

// onDisconnect 连接断开时释放会话、退出频道、清理会议绑定。
func (r *SocketioController) onDisconnect(socket *socketiolib.Socket, clientID, channel string) {
	socket.On(broadcasting.EventDisconnect, func(args ...any) {
		defer r.recoverHandler(broadcasting.EventDisconnect, clientID)

		r.log.Info(fmt.Sprintf("socketio: client %s disconnected", clientID))

		// 触发转写收尾：冲刷解码残留、归档录音、回收识别流与协程。
		r.audio.Release(clientID)

		// 清理会议绑定
		r.sessionMgr.Unbind(clientID)

		if err := r.socketio.LeaveRoom(socket, channel); err != nil {
			r.log.Warning(fmt.Sprintf("socketio: failed to leave channel %s: %v", channel, err))
		}
	})
}

// handleHello 握手探测。
func (r *SocketioController) handleHello(socket *socketiolib.Socket, _ ...any) error {
	r.emit(socket, broadcasting.EventHelloResponse, helloResponseMessage)
	return nil
}

// handleMessage 文本消息回显。
func (r *SocketioController) handleMessage(socket *socketiolib.Socket, args ...any) error {
	if len(args) == 0 {
		return fmt.Errorf("missing message argument")
	}
	r.emit(socket, broadcasting.EventMessage, args[0])
	return nil
}

// handleAudioFrame 接收音频分片并交给转写服务。
func (r *SocketioController) handleAudioFrame(socket *socketiolib.Socket, args ...any) error {
	if len(args) < 2 {
		return fmt.Errorf("insufficient arguments for %s event", broadcasting.EventWithBinary)
	}

	// 模型尚未就绪时直接丢弃，避免音频在队列中堆积。
	if !r.audio.Ready() {
		if status := r.audio.Status(); status.Error != "" {
			return fmt.Errorf("speech model unavailable: %s", status.Error)
		}
		return nil
	}

	data, err := decodeAudioPayload(args[0])
	if err != nil {
		return err
	}
	flag, err := decodeAudioFlag(args[1])
	if err != nil {
		return err
	}

	if err := r.audio.Push(string(socket.Id()), data, flag); err != nil {
		return fmt.Errorf("failed to process audio data: %w", err)
	}

	r.emit(socket, broadcasting.EventWithBinaryResponse, binaryResponseMessage)
	return nil
}

// handleSetHotwords 设置识别热词。
func (r *SocketioController) handleSetHotwords(socket *socketiolib.Socket, args ...any) error {
	if len(args) == 0 {
		r.emit(socket, broadcasting.EventHotwordsError, "Missing hotwords argument")
		return fmt.Errorf("missing hotwords argument")
	}

	hotwords, ok := args[0].(string)
	if !ok {
		r.emit(socket, broadcasting.EventHotwordsError, "Hotwords must be a string")
		return fmt.Errorf("hotwords must be a string, got %T", args[0])
	}

	var score float32
	if len(args) >= 2 {
		switch v := args[1].(type) {
		case float64:
			score = float32(v)
		case float32:
			score = v
		case int:
			score = float32(v)
		}
	}

	if err := r.audio.SetHotwords(hotwords, score); err != nil {
		r.emit(socket, broadcasting.EventHotwordsError, "Failed to set hotwords: "+err.Error())
		return fmt.Errorf("failed to set hotwords: %w", err)
	}

	applied, appliedScore := r.audio.Hotwords()
	r.emit(socket, broadcasting.EventHotwordsSet, map[string]any{
		"hotwords": applied,
		"score":    appliedScore,
	})
	return nil
}

// handleGetHotwords 查询当前热词。
func (r *SocketioController) handleGetHotwords(socket *socketiolib.Socket, _ ...any) error {
	hotwords, score := r.audio.Hotwords()
	r.emit(socket, broadcasting.EventHotwordsData, map[string]any{
		"hotwords": hotwords,
		"score":    score,
	})
	return nil
}

// handleJoinMeeting 客户端加入会议转写。
//
// 客户端发送 {meeting_id}，服务端加载会议信息，自动配置热词与说话人识别上下文。
func (r *SocketioController) handleJoinMeeting(socket *socketiolib.Socket, args ...any) error {
	clientID := string(socket.Id())

	meetingID, err := r.parseMeetingID(args)
	if err != nil {
		r.emit(socket, broadcasting.EventError, fmt.Sprintf("Invalid meeting_id: %v", err))
		return err
	}

	// 加载会议信息
	meeting, err := r.meetingService.GetMeetingById(meetingID)
	if err != nil {
		r.emit(socket, broadcasting.EventError, "Meeting not found")
		return fmt.Errorf("meeting %d not found: %w", meetingID, err)
	}

	// 解析说话人ID列表
	speakerIDs := parseCommaSeparatedIDs(meeting.SpeakerIds)

	// 解析热词库ID列表
	hotWordLibIDs := parseCommaSeparatedIDs(meeting.HotWordLibraryIds)

	// 构建会议上下文
	ctx := &services.MeetingContext{
		MeetingID:         meeting.ID,
		SpeakerIDs:        speakerIDs,
		HotWordLibraryIDs: hotWordLibIDs,
		AudioStartTime:    time.Now(),
	}

	// 加载热词并应用到识别器
	hotwordsStr := r.buildHotwordsString(hotWordLibIDs)
	ctx.HotwordsStr = hotwordsStr
	if hotwordsStr != "" {
		if err := r.audio.SetHotwords(hotwordsStr, 0); err != nil {
			r.log.Warning(fmt.Sprintf("socketio: failed to set hotwords for meeting %d: %v", meetingID, err))
		}
	}

	// 预热说话人声纹库（确保会议选择的说话人在内存中）
	if len(speakerIDs) > 0 {
		if err := r.voiceprintSvc.Warmup(); err != nil {
			r.log.Warning(fmt.Sprintf("socketio: voiceprint warmup failed: %v", err))
		}
	}

	// 绑定客户端到会议
	r.sessionMgr.Bind(clientID, ctx)

	r.log.Info(fmt.Sprintf("socketio: client %s joined meeting %d (%s), speakers=%d, hotword_libs=%d",
		clientID, meetingID, meeting.Name, len(speakerIDs), len(hotWordLibIDs)))

	r.emit(socket, broadcasting.EventJoinMeetingResponse, map[string]any{
		"meetingId":        meeting.ID,
		"meetingName":      meeting.Name,
		"speakerCount":     len(speakerIDs),
		"hotwordLibCount":  len(hotWordLibIDs),
		"hotwordsApplied":  hotwordsStr != "",
	})
	return nil
}

// handleLeaveMeeting 客户端离开会议转写。
func (r *SocketioController) handleLeaveMeeting(socket *socketiolib.Socket, _ ...any) error {
	clientID := string(socket.Id())
	r.sessionMgr.Unbind(clientID)

	r.log.Info(fmt.Sprintf("socketio: client %s left meeting", clientID))
	r.emit(socket, broadcasting.EventLeaveMeeting, "left")
	return nil
}

// parseMeetingID 从事件参数中解析会议ID。
func (r *SocketioController) parseMeetingID(args []any) (int, error) {
	if len(args) == 0 {
		return 0, fmt.Errorf("missing meeting_id")
	}

	switch v := args[0].(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case string:
		return strconv.Atoi(v)
	case map[string]any:
		if id, ok := v["meeting_id"]; ok {
			switch idv := id.(type) {
			case float64:
				return int(idv), nil
			case string:
				return strconv.Atoi(idv)
			case int:
				return idv, nil
			}
		}
		return 0, fmt.Errorf("meeting_id not found in object")
	default:
		return 0, fmt.Errorf("unexpected type for meeting_id: %T", v)
	}
}

// buildHotwordsString 从指定的热词库ID列表中加载所有热词，格式化为 sherpa-onnx 格式。
//
// 输出格式：每行 "word weight"，行与行之间以 \n 分隔。
func (r *SocketioController) buildHotwordsString(libraryIDs []uint) string {
	if len(libraryIDs) == 0 {
		return ""
	}

	var lines []string

	for _, libID := range libraryIDs {
		// 先获取热词库信息（验证是否存在）
		library, err := r.hotWordLibService.GetLibraryById(int(libID))
		if err != nil {
			r.log.Warning(fmt.Sprintf("socketio: hotword library %d not found, skipping: %v", libID, err))
			continue
		}
		_ = library

		// 分页加载该库中的全部热词
		page, pageSize := 1, 500
		for {
			words, total, err := r.hotWordService.GetHotWordList(libID, page, pageSize, "")
			if err != nil {
				r.log.Warning(fmt.Sprintf("socketio: failed to load hotwords from library %d: %v", libID, err))
				break
			}

			for _, hw := range words {
				weight := hw.Weight
				if weight <= 0 {
					weight = 10
				}
				lines = append(lines, fmt.Sprintf("%s %d", hw.Word, weight))
			}

			if int64(page*pageSize) >= total {
				break
			}
			page++
		}
	}

	return strings.Join(lines, "\n")
}

// parseCommaSeparatedIDs 解析逗号分隔的ID字符串为 uint 切片。
func parseCommaSeparatedIDs(ids string) []uint {
	if ids == "" {
		return nil
	}

	parts := strings.Split(ids, ",")
	result := make([]uint, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			continue
		}
		result = append(result, uint(id))
	}

	return result
}

// on 注册事件处理器，统一承担 panic 恢复、错误日志与错误回执。
func (r *SocketioController) on(socket *socketiolib.Socket, event string, handler socketHandler) {
	clientID := string(socket.Id())

	socket.On(event, func(args ...any) {
		defer r.recoverHandler(event, clientID)

		if err := handler(socket, args...); err != nil {
			r.log.Warning(fmt.Sprintf("socketio: handler %q failed for client %s: %v", event, clientID, err))
			r.emit(socket, broadcasting.EventError, err.Error())
		}
	})
}

// recoverHandler 捕获事件处理过程中的 panic，防止单个连接的异常拖垮进程。
func (r *SocketioController) recoverHandler(event, clientID string) {
	if rec := recover(); rec != nil {
		r.log.Error(fmt.Sprintf("socketio: handler %q panicked for client %s: %v\n%s", event, clientID, rec, debug.Stack()))
	}
}

// emit 向客户端发送消息，失败只记录日志，不影响主流程。
func (r *SocketioController) emit(socket *socketiolib.Socket, event string, args ...any) {
	if err := socket.Emit(event, args...); err != nil {
		r.log.Warning(fmt.Sprintf("socketio: failed to emit %q to client %s: %v", event, socket.Id(), err))
	}
}
