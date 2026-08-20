package audio

import (
	"testing"

	"github.com/stretchr/testify/suite"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

type SessionTimestampTestSuite struct {
	suite.Suite
}

func TestSessionTimestampTestSuite(t *testing.T) {
	suite.Run(t, new(SessionTimestampTestSuite))
}

// 模型 token 级时间戳（相对流起点，秒）正确映射到会话采样时间轴。
func (s *SessionTimestampTestSuite) TestTrackTokensModelTimestamps() {
	sess := &session{sampleRate: 16000, utteranceStreamStart: 32000}
	res := &sherpa.OnlineRecognizerResult{
		Tokens:     []string{"你", "好", "hello"},
		Timestamps: []float32{0.5, 0.8, 1.2},
	}
	sess.trackTokens(res)
	s.Len(sess.emittedTokens, 3)
	expected := []int64{32000 + 8000, 32000 + 12800, 32000 + 19200}
	for i, e := range expected {
		s.Equal(e, sess.emittedTokens[i].samplePos, "token %d samplePos", i)
		s.Equal(res.Tokens[i], sess.emittedTokens[i].token)
	}
	s.Equal(3, sess.lastTokenCount)
	s.Equal(expected[2], sess.lastEmittedEndSample)
}

// 模型时间戳非单调时钳制到前一个 token 位置，防止流式时间戳回退。
func (s *SessionTimestampTestSuite) TestTrackTokensEnforcesMonotonic() {
	sess := &session{sampleRate: 16000, utteranceStreamStart: 32000}
	res := &sherpa.OnlineRecognizerResult{
		Tokens:     []string{"a", "b", "c"},
		Timestamps: []float32{1.5, 0.2, 2.0},
	}
	sess.trackTokens(res)
	s.Len(sess.emittedTokens, 3)
	s.Equal(int64(56000), sess.emittedTokens[0].samplePos)
	s.Equal(int64(56000), sess.emittedTokens[1].samplePos) // 0.2s 被钳制
	s.Equal(int64(64000), sess.emittedTokens[2].samplePos)
}

// 增量记录：后续 GetResult 只追加新 token。
func (s *SessionTimestampTestSuite) TestTrackTokensIncremental() {
	sess := &session{sampleRate: 16000, utteranceStreamStart: 32000}
	sess.trackTokens(&sherpa.OnlineRecognizerResult{
		Tokens:     []string{"你", "好"},
		Timestamps: []float32{0.5, 0.8},
	})
	sess.trackTokens(&sherpa.OnlineRecognizerResult{
		Tokens:     []string{"你", "好", "世"},
		Timestamps: []float32{0.5, 0.8, 1.2},
	})
	s.Len(sess.emittedTokens, 3)
	s.Equal("世", sess.emittedTokens[2].token)
	s.Equal(int64(32000+19200), sess.emittedTokens[2].samplePos)
}

// 模型无 token 时间戳时，少量新 token 按窗口起点以 10ms 步进排布。
func (s *SessionTimestampTestSuite) TestTrackTokensApproxSmallBatch() {
	sess := &session{sampleRate: 16000, windowStartSample: 32000}
	sess.trackTokens(&sherpa.OnlineRecognizerResult{Tokens: []string{"a", "b"}})
	s.Len(sess.emittedTokens, 2)
	s.Equal(int64(32000), sess.emittedTokens[0].samplePos)
	s.Equal(int64(32160), sess.emittedTokens[1].samplePos) // 10ms = 160 samples
	s.Equal(int64(32160), sess.lastEmittedEndSample)
}

// 模型无 token 时间戳且一次性吐出整句（强制断句）时，
// 在 [已发射末尾, 窗口起点] 之间线性均分，避免整句挤在同一时刻。
func (s *SessionTimestampTestSuite) TestTrackTokensApproxCatchUp() {
	sess := &session{sampleRate: 16000, windowStartSample: 32000, lastEmittedEndSample: 16000}
	sess.trackTokens(&sherpa.OnlineRecognizerResult{
		Tokens: []string{"a", "b", "c", "d", "e"},
	})
	s.Len(sess.emittedTokens, 5)
	expected := []int64{16000, 20000, 24000, 28000, 32000}
	for i, e := range expected {
		s.Equal(e, sess.emittedTokens[i].samplePos, "token %d samplePos", i)
	}
}

// markUtteranceStart 优先使用能量检测到的真实语音起点。
func (s *SessionTimestampTestSuite) TestMarkUtteranceStartPrefersVoiceStart() {
	sess := &session{
		voiceStartSample: 5000,
		windowStartSample: 9000,
		emittedTokens:    []tokenEmit{{token: "你", samplePos: 10000}},
	}
	sess.markUtteranceStart()
	s.Equal(int64(5000), sess.utteranceStart)
	s.True(sess.utteranceHasText)

	// 已有起点后再次调用不覆盖。
	sess.markUtteranceStart()
	s.Equal(int64(5000), sess.utteranceStart)
}

// 无能量检测结果时，退化为第一个已发射 token 的模型时间戳。
func (s *SessionTimestampTestSuite) TestMarkUtteranceStartFallsBackToFirstToken() {
	sess := &session{
		voiceStartSample: -1,
		windowStartSample: 9000,
		emittedTokens:    []tokenEmit{{token: "你", samplePos: 10000}},
	}
	sess.markUtteranceStart()
	s.Equal(int64(10000), sess.utteranceStart)
}

// 能量检测：连续超阈值帧确认语音起点，中途低能量重置。
func (s *SessionTimestampTestSuite) TestDetectVoiceStart() {
	high := highEnergyFrame()

	sess := &session{sampleRate: 16000}
	sess.samples = high
	sess.totalSamples = 16000
	sess.detectVoiceStart()
	s.Equal(1, sess.voiceStartFrames)
	s.Equal(int64(15996), sess.voiceStartSample)

	// 连续第 2、3 帧确认。
	sess.totalSamples = 32000
	sess.detectVoiceStart()
	s.Equal(2, sess.voiceStartFrames)
	sess.totalSamples = 48000
	sess.detectVoiceStart()
	s.Equal(3, sess.voiceStartFrames)

	// 确认后不再变化。
	sess.totalSamples = 64000
	sess.detectVoiceStart()
	s.Equal(3, sess.voiceStartFrames)
	s.Equal(int64(15996), sess.voiceStartSample)
}

// 能量检测：中途低能量帧会重置计数与起点，防止噪声误触发。
func (s *SessionTimestampTestSuite) TestDetectVoiceStartResetsOnSilence() {
	sess := &session{sampleRate: 16000}
	sess.samples = highEnergyFrame()
	sess.totalSamples = 16000
	sess.detectVoiceStart()
	s.Equal(1, sess.voiceStartFrames)
	s.NotEqual(int64(-1), sess.voiceStartSample)

	sess.samples = lowEnergyFrame()
	sess.detectVoiceStart()
	s.Equal(0, sess.voiceStartFrames)
	s.Equal(int64(-1), sess.voiceStartSample)
}

// 语音段已有文本后不再做能量检测。
func (s *SessionTimestampTestSuite) TestDetectVoiceStartSkipsAfterText() {
	sess := &session{sampleRate: 16000, utteranceHasText: true, voiceStartSample: -1}
	sess.samples = highEnergyFrame()
	sess.detectVoiceStart()
	s.Equal(0, sess.voiceStartFrames)
	s.Equal(int64(-1), sess.voiceStartSample)
}

// commitEndMs 基于最后 token 的结束位置，且不超前于已接收音频、不早于上一次提交。
func (s *SessionTimestampTestSuite) TestCommitEndMs() {
	sess := &session{
		sampleRate:          16000,
		totalSamples:        48000, // 3s
		lastEmittedEndSample: 16000, // 1s
		lastCommitEnd:        8000,  // 0.5s
	}
	// token 结束 = (16000 + 2400) * 1000 / 16000 = 1150ms
	s.Equal(int64(1150), sess.commitEndMs())
}

// commitEndMs：token 末尾超出当前音频位置时，钳制到当前音频位置。
func (s *SessionTimestampTestSuite) TestCommitEndMsClampedToCurrentOffset() {
	sess := &session{
		sampleRate:          16000,
		totalSamples:        17600, // 1.1s
		lastEmittedEndSample: 16000, // 1s
		lastCommitEnd:        0,
	}
	s.Equal(int64(1100), sess.commitEndMs())
}

// currentTokenEndMs 与 utteranceStartMs 的毫秒换算。
func (s *SessionTimestampTestSuite) TestMsConversion() {
	sess := &session{
		sampleRate:          16000,
		totalSamples:        48000,
		utteranceStart:      32000,
		lastEmittedEndSample: 16000,
	}
	s.Equal(int64(2000), sess.utteranceStartMs())
	s.Equal(int64(1150), sess.currentTokenEndMs())
	s.Equal(int64(3000), sess.currentOffsetMs())
}

// resetUtteranceTracking 重置后，流起点重新标记为下一帧起点。
func (s *SessionTimestampTestSuite) TestResetUtteranceTrackingResetsStreamStart() {
	sess := &session{
		sampleRate:           16000,
		totalSamples:         48000,
		utteranceStreamStart: 16000,
		voiceStartSample:     8000,
		voiceStartFrames:     3,
	}
	sess.resetUtteranceTracking()
	s.Equal(int64(-1), sess.utteranceStreamStart)
	s.Equal(int64(-1), sess.voiceStartSample)
	s.Equal(0, sess.voiceStartFrames)
	s.Equal(int64(0), sess.lastEmittedEndSample)
	s.Equal(0, sess.lastTokenCount)
	s.Len(sess.emittedTokens, 0)
}

// 流起点未标记（Reset 后尚未收到音频帧）时，模型时间戳不可用，退化为窗口起点近似。
func (s *SessionTimestampTestSuite) TestTrackTokensFallsBackApproxNoStreamStart() {
	sess := &session{sampleRate: 16000, utteranceStreamStart: -1, windowStartSample: 32000}
	sess.trackTokens(&sherpa.OnlineRecognizerResult{
		Tokens:     []string{"a", "b"},
		Timestamps: []float32{0.5, 0.8}, // 即使有时间戳也必须忽略
	})
	s.Len(sess.emittedTokens, 2)
	s.Equal(int64(32000), sess.emittedTokens[0].samplePos)
	s.Equal(int64(32160), sess.emittedTokens[1].samplePos) // 10ms 步进
}

// 模型返回的时间戳数量不足（len(ts) < len(tokens)）时同样退化，避免越界。
func (s *SessionTimestampTestSuite) TestTrackTokensFallsBackApproxTimestampsShort() {
	sess := &session{sampleRate: 16000, utteranceStreamStart: 32000, windowStartSample: 32000}
	sess.trackTokens(&sherpa.OnlineRecognizerResult{
		Tokens:     []string{"a", "b", "c"},
		Timestamps: []float32{0.5}, // 只有 1 个，不足 3 个
	})
	s.Len(sess.emittedTokens, 3)
	s.Equal(int64(32000), sess.emittedTokens[0].samplePos)
	s.Equal(int64(32160), sess.emittedTokens[1].samplePos)
	s.Equal(int64(32320), sess.emittedTokens[2].samplePos)
}

// 模型无时间戳、窗口起点在已发射末尾之前时，近似排布不倒退。
func (s *SessionTimestampTestSuite) TestTrackTokensApproxNeverGoesBackwards() {
	sess := &session{
		sampleRate:          16000,
		windowStartSample:   16000, // 早于已发射末尾
		lastEmittedEndSample: 20000,
		emittedTokens:       []tokenEmit{{token: "a", samplePos: 20000}},
		lastTokenCount:      1,
	}
	sess.trackTokens(&sherpa.OnlineRecognizerResult{Tokens: []string{"a", "b"}})
	s.Len(sess.emittedTokens, 2)
	s.Equal(int64(20000), sess.emittedTokens[1].samplePos) // 钳制到 lastPos，不回退
}

// markUtteranceStart 无能量检测、无已发射 token 时，退化为解码窗口起点。
func (s *SessionTimestampTestSuite) TestMarkUtteranceStartFallsBackToWindowStart() {
	sess := &session{
		voiceStartSample:  -1,
		windowStartSample: 9000,
		totalSamples:      12000,
	}
	sess.markUtteranceStart()
	s.Equal(int64(9000), sess.utteranceStart)
	s.True(sess.utteranceHasText)
}

// markUtteranceStart 所有候选均缺失时，退化为当前音频位置。
func (s *SessionTimestampTestSuite) TestMarkUtteranceStartFallsBackToCurrentPos() {
	sess := &session{
		voiceStartSample:  -1,
		windowStartSample: -1,
		totalSamples:      12000,
	}
	sess.markUtteranceStart()
	s.Equal(int64(12000), sess.utteranceStart)
}

// currentTokenEndMs：token 末尾 + 尾部估计超出已接收音频时，钳制到当前音频位置，
// 保证时间戳不超前于音频实际时间。
func (s *SessionTimestampTestSuite) TestCurrentTokenEndMsClampedToAudio() {
	sess := &session{
		sampleRate:          16000,
		totalSamples:        17600, // 1.1s
		lastEmittedEndSample: 16000, // 1.0s + 0.15s tail 已超出
	}
	s.Equal(int64(1100), sess.currentTokenEndMs())
}

// commitEndMs 的结束时间绝不早于上一次提交的结束时间（跨句单调保护）。
func (s *SessionTimestampTestSuite) TestCommitEndMsNeverGoesBackwards() {
	sess := &session{
		sampleRate:          16000,
		totalSamples:        48000, // 3s
		lastEmittedEndSample: 16000, // token 结束 = 1150ms
		lastCommitEnd:        20000, // 上一次提交结束 = 1250ms
	}
	s.Equal(int64(1250), sess.commitEndMs())
}

func highEnergyFrame() []float32 {
	return []float32{0.1, 0.1, 0.1, 0.1} // RMS = 0.1 >= 0.03
}

func lowEnergyFrame() []float32 {
	return []float32{0.001, 0.001, 0.001, 0.001} // RMS = 0.001 < 0.03
}
