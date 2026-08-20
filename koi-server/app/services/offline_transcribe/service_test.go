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

type ChunkOffsetTestSuite struct {
	suite.Suite
}

func TestChunkOffsetTestSuite(t *testing.T) {
	suite.Run(t, new(ChunkOffsetTestSuite))
}

// chunkOffsetMs 基本换算：块起点采样位置 → 全局毫秒偏移。
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

// chunkWindows 音频不足一块：只产生一个窗口（从 0 开始），避免重复转写。
func (s *ChunkOffsetTestSuite) TestChunkWindowsShortAudio() {
	// 16000Hz，10s 音频 = 160000 samples < 一块 480000
	s.Equal([]int{0}, chunkWindows(160000, 480000, 32000))
	// 恰好一块
	s.Equal([]int{0}, chunkWindows(480000, 480000, 32000))
}

// chunkWindows 长音频：步进 = 块长 - 重叠，窗口序列正确。
func (s *ChunkOffsetTestSuite) TestChunkWindowsLongAudio() {
	// 16000Hz，100s 音频 = 1600000 samples
	// 块 30s(480000)、重叠 2s(32000)、步进 28s(448000)
	got := chunkWindows(1600000, 480000, 32000)
	want := []int{0, 448000, 896000, 1344000}
	s.Equal(want, got)
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

// 重新转写时间戳对齐：多块滑动窗口的全局偏移连续且不漂移。
// 每块偏移 = 块起点真实时间，块间重叠区的时间戳应重合（保证块边界连续）。
func (s *ChunkOffsetTestSuite) TestRetranscribeGlobalOffsetsContinuous() {
	const (
		sampleRate          = 16000
		chunkSamples        = sampleRate * 30 // 30s
		overlapSamples      = sampleRate * 2  // 2s
		totalSamples        = sampleRate * 65 // 65s 音频
		expectedStepSamples = sampleRate * 28 // 28s
	)

	windows := chunkWindows(totalSamples, chunkSamples, overlapSamples)
	s.Equal([]int{0, expectedStepSamples, 2 * expectedStepSamples}, windows)

	// 每块全局偏移 = 块起点换算（毫秒），无逐块累积误差。
	offsets := make([]int64, len(windows))
	for i, start := range windows {
		offsets[i] = chunkOffsetMs(start, sampleRate)
	}
	s.Equal([]int64{0, 28000, 56000}, offsets)

	// 块间重叠区：前一块末尾 2s 与后一块开头 2s 的全局时间重合。
	// 前一块末尾的句子全局 EndMs = 30s + offset(0) = 30000ms
	// 后一块开头 2s 处的句子全局 StartMs = 2s + offset(28000) = 30000ms
	s.Equal(int64(30000), chunkOffsetMs(0, sampleRate)+int64(chunkSamples*1000/sampleRate))
	s.Equal(int64(30000), chunkOffsetMs(expectedStepSamples, sampleRate)+int64(overlapSamples*1000/sampleRate))

	// 最后一块的末尾对齐总时长：56s(起点) + 本块实际长度(9s) = 65s
	lastStart := windows[len(windows)-1]
	s.Equal(int64(65000), chunkOffsetMs(lastStart, sampleRate)+int64((totalSamples-lastStart)*1000/sampleRate))
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

// 句子级时间戳应用块偏移后，落在全局时间轴内且保持句间连续。
func (s *ChunkOffsetTestSuite) TestSentenceOffsetWithinGlobalTimeline() {
	// 第 2 块（起点 28s）内的一句话：相对块起点 1.5s~3.0s
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
	// 全局时间落在音频总时长内，且不与前一块（0~30s）的句子重叠到错误区间。
	s.True(globalStart >= int64(28000))
	s.True(globalEnd <= int64(65000))
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

// 长句文本（25 个 rune），可被切为 3 句（标点位于第 7、18、25 字符处）。
const longSentenceText = "今天天气很好，我们一起去公园散步吧！你觉得怎么样？"

// splitSentencesWithTimestamps：模型 token 级时间戳路径。
// 验证句子级时间戳与逐字时间戳严格一致、采样位置与音频对齐、句间连续。
func (s *SentenceTimestampTestSuite) TestSplitSentencesWithTimestampsTokenPath() {
	tokens, times := runeTokens(longSentenceText, 0.5)
	samples := make([]float32, 16000*5) // 5s 音频

	segs := splitSentencesWithTimestamps(longSentenceText, []sherpa.OfflineRecognizerResult{
		{Tokens: tokens, Timestamps: times},
	}, 16000, samples)

	s.Len(segs, 3)
	// 第 1 句 "今天天气很好，"：rune[0:7]，时间 0.5~1.1s
	s.Equal("今天天气很好，", segs[0].text)
	s.Equal(int64(500), segs[0].startMs)
	s.Equal(int64(1100), segs[0].endMs)
	s.Equal(int(8000), segs[0].chunkStart)
	s.Equal(int(17600), segs[0].chunkEnd)
	// 第 2 句 "我们一起去公园散步吧！"：rune[7:18]，时间 1.2~2.2s
	s.Equal("我们一起去公园散步吧！", segs[1].text)
	s.Equal(int64(1200), segs[1].startMs)
	s.Equal(int64(2200), segs[1].endMs)
	// 第 3 句 "你觉得怎么样？"：rune[18:25]，时间 2.3~2.9s
	s.Equal("你觉得怎么样？", segs[2].text)
	s.Equal(int64(2300), segs[2].startMs)
	s.Equal(int64(2900), segs[2].endMs)
	s.Equal(int(46400), segs[2].chunkEnd)

	// 采样位置（chunkStart/End）不越出音频边界
	for _, seg := range segs {
		s.True(seg.chunkStart >= 0)
		s.True(seg.chunkEnd <= len(samples))
		s.True(seg.chunkStart <= seg.chunkEnd)
	}
}

// splitSentencesWithTimestamps：模型未提供 token 时间戳时退化到按音频时长近似，
// 句子时间戳单调且不越出音频总时长。
func (s *SentenceTimestampTestSuite) TestSplitSentencesWithTimestampsFallsBackApprox() {
	samples := make([]float32, 16000*5) // 5s 音频
	segs := splitSentencesWithTimestamps(longSentenceText, []sherpa.OfflineRecognizerResult{
		{Tokens: []string{"今天天气很好，我们一起去公园散步吧！你觉得怎么样？"}},
	}, 16000, samples)

	s.Len(segs, 3)
	// 音频 5000ms 按 25 个字符比例分配：句1 [0, 7/25*5000=1400]，句2 [1400, 18/25*5000=3600]，句3 [3600, 5000]
	s.Equal(int64(0), segs[0].startMs)
	s.Equal(int64(1400), segs[0].endMs)
	s.Equal(int64(1400), segs[1].startMs)
	s.Equal(int64(3600), segs[1].endMs)
	s.Equal(int64(3600), segs[2].startMs)
	s.Equal(int64(5000), segs[2].endMs)

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
	s.Nil(splitSentencesWithTimestamps("", nil, 16000, nil))
}

// buildSentenceSegments：逐字时间戳与文本字符数不匹配时退化到近似，不 panic。
func (s *SentenceTimestampTestSuite) TestBuildSentenceSegmentsMismatchFallsBack() {
	samples := make([]float32, 16000*5)
	// 时间戳比字符少 1 个
	segs := buildSentenceSegments("你好世界", []float32{0.5, 0.8, 1.2}, samples, 16000)
	s.NotEmpty(segs)
	s.True(segs[0].endMs <= 5000)
}

// simpleSplitSentences：按标点断句，保留原始标点，rune 区间准确连续。
func (s *SentenceTimestampTestSuite) TestSimpleSplitSentences() {
	spans := simpleSplitSentences(longSentenceText)
	s.Len(spans, 3)
	s.Equal("今天天气很好，", spans[0].text)
	s.Equal(0, spans[0].start)
	s.Equal(7, spans[0].end)
	s.Equal("我们一起去公园散步吧！", spans[1].text)
	s.Equal(7, spans[1].start)
	s.Equal(18, spans[1].end)
	s.Equal("你觉得怎么样？", spans[2].text)
	s.Equal(18, spans[2].start)
	s.Equal(25, spans[2].end)

	// rune 区间连续覆盖全文
	s.Equal(25, spans[2].end)
	for i := 1; i < len(spans); i++ {
		s.Equal(spans[i-1].end, spans[i].start)
	}
}

// simpleSplitSentences：少于 4 字符不切句（短句整体保留），空文本返回 nil。
func (s *SentenceTimestampTestSuite) TestSimpleSplitSentencesShortKeepsWhole() {
	spans := simpleSplitSentences("你好。")
	s.Len(spans, 1)
	s.Equal("你好。", spans[0].text)
	s.Nil(simpleSplitSentences(""))
}

// 重新转写整条链路回归：分块转写中，句子时间戳加块偏移后映射到全局时间轴，
// 且块间重叠区的句子时间戳重合，保证整段音频时间戳连续无跳变。
func (s *SentenceTimestampTestSuite) TestRetranscribeSentenceGlobalTimeline() {
	const (
		sampleRate     = 16000
		chunkSamples   = sampleRate * 30 // 30s
		overlapSamples = sampleRate * 2  // 2s
		totalSamples   = sampleRate * 60 // 60s
	)

	// 60s 音频、30s 块、2s 重叠 → 3 块：0~30s、28~58s、56~60s（末块截断）
	windows := chunkWindows(totalSamples, chunkSamples, overlapSamples)
	s.Equal([]int{0, sampleRate * 28, sampleRate * 56}, windows)

	// 每块内一句话（相对块起点 10s~12s），全局时间必须精确对齐音频采样位置。
	for _, start := range windows {
		offsetMs := chunkOffsetMs(start, sampleRate)
		seg := sentenceSegment{startMs: 10000, endMs: 12000, chunkStart: 160000, chunkEnd: 192000}
		globalStart := seg.startMs + offsetMs
		globalEnd := seg.endMs + offsetMs
		// 全局时间 = 该句第一个采样在整段音频中的位置
		wantStart := int64(seg.chunkStart+start) * 1000 / int64(sampleRate)
		wantEnd := int64(seg.chunkEnd+start) * 1000 / int64(sampleRate)
		s.Equal(wantStart, globalStart)
		s.Equal(wantEnd, globalEnd)
	}

	// 块间重叠区时间重合：第 1 块末尾 2s（28s~30s）与第 2 块开头 2s（28s~30s）
	// 的全局时间戳应一致，且总长 60s 内单调连续。
	s.Equal(int64(28000), chunkOffsetMs(0, sampleRate)+int64((chunkSamples-overlapSamples)*1000/sampleRate))
	s.Equal(int64(28000), chunkOffsetMs(sampleRate*28, sampleRate))
}

// 离线 transducer 模型不产生 token 级时间戳：其 Timestamps 是与 Tokens 等长的全 0 数组。
// 此时必须退化到近似分配，绝不能把全 0 当作有效时间戳（此前导致重新转写后全部句子时间戳为 0）。
func (s *SentenceTimestampTestSuite) TestSplitSentencesWithTimestampsZeroTimestampsFallsBack() {
	tokens, _ := runeTokens(longSentenceText, 0) // 时间戳全 0
	samples := make([]float32, 16000*5)          // 5s 音频

	segs := splitSentencesWithTimestamps(longSentenceText, []sherpa.OfflineRecognizerResult{
		{Tokens: tokens, Timestamps: make([]float32, len(tokens))},
	}, 16000, samples)

	s.Len(segs, 3)
	// 时间戳必须非零长度、单调且落在块时长内（全 0 应被拒绝）
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
	// 末句终点对齐音频末端（全静音时语音起点为 0）
	s.Equal(int64(5000), segs[2].endMs)
}

// 退化近似路径应感知语音起点：音频前段有静音时，首句时间戳从语音开始处铺开，
// 而不是把前导静音算进第一句。
func (s *SentenceTimestampTestSuite) TestApproxSpeechStartAligns() {
	const sampleRate = 16000
	samples := make([]float32, sampleRate*5) // 5s：前 2s 静音
	for i := sampleRate * 2; i < len(samples); i++ {
		samples[i] = 0.1 // 后 3s 有语音（RMS = 0.1 > 0.03）
	}

	segs := buildSentenceSegmentsApprox(longSentenceText, samples, sampleRate)

	s.Len(segs, 3)
	// 首句起点对齐语音起点（100ms 帧 → 2000ms）
	s.True(segs[0].startMs >= 2000, "首句应起始于语音起点附近，got %d", segs[0].startMs)
	s.True(segs[0].startMs <= 2100)
	// 末句终点 = 语音起点 2000ms + 3s 语音 = 5000ms
	s.Equal(int64(5000), segs[2].endMs)
	// chunk 采样区间也对应语音区间
	s.True(segs[0].chunkStart >= sampleRate*2)
	s.Equal(len(samples), segs[2].chunkEnd)
	// 句内逐字时间戳单调
	for i := 1; i < len(segs[0].wordTimestamps); i++ {
		s.True(segs[0].wordTimestamps[i].StartMs >= segs[0].wordTimestamps[i-1].StartMs)
	}
}

// 无标点长文本（离线模型常见输出）按长度强制切句，避免整块音频变成一条超长记录。
func (s *SentenceTimestampTestSuite) TestSimpleSplitSentencesNoPunctForceSplit() {
	text := "现在是八月二十日周四十九点二十二分现在识别出来是李大爷在说话但是时间圈不知道准确不准确"
	spans := simpleSplitSentences(text)

	s.Greater(len(spans), 1)
	for _, sp := range spans {
		s.LessOrEqual(sp.end-sp.start, 20, "每段不应超过强制切句阈值")
	}
	// rune 区间连续覆盖全文
	s.Equal(0, spans[0].start)
	s.Equal(len([]rune(text)), spans[len(spans)-1].end)
	for i := 1; i < len(spans); i++ {
		s.Equal(spans[i-1].end, spans[i].start)
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

// detectSpeechStart：全静音返回 0，有语音返回对齐到 100ms 帧的采样位置。
func (s *SentenceTimestampTestSuite) TestDetectSpeechStart() {
	const sampleRate = 16000
	silent := make([]float32, sampleRate*2)
	s.Equal(0, detectSpeechStart(silent, sampleRate))

	withVoice := make([]float32, sampleRate*5)
	for i := sampleRate*2 + 100; i < len(withVoice); i++ {
		withVoice[i] = 0.1
	}
	// 语音从 2s(32000 samples) 处开始，对齐 100ms 帧
	s.Equal(32000, detectSpeechStart(withVoice, sampleRate))

	// 非法参数返回 0
	s.Equal(0, detectSpeechStart(nil, sampleRate))
	s.Equal(0, detectSpeechStart(silent, 0))
}
