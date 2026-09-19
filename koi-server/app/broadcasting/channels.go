// Package broadcasting 集中定义 Socket.IO 的事件名、频道命名规则与频道授权逻辑。
//
// 控制器在连接建立时完成授权并把连接加入其私有频道；事件监听器只依赖此处的
// 命名规则进行广播，两者互不感知，事件名也不再散落在业务代码中。
package broadcasting

import (
	"errors"
	"fmt"
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
	// EventJoinMeeting 客户端加入会议转写，参数 {meeting_id}。
	EventJoinMeeting = "join-meeting"
	// EventLeaveMeeting 客户端离开会议转写。
	EventLeaveMeeting = "leave-meeting"
	// EventRegisterSpeaker 客户端框选一段转写文字，请求把对应音频注册为新说话人，
	// 参数 {meeting_id, start_ms, end_ms, name, description?, text?, request_id?}。
	EventRegisterSpeaker = "register-speaker"
)

// 出站事件（服务端 -> 客户端）。
const (
	// EventWelcome 连接欢迎语。
	EventWelcome = "welcome"
	// EventHelloResponse 握手响应。
	EventHelloResponse = "hello-response"
	// EventWithBinaryResponse 音频分片接收确认。
	EventWithBinaryResponse = "with-binary-response"
	// EventHotwordsSet 热词设置成功，负载为 {hotwords, score}。
	EventHotwordsSet = "hotwords-set"
	// EventHotwordsData 热词查询结果，负载为 {hotwords, score}。
	EventHotwordsData = "hotwords-data"
	// EventHotwordsError 热词相关错误。
	EventHotwordsError = "hotwords-error"
	// EventTranscript 实时转写结果，负载为 {text, isFinal}（保留兼容）。
	EventTranscript = "transcript"
	// EventTranscriptEnhanced 增强版转写结果，含说话人、时间戳。
	EventTranscriptEnhanced = "transcript-enhanced"
	// EventSpeakerIdentified 说话人识别结果通知。
	EventSpeakerIdentified = "speaker-identified"
	// EventSpeakerRegistered 动态注册说话人的结果通知，负载
	// {success, requestId, meetingId, startMs, endMs, relabeled, speaker?, message?}。
	EventSpeakerRegistered = "speaker-registered"
	// EventJoinMeetingResponse 加入会议响应。
	EventJoinMeetingResponse = "join-meeting-response"
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

// viewerChannelPrefix 会议只读观众频道前缀。
const viewerChannelPrefix = "meeting-viewers."

// MeetingViewerChannel 返回某场会议的只读观众频道名。
//
// 第二屏投屏等只读客户端通过 join-meeting（role=viewer）加入该频道，
// 从而在不参与音频上行、不占用转写会话的前提下收到该会议的转写结果。
// 音频采集端不会加入该频道，因此不会重复收到自己的结果。
func MeetingViewerChannel(meetingID uint) string {
	return fmt.Sprintf("%s%d", viewerChannelPrefix, meetingID)
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
