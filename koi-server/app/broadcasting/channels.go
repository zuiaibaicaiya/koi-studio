// Package broadcasting 集中定义 Socket.IO 的事件名、频道命名规则与频道授权逻辑。
//
// 控制器在连接建立时完成授权并把连接加入其私有频道；事件监听器只依赖此处的
// 命名规则进行广播，两者互不感知，事件名也不再散落在业务代码中。
package broadcasting

import (
	"errors"
	"strings"
)

// 入站事件（客户端 -> 服务端）。
const (
	// EventConnection 连接建立。
	EventConnection = "connection"
	// EventDisconnect 连接断开。
	EventDisconnect = "disconnect"
	// EventHello 握手探测。
	EventHello = "hello"
	// EventWithBinary 音频分片上行，参数为 (二进制数据, 结束标志)。
	EventWithBinary = "with-binary"
	// EventMessage 文本消息回显。
	EventMessage = "message"
	// EventSetHotwords 设置识别热词。
	EventSetHotwords = "set-hotwords"
	// EventGetHotwords 查询当前热词。
	EventGetHotwords = "get-hotwords"
)

// 出站事件（服务端 -> 客户端）。
const (
	// EventWelcome 连接欢迎语。
	EventWelcome = "welcome"
	// EventHelloResponse 握手响应。
	EventHelloResponse = "hello-response"
	// EventWithBinaryResponse 音频分片接收确认。
	EventWithBinaryResponse = "with-binary-response"
	// EventTranscript 实时转写结果，负载为 {text, isFinal}。
	EventTranscript = "transcript"
	// EventHotwordsSet 热词设置成功，负载为 {hotwords, score}。
	EventHotwordsSet = "hotwords-set"
	// EventHotwordsData 热词查询结果，负载为 {hotwords, score}。
	EventHotwordsData = "hotwords-data"
	// EventHotwordsError 热词相关错误。
	EventHotwordsError = "hotwords-error"
	// EventError 通用错误通知。
	EventError = "error"
)

// privateChannelPrefix 客户端私有频道前缀。
const privateChannelPrefix = "private-client."

// PrivateChannel 返回某个客户端的私有频道名。
//
// 每个连接在建立时会被加入以自身连接 ID 命名的私有频道，
// 转写结果只投递到该频道，天然实现「结果只发给发起者」的隔离。
func PrivateChannel(clientID string) string {
	return privateChannelPrefix + clientID
}

// ErrUnauthorized 频道授权未通过。
var ErrUnauthorized = errors.New("broadcasting: unauthorized channel access")

// Authorize 校验 clientID 是否有权访问 channel。
//
// 规则：只允许访问与自身连接 ID 对应的私有频道，
// 防止客户端伪造频道名订阅他人的转写结果。
func Authorize(clientID, channel string) error {
	if clientID == "" || channel == "" {
		return ErrUnauthorized
	}
	if !strings.HasPrefix(channel, privateChannelPrefix) {
		return ErrUnauthorized
	}
	if channel != PrivateChannel(clientID) {
		return ErrUnauthorized
	}
	return nil
}
