package audio

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"koi-server/app/models"
)

type WordTimestampTestSuite struct {
	suite.Suite
}

func TestWordTimestampTestSuite(t *testing.T) {
	suite.Run(t, new(WordTimestampTestSuite))
}

// buildRealtimeWordTimestamps：中文逐字时间戳，对齐到音频采样位置。
//
// 模型 token 时间戳是该字发音的【结束】时刻，因此每个字的区间向前回溯：
// 连续语音时区间首尾相接，首字按常规发音时长(300ms)向前回退。
// emitted 中的 samplePos 为整场会话音频内的绝对位置，无需再按句起点平移。
func (s *WordTimestampTestSuite) TestBuildRealtimeWordTimestampsChinese() {
	emitted := []tokenEmit{
		{token: "你", samplePos: 12800}, // 0.8s
		{token: "好", samplePos: 19200}, // 1.2s
		{token: "世", samplePos: 24000}, // 1.5s
		{token: "界", samplePos: 28800}, // 1.8s
	}
	words := buildRealtimeWordTimestamps("你好世界", emitted, 16000, 1800)
	s.Len(words, 4)
	s.Equal("你", words[0].Word)
	s.Equal(int64(500), words[0].StartMs) // 800 - 300ms 常规发音时长
	s.Equal(int64(800), words[0].EndMs)   // 结束 = 模型时间戳
	s.Equal("好", words[1].Word)
	s.Equal(int64(800), words[1].StartMs) // 紧接上一字结束
	s.Equal(int64(1200), words[1].EndMs)
	s.Equal("世", words[2].Word)
	s.Equal(int64(1200), words[2].StartMs)
	s.Equal(int64(1500), words[2].EndMs)
	s.Equal("界", words[3].Word)
	s.Equal(int64(1500), words[3].StartMs)
	s.Equal(int64(1800), words[3].EndMs)
}

// buildRealtimeWordTimestamps：中英混合，英文词整体一个时间戳，中文逐字。
// 词间静音（"好"结束于 1.2s，"world"结束于 2.6s，间隔 1.4s > world 的最长时长 1s）
// 判定为停顿，按词长向前回退，静音不归给任何字/词。
func (s *WordTimestampTestSuite) TestBuildRealtimeWordTimestampsMixed() {
	emitted := []tokenEmit{
		{token: "你", samplePos: 12800},      // 0.8s
		{token: "好", samplePos: 19200},      // 1.2s
		{token: "▁world", samplePos: 41600}, // 2.6s
	}
	words := buildRealtimeWordTimestamps("你好 world", emitted, 16000, 2600)
	s.Len(words, 3)
	s.Equal("你", words[0].Word)
	s.Equal(int64(500), words[0].StartMs)
	s.Equal(int64(800), words[0].EndMs)
	s.Equal("好", words[1].Word)
	s.Equal(int64(800), words[1].StartMs)
	s.Equal(int64(1200), words[1].EndMs)
	s.Equal("world", words[2].Word)
	s.Equal(int64(2000), words[2].StartMs) // 2600 - 5 字母常规时长 600ms
	s.Equal(int64(2600), words[2].EndMs)
}

// buildRealtimeWordTimestamps：无已发射 token 时返回 nil。
func (s *WordTimestampTestSuite) TestBuildRealtimeWordTimestampsEmptyEmitted() {
	words := buildRealtimeWordTimestamps("你好", nil, 16000, 1500)
	s.Nil(words)
}

// buildRealtimeWordTimestamps：采样率非法时返回 nil。
func (s *WordTimestampTestSuite) TestBuildRealtimeWordTimestampsInvalidSampleRate() {
	emitted := []tokenEmit{{token: "你", samplePos: 8000}}
	s.Nil(buildRealtimeWordTimestamps("你", emitted, 0, 1000))
}

// buildRealtimeWordTimestamps：词尾超出 endMs 时被裁剪，且不产生零宽区间。
func (s *WordTimestampTestSuite) TestBuildRealtimeWordTimestampsClamped() {
	emitted := []tokenEmit{
		{token: "你", samplePos: 16000}, // 1.0s
		{token: "好", samplePos: 48000}, // 3.0s
	}
	words := buildRealtimeWordTimestamps("你好", emitted, 16000, 2000)
	s.Len(words, 2)
	// 好 的区间 [2700, 3000] 整体落在 endMs=2000 之后：保留原区间而不是压成零宽。
	s.Equal(int64(2700), words[1].StartMs)
	s.Equal(int64(3000), words[1].EndMs)
}

// buildRealtimeWordTimestamps：句子起点晚于首字结束时，首字区间不得被压成零宽。
//
// 历史 bug：曾用能量检测得到的句子开始时间作为词区间的下界裁剪，
// 当该起点偏晚（噪声/检测滞后）时首字被压成 [startMs, startMs]，
// 前端既无法把该字高亮为「正在播放」，又会自行补时长导致与后一字重叠。
func (s *WordTimestampTestSuite) TestBuildRealtimeWordTimestampsKeepsFirstWordSpan() {
	emitted := []tokenEmit{
		{token: "你", samplePos: 12800}, // 0.8s
		{token: "好", samplePos: 19200}, // 1.2s
	}
	words := buildRealtimeWordTimestamps("你好", emitted, 16000, 1200)
	s.Len(words, 2)
	s.Less(words[0].StartMs, words[0].EndMs) // 区间长度 > 0
	s.Equal(int64(800), words[0].EndMs)
}

// clampWordEnds：结束时间晚于 endMs 时裁剪。
func (s *WordTimestampTestSuite) TestClampWordEndsClampsEnd() {
	words := []models.WordTimestamp{
		{Word: "a", StartMs: 600, EndMs: 900},
	}
	out := clampWordEnds(words, 800)
	s.Len(out, 1)
	s.Equal(int64(600), out[0].StartMs)
	s.Equal(int64(800), out[0].EndMs)
}

// clampWordEnds：完全不裁剪下界——首字起点早于句子起点时原样保留。
func (s *WordTimestampTestSuite) TestClampWordEndsKeepsEarlyStart() {
	words := []models.WordTimestamp{
		{Word: "a", StartMs: 100, EndMs: 300},
	}
	out := clampWordEnds(words, 2000)
	s.Len(out, 1)
	s.Equal("a", out[0].Word)
	s.Equal(int64(100), out[0].StartMs)
	s.Equal(int64(300), out[0].EndMs)
}

// clampWordEnds：后续词时间回退时钳制为前一个词的结束时间，保证单调不减。
func (s *WordTimestampTestSuite) TestClampWordEndsEnforcesMonotonic() {
	words := []models.WordTimestamp{
		{Word: "a", StartMs: 700, EndMs: 700},
		{Word: "b", StartMs: 600, EndMs: 650}, // 整体回退
	}
	out := clampWordEnds(words, 2000)
	s.Len(out, 2)
	s.Equal(int64(700), out[1].StartMs)
	s.Equal(int64(700), out[1].EndMs)
}

// clampWordEnds：空输入返回 nil。
func (s *WordTimestampTestSuite) TestClampWordEndsEmpty() {
	s.Nil(clampWordEnds(nil, 2000))
}

// timestampsValid：全 0 的时间戳视为无效（sherpa 在某些情况下会返回等长全 0 数组）。
func (s *WordTimestampTestSuite) TestTimestampsValid() {
	s.False(timestampsValid(nil))
	s.False(timestampsValid([]float32{0, 0, 0}))
	s.True(timestampsValid([]float32{0, 0.4}))
}

// splitWords：中文为主时按字切分，英文单词作为整体。
func (s *WordTimestampTestSuite) TestSplitWordsChinese() {
	s.Equal([]string{"你", "好", "世", "界"}, splitWords("你好世界"))
}

// splitWords：纯英文按空格分词。
func (s *WordTimestampTestSuite) TestSplitWordsEnglish() {
	s.Equal([]string{"hello", "world"}, splitWords("hello world"))
}

// splitWords：中英混合，中文逐字、英文整体。
func (s *WordTimestampTestSuite) TestSplitWordsMixed() {
	s.Equal([]string{"你", "好", "world"}, splitWords("你好 world"))
}

// splitWords：空文本与纯空白返回 nil。
func (s *WordTimestampTestSuite) TestSplitWordsEmpty() {
	s.Nil(splitWords(""))
	s.Nil(splitWords("   "))
}
