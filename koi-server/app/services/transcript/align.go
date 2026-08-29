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

// WordsFromCharTimes 依据逐字时间戳（秒）生成字/词级“时间点”时间戳（毫秒）。
//
// 注意：times 来自模型 token 时间戳，语义是该字发音的【结束】时刻，因此本函数
// 产出的 StartMs/EndMs 也只是“结束时刻”，字/词区间长度为 0（单词素）。
// 需要真正的时间区间请改用 WordsFromCharTimesIntervals。
//
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

// WordsFromCharTimesIntervals 基于逐字时间戳（秒）生成“文字级”时间戳（毫秒）：
// 每个字/词占据一段连续且与音频真实发音对齐的时间区间。
//
// 关键语义：sherpa-onnx 在线识别器产出的 token 时间戳是该 token「被识别出来」的
// 时刻，实测约等于该字发音的【结束】时刻，而不是开始时刻。对照实验
// （tests/asr/synthonset_test.go：把已知边界的单字拼成连续语音）结果：
//
//	真实区间 [2.00, 2.38] -> 模型时间戳 2.24
//	真实区间 [2.38, 2.76] -> 模型时间戳 2.64
//	真实区间 [2.76, 3.06] -> 模型时间戳 3.08
//
// 即时间戳落在字的末尾而非开头。因此这里把 times[i] 作为本字/词的【结束】时刻，
// 起始时刻按如下规则向前回溯：
//
//   - 与上一个字/词的结束时刻间距在「按词长估计的最长发音时长」内：视为连续语音，
//     起点直接取上一个字/词的结束时刻，区间首尾相接；
//   - 否则（间距过大，说明中间是停顿/静音；或本字/词是第一个）：按词长向前回退
//     一个常规发音时长。
//
// 这样既不会把停顿/静音整段算到前一个字上，也不会把每个字的时间整体推后约一个字
// （此前把时间戳当作起始时刻，导致点击第 i 个字实际播放的是第 i+1 个字的音频）。
func WordsFromCharTimesIntervals(text string, times []float32) []models.WordTimestamp {
	ivs := WordsWithSpansFromCharTimes(text, times)
	if len(ivs) == 0 {
		return nil
	}
	out := make([]models.WordTimestamp, 0, len(ivs))
	for _, iv := range ivs {
		out = append(out, models.WordTimestamp{Word: iv.Word, StartMs: iv.StartMs, EndMs: iv.EndMs})
	}
	return out
}

// WordInterval 描述文本中一个字/词的位置（rune 区间）与它在音频上的时间区间。
type WordInterval struct {
	Word    string
	Start   int // 在 []rune(text) 中的起始下标（含）
	End     int // 在 []rune(text) 中的结束下标（不含）
	StartMs int64
	EndMs   int64
}

// WordsWithSpansFromCharTimes 与 WordsFromCharTimesIntervals 的时间计算完全一致，
// 但额外返回每个字/词的 rune 区间，便于调用方在不重新分词的前提下按句子切分。
//
// 注意：字/词区间必须基于【整段文本】一次性计算后再切分。若逐句独立计算，
// 每句首字都会因为没有前驱时间戳而向前回退一个常规发音时长，导致相邻句子的
// 时间区间相互重叠、且首字起点偏早。
func WordsWithSpansFromCharTimes(text string, times []float32) []WordInterval {
	runes := []rune(text)
	if len(times) != len(runes) || len(runes) == 0 {
		return nil
	}
	spans := splitWordSpans(text)
	if len(spans) == 0 {
		return nil
	}

	var result []WordInterval
	prevEnd := int64(-1) // 上一个字/词的结束时刻；-1 表示尚无前驱
	for _, sp := range spans {
		if sp.end <= sp.start || sp.start >= len(times) {
			continue
		}
		end := sp.end
		if end > len(times) {
			end = len(times)
		}
		if end <= sp.start {
			continue
		}

		// 模型时间戳 = 本字/词发音结束的时刻。
		endMs := int64(times[end-1] * 1000)
		if endMs < 0 {
			endMs = 0
		}

		// 默认起点：按词长向前回退一个常规发音时长。
		startMs := endMs - nominalWordDurationMs(sp.text)
		// 连续语音（与前一个字/词的间距在最长发音时长内）时首尾相接。
		if prevEnd >= 0 && endMs-prevEnd > 0 && endMs-prevEnd <= estimatedWordDurationMs(sp.text) {
			startMs = prevEnd
		}
		if startMs < 0 {
			startMs = 0
		}
		if startMs > endMs {
			startMs = endMs
		}

		prevEnd = endMs
		result = append(result, WordInterval{
			Word:    sp.text,
			Start:   sp.start,
			End:     end,
			StartMs: startMs,
			EndMs:   endMs,
		})
	}
	return result
}

// maxCJKCharDurationMs 单个汉字在词级时间戳区间中的最长估计时长（毫秒）。
// 正常语速下汉字约 0.2~0.4s；取 500ms 作为保守上限，用于判断两个字之间是否
// 属于连续语音（超过该间隔即认为中间存在停顿/静音）。
const maxCJKCharDurationMs = 500

// maxASCIICharDurationMs 单个英文字母/数字在词级时间戳区间中的最长估计时长（毫秒）。
// 英文字母语速明显快于汉字（约 0.08~0.15s/字母），取 200ms 作为上限。
const maxASCIICharDurationMs = 200

// nominalCJKCharDurationMs 正常语速下单字汉字的常规发音时长（毫秒）。
// 仅在没有前驱时间戳可依据（首字，或前面是停顿/静音）时用于向前回退估算起始时刻。
const nominalCJKCharDurationMs = 300

// nominalASCIICharDurationMs 正常语速下单个英文字母/数字的常规发音时长（毫秒）。
const nominalASCIICharDurationMs = 120

// estimatedWordDurationMs 按词长估计一个词的最长合理时长（毫秒）：
// 汉字按 500ms/字、其余字符（英文字母、数字、标点等）按 200ms/字符累加，
// 空词兜底为一个汉字时长。用于判断词与词之间是否连续（间隔超过该值视为停顿）。
func estimatedWordDurationMs(word string) int64 {
	var total int64
	n := 0
	for _, r := range word {
		n++
		if unicode.Is(unicode.Han, r) {
			total += maxCJKCharDurationMs
		} else {
			total += maxASCIICharDurationMs
		}
	}
	if n == 0 || total <= 0 {
		return maxCJKCharDurationMs
	}
	return total
}

// nominalWordDurationMs 按词长估计一个词的常规发音时长（毫秒）：
// 汉字按 300ms/字、其余字符按 120ms/字符累加，空词兜底为一个汉字时长。
// 用于在没有前驱时间戳可依据时向前回退估算该词的起始时刻。
func nominalWordDurationMs(word string) int64 {
	var total int64
	n := 0
	for _, r := range word {
		n++
		if unicode.Is(unicode.Han, r) {
			total += nominalCJKCharDurationMs
		} else {
			total += nominalASCIICharDurationMs
		}
	}
	if n == 0 || total <= 0 {
		return nominalCJKCharDurationMs
	}
	return total
}
