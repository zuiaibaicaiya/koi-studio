// Package listeners 定义应用级事件监听器。
package listeners

import (
	"fmt"

	"github.com/goravel/framework/contracts/event"

	"koi-server/app/broadcasting"
	"koi-server/app/facades"
	"koi-server/packages/socketio"
)

// BroadcastTranscript 把转写结果广播到采集端的私有频道，
// 以及所属会议的只读观众频道（第二屏投屏等订阅端）。
type BroadcastTranscript struct{}

// Signature 监听器唯一标识。
func (r *BroadcastTranscript) Signature() string {
	return "audio:broadcast_transcript"
}

// Queue 实时转写要求低延迟，固定同步执行，不进入队列。
func (r *BroadcastTranscript) Queue(args ...any) event.Queue {
	return event.Queue{Enable: false}
}

// transcriptEvent 由转写事件的固定位置参数解析而来。
//
// 参数布局（由 audio 服务侧的 BroadcastTranscript 事件决定）：
//
//	[0] clientID            string  采集端连接 ID
//	[1] text                string  本句文本
//	[2] isFinal             bool    是否定稿
//	[3] startMs             int64   句起点（仅定稿携带有效值）
//	[4] endMs               int64   句终点（仅定稿携带有效值）
//	[5] speakerName         string  说话人名（仅定稿携带有效值）
//	[6] speakerID           *uint   说话人 ID（仅定稿携带有效值）
//	[7] meetingID           uint    会议 ID（中间结果同样携带）
//	[8] wordTimestamps      string  词级时间戳（仅定稿携带有效值）
//	[9] speakerDescription  string  说话人描述
type transcriptEvent struct {
	clientID           string
	text               string
	isFinal            bool
	startMs            int64
	endMs              int64
	speakerName        string
	speakerID          *uint
	meetingID          uint
	wordTimestamps     string
	speakerDescription string
	// enhanced 表示本次事件携带完整的说话人/时间戳参数。
	// 仅定稿结果才置位：中间结果没有经过说话人识别管线，
	// 其 SpeakerName / SpeakerID 均为零值，下发会错误地覆盖前端已展示的说话人。
	enhanced bool
}

// parseTranscriptEvent 解析事件参数，兼容旧版 3 参数布局。
func parseTranscriptEvent(args []any) (*transcriptEvent, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("broadcast transcript: expected at least 3 arguments, got %d", len(args))
	}

	clientID, ok := args[0].(string)
	if !ok || clientID == "" {
		return nil, fmt.Errorf("broadcast transcript: invalid client id %v", args[0])
	}
	text, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("broadcast transcript: invalid text %v", args[1])
	}
	isFinal, ok := args[2].(bool)
	if !ok {
		return nil, fmt.Errorf("broadcast transcript: invalid isFinal %v", args[2])
	}

	e := &transcriptEvent{clientID: clientID, text: text, isFinal: isFinal}

	// meetingID 与结果是否定稿无关：中间结果同样要投递到会议观众频道。
	if len(args) >= 8 {
		e.meetingID, _ = toUint(args[7])
	}
	if len(args) >= 9 && isFinal {
		e.enhanced = true
		e.startMs, _ = toInt64(args[3])
		e.endMs, _ = toInt64(args[4])
		e.speakerName, _ = args[5].(string)
		e.speakerID, _ = args[6].(*uint)
		e.wordTimestamps, _ = args[8].(string)
	}
	if len(args) >= 10 {
		e.speakerDescription, _ = args[9].(string)
	}

	return e, nil
}

// speaker 说话人子对象，供各事件复用。
func (e *transcriptEvent) speaker() map[string]any {
	if e.speakerID != nil {
		return map[string]any{
			"id":          *e.speakerID,
			"name":        e.speakerName,
			"description": e.speakerDescription,
		}
	}
	return map[string]any{"name": e.speakerName}
}

// identified 说话人是否已明确识别（排除占位的未知说话人）。
func (e *transcriptEvent) identified() bool {
	return e.enhanced && e.speakerID != nil && e.speakerName != "未知说话人"
}

// Handle 解析事件参数并广播转写结果。
func (r *BroadcastTranscript) Handle(args ...any) error {
	e, err := parseTranscriptEvent(args)
	if err != nil {
		return err
	}

	socketioFacade := facades.Socketio()
	privateChannel := broadcasting.PrivateChannel(e.clientID)
	viewerChannel := broadcasting.MeetingViewerChannel(e.meetingID)

	// ── 基础转写结果（向后兼容） ──
	// 中间结果仅下发 text + isFinal，不携带说话人信息，
	// 避免空说话人覆盖前端已展示的正确结果。
	transcriptPayload := map[string]any{
		"text":    e.text,
		"isFinal": e.isFinal,
	}
	if e.enhanced && e.text != "" {
		transcriptPayload["speaker"] = e.speaker()
		transcriptPayload["startMs"] = e.startMs
		transcriptPayload["endMs"] = e.endMs
		transcriptPayload["meetingId"] = e.meetingID
	}

	socketioFacade.EmitToRoom(socketio.DefaultNamespace, privateChannel,
		broadcasting.EventTranscript, transcriptPayload)

	// 会议观众频道（第二屏投屏等只读客户端）与私有频道投递相同负载。
	// 采集端不在该频道内，因此不会重复收到自己的转写结果。
	if e.meetingID > 0 {
		socketioFacade.EmitToRoom(socketio.DefaultNamespace, viewerChannel,
			broadcasting.EventTranscript, transcriptPayload)
	}

	// ── 增强版转写结果（仅定稿，含说话人对象 + 词级时间戳） ──
	if e.enhanced && e.text != "" {
		socketioFacade.EmitToRoom(socketio.DefaultNamespace, privateChannel,
			broadcasting.EventTranscriptEnhanced, map[string]any{
				"text":           e.text,
				"isFinal":        e.isFinal,
				"startMs":        e.startMs,
				"endMs":          e.endMs,
				"meetingId":      e.meetingID,
				"wordTimestamps": e.wordTimestamps,
				"speaker":        e.speaker(),
			})

		// 识别到明确说话人时额外推送独立的说话人识别事件。
		if e.identified() {
			socketioFacade.EmitToRoom(socketio.DefaultNamespace, privateChannel,
				broadcasting.EventSpeakerIdentified, map[string]any{
					"speaker":   e.speaker(),
					"meetingId": e.meetingID,
				})
		}
		return nil
	}

	// ── 纯说话人识别事件（text 为空时只发 speaker-identified） ──
	if e.identified() {
		socketioFacade.EmitToRoom(socketio.DefaultNamespace, privateChannel,
			broadcasting.EventSpeakerIdentified, map[string]any{
				"speaker":   e.speaker(),
				"meetingId": e.meetingID,
			})
	}

	return nil
}

// toInt64 安全转换任意类型为 int64。
func toInt64(v any) (int64, bool) {
	switch val := v.(type) {
	case int64:
		return val, true
	case int:
		return int64(val), true
	case int32:
		return int64(val), true
	case float64:
		return int64(val), true
	default:
		return 0, false
	}
}

// toUint 安全转换任意类型为 uint。
func toUint(v any) (uint, bool) {
	switch val := v.(type) {
	case uint:
		return val, true
	case int:
		return uint(val), true
	case int64:
		return uint(val), true
	case float64:
		return uint(val), true
	default:
		return 0, false
	}
}
