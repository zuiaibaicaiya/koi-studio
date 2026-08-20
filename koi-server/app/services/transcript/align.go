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
// 思路：先去掉空白，把各 token（去掉 "▁" 前缀）展开成的字符序列与 text 逐字对齐。
// 两者字符数一致时按位置一一对应；不一致（中英混合场景下模型 BPE 分词、标点、
// 大小写等与最终文本存在差异）时基于最长公共子序列（LCS）做容错对齐：text 中
// 无法匹配的字符继承最近一个匹配 token 的时间。返回的切片长度等于 []rune(text)，
// 其中的空格符会继承相邻非空格字符的时间，从而保证每个可见字都能拿到与音频
// 对齐的时间。
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
	var cleanTimes []float32
	if len(cleanText) == len(chars) {
		cleanTimes = make([]float32, len(cleanText))
		for i := range cleanText {
			cleanTimes[i] = chars[i].t
		}
	} else {
		cleanChars := make([]rune, len(chars))
		charsTimes := make([]float32, len(chars))
		for i, c := range chars {
			cleanChars[i] = c.r
			charsTimes[i] = c.t
		}
		cleanTimes = alignByLCS(cleanText, cleanChars, charsTimes)
		if cleanTimes == nil {
			return nil, false
		}
	}

	runes := []rune(text)
	out := make([]float32, len(runes))
	ci := 0
	for i, r := range runes {
		if unicode.IsSpace(r) {
			continue
		}
		out[i] = cleanTimes[ci]
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
	// 对齐结果若没有任何有效时间戳（例如模型 tokens 与 text 完全无法对应时，
	// LCS 会退化为全 0），视为无有效时间戳，由调用方退化为近似方案，
	// 避免产出 0-0 的错误句子时间戳。
	has := false
	for _, t := range out {
		if t > 0 {
			has = true
			break
		}
	}
	if !has {
		return nil, false
	}
	return out, true
}

// alignByLCS 基于最长公共子序列（LCS）将 text 字符序列与 token 展开的字符序列
// 对齐，返回长度为 len(text) 的时间数组（单位秒）。
//
// text 中无法匹配任何 token 字符的字符（如文本中的标点、大小写差异）继承最近
// 一个匹配 token 的时间；token 中多出的字符（BPE 子词等）被跳过。字符序列过长
// 时返回 nil，由调用方退化为近似方案。
func alignByLCS(text, chars []rune, times []float32) []float32 {
	const maxAlignRunes = 2048
	if len(text) > maxAlignRunes || len(chars) > maxAlignRunes || len(times) != len(chars) {
		return nil
	}
	n, m := len(text), len(chars)
	// dp[i][j] 表示 text[i:] 与 chars[j:] 的 LCS 长度（反向计算，便于正向回溯）。
	dp := make([][]int16, n+1)
	for i := range dp {
		dp[i] = make([]int16, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if runeEq(text[i], chars[j]) {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	result := make([]float32, n)
	i, j := 0, 0
	lastT := float32(0)
	for i < n && j < m {
		if runeEq(text[i], chars[j]) {
			lastT = times[j]
			result[i] = lastT
			i++
			j++
			continue
		}
		if dp[i+1][j] >= dp[i][j+1] {
			// text[i] 在最优对齐中无匹配 token：继承最近匹配的时间。
			result[i] = lastT
			i++
		} else {
			j++ // 跳过 token 中多出的字符（BPE 子词等）。
		}
	}
	for ; i < n; i++ {
		result[i] = lastT
	}
	return result
}

// runeEq 判断两个字符在时间戳对齐中是否视为相同：
// 完全相等，或同为 ASCII 字母且忽略大小写相等（兼容英文大小写差异）。
func runeEq(a, b rune) bool {
	if a == b {
		return true
	}
	la, lb := unicode.ToLower(a), unicode.ToLower(b)
	return la == lb && la >= 'a' && la <= 'z'
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
