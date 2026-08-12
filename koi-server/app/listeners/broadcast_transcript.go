// Package listeners 定义应用级事件监听器。
package listeners

import (
	"fmt"

	"github.com/goravel/framework/contracts/event"

	"koi-server/app/broadcasting"
	"koi-server/app/facades"
	"koi-server/packages/socketio"
)

// BroadcastTranscript 把转写结果广播到客户端的私有频道。
type BroadcastTranscript struct{}

// Signature 监听器唯一标识。
func (r *BroadcastTranscript) Signature() string {
	return "audio:broadcast_transcript"
}

// Queue 实时转写要求低延迟，固定同步执行，不进入队列。
func (r *BroadcastTranscript) Queue(args ...any) event.Queue {
	return event.Queue{Enable: false}
}

// Handle 解析事件参数并广播增强版转写结果。
//
// 向后兼容旧版参数（3 个参数：clientID, text, isFinal），
// 同时支持完整参数（含说话人对象与时间戳）。
func (r *BroadcastTranscript) Handle(args ...any) error {
	if len(args) < 3 {
		return fmt.Errorf("broadcast transcript: expected at least 3 arguments, got %d", len(args))
	}

	clientID, ok := args[0].(string)
	if !ok || clientID == "" {
		return fmt.Errorf("broadcast transcript: invalid client id %v", args[0])
	}
	text, ok := args[1].(string)
	if !ok {
		return fmt.Errorf("broadcast transcript: invalid text %v", args[1])
	}
	isFinal, ok := args[2].(bool)
	if !ok {
		return fmt.Errorf("broadcast transcript: invalid isFinal %v", args[2])
	}

	channel := broadcasting.PrivateChannel(clientID)

	// ── 解析增强参数（仅最终结果才携带完整的说话人和时间戳信息） ──
	var (
		startMs            int64
		endMs              int64
		speakerName        string
		speakerID          *uint
		meetingID          uint
		wordTimestamps     string
		speakerDescription string
		hasEnhanced        bool
	)
	if len(args) >= 9 && isFinal {
		// 仅最终结果才解析增强参数：中间结果没有经过说话人识别管线，
		// 其 SpeakerName / SpeakerID 均为零值，下发会错误地覆盖前端已展示的说话人。
		hasEnhanced = true
		startMs, _ = toInt64(args[3])
		endMs, _ = toInt64(args[4])
		speakerName, _ = args[5].(string)
		speakerID, _ = args[6].(*uint)
		meetingID, _ = toUint(args[7])
		wordTimestamps, _ = args[8].(string)
	}
	if len(args) >= 10 {
		speakerDescription, _ = args[9].(string)
	}

	// 构建 speaker 子对象，供各事件复用。
	buildSpeaker := func() map[string]any {
		if speakerID != nil {
			return map[string]any{
				"id":          *speakerID,
				"name":        speakerName,
				"description": speakerDescription,
			}
		}
		return map[string]any{"name": speakerName}
	}

	// ── 基础转写结果（向后兼容） ──
	// 中间结果仅下发 text + isFinal，不携带说话人信息，
	// 避免空说话人覆盖前端已展示的正确结果。
	transcriptPayload := map[string]any{
		"text":    text,
		"isFinal": isFinal,
	}
	if hasEnhanced && text != "" {
		transcriptPayload["speaker"] = buildSpeaker()
		transcriptPayload["startMs"] = startMs
		transcriptPayload["endMs"] = endMs
		transcriptPayload["meetingId"] = meetingID
	}
	facades.Socketio().EmitToRoom(
		socketio.DefaultNamespace,
		channel,
		broadcasting.EventTranscript,
		transcriptPayload,
	)

	// ── 增强版转写结果（仅最终结果，含说话人对象 + 词级时间戳） ──
	if hasEnhanced && text != "" {
		facades.Socketio().EmitToRoom(
			socketio.DefaultNamespace,
			channel,
			broadcasting.EventTranscriptEnhanced,
			map[string]any{
				"text":           text,
				"isFinal":        isFinal,
				"startMs":        startMs,
				"endMs":          endMs,
				"meetingId":      meetingID,
				"wordTimestamps": wordTimestamps,
				"speaker":        buildSpeaker(),
			},
		)

		// 识别到明确说话人时额外推送独立的说话人识别事件。
		if speakerID != nil && speakerName != "未知说话人" {
			facades.Socketio().EmitToRoom(
				socketio.DefaultNamespace,
				channel,
				broadcasting.EventSpeakerIdentified,
				map[string]any{
					"speaker":   buildSpeaker(),
					"meetingId": meetingID,
				},
			)
		}
	}

	// ── 纯说话人识别事件（text 为空时只发 speaker-identified） ──
	if hasEnhanced && text == "" && speakerID != nil && speakerName != "未知说话人" {
		facades.Socketio().EmitToRoom(
			socketio.DefaultNamespace,
			channel,
			broadcasting.EventSpeakerIdentified,
			map[string]any{
				"speaker":   buildSpeaker(),
				"meetingId": meetingID,
			},
		)
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
