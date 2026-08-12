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
//	args[0]  string 客户端 ID
//	args[1]  string 转写文本
//	args[2]  bool   是否为最终结果
//	args[3]  int64  起始毫秒偏移
//	args[4]  int64  结束毫秒偏移
//	args[5]  string 说话人名称
//	args[6]  *uint  说话人ID（nil = 未知）
//	args[7]  uint   会议ID（0 = 未绑定）
//	args[8]  string 词级时间戳JSON
//	args[9]  string 说话人描述
type TranscriptGenerated struct{}

// Handle 事件本身不做加工，原样透传给监听器。
func (r *TranscriptGenerated) Handle(args []event.Arg) ([]event.Arg, error) {
	return args, nil
}
