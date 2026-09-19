// Package audio 定义音频实时转写模块对外暴露的契约。
//
// 上层组件（HTTP/Socket.IO 控制器、事件监听器、队列任务、调度任务）一律依赖
// 本包中的接口，而不依赖 app/services/audio 中的具体实现，从而满足 Goravel
// 的契约设计原则：实现可通过 IoC 容器替换，单元测试可注入替身。
package audio

import (
	"errors"
	"fmt"
	"time"
)

// Binding 音频转写服务在 IoC 容器中的绑定键。
const Binding = "koi.audio.transcriber"

// SpeakerNameMaxRunes 说话人名称长度上限（与 HTTP 注册接口的校验保持一致）。
const SpeakerNameMaxRunes = 32

// 动态注册说话人过程中可能返回的业务错误。
// 定义在契约层，便于 Socket.IO 控制器把失败原因转换为面向用户的提示文案。
var (
	// ErrSpeakerNameRequired 未提供说话人名称。
	ErrSpeakerNameRequired = errors.New("请填写说话人名称")
	// ErrSpeakerNameTooLong 说话人名称超出长度上限。
	ErrSpeakerNameTooLong = fmt.Errorf("说话人名称不能超过 %d 个字符", SpeakerNameMaxRunes)
	// ErrMeetingNotBound 客户端尚未加入会议，无法确定音频归属的会议。
	ErrMeetingNotBound = errors.New("当前未加入会议，无法注册说话人")
	// ErrMeetingMismatch 请求中的会议与当前连接绑定的会议不一致。
	// 片段时间是相对「本连接音频起点」的偏移，跨会议使用会造成归属错乱，故直接拒绝。
	ErrMeetingMismatch = errors.New("请求的会议与当前连接绑定的会议不一致")
	// ErrSpeakerRegistrationUnavailable 说话人声纹相关依赖不可用。
	ErrSpeakerRegistrationUnavailable = errors.New("说话人声纹服务不可用")
)

// ModelStatus 描述语音识别模型的加载状态，用于健康检查与前端提示。
type ModelStatus struct {
	// Error 为模型加载失败的原因，加载成功时为空字符串。
	Error string `json:"error,omitempty"`
	// Loaded 表示模型是否已就绪。
	Loaded bool `json:"loaded"`
}

// Result 表示一次转写输出。
type Result struct {
	// ClientID 产生该结果的客户端标识（Socket.IO 连接 ID）。
	ClientID string `json:"clientId"`
	// Text 本次转写出的文本。
	Text string `json:"text"`
	// IsFinal 为 true 表示识别到句子端点，该文本已确定不再变化；
	// 为 false 表示这是仍可能被修正的中间结果。
	IsFinal bool `json:"isFinal"`
	// StartMs 语句起始时间（毫秒，相对音频开头）。
	StartMs int64 `json:"startMs"`
	// EndMs 语句结束时间（毫秒，相对音频开头）。
	EndMs int64 `json:"endMs"`
	// SpeakerName 识别的说话人名称，未识别时为"未知说话人"。
	SpeakerName string `json:"speakerName"`
	// SpeakerID 识别的说话人ID，未识别时为 nil。
	SpeakerID *uint `json:"speakerId"`
	// SpeakerDescription 说话人描述（如"技术部产品经理"），仅在识别到说话人时填充。
	SpeakerDescription string `json:"speakerDescription"`
	// MeetingID 关联的会议ID，未绑定会议时为 0。
	MeetingID uint `json:"meetingId"`
	// WordTimestamps 词级时间戳 JSON。
	WordTimestamps string `json:"wordTimestamps"`
}

// SpeakerRegistration 描述一次「从实时转写片段动态注册说话人」的请求。
//
// 时间戳与会话转写结果使用同一时间基准（毫秒，相对本次音频会话起点），
// 服务端据此从会话录音中截取对应音频并提取声纹。
type SpeakerRegistration struct {
	// MeetingID 会议ID，0 表示使用客户端当前绑定的会议。
	MeetingID uint
	// StartMs 选中片段的起始毫秒。
	StartMs int64
	// EndMs 选中片段的结束毫秒。
	EndMs int64
	// Name 说话人名称（必填，全局唯一）。
	Name string
	// Description 说话人描述，可空。
	Description string
	// Text 选中的转写文本，作为声纹音频备注保存，便于回溯。
	Text string
}

// SpeakerRegistrationResult 动态注册说话人的结果。
type SpeakerRegistrationResult struct {
	// SpeakerID 说话人ID。
	SpeakerID uint
	// SpeakerName 说话人名称。
	SpeakerName string
	// SpeakerDescription 说话人描述。
	SpeakerDescription string
	// MeetingID 实际关联的会议ID。
	MeetingID uint
	// StartMs 本次注册使用音频段的起始毫秒。
	StartMs int64
	// EndMs 本次注册使用音频段的结束毫秒。
	EndMs int64
	// AudioDuration 参与声纹提取的音频时长（秒）。
	AudioDuration float64
	// ValidDuration 去除静音后的有效语音时长（秒）。
	ValidDuration float64
	// RelabeledCount 被重新归属到该说话人的历史转写条数。
	RelabeledCount int64
}

// Publisher 转写结果发布器契约。
//
// 由基础设施层实现（派发框架事件 -> 监听器广播到 Socket.IO 频道），
// 使转写服务与通信层彻底解耦：转写服务不感知 Socket.IO 的存在。
type Publisher interface {
	// Publish 发布一条转写结果。实现必须并发安全，且不得阻塞调用方。
	Publish(result Result)
}

// RecordingArchiver 录音归档器契约。
//
// 会话结束时，转写服务把原始 PCM 临时文件交给归档器，由其负责转码落盘。
// 默认实现通过 Goravel 队列异步执行，避免阻塞转写工作协程。
type RecordingArchiver interface {
	// Archive 归档指定客户端的录音临时文件，并关联到会议。
	// meetingID 为 0 表示未绑定会议（仅归档，不关联）。
	// 实现负责在成功后删除临时文件；失败时应保留文件以便重试。
	Archive(clientID, tempFile string, meetingID uint) error
}

// Transcriber 音频实时转写服务契约。
type Transcriber interface {
	// Ready 报告语音模型是否已加载完成。
	Ready() bool

	// Status 返回模型加载状态。
	Status() ModelStatus

	// Push 接收一帧 16kHz / 16bit / 单声道小端 PCM 数据。
	// flag 为 0 表示这是本次会话的最后一帧，服务将执行收尾流程。
	// 该方法保证快速返回，不会阻塞调用方（Socket.IO 事件循环）。
	Push(clientID string, pcm []byte, flag int) error

	// Release 释放客户端会话，通常在连接断开时调用。
	Release(clientID string)

	// Transcript 返回客户端当前累积的完整转写文本。
	Transcript(clientID string) string

	// SetHotwords 设置热词及其权重，用于提升特定词汇的识别率。
	// 热词变更会热替换识别器，并对所有在线会话生效。
	SetHotwords(hotwords string, score float32) error

	// Hotwords 返回当前生效的热词及权重。
	Hotwords() (string, float32)

	// SegmentPCM 读取客户端会话中 [startMs, endMs) 区间的原始 16bit 单声道 PCM。
	// 时间基准与转写结果的 StartMs/EndMs 完全一致（相对本次会话音频起点）。
	SegmentPCM(clientID string, startMs, endMs int64) ([]byte, error)

	// RegisterSpeakerFromSegment 从会话音频的指定时间段提取声纹并动态注册说话人。
	// 注册结果同时写入内存声纹库与会议的说话人列表，使后续实时转写能够
	// 通过声纹检索识别并区分出该说话人。
	RegisterSpeakerFromSegment(clientID string, req SpeakerRegistration) (SpeakerRegistrationResult, error)

	// CleanupInactive 回收空闲时间超过 timeout 的会话，返回被回收的会话数。
	CleanupInactive(timeout time.Duration) int

	// Close 释放服务持有的全部资源，用于进程优雅退出。
	Close() error
}
