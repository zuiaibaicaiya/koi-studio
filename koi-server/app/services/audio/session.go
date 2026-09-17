package audio

import (
	"math"
	"os"
	"strings"
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

// tokenEmit 记录一个 token 对应的真实音频采样位置（绝对，自会话开始），
// 用于把字/词级时间戳对齐到音频实际时间（而非按字符数均匀插值）。
type tokenEmit struct {
	token     string
	samplePos int64
}

// session 表示单个客户端的转写会话。
//
// 并发模型：
//   - stream / recognizer / tempFile / tempName / samples / batch /
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
	lastSentText string
	lastSentAt   time.Time

	// 跨协程共享状态
	mu         sync.Mutex
	transcript string
	activity   time.Time
	pending    *sherpa.OnlineRecognizer

	// --- 时间戳追踪（仅工作协程访问，无需加锁）---
	// sampleRate 音频采样率（Hz），用于采样位置与毫秒时间的换算。
	sampleRate int
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

	// --- 实时 token 时间戳追踪（仅工作协程访问，无需加锁）---
	//
	// emittedTokens 当前识别流自建立以来发射的全部 token 及其绝对采样位置。
	//
	// 关键设计：整场会话只使用**一条连续识别流**，不在一句话结束时 Reset。
	// 原因是 sherpa-onnx 的 OnlineStream.Reset() 只重置解码器状态，已经喂入但
	// 尚未被 chunk 消费的尾部音频仍留在特征管线里，Reset 后下一句开头的音频
	// 会与上一句尾部拼在同一个 chunk 里，造成两类问题（实测数据见
	// app/services/audio/realtime_stream_test.go 与 tests/asr/synthonset_test.go）：
	//   1) 时间戳原点不再是 Reset 的那一帧，逐字时间戳整体偏晚 0.1~0.2s，
	//      且首字常被压成零宽区间（前端表现为点不中/高亮串字）；
	//   2) 首字音素被截断，识别准确率下降（实测「测试播放」被识别为「差是播放」）。
	//
	// 因此改用「连续识别流 + 自行按尾部静音断句」：一帧不丢地连续解码，
	// 逐字时间戳与整段离线解码结果完全一致（逐字偏差 0ms）。
	emittedTokens []tokenEmit
	// lastTokenCount 上一次 GetResult 返回的 token 数量（当前识别流内），用于增量记录新 token。
	lastTokenCount int
	// uttTokenStart 当前尚未提交的语句在 emittedTokens 中的起始下标。
	uttTokenStart int
	// textCursor 当前识别流累积文本中已提交部分的 rune 数。
	textCursor int
	// pendingRunes 最近一次 GetResult 得到的累积文本 rune 数（用于判断是否有待提交内容）。
	pendingRunes int
	// utteranceStreamStart 当前识别流的起点采样位置（仅在识别流重建后变化）。
	// 模型产出的 token 级时间戳（秒，相对流起点）以此为基准映射到会话时间轴；
	// -1 表示识别流建立后尚未收到音频帧。
	utteranceStreamStart int64
	// windowStartSample 最近一次解码窗口（当前帧）的起点采样位置。
	windowStartSample int64
	// voiceStartSample 能量检测到的语音起点采样位置，-1 表示尚未检测到。
	voiceStartSample int64
	// voiceStartFrames 连续超过能量阈值的帧数，用于确认语音起点（防噪声误触发）。
	voiceStartFrames int
	// lastEmittedEndSample 已发射 token 的最后一个采样位置，用于计算结果 EndMs。
	lastEmittedEndSample int64
	// lastSentEndMs 上一次下发中间结果的结束毫秒时间，用于保证流式时间戳单调不跳变。
	lastSentEndMs int64
}

// newSession 创建会话，队列容量由配置决定。
func newSession(clientID string, queueSize int, recognizer *sherpa.OnlineRecognizer, stream *sherpa.OnlineStream, tempFile *os.File, tempName string, sampleRate int) *session {
	now := time.Now()
	return &session{
		clientID:   clientID,
		chunks:     make(chan chunk, queueSize),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		stream:     stream,
		recognizer: recognizer,
		tempFile:   tempFile,
		tempName:   tempName,
		samples:    make([]float32, 0, 2048),
		lastSentAt: now,
		activity:   now,
		sampleRate: sampleRate,
		// -1 表示流起点/语音起点尚未确定，等待首个音频帧后标记。
		utteranceStreamStart: -1,
		voiceStartSample:     -1,
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

// resetUtterance 在一句话提交后重置「语句级」状态。
//
// 注意：token 追踪（emittedTokens / uttTokenStart）与累积文本游标（textCursor）
// 属于识别流而非单句，绝不能在这里清空——它们保证整场会话的时间轴连续。
func (s *session) resetUtterance() {
	s.lastSentText = ""
	s.lastSentAt = time.Now()
	s.voiceStartSample = -1
	s.voiceStartFrames = 0
	s.utteranceHasText = false
	s.lastSentEndMs = 0
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
func (s *session) currentOffsetMs() int64 {
	if s.sampleRate <= 0 {
		return 0
	}
	return s.totalSamples * 1000 / int64(s.sampleRate)
}

// markUtteranceStart 标记当前语音段起始采样位置。
//
// 流式模型需要累积一定上下文才会产出第一个 token，若以「首次出现文本」的
// 时刻作为语音起点，整句时间戳会系统性偏晚（解码延迟漂移）。因此优先采用
// 能量检测到的真实语音起点；未检测到时退化为第一个已发射 token 的模型时间
// 戳；再退化为当前解码窗口起点。
func (s *session) markUtteranceStart() {
	if s.utteranceHasText {
		return
	}
	start := s.voiceStartSample
	if start < 0 {
		if tokens := s.utteranceTokens(); len(tokens) > 0 {
			start = tokens[0].samplePos
		}
	}
	if start < 0 {
		start = s.windowStartSample
	}
	if start < 0 {
		start = s.totalSamples
	}
	s.utteranceStart = start
	s.utteranceHasText = true
}

// utteranceStartMs 返回当前语音段起始时间的毫秒偏移。
func (s *session) utteranceStartMs() int64 {
	if s.sampleRate <= 0 {
		return 0
	}
	return s.utteranceStart * 1000 / int64(s.sampleRate)
}

// currentTokenEndMs 返回当前已发射 token 末尾的毫秒时间。
// 以最后一个 token 的起始位置加约 0.15s 的尾部估计作为语音结尾，
// 且不超前于已接收音频的位置，保证与音频实际时间对齐。
func (s *session) currentTokenEndMs() int64 {
	if s.sampleRate <= 0 {
		return 0
	}
	end := s.lastEmittedEndSample
	if end < 0 {
		end = 0
	}
	tail := int64(float64(s.sampleRate) * 0.15)
	if end+tail > s.totalSamples {
		return s.currentOffsetMs()
	}
	return (end + tail) * 1000 / int64(s.sampleRate)
}

// commitEndMs 返回当前语音段结束时间的毫秒偏移。
// 以最后一个已发射 token 的结束位置为准（贴近真实语音结尾），并受
// 「不超前于已接收音频」与「不早于上一次提交」约束，保证跨句单调。
func (s *session) commitEndMs() int64 {
	if s.sampleRate <= 0 {
		return 0
	}
	end := s.currentTokenEndMs()
	if cur := s.currentOffsetMs(); end > cur {
		end = cur
	}
	if prev := s.lastCommitEnd * 1000 / int64(s.sampleRate); end < prev {
		end = prev
	}
	return end
}

// resetStreamTracking 在识别流被重建后重置全部追踪状态：
// 新的识别流没有历史音频，模型时间戳重新从 0 开始，累积文本也重新开始。
// 触发场景：热词热替换（applyPending）、会话收尾（finalize）。
func (s *session) resetStreamTracking() {
	s.utteranceStart = s.totalSamples
	s.lastCommitEnd = s.totalSamples
	s.utteranceHasText = false
	s.utterancePCM = s.utterancePCM[:0]
	s.emittedTokens = nil
	s.lastTokenCount = 0
	s.uttTokenStart = 0
	s.textCursor = 0
	s.pendingRunes = 0
	s.utteranceStreamStart = -1
	s.windowStartSample = 0
	s.voiceStartSample = -1
	s.voiceStartFrames = 0
	s.lastEmittedEndSample = 0
	s.lastSentEndMs = 0
	s.lastSentText = ""
	s.lastSentAt = time.Now()
}

// utteranceTokens 返回当前尚未提交的语句对应的 token 切片。
func (s *session) utteranceTokens() []tokenEmit {
	if s.uttTokenStart >= len(s.emittedTokens) {
		return nil
	}
	return s.emittedTokens[s.uttTokenStart:]
}

// hasPendingText 报告当前识别流中是否有尚未提交的文本。
func (s *session) hasPendingText() bool {
	return s.pendingRunes > s.textCursor
}

// textSuffix 取识别流累积文本中尚未提交的后缀（去掉首尾空白）。
//
// 整场会话不再 Reset，因此 GetResult 返回的是自会话开始累积的全部文本；
// 每句话的文本由「本次结果 - 已提交前缀」得到，与同一识别流的 token 严格对应。
func (s *session) textSuffix(full string) string {
	runes := []rune(full)
	// 识别流重建时累积文本会回到空串，游标随之失效（防御性处理）。
	if s.textCursor > len(runes) {
		s.textCursor = 0
	}
	return strings.TrimSpace(string(runes[s.textCursor:]))
}

// markCommitted 把累积文本的前 fullRunes 个字符标记为已提交，
// 并把本句 token 从「未提交」区间移出。
func (s *session) markCommitted(fullRunes int) {
	if fullRunes < 0 {
		fullRunes = 0
	}
	s.textCursor = fullRunes
	s.pendingRunes = fullRunes
	s.uttTokenStart = len(s.emittedTokens)
	s.utterancePCM = s.utterancePCM[:0]
	s.utteranceStart = s.totalSamples
	s.lastCommitEnd = s.totalSamples
}

// trailingSilenceMs 返回「距最后一个已发射 token」的音频时长（毫秒），用于断句判定。
//
// 流式模型发射 token 存在固有延迟（约 0.2~0.4s），该延迟会计入本值，
// 因此断句阈值需要明显大于期望的真实静音时长（见 Config.SegmentSilenceMs）。
func (s *session) trailingSilenceMs() int64 {
	if s.sampleRate <= 0 || s.lastEmittedEndSample <= 0 {
		return 0
	}
	gap := s.currentOffsetMs() - s.lastEmittedEndSample*1000/int64(s.sampleRate)
	if gap < 0 {
		return 0
	}
	return gap
}

// trackTokens 增量记录本次 GetResult 新出现的 token，并标注其对应的真实音频采样位置。
//
// 优先采用模型产出的 token 级时间戳（sherpa-onnx 在线流的 Timestamps，
// 单位为秒、相对当前语音段 Reset 后的流起点）映射到会话时间轴，从而消除
// 「token 被解码发现晚于其实际发音」带来的系统性时间漂移；模型未提供
// 时间戳时退化为窗口起点估计（见 trackTokensApprox）。
func (s *session) trackTokens(result *sherpa.OnlineRecognizerResult) {
	tokens := result.Tokens
	n := len(tokens)
	if n <= s.lastTokenCount {
		return
	}
	start := s.lastTokenCount
	ts := result.Timestamps
	lastPos := s.lastEmittedEndSample
	if len(s.emittedTokens) > 0 {
		lastPos = s.emittedTokens[len(s.emittedTokens)-1].samplePos
	}
	if s.utteranceStreamStart >= 0 && s.sampleRate > 0 && len(ts) >= n && timestampsValid(ts[start:]) {
		for i := start; i < n; i++ {
			pos := s.utteranceStreamStart + int64(ts[i]*float32(s.sampleRate))
			if pos < lastPos {
				pos = lastPos
			}
			s.emittedTokens = append(s.emittedTokens, tokenEmit{token: tokens[i], samplePos: pos})
			lastPos = pos
		}
	} else {
		s.trackTokensApprox(tokens[start:])
	}
	s.lastTokenCount = n
	if len(s.emittedTokens) > 0 {
		s.lastEmittedEndSample = s.emittedTokens[len(s.emittedTokens)-1].samplePos
	}
}

// timestampsValid 判断模型给出的 token 时间戳是否真实可用。
// sherpa-onnx 在部分情况下会返回与 token 等长的全 0 时间戳（数值无意义），
// 若不作区分会被误当作有效值，导致整句时间戳塌缩到同一时刻，因此全 0 视为无效。
func timestampsValid(ts []float32) bool {
	for _, v := range ts {
		if v > 0 {
			return true
		}
	}
	return false
}

// trackTokensApprox 在模型未提供 token 时间戳时，估计新 token 的采样位置。
// 少量新 token（正常逐帧解码）按当前解码窗口起点以 10ms 步进排布；
// 大量新 token（强制断句/补付解码一次性吐出整句）在 [已发射末尾, 窗口起点]
// 之间线性均分，避免整句时间戳被压缩到同一时刻。
func (s *session) trackTokensApprox(tokens []string) {
	k := len(tokens)
	if k == 0 {
		return
	}
	frame := int64(0)
	if s.sampleRate > 0 {
		frame = int64(s.sampleRate) / 100 // 10ms 一帧
	}
	lastPos := s.lastEmittedEndSample
	if len(s.emittedTokens) > 0 {
		lastPos = s.emittedTokens[len(s.emittedTokens)-1].samplePos
	}
	if k <= 3 {
		for j := 0; j < k; j++ {
			pos := s.windowStartSample + int64(j)*frame
			if pos < lastPos {
				pos = lastPos
			}
			s.emittedTokens = append(s.emittedTokens, tokenEmit{token: tokens[j], samplePos: pos})
			lastPos = pos
		}
		return
	}
	endPos := s.windowStartSample
	if endPos <= lastPos {
		endPos = lastPos + int64(k)*frame
	}
	for j := 0; j < k; j++ {
		pos := lastPos + (endPos-lastPos)*int64(j)/int64(k-1)
		if pos > endPos {
			pos = endPos
		}
		s.emittedTokens = append(s.emittedTokens, tokenEmit{token: tokens[j], samplePos: pos})
	}
}

// --- 语音起点能量检测 ---

// voiceStartThreshold 判定为语音的最小帧能量（RMS，约 -30dBFS）。
const voiceStartThreshold = 0.03

// voiceStartMinFrames 确认语音起点所需的连续超阈值帧数（每帧约 20ms）。
const voiceStartMinFrames = 3

// detectVoiceStart 在当前语音段尚未开始且尚无文本时，用帧能量检测真实语音起点。
// 语音起点比流式模型首次产出文本的时刻更早、更贴近音频实际时间。
func (s *session) detectVoiceStart() {
	if s.utteranceHasText || s.voiceStartFrames >= voiceStartMinFrames {
		return
	}
	if frameRMS(s.samples) >= voiceStartThreshold {
		if s.voiceStartFrames == 0 {
			s.voiceStartSample = s.totalSamples - int64(len(s.samples))
		}
		s.voiceStartFrames++
	} else {
		// 未达到阈值：重置，防止把零散噪声误判为语音起点。
		s.voiceStartFrames = 0
		s.voiceStartSample = -1
	}
}

// frameRMS 计算一帧浮点样本的均方根能量。
func frameRMS(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, v := range samples {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(samples)))
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
func (s *session) utteranceDuration() float64 {
	if s.sampleRate <= 0 {
		return 0
	}
	// 16bit PCM: 每个采样占 2 字节
	return float64(len(s.utterancePCM)/2) / float64(s.sampleRate)
}
