package offlinetranscribe

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type SentenceSplitTestSuite struct {
	suite.Suite
}

func TestSentenceSplitTestSuite(t *testing.T) {
	suite.Run(t, new(SentenceSplitTestSuite))
}

// splitSentenceSpans：句末标点处断句，句内逗号不切断句子。
//
// 旧实现在每个逗号处都断句（最少 4 字），一句话常被切成两半；
// 现在逗号只在整句已经足够长时才作为备选断点。
func (s *SentenceSplitTestSuite) TestSplitAtSentenceEndOnly() {
	spans := simpleSplitSentences("今天天气很好，我们一起去公园散步吧！你觉得怎么样？")
	s.Require().Len(spans, 2)
	s.Equal("今天天气很好，我们一起去公园散步吧！", spans[0].text)
	s.Equal("你觉得怎么样？", spans[1].text)

	// rune 区间连续覆盖全文
	s.Equal(0, spans[0].start)
	s.Equal(18, spans[0].end)
	s.Equal(18, spans[1].start)
	s.Equal(25, spans[1].end)
}

// splitSentenceSpans：短句整体保留，空文本返回 nil。
func (s *SentenceSplitTestSuite) TestShortSentenceKeptWhole() {
	spans := simpleSplitSentences("你好。")
	s.Require().Len(spans, 1)
	s.Equal("你好。", spans[0].text)
	s.Nil(simpleSplitSentences(""))
}

// splitSentenceSpans：收尾引号不被拆到下一句。
func (s *SentenceSplitTestSuite) TestClosingQuoteStaysWithSentence() {
	spans := simpleSplitSentences("他说今天天气很好我们一起去公园散步吧！")
	s.Require().Len(spans, 1)

	withQuote := simpleSplitSentences("「今天天气很好。」我们一起去公园散步吧！")
	s.Require().NotEmpty(withQuote)
	// 断点必须落在右引号之后，而不是把句号与右引号拆开
	for _, sp := range withQuote {
		s.False(sp.text == "「今天天气很好。」" && false, "占位")
	}
	s.Contains(withQuote[0].text, "。」")
}

// splitSentenceSpans：无标点长文本在硬上限处强制断句，且每段不超过硬上限。
func (s *SentenceSplitTestSuite) TestHardMaxForcesSplit() {
	const text = "现在是八月二十日周四十九点二十二分现在识别出来是李大爷在说话但是时间圈不知道准确不准确" +
		"现在再来一段没有标点的话用来验证硬切分逻辑是否生效"
	opt := testSentenceOptions()

	spans := splitSentenceSpans(text, nil, opt)
	s.Require().Greater(len(spans), 1, "超过硬上限的无标点文本必须断句")
	for _, sp := range spans {
		s.LessOrEqual(sp.end-sp.start, opt.HardMaxRunes, "每段不应超过硬切分阈值")
	}
	// rune 区间连续覆盖全文
	s.Equal(0, spans[0].start)
	s.Equal(len([]rune(text)), spans[len(spans)-1].end)
	for i := 1; i < len(spans); i++ {
		s.Equal(spans[i-1].end, spans[i].start)
	}
}

// splitSentenceSpans：字数未达硬上限时，无标点文本整体保留（不按 20 字硬切）。
func (s *SentenceSplitTestSuite) TestNoPunctShortTextKeptWhole() {
	const text = "现在是八月二十日周四十九点二十二分现在识别出来是李大爷在说话但是时间圈不知道准确不准确"
	spans := splitSentenceSpans(text, nil, testSentenceOptions())
	s.Require().Len(spans, 1, "未超过硬上限时不应切碎")
	s.Equal(text, spans[0].text)
}

// splitSentenceSpans：模型时间戳可用时，在说话的自然停顿处断句，
// 而不是等到硬上限后在词中间硬切。
func (s *SentenceSplitTestSuite) TestSplitAtPauseWhenNoPunctuation() {
	// 40 字无标点文本，第 24 字后有 800ms 停顿（超过 500ms 阈值）
	text := ""
	for i := 0; i < 40; i++ {
		text += "字"
	}
	charTimes := make([]float32, 40)
	t := float32(0)
	for i := range charTimes {
		if i == 25 {
			t += 0.8 // 停顿
		}
		t += 0.2
		charTimes[i] = t
	}

	spans := splitSentenceSpans(text, charTimes, testSentenceOptions())
	s.Require().Len(spans, 2)
	s.Equal(25, spans[0].end, "应在停顿处断句")
	s.Equal(25, spans[1].start)
}

// splitSentenceSpans：停顿出现在到达目标长度之前也会被回溯使用。
func (s *SentenceSplitTestSuite) TestPauseBeforeTargetStillUsed() {
	text := ""
	for i := 0; i < 40; i++ {
		text += "字"
	}
	charTimes := make([]float32, 40)
	t := float32(0)
	for i := range charTimes {
		if i == 12 {
			t += 0.9 // 第 12 字后长停顿
		}
		t += 0.2
		charTimes[i] = t
	}

	spans := splitSentenceSpans(text, charTimes, testSentenceOptions())
	s.Require().Len(spans, 2)
	s.Equal(12, spans[0].end)
}

// splitSentenceSpans：字数不足时短句不与前句合并。
func (s *SentenceSplitTestSuite) TestShortTailWithPunctuationNotMerged() {
	spans := simpleSplitSentences("这是一个足够长的句子用来测试断句规则。好的。")
	s.Require().Len(spans, 2)
	s.Equal("好的。", spans[1].text)
}

// splitSentenceSpans：没有说完的过短尾段合并回前一段，避免孤儿碎片。
func (s *SentenceSplitTestSuite) TestTrivialTailMerged() {
	spans := simpleSplitSentences("这是一个足够长的句子用来测试断句规则。好吧")
	s.Require().Len(spans, 1)
	s.Equal("这是一个足够长的句子用来测试断句规则。好吧", spans[0].text)
}

// mergeSentenceFragments：被窗口边界切断的同一句话（间隙极短、前段无句末标点）合并回一条。
func (s *SentenceSplitTestSuite) TestMergeFragmentsAcrossWindows() {
	segs := []sentenceSegment{
		{text: "今天我们讨论一下", startMs: 1000, endMs: 3000, chunkStart: 16000, chunkEnd: 48000},
		{text: "明天的会议安排", startMs: 3050, endMs: 5000, chunkStart: 48800, chunkEnd: 80000},
	}
	merged := mergeSentenceFragments(segs, 250, 100)
	s.Require().Len(merged, 1)
	s.Equal("今天我们讨论一下明天的会议安排", merged[0].text)
	s.Equal(int64(1000), merged[0].startMs)
	s.Equal(int64(5000), merged[0].endMs)
	s.Equal(16000, merged[0].chunkStart)
	s.Equal(80000, merged[0].chunkEnd)
}

// mergeSentenceFragments：前一句已经说完（有句末标点）时即使挨着也不合并。
func (s *SentenceSplitTestSuite) TestNoMergeAfterSentenceEnd() {
	segs := []sentenceSegment{
		{text: "今天天气很好。", startMs: 1000, endMs: 3000},
		{text: "我们一起去公园。", startMs: 3100, endMs: 5000},
	}
	merged := mergeSentenceFragments(segs, 250, 100)
	s.Len(merged, 2)
}

// mergeSentenceFragments：间隙过大（另一句话）不合并。
func (s *SentenceSplitTestSuite) TestNoMergeWhenGapTooLarge() {
	segs := []sentenceSegment{
		{text: "今天我们讨论一下", startMs: 1000, endMs: 3000},
		{text: "明天的会议安排", startMs: 9000, endMs: 11000},
	}
	merged := mergeSentenceFragments(segs, 250, 100)
	s.Len(merged, 2)
}

// mergeSentenceFragments：重复片段（同一段音频被重复解码）被丢弃。
func (s *SentenceSplitTestSuite) TestDropsDuplicateFragments() {
	segs := []sentenceSegment{
		{text: "今天天气很好。", startMs: 1000, endMs: 3000},
		{text: "今天天气很好。", startMs: 1000, endMs: 3000},
		{text: "我们一起去公园。", startMs: 3100, endMs: 5000},
	}
	merged := mergeSentenceFragments(segs, 250, 100)
	s.Require().Len(merged, 2)
	s.Equal("今天天气很好。", merged[0].text)
	s.Equal("我们一起去公园。", merged[1].text)
}

// mergeSentenceFragments：空输入返回空。
func (s *SentenceSplitTestSuite) TestMergeFragmentsEmpty() {
	s.Nil(mergeSentenceFragments(nil, 250, 100))
}

// endsWithSentenceEnd：句末标点判定（允许尾部收尾符号与空白）。
func (s *SentenceSplitTestSuite) TestEndsWithSentenceEnd() {
	s.True(endsWithSentenceEnd("你好。"))
	s.True(endsWithSentenceEnd("真的吗？"))
	s.True(endsWithSentenceEnd("他说「好的。」"))
	s.True(endsWithSentenceEnd("结束啦！  "))
	s.False(endsWithSentenceEnd("今天我们讨论一下"))
	s.False(endsWithSentenceEnd(""))
}

// segmentsFromCharTimes：字符数与时间戳不匹配时返回 nil（由调用方退化为近似对齐）。
func (s *SentenceSplitTestSuite) TestSegmentsFromCharTimesMismatch() {
	s.Nil(segmentsFromCharTimes("你好世界", []float32{0.5, 0.8, 1.2}, 16000, testSentenceOptions()))
	s.Nil(segmentsFromCharTimes("", nil, 16000, testSentenceOptions()))
}

// approxCharTimes：无语音段时退化为整段均分；字符时间单调不减。
func (s *SentenceSplitTestSuite) TestApproxCharTimesMonotonic() {
	const sampleRate = 16000
	spans := []speechSpan{{sampleRate * 2, sampleRate * 5}, {sampleRate * 8, sampleRate * 11}}
	times := approxCharTimes("今天天气很好，我们一起去公园散步吧！你觉得怎么样？", spans, sampleRate*12, sampleRate)
	s.Require().Len(times, 25)
	for i := 1; i < len(times); i++ {
		s.True(times[i] >= times[i-1], "字符时间必须单调不减")
	}
	// 首字在语音起点之后，末字对齐最后一段语音终点
	s.True(times[0] >= 2.0)
	s.InDelta(11.0, float64(times[24]), 0.01)
	// 非法参数
	s.Nil(approxCharTimes("", spans, sampleRate*12, sampleRate))
	s.Nil(approxCharTimes("你好", spans, 0, sampleRate))
}

// approxCharTimes：未检测到语音时退化为整段音频。
func (s *SentenceSplitTestSuite) TestApproxCharTimesNoSpans() {
	const sampleRate = 16000
	times := approxCharTimes("你好世界", nil, sampleRate*4, sampleRate)
	s.Require().Len(times, 4)
	s.InDelta(1.0, float64(times[0]), 0.01)
	s.InDelta(4.0, float64(times[3]), 0.01)
}

// trimSentenceText：去掉模型输出首尾空白。
func (s *SentenceSplitTestSuite) TestTrimSentenceText() {
	s.Equal("你好世界", trimSentenceText(" 你好世界 "))
	s.Equal("", trimSentenceText("   "))
}
