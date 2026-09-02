package offlinetranscribe

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type ChunkPlanTestSuite struct {
	suite.Suite
}

func TestChunkPlanTestSuite(t *testing.T) {
	suite.Run(t, new(ChunkPlanTestSuite))
}

// planASRWindows：没有语音段时退化为均匀切分。
func (s *ChunkPlanTestSuite) TestPlanASRWindowsNoSpeech() {
	const sampleRate = 16000
	total := sampleRate * 65
	windows := planASRWindows(total, sampleRate, nil, nil, chunkOptions{MaxChunkSeconds: 30})
	s.Require().Len(windows, 3)
	s.Equal(0, windows[0].Start)
	s.Equal(sampleRate*30, windows[0].End)
	s.Equal(sampleRate*30, windows[1].Start)
	s.Equal(sampleRate*60, windows[1].End)
	s.Equal(sampleRate*60, windows[2].Start)
	s.Equal(total, windows[2].End)
}

// planASRWindows：短语音全部装进一个窗口。
func (s *ChunkPlanTestSuite) TestPlanASRWindowsSingleWindow() {
	const sampleRate = 16000
	total := sampleRate * 10
	spans := []speechSpan{{0, sampleRate * 3}, {sampleRate * 5, sampleRate * 8}}
	samples := make([]float32, total)

	windows := planASRWindows(total, sampleRate, spans, samples, chunkOptions{MaxChunkSeconds: 30, MinSilenceCutSeconds: 0.4})
	s.Require().Len(windows, 1)
	s.Equal(0, windows[0].Start)
	s.Equal(sampleRate*8, windows[0].End)
}

// planASRWindows：核心回归——切分点只能落在静音处，
// 语音段不会被从中间切开（这正是「句子被从中间分开」的根因）。
func (s *ChunkPlanTestSuite) TestPlanASRWindowsNeverCutsInsideSpeech() {
	const sampleRate = 16000
	total := sampleRate * 70
	// 4 段语音，每段 20s，间隔 1s 静音：任何一段都无法与相邻段同处一个 30s 窗口。
	spans := []speechSpan{
		{0, sampleRate * 20},
		{sampleRate * 21, sampleRate * 41},
		{sampleRate * 42, sampleRate * 62},
		{sampleRate * 63, sampleRate * 70},
	}
	samples := make([]float32, total)

	windows := planASRWindows(total, sampleRate, spans, samples, chunkOptions{MaxChunkSeconds: 30, MinSilenceCutSeconds: 0.4})
	s.Require().NotEmpty(windows)

	for _, w := range windows {
		for _, sp := range spans {
			// 窗口不能只覆盖某个语音段的一部分：要么完整包含，要么完全不相交。
			overlaps := w.Start < sp.End && sp.Start < w.End
			if !overlaps {
				continue
			}
			s.True(w.Start <= sp.Start && sp.End <= w.End,
				"窗口 [%d,%d) 把语音段 [%d,%d) 从中间切开了", w.Start, w.End, sp.Start, sp.End)
		}
	}
	// 窗口互不重叠、按序递增
	for i := 1; i < len(windows); i++ {
		s.True(windows[i-1].End <= windows[i].Start)
	}
}

// planASRWindows：静音太短（<MinSilenceCutSeconds）时宁可让窗口超长，也不切开语音。
func (s *ChunkPlanTestSuite) TestPlanASRWindowsPrefersLongWindowOverCutting() {
	const sampleRate = 16000
	total := sampleRate * 60
	// 两段 29s 语音，间隔仅 200ms（一句话内的停顿）
	spans := []speechSpan{
		{0, sampleRate * 29},
		{sampleRate*29 + sampleRate/5, sampleRate * 58},
	}
	samples := make([]float32, total)

	windows := planASRWindows(total, sampleRate, spans, samples, chunkOptions{MaxChunkSeconds: 30, MinSilenceCutSeconds: 0.4})
	s.Require().Len(windows, 1, "静音不足时不应切窗")
	s.Equal(0, windows[0].Start)
	s.Equal(sampleRate*58, windows[0].End)
}

// planASRWindows：过长的语音段在其内部停顿处切分，而不是按固定长度硬切。
func (s *ChunkPlanTestSuite) TestPlanASRWindowsSplitsLongSpanAtPause() {
	const sampleRate = 16000
	total := sampleRate * 80
	// 一段 80s 的「语音」：内部在 30s 与 60s 处各有 1s 停顿
	spans := []speechSpan{{0, total}}
	samples := make([]float32, total)
	for i := range samples {
		samples[i] = 0.1
	}
	for i := sampleRate * 30; i < sampleRate*31; i++ {
		samples[i] = 0
	}
	for i := sampleRate * 60; i < sampleRate*61; i++ {
		samples[i] = 0
	}

	windows := planASRWindows(total, sampleRate, spans, samples, chunkOptions{MaxChunkSeconds: 30, MinSilenceCutSeconds: 0.4})
	s.Require().Len(windows, 3, "超长语音段应在两处停顿处切成 3 个窗口")
	// 切分点必须落在静音区内，不能在语音中间
	s.Equal(sampleRate*31, windows[1].Start)
	s.Equal(sampleRate*61, windows[2].Start)
	for i := 1; i < len(windows); i++ {
		s.True(windows[i].Start >= sampleRate*30, "切分点应在 30s 静音区之后")
	}
}

// planASRWindows：持续语音（无任何停顿）时按上限硬切，仍保证覆盖完整、互不重叠。
func (s *ChunkPlanTestSuite) TestPlanASRWindowsHardSplitContinuousSpeech() {
	const sampleRate = 16000
	total := sampleRate * 75
	spans := []speechSpan{{0, total}}
	samples := make([]float32, total)
	for i := range samples {
		samples[i] = 0.1
	}

	windows := planASRWindows(total, sampleRate, spans, samples, chunkOptions{MaxChunkSeconds: 30, MinSilenceCutSeconds: 0.4})
	s.Require().Len(windows, 3)
	s.Equal(0, windows[0].Start)
	s.Equal(sampleRate*30, windows[0].End)
	s.Equal(sampleRate*30, windows[1].Start)
	s.Equal(sampleRate*60, windows[1].End)
	s.Equal(sampleRate*60, windows[2].Start)
	s.Equal(total, windows[2].End)
}

// planASRWindows：越界的语音段先被裁剪到音频范围内，窗口不越界。
func (s *ChunkPlanTestSuite) TestPlanASRWindowsClampedToAudio() {
	const sampleRate = 16000
	total := sampleRate * 10
	spans := []speechSpan{{sampleRate, sampleRate * 100}} // 越界
	samples := make([]float32, total)

	windows := planASRWindows(total, sampleRate, spans, samples, chunkOptions{MaxChunkSeconds: 5})
	s.Require().Len(windows, 2)
	s.Equal(sampleRate, windows[0].Start)
	s.Equal(sampleRate*6, windows[0].End)
	s.Equal(total, windows[len(windows)-1].End)
	for _, w := range windows {
		s.True(w.Start >= 0 && w.End <= total, "窗口越界: [%d,%d)", w.Start, w.End)
	}
}

// planASRWindows：非法参数返回空。
func (s *ChunkPlanTestSuite) TestPlanASRWindowsInvalid() {
	s.Empty(planASRWindows(0, 16000, nil, nil, chunkOptions{}))
	s.Empty(planASRWindows(16000, 0, nil, nil, chunkOptions{}))
}

// uniformWindows：均匀切分且互不重叠。
func (s *ChunkPlanTestSuite) TestUniformWindows() {
	const sampleRate = 16000
	total := sampleRate * 65
	windows := uniformWindows(total, sampleRate*30)
	s.Require().Len(windows, 3)
	s.Equal(total, windows[2].End)
	for i := 1; i < len(windows); i++ {
		s.Equal(windows[i-1].End, windows[i].Start)
	}
	s.Empty(uniformWindows(0, 100))
}

// clampWindows：重叠窗口被合并，空窗口被丢弃。
func (s *ChunkPlanTestSuite) TestClampWindows() {
	windows := clampWindows([]asrWindow{{0, 100}, {50, 150}, {200, 250}, {300, 300}}, 400)
	s.Equal([]asrWindow{{0, 150}, {200, 250}}, windows)
}

// hardSplitSpan：等分且不越界。
func (s *ChunkPlanTestSuite) TestHardSplitSpan() {
	s.Equal([]speechSpan{{0, 10}, {10, 20}, {20, 25}}, hardSplitSpan(speechSpan{0, 25}, 10))
	s.Equal([]speechSpan{{5, 10}}, hardSplitSpan(speechSpan{5, 10}, 10))
	s.Nil(hardSplitSpan(speechSpan{5, 5}, 10))
}

// packSpans：贪心装箱，单个超长段递归切分。
func (s *ChunkPlanTestSuite) TestPackSpans() {
	const sampleRate = 16000
	samples := make([]float32, sampleRate*2)
	for i := range samples {
		samples[i] = 0.1
	}

	// 相邻小段合成一片；第 2 片起标记 newWindow
	packed := packSpans([]speechSpan{{0, 10}, {12, 22}, {30, 40}}, samples, sampleRate, 25, 10, 0)
	s.Require().Len(packed, 2)
	s.Equal(speechSpan{0, 22}, packed[0].speechSpan)
	s.False(packed[0].newWindow)
	s.True(packed[1].newWindow)
}

// splitOneSpan：超长段切分后每段都标记为「必须新开窗口」，
// 否则切完的小片会在后续装箱阶段被重新合并成一个超长窗口。
func (s *ChunkPlanTestSuite) TestSplitOneSpanMarksNewWindow() {
	const sampleRate = 16000
	samples := make([]float32, sampleRate*75)
	for i := range samples {
		samples[i] = 0.1 // 持续语音，无停顿
	}

	pieces := splitOneSpan(speechSpan{0, sampleRate * 75}, samples, sampleRate, sampleRate*30, sampleRate*2/5, 0)
	s.Require().Len(pieces, 3)
	s.False(pieces[0].newWindow)
	s.True(pieces[1].newWindow)
	s.True(pieces[2].newWindow)
}

// splitOneSpan：未超长的语音段不切分、不强制新开窗口。
func (s *ChunkPlanTestSuite) TestSplitOneSpanShortSpanUntouched() {
	const sampleRate = 16000
	samples := make([]float32, sampleRate*10)
	pieces := splitOneSpan(speechSpan{0, sampleRate * 5}, samples, sampleRate, sampleRate*30, sampleRate*2/5, 0)
	s.Require().Len(pieces, 1)
	s.False(pieces[0].newWindow)
}

// chunkOptions.normalized：兜底非法参数。
func (s *ChunkPlanTestSuite) TestChunkOptionsNormalized() {
	opt := chunkOptions{}.normalized()
	s.Equal(float64(30), opt.MaxChunkSeconds)
	s.Equal(float64(0), opt.MinSilenceCutSeconds)
	// 负值兜底为 0；显式值保持不变
	s.Equal(float64(0), chunkOptions{MaxChunkSeconds: 0.5, MinSilenceCutSeconds: -1}.normalized().MinSilenceCutSeconds)
	s.Equal(float64(0.5), chunkOptions{MaxChunkSeconds: 0.5, MinSilenceCutSeconds: 0.5}.normalized().MinSilenceCutSeconds)
}

// sentenceSegment 的采样下标在窗口偏移后仍落在音频范围内（说话人识别切片安全）。
func (s *ChunkPlanTestSuite) TestClampInt() {
	s.Equal(5, clampInt(5, 0, 10))
	s.Equal(0, clampInt(-1, 0, 10))
	s.Equal(10, clampInt(11, 0, 10))
	s.Equal(5, clampInt(5, 10, 0)) // lo > hi 时交换后再裁剪
}
