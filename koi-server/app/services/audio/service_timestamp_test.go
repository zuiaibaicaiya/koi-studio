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
func (s *WordTimestampTestSuite) TestBuildRealtimeWordTimestampsChinese() {
	emitted := []tokenEmit{
		{token: "你", samplePos: 8000},   // 0.5s
		{token: "好", samplePos: 12800},  // 0.8s
		{token: "世", samplePos: 19200},  // 1.2s
		{token: "界", samplePos: 24000},  // 1.5s
	}
	words := buildRealtimeWordTimestamps("你好世界", emitted, 16000, 500, 1500)
	s.Len(words, 4)
	s.Equal("你", words[0].Word)
	s.Equal(int64(500), words[0].StartMs)
	s.Equal(int64(500), words[0].EndMs)
	s.Equal("好", words[1].Word)
	s.Equal(int64(800), words[1].StartMs)
	s.Equal("界", words[3].Word)
	s.Equal(int64(1500), words[3].StartMs)
	s.Equal(int64(1500), words[3].EndMs)
}

// buildRealtimeWordTimestamps：中英混合，英文词整体一个时间戳，中文逐字。
func (s *WordTimestampTestSuite) TestBuildRealtimeWordTimestampsMixed() {
	emitted := []tokenEmit{
		{token: "你", samplePos: 8000},    // 0.5s
		{token: "好", samplePos: 12800},   // 0.8s
		{token: "▁world", samplePos: 28800}, // 1.8s
	}
	words := buildRealtimeWordTimestamps("你好 world", emitted, 16000, 500, 2200)
	s.Len(words, 3)
	s.Equal("你", words[0].Word)
	s.Equal(int64(500), words[0].StartMs)
	s.Equal("好", words[1].Word)
	s.Equal(int64(800), words[1].StartMs)
	s.Equal("world", words[2].Word)
	s.Equal(int64(1800), words[2].StartMs)
	s.Equal(int64(1800), words[2].EndMs)
}

// buildRealtimeWordTimestamps：无已发射 token 时返回 nil。
func (s *WordTimestampTestSuite) TestBuildRealtimeWordTimestampsEmptyEmitted() {
	words := buildRealtimeWordTimestamps("你好", nil, 16000, 500, 1500)
	s.Nil(words)
}

// buildRealtimeWordTimestamps：endMs <= startMs 时返回 nil。
func (s *WordTimestampTestSuite) TestBuildRealtimeWordTimestampsInvalidRange() {
	emitted := []tokenEmit{{token: "你", samplePos: 8000}}
	words := buildRealtimeWordTimestamps("你", emitted, 16000, 1000, 500)
	s.Nil(words)
}

// buildRealtimeWordTimestamps：词时间戳必须落在 [startMs, endMs] 内且单调不减。
func (s *WordTimestampTestSuite) TestBuildRealtimeWordTimestampsClamped() {
	emitted := []tokenEmit{
		{token: "你", samplePos: 16000},  // 1.0s
		{token: "好", samplePos: 48000},  // 3.0s
	}
	words := buildRealtimeWordTimestamps("你好", emitted, 16000, 1000, 2000)
	s.Len(words, 2)
	// 好 的 3.0s 超出 endMs=2000，被裁剪到 2000。
	s.Equal(int64(2000), words[1].StartMs)
	s.Equal(int64(2000), words[1].EndMs)
}

// clampWords：起始时间早于 startMs 时裁剪，且结束后时间被钳制为不小于起点。
func (s *WordTimestampTestSuite) TestClampWordsClampsStart() {
	words := []models.WordTimestamp{
		{Word: "a", StartMs: 100, EndMs: 300},
	}
	out := clampWords(words, 500, 2000)
	s.Len(out, 1)
	s.Equal("a", out[0].Word)
	s.Equal(int64(500), out[0].StartMs)
	s.Equal(int64(500), out[0].EndMs)
}

// clampWords：结束时间晚于 endMs 时裁剪。
func (s *WordTimestampTestSuite) TestClampWordsClampsEnd() {
	words := []models.WordTimestamp{
		{Word: "a", StartMs: 600, EndMs: 900},
	}
	out := clampWords(words, 500, 800)
	s.Len(out, 1)
	s.Equal(int64(600), out[0].StartMs)
	s.Equal(int64(800), out[0].EndMs)
}

// clampWords：后续词时间回退时钳制为前一个词的结束时间，保证单调不减。
func (s *WordTimestampTestSuite) TestClampWordsEnforcesMonotonic() {
	words := []models.WordTimestamp{
		{Word: "a", StartMs: 700, EndMs: 700},
		{Word: "b", StartMs: 600, EndMs: 650}, // 整体回退
	}
	out := clampWords(words, 500, 2000)
	s.Len(out, 2)
	s.Equal(int64(700), out[1].StartMs)
	s.Equal(int64(700), out[1].EndMs)
}

// clampWords：空输入返回 nil。
func (s *WordTimestampTestSuite) TestClampWordsEmpty() {
	s.Nil(clampWords(nil, 500, 2000))
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
