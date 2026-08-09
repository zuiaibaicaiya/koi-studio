package api

import (
	"fmt"
	"runtime/debug"

	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/log"
	socketiolib "github.com/zishang520/socket.io/servers/socket/v3"

	"koi-server/app/broadcasting"
	contractsaudio "koi-server/app/contracts/audio"
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
	socketio contracts.Socketio
	audio    contractsaudio.Transcriber
	log      log.Log
}

// NewSocketioController 通过依赖注入构造控制器。
// 依赖在组合根（routes/api.go）中从容器解析，便于测试时替换为替身。
func NewSocketioController(socketio contracts.Socketio, transcriber contractsaudio.Transcriber, logger log.Log) *SocketioController {
	return &SocketioController{
		socketio: socketio,
		audio:    transcriber,
		log:      logger,
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
	r.onDisconnect(socket, clientID, channel)

	r.log.Info(fmt.Sprintf("socketio: client %s joined channel %s", clientID, channel))
	r.emit(socket, broadcasting.EventWelcome, welcomeMessage)

	return nil
}

// onDisconnect 连接断开时释放会话并退出频道。
func (r *SocketioController) onDisconnect(socket *socketiolib.Socket, clientID, channel string) {
	socket.On(broadcasting.EventDisconnect, func(args ...any) {
		defer r.recoverHandler(broadcasting.EventDisconnect, clientID)

		r.log.Info(fmt.Sprintf("socketio: client %s disconnected", clientID))

		// 触发转写收尾：冲刷解码残留、归档录音、回收识别流与协程。
		r.audio.Release(clientID)

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
//
// 该方法运行在 Socket.IO 事件循环上，必须保持轻量：
// 仅做参数解析与入队，解码在转写服务的客户端专属协程中完成。
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

	// 权重可选，缺省时由转写服务回落到配置中的默认值。
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

// on 注册事件处理器，统一承担 panic 恢复、错误日志与错误回执。
//
// 事件回调运行在库的内部协程中，未捕获的 panic 会直接终止整个进程，
// 因此所有处理器都必须经过这一层包装。
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
