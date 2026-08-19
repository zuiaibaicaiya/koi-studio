// Package transcript 提供将模型 token 级时间戳对齐到文本字/词的通用工具，
// 供离线转写与实时转写复用，确保时间戳与音频真实时间对齐。
package transcript

import (
	"strings"
	"unicode"

	"koi-server/app/models"
)

// TokenTimestamp 表示模型产出的单个 token 及其相对音频起点的时间戳（秒）。
type TokenTimestamp struct {
	Token   string
	TimeSec float32
}

// AlignCharTimes 将 token 级时间戳展开为 text 中每个字符（rune）的时间戳（秒）。
//
// 思路：先去掉空白，把各 token（去掉 "▁" 前缀）展开成的字符序列与 text 逐字对齐；
// 若两者字符数不一致（模型分词结果与文本存在不可对齐的差异），返回 ok=false，
// 由调用方退化为近似方案。返回的切片长度等于 []rune(text)，其中的空格符会继承
// 相邻非空格字符的时间，从而保证每个可见字都能拿到与音频对齐的时间。
func AlignCharTimes(text string, tokenTimes []TokenTimestamp) (charTimes []float32, ok bool) {
	type ct struct {
		r rune
		t float32
	}
	var chars []ct
	for _, tt := range tokenTimes {
		raw := strings.TrimPrefix(tt.Token, "▁")
		if raw == "" {
			continue
		}
		for _, r := range raw {
			chars = append(chars, ct{r: r, t: tt.TimeSec})
		}
	}
	if len(chars) == 0 {
		return nil, false
	}

	cleanText := removeSpaces(text)
	cleanChars := make([]rune, 0, len(chars))
	for _, c := range chars {
		cleanChars = append(cleanChars, c.r)
	}
	if len(cleanText) != len(cleanChars) {
		return nil, false
	}

	runes := []rune(text)
	out := make([]float32, len(runes))
	ci := 0
	for i, r := range runes {
		if unicode.IsSpace(r) {
			continue
		}
		out[i] = chars[ci].t
		ci++
	}
	// 空白字符继承相邻非空白字符的时间（先向后填充，再向前处理开头空格）。
	last := float32(0)
	for i := 0; i < len(runes); i++ {
		if !unicode.IsSpace(runes[i]) {
			last = out[i]
		} else if last != 0 {
			out[i] = last
		}
	}
	for i := len(runes) - 1; i >= 0; i-- {
		if unicode.IsSpace(runes[i]) && out[i] == 0 {
			out[i] = last
		} else {
			last = out[i]
		}
	}
	return out, true
}

func removeSpaces(s string) []rune {
	var out []rune
	for _, r := range s {
		if !unicode.IsSpace(r) {
			out = append(out, r)
		}
	}
	return out
}

// wordSpan 表示 text 中一个词/字对应的连续 rune 区间（含非空格字符，空格为分隔符）。
type wordSpan struct {
	text  string
	start int // 在原 text 中的 rune 起始下标（含）
	end   int // rune 结束下标（不含）
}

// splitWordSpans 将文本切分为词/字区间，中文按字、英文按词，空格仅作分隔。
// 返回的区间下标直接对应 []rune(text)，便于与逐字时间戳对齐。
func splitWordSpans(text string) []wordSpan {
	runes := []rune(text)
	var spans []wordSpan
	curStart := -1
	for i, r := range runes {
		if unicode.IsSpace(r) {
			if curStart >= 0 {
				spans = append(spans, wordSpan{text: string(runes[curStart:i]), start: curStart, end: i})
				curStart = -1
			}
			continue
		}
		if curStart < 0 {
			curStart = i
		}
		if unicode.Is(unicode.Han, r) {
			if curStart < i {
				spans = append(spans, wordSpan{text: string(runes[curStart:i]), start: curStart, end: i})
			}
			spans = append(spans, wordSpan{text: string(r), start: i, end: i + 1})
			curStart = -1
		}
	}
	if curStart >= 0 {
		spans = append(spans, wordSpan{text: string(runes[curStart:]), start: curStart, end: len(runes)})
	}
	return spans
}

// WordsFromCharTimes 依据逐字时间戳（秒）生成与音频对齐的字/词级时间戳（毫秒）。
// times 长度必须等于 []rune(text)；否则返回 nil，由调用方退化为近似方案。
func WordsFromCharTimes(text string, times []float32) []models.WordTimestamp {
	if len(times) != len([]rune(text)) {
		return nil
	}
	spans := splitWordSpans(text)
	if len(spans) == 0 {
		return nil
	}
	var result []models.WordTimestamp
	for _, sp := range spans {
		if sp.end <= sp.start {
			continue
		}
		if sp.end > len(times) {
			sp.end = len(times)
		}
		result = append(result, models.WordTimestamp{
			Word:    sp.text,
			StartMs: int64(times[sp.start] * 1000),
			EndMs:   int64(times[sp.end-1] * 1000),
		})
	}
	if len(result) == 0 {
		return nil
	}
	// 保证时间单调递增，并把最后一个词结束时间对齐到文本真实结尾。
	last := int64(0)
	for i := range result {
		if result[i].StartMs < last {
			result[i].StartMs = last
		}
		if result[i].EndMs < result[i].StartMs {
			result[i].EndMs = result[i].StartMs
		}
		last = result[i].EndMs
	}
	result[len(result)-1].EndMs = int64(times[len(times)-1] * 1000)
	return result
}
