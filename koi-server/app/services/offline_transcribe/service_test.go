package offlinetranscribe

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"koi-server/app/models"
)

// testSentenceOptions 测试中使用的断句参数（与线上默认值一致）。
func testSentenceOptions() sentenceOptions {
	return sentenceOptions{}.normalized()
}

type ChunkOffsetTestSuite struct {
	suite.Suite
}

func TestChunkOffsetTestSuite(t *testing.T) {
	suite.Run(t, new(ChunkOffsetTestSuite))
}

// chunkOffsetMs 基本换算：窗口起点采样位置 → 全局毫秒偏移。
func (s *ChunkOffsetTestSuite) TestChunkOffsetMsBasic() {
	// 16000Hz：1s = 16000 samples
	s.Equal(int64(0), chunkOffsetMs(0, 16000))
	s.Equal(int64(1000), chunkOffsetMs(16000, 16000))
	s.Equal(int64(28000), chunkOffsetMs(448000, 16000)) // 28s
	s.Equal(int64(56000), chunkOffsetMs(896000, 16000)) // 56s
	// 精确整除无余数漂移
	s.Equal(int64(30000), chunkOffsetMs(480000, 16000))
	s.Equal(int64(65000), chunkOffsetMs(1040000, 16000))
}

// chunkOffsetMs 采样率非法时返回 0。
func (s *ChunkOffsetTestSuite) TestChunkOffsetMsInvalidRate() {
	s.Equal(int64(0), chunkOffsetMs(16000, 0))
	s.Equal(int64(0), chunkOffsetMs(16000, -1))
}

// chunkOffsetMs 非整除时向下取整（不越过真实时间点）。
func (s *ChunkOffsetTestSuite) TestChunkOffsetMsRoundsDown() {
	// 3.5s = 56000 samples
	s.Equal(int64(3500), chunkOffsetMs(56000, 16000))
	// 0.1s = 1600 samples
	s.Equal(int64(100), chunkOffsetMs(1600, 16000))
}

// chunkWindows 音频不足一块：只产生一个窗口。
func (s *ChunkOffsetTestSuite) TestChunkWindowsShortAudio() {
	s.Equal([]int{0}, chunkWindows(160000, 480000, 32000))
	s.Equal([]int{0}, chunkWindows(480000, 480000, 32000))
}

// chunkWindows 非法参数返回 nil。
func (s *ChunkOffsetTestSuite) TestChunkWindowsInvalid() {
	s.Nil(chunkWindows(0, 480000, 32000))
	s.Nil(chunkWindows(1600000, 0, 32000))
	s.Nil(chunkWindows(0, 0, 0))
}

// chunkWindows 重叠超过块长（step<=0）：退化为单窗口，防死循环。
func (s *ChunkOffsetTestSuite) TestChunkWindowsOverlapTooBig() {
	s.Equal([]int{0}, chunkWindows(1600000, 480000, 600000))
}

// 重新转写回归：识别窗口互不重叠。
//
// 旧实现使用「30s 块 + 2s 重叠」的滑动窗口，重叠区会被两块各转写一次，
// 重新转写后出现重复句子；新实现按静音切分，窗口严格不重叠。
func (s *ChunkOffsetTestSuite) TestRetranscribeWindowsNeverOverlap() {
	const sampleRate = 16000
	// 65s 音频：3 段语音，间隔都足够长（1s），每段 5s。
	spans := []speechSpan{
		{Start: 0, End: sampleRate * 5},
		{Start: sampleRate * 10, End: sampleRate * 15},
		{Start: sampleRate * 60, End: sampleRate * 65},
	}
	samples := make([]float32, sampleRate*65)

	windows := planASRWindows(sampleRate*65, sampleRate, spans, samples, chunkOptions{MaxChunkSeconds: 30, MinSilenceCutSeconds: 0.4})
	s.Require().NotEmpty(windows)
	for i := 1; i < len(windows); i++ {
		s.True(windows[i-1].End <= windows[i].Start,
			"窗口 %d 与 %d 重叠: [%d,%d) vs [%d,%d)", i-1, i,
			windows[i-1].Start, windows[i-1].End, windows[i].Start, windows[i].End)
	}
	// 所有语音段都被某个窗口覆盖（不会漏转写）
	for _, sp := range spans {
		covered := false
		for _, w := range windows {
			if w.Start <= sp.Start && sp.End <= w.End {
				covered = true
				break
			}
		}
		s.True(covered, "语音段 [%d,%d) 未被任何窗口覆盖", sp.Start, sp.End)
	}
}

// offsetWordTimestamps：偏移应用到词级时间戳，不修改原切片。
func (s *ChunkOffsetTestSuite) TestOffsetWordTimestamps() {
	wts := []models.WordTimestamp{
		{Word: "你", StartMs: 500, EndMs: 500},
		{Word: "好", StartMs: 800, EndMs: 800},
	}
	out := offsetWordTimestamps(wts, 28000)
	s.Len(out, 2)
	s.Equal("你", out[0].Word)
	s.Equal(int64(28500), out[0].StartMs)
	s.Equal(int64(28500), out[0].EndMs)
	s.Equal(int64(28800), out[1].StartMs)
	s.Equal(int64(28800), out[1].EndMs)
	// 原切片不被修改
	s.Equal(int64(500), wts[0].StartMs)
}

// offsetWordTimestamps：offset 为 0 或空输入时原样返回。
func (s *ChunkOffsetTestSuite) TestOffsetWordTimestampsNoop() {
	wts := []models.WordTimestamp{{Word: "a", StartMs: 100, EndMs: 200}}
	s.Equal(wts, offsetWordTimestamps(wts, 0))
	s.Nil(offsetWordTimestamps(nil, 28000))
	s.Nil(offsetWordTimestamps(nil, 0))
}

// 句子级时间戳应用窗口偏移后，落在全局时间轴内。
func (s *ChunkOffsetTestSuite) TestSentenceOffsetWithinGlobalTimeline() {
	// 第 2 个窗口（起点 28s）内的一句话：相对窗口起点 1.5s~3.0s
	seg := sentenceSegment{
		text:       "测试句子",
		startMs:    1500,
		endMs:      3000,
		chunkStart: 24000,
		chunkEnd:   48000,
	}
	offset := chunkOffsetMs(448000, 16000) // 28000ms
	globalStart := seg.startMs + offset
	globalEnd := seg.endMs + offset
	s.Equal(int64(29500), globalStart)
	s.Equal(int64(31000), globalEnd)
	s.True(globalStart >= offset)
	s.True(globalEnd <= offset+30000)
}

type SentenceTimestampTestSuite struct {
	suite.Suite
}

func TestSentenceTimestampTestSuite(t *testing.T) {
	suite.Run(t, new(SentenceTimestampTestSuite))
}

// 构造逐字 token 时间戳：每个 rune 一个 token，时间从 base 起每 0.1s 递增。
func runeTokens(text string, base float32) ([]string, []float32) {
	var tokens []string
	var times []float32
	for i, r := range []rune(text) {
		tokens = append(tokens, string(r))
		times = append(times, base+float32(i)*0.1)
	}
	return tokens, times
}

// 长句文本（25 个 rune）：句末标点位于第 18（！）与第 25（？）个字符处。
const longSentenceText = "今天天气很好，我们一起去公园散步吧！你觉得怎么样？"

// splitSentencesWithTimestamps：模型 token 级时间戳路径。
//
// 断句只在句末标点处发生：逗号（第 7 字符）不再把一句话切成两半。
func (s *SentenceTimestampTestSuite) TestSplitSentencesWithTimestampsTokenPath() {
	tokens, times := runeTokens(longSentenceText, 0.5)
	samples := make([]float32, 16000*5) // 5s 音频

	segs := splitSentencesWithTimestamps(longSentenceText, []sherpa.OnlineRecognizerResult{
		{Tokens: tokens, Timestamps: times},
	}, 16000, samples, nil, testSentenceOptions())

	s.Require().Len(segs, 2)
	// 第 1 句 "今天天气很好，我们一起去公园散步吧！"：rune[0:18]
	// 模型时间戳是字的「发音结束时刻」，首字区间向前回退 300ms -> 0.2s 起。
	s.Equal("今天天气很好，我们一起去公园散步吧！", segs[0].text)
	s.Equal(int64(200), segs[0].startMs)
	s.Equal(int64(2200), segs[0].endMs)
	s.Equal(int(3200), segs[0].chunkStart)
	s.Equal(int(35200), segs[0].chunkEnd)
	// 第 2 句 "你觉得怎么样？"：rune[18:25]，紧接上一句，句间不重叠、不留缝。
	s.Equal("你觉得怎么样？", segs[1].text)
	s.Equal(int64(2200), segs[1].startMs)
	s.Equal(int64(2900), segs[1].endMs)
	s.Equal(int(46400), segs[1].chunkEnd)

	// 采样位置（chunkStart/End）不越出音频边界
	for _, seg := range segs {
		s.True(seg.chunkStart >= 0)
		s.True(seg.chunkEnd <= len(samples))
		s.True(seg.chunkStart <= seg.chunkEnd)
	}
}

// splitSentencesWithTimestamps：模型未提供 token 时间戳时退化到按音频的语音段近似，
// 句子时间戳单调且不越出音频总时长。
func (s *SentenceTimestampTestSuite) TestSplitSentencesWithTimestampsFallsBackApprox() {
	samples := make([]float32, 16000*5) // 5s 音频
	segs := splitSentencesWithTimestamps(longSentenceText, []sherpa.OnlineRecognizerResult{
		{Tokens: []string{"今天天气很好，我们一起去公园散步吧！你觉得怎么样？"}},
	}, 16000, samples, nil, testSentenceOptions())

	s.Require().Len(segs, 2)
	// 音频 5000ms 按 25 个字符比例分配：句1 [0, 18/25*5000=3600]，句2 [3600, 5000]
	s.Equal(int64(0), segs[0].startMs)
	s.Equal(int64(3600), segs[0].endMs)
	s.Equal(int64(3600), segs[1].startMs)
	s.Equal(int64(5000), segs[1].endMs)

	// 句间单调连续、不越界
	prev := int64(0)
	for _, seg := range segs {
		s.True(seg.startMs >= prev)
		s.True(seg.endMs >= seg.startMs)
		s.True(seg.endMs <= 5000)
		s.True(seg.chunkStart >= 0)
		s.True(seg.chunkEnd <= len(samples))
		prev = seg.endMs
	}
}

// splitSentencesWithTimestamps：空文本返回 nil。
func (s *SentenceTimestampTestSuite) TestSplitSentencesWithTimestampsEmpty() {
	s.Nil(splitSentencesWithTimestamps("", nil, 16000, nil, nil, testSentenceOptions()))
}

// 离线 transducer 模型不产生 token 级时间戳：其 Timestamps 是与 Tokens 等长的全 0 数组。
// 此时必须退化到近似分配，绝不能把全 0 当作有效时间戳（此前导致重新转写后全部句子时间戳为 0）。
func (s *SentenceTimestampTestSuite) TestSplitSentencesWithTimestampsZeroTimestampsFallsBack() {
	tokens, _ := runeTokens(longSentenceText, 0) // 时间戳全 0
	samples := make([]float32, 16000*5)          // 5s 音频

	segs := splitSentencesWithTimestamps(longSentenceText, []sherpa.OnlineRecognizerResult{
		{Tokens: tokens, Timestamps: make([]float32, len(tokens))},
	}, 16000, samples, nil, testSentenceOptions())

	s.Require().Len(segs, 2)
	prev := int64(0)
	for _, seg := range segs {
		s.True(seg.startMs >= prev)
		s.True(seg.endMs > seg.startMs, "句子时间戳不能是零长度")
		s.True(seg.endMs <= 5000)
		s.True(seg.chunkStart >= 0)
		s.True(seg.chunkEnd <= len(samples))
		s.True(seg.chunkStart <= seg.chunkEnd)
		prev = seg.endMs
	}
	s.Equal(int64(5000), segs[1].endMs)
}

// 退化近似路径应感知语音段：音频前段有静音时，首句时间戳从语音开始处铺开，
// 而不是把前导静音算进第一句。
func (s *SentenceTimestampTestSuite) TestApproxSpeechStartAligns() {
	const sampleRate = 16000
	samples := make([]float32, sampleRate*5) // 5s：前 2s 静音
	spans := []speechSpan{{Start: sampleRate * 2, End: sampleRate * 5}}

	segs := buildSentenceSegmentsApprox(longSentenceText, samples, sampleRate, spans, testSentenceOptions())

	s.Require().Len(segs, 2)
	// 首句起点落在语音起点附近（2s），前导静音不计入文字时间
	s.True(segs[0].startMs >= 1800, "首句应起始于语音起点附近，got %d", segs[0].startMs)
	s.True(segs[0].startMs <= 2100)
	// 末句终点 = 语音段终点 5s
	s.Equal(int64(5000), segs[1].endMs)
	// 句内逐字时间戳单调
	for i := 1; i < len(segs[0].wordTimestamps); i++ {
		s.True(segs[0].wordTimestamps[i].StartMs >= segs[0].wordTimestamps[i-1].StartMs)
	}
}

// 退化近似路径应保留中间静音：音频有多段语音时，文字时间只铺在语音段内，
// 不能把静音区间压缩掉（否则文字时间与音频实际发音位置对不上）。
func (s *SentenceTimestampTestSuite) TestApproxMultiSpeechSegmentsKeepSilence() {
	const sampleRate = 16000
	// 12s：语音段 3s~5s 与 8s~11s
	samples := make([]float32, sampleRate*12)
	spans := []speechSpan{
		{Start: sampleRate * 3, End: sampleRate * 5},
		{Start: sampleRate * 8, End: sampleRate * 11},
	}

	segs := buildSentenceSegmentsApprox(longSentenceText, samples, sampleRate, spans, testSentenceOptions())

	s.Require().Len(segs, 2)
	// 没有任何句子时间戳落在中间静音区间 (5000ms, 8000ms)
	for _, seg := range segs {
		s.True(seg.startMs <= 5000 || seg.startMs >= 8000, "句子起点不应落在中间静音区间: %d", seg.startMs)
		s.True(seg.endMs <= 5000 || seg.endMs >= 8000, "句子终点不应落在中间静音区间: %d", seg.endMs)
		s.True(seg.chunkStart >= 0 && seg.chunkEnd <= len(samples), "chunk 区间越界")
		s.True(seg.chunkStart <= seg.chunkEnd, "chunk 区间倒置")
	}
	// 末句终点对齐最后一段语音终点（11s），而不是音频末尾 12s
	s.Equal(int64(11000), segs[1].endMs)
	// 句内逐字时间戳单调且不越出所在句子的时间区间
	for _, seg := range segs {
		for i := 1; i < len(seg.wordTimestamps); i++ {
			s.True(seg.wordTimestamps[i].StartMs >= seg.wordTimestamps[i-1].StartMs)
		}
		if len(seg.wordTimestamps) > 0 {
			s.True(seg.wordTimestamps[0].StartMs >= seg.startMs)
			s.True(seg.wordTimestamps[len(seg.wordTimestamps)-1].EndMs <= seg.endMs)
		}
	}
}

// estimateWavDuration 必须读取标准 wav 头偏移 40-43 处的 data size。
// 旧实现误读 44-47（PCM 数据区开头），对非零数据会算出荒谬的时长。
func (s *SentenceTimestampTestSuite) TestEstimateWavDurationDataSize() {
	dir := s.T().TempDir()
	path := filepath.Join(dir, "test.wav")

	const (
		sampleRate = 16000
		byteRate   = sampleRate * 2 // 16bit mono
		dataSize   = 1000
	)
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	header[16] = 16 // fmt chunk size
	header[20] = 1  // PCM
	header[22] = 1  // mono
	header[24] = 0x80
	header[25] = 0x3e // sampleRate 16000 little-endian
	header[26] = 0
	header[27] = 0
	header[28] = 0x00
	header[29] = 0x7d // byteRate 32000 little-endian
	header[30] = 0
	header[31] = 0
	header[32] = 2  // blockAlign
	header[34] = 16 // bits
	copy(header[36:40], "data")
	header[40] = 0xe8
	header[41] = 0x03 // dataSize 1000 little-endian
	header[42] = 0
	header[43] = 0

	data := bytes.Repeat([]byte{0x01}, dataSize) // 非零数据，暴露旧实现读错偏移
	if err := os.WriteFile(path, append(header, data...), 0644); err != nil {
		s.T().Fatal(err)
	}

	dur, err := (&Service{}).estimateWavDuration(path)
	s.NoError(err)
	// 1000 字节 / 32000 byteRate = 0.03125s（旧实现会读出 ~526s）
	s.InDelta(0.03125, dur, 0.0001)
}
