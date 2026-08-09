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

// Handle 解析事件参数并广播。
func (r *BroadcastTranscript) Handle(args ...any) error {
	if len(args) < 3 {
		return fmt.Errorf("broadcast transcript: expected 3 arguments, got %d", len(args))
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

	facades.Socketio().EmitToRoom(
		socketio.DefaultNamespace,
		broadcasting.PrivateChannel(clientID),
		broadcasting.EventTranscript,
		map[string]any{
			"text":    text,
			"isFinal": isFinal,
		},
	)
	return nil
}
