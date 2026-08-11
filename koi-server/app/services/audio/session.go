package audio

import (
	"os"
	"sync"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// chunk 一帧待处理的音频分片。
type chunk struct {
	data []byte
	// last 标记该帧是否为本次会话的结束帧。
	last bool
}

// session 表示单个客户端的转写会话。
//
// 并发模型：
//   - stream / recognizer / tempFile / tempName / samples / batch / utteranceAt /
//     lastSentText / lastSentAt 仅由该会话的工作协程访问，无需加锁；
//   - transcript / activity / pending 会被其它协程读写，统一由 mu 保护；
//   - chunks 只写不关闭。工作协程退出后剩余分片由 GC 回收，
//     从根本上杜绝「向已关闭通道发送数据」导致的 panic。
type session struct {
	clientID string

	chunks   chan chunk
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once

	// 工作协程私有状态
	stream       *sherpa.OnlineStream
	recognizer   *sherpa.OnlineRecognizer
	tempFile     *os.File
	tempName     string
	samples      []float32
	batch        int
	utteranceAt  time.Time
	lastSentText string
	lastSentAt   time.Time

	// 跨协程共享状态
	mu         sync.Mutex
	transcript string
	activity   time.Time
	pending    *sherpa.OnlineRecognizer

	// --- 时间戳追踪（仅工作协程访问，无需加锁）---
	// totalSamples 自会话开始累计的 PCM 采样数（用于计算相对时间）。
	totalSamples int64
	// utteranceStart 当前语音段的起始采样位置。
	utteranceStart int64
	// lastCommitEnd 上一次提交结束的采样位置。
	lastCommitEnd int64
	// utteranceHasText 当前语音段是否已有文本内容。
	utteranceHasText bool

	// --- 说话人识别（仅工作协程访问，无需加锁）---
	// utterancePCM 当前语音段的原始 PCM 缓冲（16bit 小端），用于说话人识别。
	utterancePCM []byte
}

// newSession 创建会话，队列容量由配置决定。
func newSession(clientID string, queueSize int, recognizer *sherpa.OnlineRecognizer, stream *sherpa.OnlineStream, tempFile *os.File, tempName string) *session {
	now := time.Now()
	return &session{
		clientID:    clientID,
		chunks:      make(chan chunk, queueSize),
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
		stream:      stream,
		recognizer:  recognizer,
		tempFile:    tempFile,
		tempName:    tempName,
		samples:     make([]float32, 0, 2048),
		utteranceAt: now,
		lastSentAt:  now,
		activity:    now,
	}
}

// touch 刷新最近活跃时间。
func (s *session) touch() {
	s.mu.Lock()
	s.activity = time.Now()
	s.mu.Unlock()
}

// activityAt 返回最近活跃时间。
func (s *session) activityAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activity
}

// appendTranscript 追加一段已确定的文本到完整转写结果。
func (s *session) appendTranscript(text string) {
	s.mu.Lock()
	s.transcript += text
	s.mu.Unlock()
}

// text 返回当前累积的完整转写结果。
func (s *session) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transcript
}

// setPending 登记一个待生效的识别器（热词变更时使用）。
// 真正的切换由工作协程在两帧之间完成，避免与解码过程产生竞争。
func (s *session) setPending(recognizer *sherpa.OnlineRecognizer) {
	s.mu.Lock()
	s.pending = recognizer
	s.mu.Unlock()
}

// takePending 取出并清空待生效的识别器。
func (s *session) takePending() *sherpa.OnlineRecognizer {
	s.mu.Lock()
	defer s.mu.Unlock()
	recognizer := s.pending
	s.pending = nil
	return recognizer
}

// resetUtterance 在一句话结束后重置断句相关状态。
func (s *session) resetUtterance() {
	now := time.Now()
	s.utteranceAt = now
	s.lastSentText = ""
	s.lastSentAt = now
}

// requestStop 通知工作协程收尾退出，可安全重复调用。
func (s *session) requestStop() {
	s.stopOnce.Do(func() {
		close(s.stop)
	})
}

// --- 时间戳追踪 ---

// addSamples 累计采样计数。
func (s *session) addSamples(n int) {
	s.totalSamples += int64(n)
}

// currentOffsetMs 返回当前时间偏移（毫秒），基于累计采样数和采样率。
func (s *session) currentOffsetMs(sampleRate int) int64 {
	if sampleRate <= 0 {
		return 0
	}
	return s.totalSamples * 1000 / int64(sampleRate)
}

// markUtteranceStart 标记当前语音段起始采样位置。
func (s *session) markUtteranceStart() {
	if !s.utteranceHasText {
		s.utteranceStart = s.totalSamples
		s.utteranceHasText = true
	}
}

// utteranceStartMs 返回当前语音段起始时间的毫秒偏移。
func (s *session) utteranceStartMs(sampleRate int) int64 {
	if sampleRate <= 0 {
		return 0
	}
	return s.utteranceStart * 1000 / int64(sampleRate)
}

// commitEndMs 返回当前语音段结束时间的毫秒偏移。
func (s *session) commitEndMs(sampleRate int) int64 {
	return s.currentOffsetMs(sampleRate)
}

// resetUtteranceTracking 重置单个语音段的追踪状态。
func (s *session) resetUtteranceTracking() {
	s.utteranceStart = s.totalSamples
	s.lastCommitEnd = s.totalSamples
	s.utteranceHasText = false
	s.utterancePCM = s.utterancePCM[:0]
}

// --- 说话人识别 PCM 缓冲 ---

// collectUtterancePCM 积累当前语音段的 PCM 数据。
func (s *session) collectUtterancePCM(data []byte) {
	s.utterancePCM = append(s.utterancePCM, data...)
}

// flushUtterancePCM 取出并清空当前语音段的 PCM 缓冲区。
func (s *session) flushUtterancePCM() []byte {
	data := s.utterancePCM
	s.utterancePCM = nil
	return data
}

// utteranceDuration 返回当前缓冲的语音段时长（秒）。
func (s *session) utteranceDuration(sampleRate int) float64 {
	if sampleRate <= 0 {
		return 0
	}
	// 16bit PCM: 每个采样占 2 字节
	return float64(len(s.utterancePCM)/2) / float64(sampleRate)
}
