// Package events 定义应用级领域事件。
package events

import (
	"github.com/goravel/framework/contracts/event"
)

// TranscriptGenerated 实时转写结果事件。
//
// 转写服务每产出一条中间或最终结果即派发本事件，由监听器负责广播，
// 使转写逻辑与通信协议解耦，后续新增落库、审计等副作用只需追加监听器。
//
// 参数约定（顺序敏感）：
//
//	args[0] string 客户端 ID
//	args[1] string 转写文本
//	args[2] bool   是否为最终结果
type TranscriptGenerated struct{}

// Handle 事件本身不做加工，原样透传给监听器。
func (r *TranscriptGenerated) Handle(args []event.Arg) ([]event.Arg, error) {
	return args, nil
}
