package transcript

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type AlignTestSuite struct {
	suite.Suite
}

func TestAlignTestSuite(t *testing.T) {
	suite.Run(t, new(AlignTestSuite))
}

// 长度一致：中文逐字对齐。
func (s *AlignTestSuite) TestAlignCharTimesExactChinese() {
	text := "你好世界"
	times, ok := AlignCharTimes(text, []TokenTimestamp{
		{Token: "你", TimeSec: 0.5},
		{Token: "好", TimeSec: 0.8},
		{Token: "世", TimeSec: 1.2},
		{Token: "界", TimeSec: 1.5},
	})
	s.True(ok)
	s.Equal([]float32{0.5, 0.8, 1.2, 1.5}, times)
}

// 长度一致：中文+英文混合，含空格，空格继承相邻时间。
func (s *AlignTestSuite) TestAlignCharTimesMixedWithSpaces() {
	text := "你好 world"
	times, ok := AlignCharTimes(text, []TokenTimestamp{
		{Token: "你", TimeSec: 0.5},
		{Token: "好", TimeSec: 0.8},
		{Token: "▁world", TimeSec: 1.8},
	})
	s.True(ok)
	// []rune("你好 world") = [你, 好, ' ', w, o, r, l, d]
	s.Equal([]float32{0.5, 0.8, 0.8, 1.8, 1.8, 1.8, 1.8, 1.8}, times)
}

// 长度不一致（文本含标点、token 无标点）：LCS 容错对齐，标点继承最近时间。
func (s *AlignTestSuite) TestAlignCharTimesPunctuationLCS() {
	text := "你好，世界"
	times, ok := AlignCharTimes(text, []TokenTimestamp{
		{Token: "你", TimeSec: 0.5},
		{Token: "好", TimeSec: 0.8},
		{Token: "世", TimeSec: 1.2},
		{Token: "界", TimeSec: 1.5},
	})
	s.True(ok)
	// cleanText = 你好，世界（5），chars = 你好世界（4）→ LCS：逗号继承 0.8。
	s.Equal([]float32{0.5, 0.8, 0.8, 1.2, 1.5}, times)
}

// 长度不一致（文本为 "hello"，token 为 "Hello"）：忽略大小写匹配，各字母继承词时间。
func (s *AlignTestSuite) TestAlignCharTimesCaseInsensitive() {
	text := "hello"
	times, ok := AlignCharTimes(text, []TokenTimestamp{
		{Token: "Hello", TimeSec: 1.8},
	})
	s.True(ok)
	s.Equal([]float32{1.8, 1.8, 1.8, 1.8, 1.8}, times)
}

// 长度不一致（token 为 BPE 子词展开，比文本长）：多出的 token 字符被跳过。
func (s *AlignTestSuite) TestAlignCharTimesTokenLongerThanText() {
	text := "你好"
	times, ok := AlignCharTimes(text, []TokenTimestamp{
		{Token: "你", TimeSec: 0.5},
		{Token: "好", TimeSec: 0.8},
		{Token: "呀", TimeSec: 1.0}, // 文本中没有的 token 字符
		{Token: "啊", TimeSec: 1.2},
	})
	s.True(ok)
	// LCS：你、好 匹配；呀、啊 被跳过。
	s.Equal([]float32{0.5, 0.8}, times)
}

// 文本比 token 长（中间结果 hypothesis 尾部未固定）：尾部字符继承最后一个 token 时间。
func (s *AlignTestSuite) TestAlignCharTimesTextLongerThanTokens() {
	text := "你好世界"
	times, ok := AlignCharTimes(text, []TokenTimestamp{
		{Token: "你", TimeSec: 0.5},
		{Token: "好", TimeSec: 0.8},
	})
	s.True(ok)
	// 世、界 无匹配 → 继承 0.8。
	s.Equal([]float32{0.5, 0.8, 0.8, 0.8}, times)
}

// 长度不一致（文本 "Hello world!" 含标点，token 为小写英文）：
// LCS 忽略大小写匹配，感叹号继承最近匹配 token 的时间。
func (s *AlignTestSuite) TestAlignCharTimesEnglishCaseLCS() {
	text := "Hello world!"
	times, ok := AlignCharTimes(text, []TokenTimestamp{
		{Token: "hello", TimeSec: 1.8},
		{Token: "▁world", TimeSec: 2.5},
	})
	s.True(ok)
	s.Len(times, len([]rune(text)))
	// []rune = [H,e,l,l,o,' ',w,o,r,l,d,!]
	s.Equal([]float32{1.8, 1.8, 1.8, 1.8, 1.8, 1.8, 2.5, 2.5, 2.5, 2.5, 2.5, 2.5}, times)
}

// 空 token 列表：返回 false。
func (s *AlignTestSuite) TestAlignCharTimesEmptyTokens() {
	times, ok := AlignCharTimes("你好", nil)
	s.False(ok)
	s.Nil(times)
}

// tokens 与 text 完全无法匹配（如实际环境中离线 transducer 的 Tokens 为乱码）：
// LCS 退化为全 0，必须返回失败，由调用方退化为近似方案，杜绝 0-0 句子时间戳。
func (s *AlignTestSuite) TestAlignCharTimesUnmatchableFallsBack() {
	text := "这是伤转环游啊孩子们走轮步伴国面科以口听身心"
	times, ok := AlignCharTimes(text, []TokenTimestamp{
		{Token: "Əťļ", TimeSec: 0.1},
		{Token: "ƍĻŕ", TimeSec: 0.5},
		{Token: "ƋŢŊ", TimeSec: 1.0},
		{Token: "ƏţŒ", TimeSec: 1.5},
	})
	s.False(ok)
	s.Nil(times)
}

// token 时间戳全为 0（部分模型不产出时间戳）：不得当作有效时间戳，应返回失败。
func (s *AlignTestSuite) TestAlignCharTimesAllZeroTimesFallsBack() {
	times, ok := AlignCharTimes("你好世界", []TokenTimestamp{
		{Token: "你", TimeSec: 0},
		{Token: "好", TimeSec: 0},
		{Token: "世", TimeSec: 0},
		{Token: "界", TimeSec: 0},
	})
	s.False(ok)
	s.Nil(times)
}

// 超长文本且长度不一致：LCS 保护上限，返回 false（由调用方退化）。
func (s *AlignTestSuite) TestAlignCharTimesTooLongFallsBack() {
	long := make([]TokenTimestamp, 0, 2199)
	for i := 0; i < 2199; i++ {
		long = append(long, TokenTimestamp{Token: "字", TimeSec: 0.01 * float32(i)})
	}
	times, ok := AlignCharTimes(makeLongText(2200), long)
	s.False(ok)
	s.Nil(times)
}

// 中英混合全文：每个可见字都应拿到单调递增的时间戳。
func (s *AlignTestSuite) TestAlignCharTimesMixedEndToEnd() {
	text := "我们开始今天的会议 meeting 关于项目进度"
	tokens := []TokenTimestamp{
		{Token: "我", TimeSec: 0.4},
		{Token: "们", TimeSec: 0.7},
		{Token: "开", TimeSec: 1.0},
		{Token: "始", TimeSec: 1.3},
		{Token: "今", TimeSec: 1.6},
		{Token: "天", TimeSec: 1.9},
		{Token: "的", TimeSec: 2.2},
		{Token: "会", TimeSec: 2.5},
		{Token: "议", TimeSec: 2.8},
		{Token: "▁meeting", TimeSec: 3.1},
		{Token: "关", TimeSec: 4.0},
		{Token: "于", TimeSec: 4.3},
		{Token: "项", TimeSec: 4.6},
		{Token: "目", TimeSec: 4.9},
		{Token: "进", TimeSec: 5.2},
		{Token: "度", TimeSec: 5.5},
	}
	times, ok := AlignCharTimes(text, tokens)
	s.True(ok)
	s.Len(times, len([]rune(text)))
	// 忽略空格与英文内部字符的继承，非空格字符的时间必须单调不减且落在 token 时间附近。
	var last float32
	checked := 0
	for i, r := range []rune(text) {
		if r == ' ' {
			continue
		}
		checked++
		s.GreaterOrEqual(times[i], last, "char %q at rune %d must not go backwards", string(r), i)
		last = times[i]
	}
	s.Equal(22, checked) // 13 个汉字 + meeting(7 字母)
}

func makeLongText(n int) string {
	runes := make([]rune, 0, n)
	for i := 0; i < n; i++ {
		runes = append(runes, '字')
	}
	return string(runes)
}

// WordsFromCharTimes：中文按字、英文按词切分，时间单调递增，词尾对齐文本结尾。
func (s *AlignTestSuite) TestWordsFromCharTimesMixed() {
	text := "你好 world"
	times := []float32{0.5, 0.8, 0.8, 1.8, 1.9, 2.0, 2.1, 2.2}
	words := WordsFromCharTimes(text, times)
	s.Len(words, 3)
	s.Equal("你", words[0].Word)
	s.Equal(int64(500), words[0].StartMs)
	s.Equal(int64(500), words[0].EndMs)
	s.Equal("好", words[1].Word)
	s.Equal(int64(800), words[1].StartMs)
	s.Equal(int64(800), words[1].EndMs)
	s.Equal("world", words[2].Word)
	s.Equal(int64(1800), words[2].StartMs)
	s.Equal(int64(2200), words[2].EndMs)
}

// WordsFromCharTimes：times 长度不匹配时返回 nil。
func (s *AlignTestSuite) TestWordsFromCharTimesLengthMismatch() {
	words := WordsFromCharTimes("你好", []float32{0.5})
	s.Nil(words)
}

// WordsFromCharTimesIntervals：时间点展开为区间——字/词 i 的结束时间 =
// 下一个字/词的开始时间（连续语音时），但不超过按词长估计的最长时长；
// 静音间隙（此处"好"与"world"间隔 1s）会被截断，不会整段归到前一个字上。
func (s *AlignTestSuite) TestWordsFromCharTimesIntervalsMixed() {
	text := "你好 world"
	times := []float32{0.5, 0.8, 0.8, 1.8, 1.9, 2.0, 2.1, 2.2}
	words := WordsFromCharTimesIntervals(text, times)
	s.Len(words, 3)
	s.Equal("你", words[0].Word)
	s.Equal(int64(500), words[0].StartMs)
	s.Equal(int64(800), words[0].EndMs) // 展开到下一字开始
	s.Equal("好", words[1].Word)
	s.Equal(int64(800), words[1].StartMs)
	s.Equal(int64(1300), words[1].EndMs) // 单字上限 500ms，静音间隙被截断
	s.Equal("world", words[2].Word)
	s.Equal(int64(1800), words[2].StartMs)
	s.Equal(int64(2200), words[2].EndMs) // 末词保留末字真实结尾
}

// WordsFromCharTimesIntervals：与 WordsFromCharTimes 相同的输入校验。
func (s *AlignTestSuite) TestWordsFromCharTimesIntervalsInvalidInput() {
	s.Nil(WordsFromCharTimesIntervals("你好", []float32{0.5}))
	s.Nil(WordsFromCharTimesIntervals("", nil))
}

// WordsFromCharTimesIntervals：连续语音（字间距 ≤ 500ms 上限）时区间首尾相接、
// 时间单调不减，除末字外每个字都有非零时长。
func (s *AlignTestSuite) TestWordsFromCharTimesIntervalsMonotonic() {
	text := "一二三四五六七八九十"
	times := make([]float32, 10)
	for i := range times {
		times[i] = 1.0 + 0.5*float32(i)
	}
	words := WordsFromCharTimesIntervals(text, times)
	s.Len(words, 10)
	for i := 1; i < len(words); i++ {
		s.Equal(words[i-1].EndMs, words[i].StartMs, "区间应首尾相接")
		s.GreaterOrEqual(words[i].StartMs, words[i-1].StartMs)
	}
	for i := 0; i < len(words)-1; i++ {
		s.Greater(words[i].EndMs, words[i].StartMs, "字 %q 应有非零时长", words[i].Word)
	}
}

// WordsFromCharTimesIntervals：词间存在长静音时，静音时长不得整段归到前一个词上——
// 每个词的时长应被限制在按词长估计的上限内（单字 ≤ 500ms）。
func (s *AlignTestSuite) TestWordsFromCharTimesIntervalsCapsSilenceGap() {
	// "播放" 与 "现在" 之间约 5.4s 静音（复现 李大爷.wav 中 "放" 字被拉长到 5.48s 的根因）。
	text := "播放现在是"
	// 播=6.24s 放=6.68s 现=12.16s 在=12.24s 是=12.52s（放 与 现 之间 5.48s 静音）
	times := []float32{6.24, 6.68, 12.16, 12.24, 12.52}
	words := WordsFromCharTimesIntervals(text, times)
	s.Len(words, 5)

	// 每个单字时长不超过 500ms（CJK 单字上限）。
	for _, w := range words {
		dur := w.EndMs - w.StartMs
		s.LessOrEqual(dur, int64(maxCJKCharDurationMs), "字 %q 时长 %dms 超出上限，静音可能被错误归到该字上", w.Word, dur)
		s.Greater(dur, int64(0), "字 %q 应有非零时长", w.Word)
	}

	// 关键回归：紧邻 5.48s 静音之前的 "放" 字不得跨过静音。
	s.Equal("放", words[1].Word)
	s.Equal(int64(6680), words[1].StartMs)
	s.Equal(int64(7180), words[1].EndMs) // 6680 + 500ms，而非下一字 "现" 的 12160

	// "现" 的起点保持其真实时间（不被前一词的截断终点所移动）。
	s.Equal(int64(12160), words[2].StartMs)
}
