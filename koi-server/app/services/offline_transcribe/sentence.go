package offlinetranscribe

import (
	"strings"
	"unicode"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"koi-server/app/models"
	"koi-server/app/services/transcript"
)

// 断句优先级：数值越大越适合在此处断句。
const (
	breakNone     = iota // 0：不可断（普通字）
	breakPause           // 1：字间静音停顿
	breakClause          // 2：句内标点（逗号、顿号、冒号等）
	breakSentence        // 3：句末标点（句号、问号、叹号等）
)

// sentenceOptions 断句参数。
//
// 断句的目标是「不把一句话从中间切开」：优先在句末标点处断，其次在句内标点，
// 再次在说话的自然停顿处断；只有在超过硬上限（整段话特别长且既无标点也无停顿）
// 时才强制切分，且切分点仍取窗口内最优位置，而非简单地按固定字数硬切。
type sentenceOptions struct {
	// MinRunes 断句后每段的最少字符数，避免产出碎片化的短句。
	MinRunes int
	// TargetRunes 达到该长度后，才允许在句内标点或停顿处断句。
	TargetRunes int
	// HardMaxRunes 硬上限：超过该长度必须断句（优先挑窗口内最优断点）。
	HardMaxRunes int
	// PauseMs 相邻字的时间间隔超过该毫秒数视为可断句的停顿。
	PauseMs int
	// MergeGapMs 跨识别窗口合并碎片时允许的最大静音间隙（毫秒）。
	// 两段之间几乎没有静音、且前一段没有句末标点时，说明是同一次切分留下的碎片。
	MergeGapMs int
}

// 断句参数默认值。
const (
	defaultSentenceMinRunes     = 8
	defaultSentenceTargetRunes  = 30
	defaultSentenceHardMaxRunes = 50
	defaultSentencePauseMs      = 500
	defaultSentenceMergeGapMs   = 250
)

// normalized 兜底非法断句参数。
func (o sentenceOptions) normalized() sentenceOptions {
	if o.MinRunes <= 0 {
		o.MinRunes = defaultSentenceMinRunes
	}
	if o.TargetRunes <= 0 {
		o.TargetRunes = defaultSentenceTargetRunes
	}
	if o.HardMaxRunes <= 0 {
		o.HardMaxRunes = defaultSentenceHardMaxRunes
	}
	if o.TargetRunes < o.MinRunes {
		o.TargetRunes = o.MinRunes
	}
	if o.HardMaxRunes < o.TargetRunes {
		o.HardMaxRunes = o.TargetRunes
	}
	if o.PauseMs <= 0 {
		o.PauseMs = defaultSentencePauseMs
	}
	if o.MergeGapMs <= 0 {
		o.MergeGapMs = defaultSentenceMergeGapMs
	}
	return o
}

// isSentenceEnd 判断字符是否为句末标点。
func isSentenceEnd(r rune) bool {
	switch r {
	case '。', '！', '？', '!', '?', '；', ';', '…':
		return true
	}
	return false
}

// isClauseEnd 判断字符是否为句内标点（逗号、顿号、冒号等）。
func isClauseEnd(r rune) bool {
	switch r {
	case '，', ',', '、', '：', ':':
		return true
	}
	return false
}

// isClosingMark 判断字符是否为收尾符号（引号、括号等）。
// 断句点应落在其后，避免把句号与右侧引号拆到两条记录里。
func isClosingMark(r rune) bool {
	switch r {
	case '”', '’', '」', '』', '）', ')', '】', ']', '》', '>', '"', '\'':
		return true
	}
	return false
}

// splitSentenceSpans 把文本切分为句子片段。
//
// charTimes 为每个字符的时间戳（秒，语义是该字发音的结束时刻）；
// 为空时只依据标点断句。返回的片段区间连续覆盖全文，可直接用于切片逐字时间戳。
func splitSentenceSpans(text string, charTimes []float32, opt sentenceOptions) []textSpan {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	opt = opt.normalized()

	prio := make([]int, len(runes))
	for i, r := range runes {
		switch {
		case isSentenceEnd(r):
			prio[i] = breakSentence
		case isClauseEnd(r):
			prio[i] = breakClause
		}
	}
	// 收尾符号：把断点挪到它之后（如「你好。」的右引号）。
	for i := 0; i+1 < len(runes); i++ {
		if prio[i] >= breakClause && isClosingMark(runes[i+1]) {
			prio[i], prio[i+1] = breakNone, prio[i]
			i++
		}
	}
	// 字间静音：模型时间戳可用时，把长停顿作为弱断点。
	if len(charTimes) == len(runes) && opt.PauseMs > 0 {
		for i := 1; i < len(runes); i++ {
			gapMs := int64((charTimes[i] - charTimes[i-1]) * 1000)
			if gapMs >= int64(opt.PauseMs) && prio[i-1] < breakPause {
				prio[i-1] = breakPause
			}
		}
	}

	// bestIdx/bestPrio 记录当前窗口内已出现的最优断点（同级取最靠后者），
	// 这样即使断点出现在到达目标长度之前，也会被回溯使用，
	// 不会因为在断点处字数还不够就永远错过它。
	var out []textSpan
	start := 0
	bestIdx, bestPrio := -1, breakNone
	for i := range runes {
		if prio[i] > breakNone && i >= start+opt.MinRunes-1 && prio[i] >= bestPrio {
			bestIdx, bestPrio = i, prio[i]
		}
		n := i + 1 - start
		if n < opt.MinRunes {
			continue
		}
		// 句末标点：只要够长就断（一句话已经说完）；
		// 句内标点/停顿：攒到目标长度才断（避免把一句话切碎）。
		if bestPrio == breakSentence || (n >= opt.TargetRunes && bestPrio >= breakPause) {
			out = append(out, textSpan{text: string(runes[start : bestIdx+1]), start: start, end: bestIdx + 1})
			start = bestIdx + 1
			bestIdx, bestPrio = -1, breakNone
			continue
		}
		// 硬上限：无论如何都要断，取窗口内最优断点（优先级最高、同级取最靠后者），
		// 而不是在正说着的地方直接切断。
		if n >= opt.HardMaxRunes {
			cut := i
			if bestIdx >= 0 {
				cut = bestIdx
			}
			out = append(out, textSpan{text: string(runes[start : cut+1]), start: start, end: cut + 1})
			start = cut + 1
			bestIdx, bestPrio = -1, breakNone
		}
	}
	if start < len(runes) {
		out = append(out, textSpan{text: string(runes[start:]), start: start, end: len(runes)})
	}

	return mergeTrivialTail(out, opt.MinRunes)
}

// mergeTrivialTail 把没有说完的过短尾段合并回前一段，避免留下孤儿碎片。
// 尾段自带句末标点时说明是一句完整的话（如「好的。」），不合并。
func mergeTrivialTail(spans []textSpan, minRunes int) []textSpan {
	if len(spans) < 2 {
		return spans
	}
	last := spans[len(spans)-1]
	if last.end-last.start >= minRunes || endsWithSentenceEnd(last.text) {
		return spans
	}
	prev := spans[len(spans)-2]
	out := make([]textSpan, 0, len(spans)-1)
	out = append(out, spans[:len(spans)-2]...)
	out = append(out, textSpan{text: prev.text + last.text, start: prev.start, end: last.end})
	return out
}

// simpleSplitSentences 仅依据文本（无音频时间戳）断句。
func simpleSplitSentences(text string) []textSpan {
	return splitSentenceSpans(text, nil, sentenceOptions{})
}

// segmentsFromCharTimes 依据逐字时间戳生成句子分段与词级时间戳。
//
// 字/词区间必须基于整段文本一次性计算后再按句切分：若逐句独立计算，
// 每句首字都会因为没有前驱时间戳而向前回退一个常规发音时长，
// 导致相邻句子的时间区间相互重叠。
func segmentsFromCharTimes(text string, charTimes []float32, sampleRate int, opt sentenceOptions) []sentenceSegment {
	runes := []rune(text)
	if len(runes) == 0 || len(charTimes) != len(runes) {
		return nil
	}

	allWords := transcript.WordsWithSpansFromCharTimes(text, charTimes)
	var out []sentenceSegment
	for _, sp := range splitSentenceSpans(text, charTimes, opt) {
		start, end := sp.start, sp.end
		if end > len(runes) {
			end = len(runes)
		}
		if end <= start {
			continue
		}
		// 取起点落在 [start, end) 内的字/词：按起点归句，保证每个字/词只属于一句。
		var words []models.WordTimestamp
		for _, iv := range allWords {
			if iv.Start < start {
				continue
			}
			if iv.Start >= end {
				break
			}
			words = append(words, models.WordTimestamp{Word: iv.Word, StartMs: iv.StartMs, EndMs: iv.EndMs})
		}
		// 逐字时间戳是「该字发音结束时刻」，句子起止必须取回溯后的字/词区间端点，
		// 否则整句时间会比真实语音晚约一个字。
		startMs := int64(charTimes[start] * 1000)
		endMs := int64(charTimes[end-1] * 1000)
		if len(words) > 0 {
			startMs = words[0].StartMs
			endMs = words[len(words)-1].EndMs
		}
		if endMs < startMs {
			endMs = startMs
		}
		chunkStart := msToSamples(startMs, sampleRate)
		chunkEnd := msToSamples(endMs, sampleRate)
		if chunkEnd < chunkStart {
			chunkEnd = chunkStart
		}
		out = append(out, sentenceSegment{
			text:           sp.text,
			startMs:        startMs,
			endMs:          endMs,
			chunkStart:     chunkStart,
			chunkEnd:       chunkEnd,
			wordTimestamps: words,
		})
	}
	return out
}

// approxCharTimes 在模型未给出 token 级时间戳时，把文本按字符数比例铺到音频的
// 语音段上，得到每个字符的近似时间（秒）。
//
// 近似时间轴只覆盖有语音的区间：静音段不分配任何文字时间，
// 保证文字时间戳与音频实际发音位置一一对应。
// 未检测到语音（如全静音的合成音频）时退化为在整段音频上均分。
func approxCharTimes(text string, spans []speechSpan, totalSamples, sampleRate int) []float32 {
	runes := []rune(text)
	if len(runes) == 0 || sampleRate <= 0 || totalSamples <= 0 {
		return nil
	}

	segs := make([][2]int, 0, len(spans))
	totalSpeech := 0
	for _, sp := range spans {
		if sp.End <= sp.Start {
			continue
		}
		start, end := sp.Start, sp.End
		if start < 0 {
			start = 0
		}
		if end > totalSamples {
			end = totalSamples
		}
		if end <= start {
			continue
		}
		segs = append(segs, [2]int{start, end})
		totalSpeech += end - start
	}
	if len(segs) == 0 {
		segs = [][2]int{{0, totalSamples}}
		totalSpeech = totalSamples
	}
	if totalSpeech <= 0 {
		totalSpeech = 1
	}

	// pref[i] = 前 i 段累计语音时长（采样数）。
	pref := make([]int, len(segs)+1)
	for i, seg := range segs {
		pref[i+1] = pref[i] + (seg[1] - seg[0])
	}

	// charToSample：把字符序号按比例落在「总语音时长」轴上，再映射回真实音频
	// 采样位置（只落在语音段内，静音段不产生任何文字时间）。
	charToSample := func(runeIdx int) int {
		if runeIdx >= len(runes) {
			return segs[len(segs)-1][1]
		}
		p := int(float64(totalSpeech) * float64(runeIdx) / float64(len(runes)))
		if p < 0 {
			p = 0
		}
		if p >= totalSpeech {
			return segs[len(segs)-1][1]
		}
		lo, hi := 0, len(segs)
		for lo+1 < hi {
			mid := (lo + hi) / 2
			if pref[mid] <= p {
				lo = mid
			} else {
				hi = mid
			}
		}
		return segs[lo][0] + (p - pref[lo])
	}

	out := make([]float32, len(runes))
	for i := range runes {
		out[i] = float32(float64(charToSample(i+1)) / float64(sampleRate))
	}
	// 保证单调不减（语音段边界取整可能带来抖动）。
	for i := 1; i < len(out); i++ {
		if out[i] < out[i-1] {
			out[i] = out[i-1]
		}
	}
	return out
}

// buildSentenceSegmentsApprox 在没有 token 时间戳时，按音频的语音段近似分配时间戳。
func buildSentenceSegmentsApprox(text string, samples []float32, sampleRate int, spans []speechSpan, opt sentenceOptions) []sentenceSegment {
	return segmentsFromCharTimes(text, approxCharTimes(text, spans, len(samples), sampleRate), sampleRate, opt)
}

// splitSentencesWithTimestamps 把一段识别结果切分为带时间戳的句子。
//
// 优先使用模型产出的 token 级时间戳（精确到字），否则退化为按音频的语音段
// 近似分配。两条路径共用同一套断句规则，保证断句位置一致。
func splitSentencesWithTimestamps(
	text string,
	results []sherpa.OnlineRecognizerResult,
	sampleRate int,
	samples []float32,
	spans []speechSpan,
	opt sentenceOptions,
) []sentenceSegment {
	if text == "" {
		return nil
	}

	var ts []float32
	var tokens []string
	for _, r := range results {
		ts = append(ts, r.Timestamps...)
		tokens = append(tokens, r.Tokens...)
	}

	if len(ts) > 0 && len(ts) == len(tokens) && hasValidTimestamps(ts) {
		tokenTimes := make([]transcript.TokenTimestamp, len(tokens))
		for i := range tokens {
			tokenTimes[i] = transcript.TokenTimestamp{Token: tokens[i], TimeSec: ts[i]}
		}
		if charTimes, ok := transcript.AlignCharTimes(text, tokenTimes); ok && len(charTimes) == len([]rune(text)) {
			return segmentsFromCharTimes(text, charTimes, sampleRate, opt)
		}
	}

	return buildSentenceSegmentsApprox(text, samples, sampleRate, spans, opt)
}

// mergeSentenceFragments 合并被识别窗口边界切断的句子碎片。
//
// 碎片特征：两段之间的静音极短（说明语音是连续的），且前一段没有以句末标点结尾
// （说明话还没说完）。合并后一条记录就是一句完整的话，而不是两个半句。
//
// 文本与时间高度重合的重复片段（同一段音频被重复解码的产物）会被丢弃。
func mergeSentenceFragments(segments []sentenceSegment, gapMs int64, maxRunes int) []sentenceSegment {
	if len(segments) == 0 {
		return nil
	}

	out := make([]sentenceSegment, 0, len(segments))
	for _, seg := range segments {
		if len(out) == 0 {
			out = append(out, seg)
			continue
		}
		prev := &out[len(out)-1]

		// 重复片段：文本相同且时间高度重合时丢弃后者。
		if prev.text == seg.text && abs64(seg.startMs-prev.startMs) <= duplicateToleranceMs {
			continue
		}

		gap := seg.startMs - prev.endMs
		canMerge := gap <= gapMs &&
			!endsWithSentenceEnd(prev.text) &&
			runeLen(prev.text)+runeLen(seg.text) <= maxRunes
		if canMerge {
			prev.text += seg.text
			if seg.endMs > prev.endMs {
				prev.endMs = seg.endMs
			}
			if seg.chunkEnd > prev.chunkEnd {
				prev.chunkEnd = seg.chunkEnd
			}
			if seg.chunkStart < prev.chunkStart {
				prev.chunkStart = seg.chunkStart
			}
			prev.wordTimestamps = append(prev.wordTimestamps, seg.wordTimestamps...)
			continue
		}
		out = append(out, seg)
	}
	return out
}

// duplicateToleranceMs 判定重复片段时允许的时间偏差（毫秒）。
const duplicateToleranceMs int64 = 500

// endsWithSentenceEnd 判断文本是否以句末标点结尾（允许尾部有收尾符号/空白）。
func endsWithSentenceEnd(text string) bool {
	trimmed := strings.TrimRight(text, " \t\r\n")
	runes := []rune(trimmed)
	for i := len(runes) - 1; i >= 0; i-- {
		if isClosingMark(runes[i]) {
			continue
		}
		return isSentenceEnd(runes[i])
	}
	return false
}

// runeLen 返回文本的字符数。
func runeLen(s string) int {
	return len([]rune(s))
}

// abs64 返回绝对值。
func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// trimSentenceText 去掉句子首尾空白（模型输出常带前导空格）。
func trimSentenceText(text string) string {
	return strings.TrimFunc(text, unicode.IsSpace)
}
