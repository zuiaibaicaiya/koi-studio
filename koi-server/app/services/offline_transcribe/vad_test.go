package offlinetranscribe

import (
	"math"
	"testing"

	"github.com/stretchr/testify/suite"
)

type VADTestSuite struct {
	suite.Suite
}

func TestVADTestSuite(t *testing.T) {
	suite.Run(t, new(VADTestSuite))
}

// filled 在 [from, to) 区间填充幅度 amp 的样本，用于合成语音/静音。
func (s *VADTestSuite) filled(total, from, to int, amp float32) []float32 {
	samples := make([]float32, total)
	for i := from; i < to && i < total; i++ {
		samples[i] = amp
	}
	return samples
}

// detectEnergySpans：全静音返回空。
func (s *VADTestSuite) TestDetectEnergySpansSilenceOnly() {
	const sampleRate = 16000
	spans := detectEnergySpans(make([]float32, sampleRate*2), sampleRate, vadPostOptions{})
	s.Empty(spans)
}

// detectEnergySpans：单段语音，边界落到帧精度内并含首尾 padding。
func (s *VADTestSuite) TestDetectEnergySpansSingleSegment() {
	const sampleRate = 16000
	samples := s.filled(sampleRate*5, sampleRate*2, sampleRate*5, 0.1)

	spans := detectEnergySpans(samples, sampleRate, vadPostOptions{MinSilenceMs: 300, MinSpeechMs: 150, PaddingMs: 80})
	s.Require().Len(spans, 1)
	// 语音真实区间 2s~5s，padding 80ms 后起点前移、终点被音频末尾截断。
	s.InDelta(sampleRate*2-sampleRate*80/1000, spans[0].Start, float64(sampleRate)*0.03)
	s.Equal(sampleRate*5, spans[0].End)
}

// detectEnergySpans：多段语音，中间长静音不合并。
func (s *VADTestSuite) TestDetectEnergySpansMultipleSegments() {
	const sampleRate = 16000
	samples := s.filled(sampleRate*12, sampleRate*3, sampleRate*5, 0.1)
	for i := sampleRate * 8; i < sampleRate*11; i++ {
		samples[i] = 0.1
	}

	spans := detectEnergySpans(samples, sampleRate, vadPostOptions{MinSilenceMs: 300, MinSpeechMs: 150, PaddingMs: 0})
	s.Require().Len(spans, 2)
	s.Equal(sampleRate*3, spans[0].Start)
	s.Equal(sampleRate*5, spans[0].End)
	s.Equal(sampleRate*8, spans[1].Start)
	s.Equal(sampleRate*11, spans[1].End)
}

// detectEnergySpans：说话中的短暂停顿（≤300ms）合并为同一段。
func (s *VADTestSuite) TestDetectEnergySpansMergesShortPause() {
	const sampleRate = 16000
	samples := s.filled(sampleRate*5, 0, sampleRate*5, 0.1)
	for i := sampleRate*2 - 1600; i < sampleRate*2+1600; i++ {
		samples[i] = 0 // 200ms 停顿
	}

	spans := detectEnergySpans(samples, sampleRate, vadPostOptions{MinSilenceMs: 300, MinSpeechMs: 150, PaddingMs: 0})
	s.Len(spans, 1)
}

// detectEnergySpans：瞬时噪声（<150ms）被丢弃，不会污染切分。
func (s *VADTestSuite) TestDetectEnergySpansDropsNoise() {
	const sampleRate = 16000
	samples := s.filled(sampleRate*5, sampleRate, sampleRate*4, 0.1)
	for i := sampleRate * 4; i < sampleRate*4+800; i++ {
		samples[i] = 0.2 // 50ms 冲击噪声
	}

	spans := detectEnergySpans(samples, sampleRate, vadPostOptions{MinSilenceMs: 300, MinSpeechMs: 150, PaddingMs: 0})
	s.Require().Len(spans, 1)
	s.True(spans[0].End <= sampleRate*4+sampleRate/10, "噪声段不应被单独识别为语音")
}

// detectEnergySpans：阈值自适应——整体音量很小（0.005 RMS）时仍能检出语音，
// 固定阈值 0.03 的实现会把这类音频整段判成静音。
func (s *VADTestSuite) TestDetectEnergySpansAdaptiveThreshold() {
	const sampleRate = 16000
	samples := s.filled(sampleRate*4, sampleRate, sampleRate*3, 0.005)

	spans := detectEnergySpans(samples, sampleRate, vadPostOptions{MinSilenceMs: 300, MinSpeechMs: 150, PaddingMs: 0})
	s.Require().Len(spans, 1)
	s.True(spans[0].Start <= sampleRate+sampleRate/10)
	s.True(spans[0].End >= sampleRate*3-sampleRate/10)
}

// detectEnergySpans：非法参数返回空。
func (s *VADTestSuite) TestDetectEnergySpansInvalid() {
	s.Empty(detectEnergySpans(nil, 16000, vadPostOptions{}))
	s.Empty(detectEnergySpans(make([]float32, 16000), 0, vadPostOptions{}))
}

// postProcessSpans：合并、padding、裁剪与噪声过滤的组合行为。
func (s *VADTestSuite) TestPostProcessSpans() {
	const sampleRate = 16000
	total := sampleRate * 10

	// 间隔 100ms 的两段被合并（≤300ms）
	merged := postProcessSpans([]speechSpan{{0, sampleRate}, {sampleRate + sampleRate/10, sampleRate * 2}},
		total, sampleRate, vadPostOptions{MinSilenceMs: 300, MinSpeechMs: 0, PaddingMs: 0})
	s.Len(merged, 1)

	// 间隔 1s 的两段保持独立
	split := postProcessSpans([]speechSpan{{0, sampleRate}, {sampleRate * 2, sampleRate * 3}},
		total, sampleRate, vadPostOptions{MinSilenceMs: 300, MinSpeechMs: 0, PaddingMs: 0})
	s.Len(split, 2)

	// padding 不越出音频边界
	padded := postProcessSpans([]speechSpan{{0, total}}, total, sampleRate,
		vadPostOptions{MinSilenceMs: 300, MinSpeechMs: 0, PaddingMs: 100})
	s.Require().Len(padded, 1)
	s.Equal(0, padded[0].Start)
	s.Equal(total, padded[0].End)

	// 短于 150ms 的段被丢弃
	s.Empty(postProcessSpans([]speechSpan{{0, sampleRate / 10}}, total, sampleRate,
		vadPostOptions{MinSilenceMs: 300, MinSpeechMs: 150, PaddingMs: 0}))

	// 空输入、乱序输入
	s.Empty(postProcessSpans(nil, total, sampleRate, vadPostOptions{}))
	unordered := postProcessSpans([]speechSpan{{sampleRate * 2, sampleRate * 3}, {0, sampleRate}},
		total, sampleRate, vadPostOptions{MinSilenceMs: 0, MinSpeechMs: 0, PaddingMs: 0})
	s.Require().Len(unordered, 2)
	s.Equal(0, unordered[0].Start)
}

// resampleLinear：长度按比例换算，端点保持。
func (s *VADTestSuite) TestResampleLinear() {
	src := make([]float32, 8000) // 0.5s @16k
	for i := range src {
		src[i] = float32(i) / 8000.0
	}
	out := resampleLinear(src, 16000, 8000)
	s.Require().Len(out, 4000)
	s.InDelta(0.0, float64(out[0]), 0.01)
	s.InDelta(1.0, float64(out[len(out)-1]), 0.01)

	// 非法参数
	s.Nil(resampleLinear(nil, 16000, 8000))
	s.Nil(resampleLinear(src, 0, 8000))
	// 同采样率直接返回原切片
	s.Equal(src, resampleLinear(src, 16000, 16000))
}

// scaleSampleIndex：采样坐标在不同采样率间换算。
func (s *VADTestSuite) TestScaleSampleIndex() {
	s.Equal(8000, scaleSampleIndex(16000, 16000, 8000))
	s.Equal(16000, scaleSampleIndex(8000, 8000, 16000))
	s.Equal(16000, scaleSampleIndex(16000, 16000, 16000))
	// 采样率非法时原样返回（调用方已保证 rate > 0）
	s.Equal(16000, scaleSampleIndex(16000, 0, 16000))
}

// percentile：分位数取值（最近秩法，下标 = floor(p * (n-1))）。
func (s *VADTestSuite) TestPercentile() {
	sorted := []float64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	s.InDelta(0, percentile(sorted, 0), 0.001)
	s.InDelta(0, percentile(sorted, 0.1), 0.001) // floor(0.1*9)=0
	s.InDelta(4, percentile(sorted, 0.5), 0.001) // floor(0.5*9)=4
	s.InDelta(9, percentile(sorted, 1), 0.001)
	s.Equal(float64(0), percentile(nil, 0.5))
}

// msToSamples / samplesToMs 往返换算。
func (s *VADTestSuite) TestSampleMsConversion() {
	s.Equal(16000, msToSamples(1000, 16000))
	s.Equal(1600, msToSamplesInt(100, 16000))
	s.Equal(0, msToSamples(1000, 0))
	s.Equal(1000, samplesToMs(16000, 16000))
	s.Equal(0, samplesToMs(16000, 0))
}

// newVAD：模型文件不存在时自动退化为能量检测，不抛错、不影响转写。
func (s *VADTestSuite) TestVADFallbackWhenModelMissing() {
	cfg := (Config{VadEnabled: true, VadModel: "models/__not_exist__.onnx"}).normalized()
	v := newVAD(cfg, noopLog{})
	defer v.Close()

	s.False(v.UsesSilero(), "模型缺失时应退化为能量检测")

	const sampleRate = 16000
	samples := s.filled(sampleRate*4, sampleRate, sampleRate*3, 0.1)
	spans := v.Detect(samples, sampleRate)
	s.Require().Len(spans, 1)
	s.True(math.Abs(float64(spans[0].Start-sampleRate)) < float64(sampleRate)/5)

	// 非法输入
	s.Empty(v.Detect(nil, sampleRate))
	s.Empty(v.Detect(samples, 0))
}

// newVAD：关闭开关时同样退化为能量检测。
func (s *VADTestSuite) TestVADDisabled() {
	cfg := Config{VadEnabled: false}
	v := newVAD(cfg, noopLog{})
	defer v.Close()
	s.False(v.UsesSilero())
}

// Close 可安全重复调用。
func (s *VADTestSuite) TestVADCloseIdempotent() {
	v := newVAD(Config{}, noopLog{})
	v.Close()
	v.Close()
}
