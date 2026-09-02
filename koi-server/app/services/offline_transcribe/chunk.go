package offlinetranscribe

// asrWindow 一次送入识别器的音频区间 [Start, End)，采样下标（相对整段音频）。
//
// 窗口由语音活动检测（VAD）结果规划而来：
//   - 只在静音处切分，语音不会被从中间截断；
//   - 窗口互不重叠，同一段音频只被转写一次（旧的固定 30s + 2s 重叠滑窗
//     会把重叠区重复解码，重新转写后出现重复句子）。
type asrWindow struct {
	Start int
	End   int
}

// planASRWindows 依据语音段规划识别窗口。
//
// 策略：
//  1. 过长的语音段先在其内部停顿处切小，实在没有停顿才按上限硬切；
//  2. 依次把语音段装入当前窗口，只有「静音足够长」且「加入后会超长」才开新窗口；
//  3. 完全没有语音（全静音或 VAD 不可用）时退化为均匀切分，保证仍会尝试转写。
func planASRWindows(total, sampleRate int, spans []speechSpan, samples []float32, opt chunkOptions) []asrWindow {
	if total <= 0 || sampleRate <= 0 {
		return nil
	}
	opt = opt.normalized()
	maxChunk := int(opt.MaxChunkSeconds * float64(sampleRate))
	if maxChunk <= 0 {
		maxChunk = total
	}
	minCut := int(opt.MinSilenceCutSeconds * float64(sampleRate))
	if minCut < 0 {
		minCut = 0
	}

	// 越界的语音段先裁剪到音频范围内，避免规划出超出音频的窗口。
	clamped := make([]speechSpan, 0, len(spans))
	for _, sp := range spans {
		if sp.Start < 0 {
			sp.Start = 0
		}
		if sp.End > total {
			sp.End = total
		}
		if sp.End > sp.Start {
			clamped = append(clamped, sp)
		}
	}
	if len(clamped) == 0 {
		return uniformWindows(total, maxChunk)
	}

	pieces := splitOversizedSpans(clamped, samples, sampleRate, maxChunk, minCut)
	if len(pieces) == 0 {
		return uniformWindows(total, maxChunk)
	}

	windows := make([]asrWindow, 0, len(pieces))
	current := pieces[0].speechSpan
	for _, piece := range pieces[1:] {
		if piece.End <= piece.Start {
			continue
		}
		// 只在「静音足够长」的间隙切窗；间隙太短说明是一句话内部的停顿，
		// 切开会把句子从中间截断，因此宁可让窗口略超上限。
		// 唯一的例外是被强制切分出来的片段（newWindow），它们必须各自成窗，
		// 否则长语音段会在切成小片后又被合并回一个超长窗口。
		if piece.newWindow || (piece.Start-current.End >= minCut && piece.End-current.Start > maxChunk) {
			windows = append(windows, asrWindow{Start: current.Start, End: current.End})
			current = piece.speechSpan
			continue
		}
		if piece.End > current.End {
			current.End = piece.End
		}
	}
	windows = append(windows, asrWindow{Start: current.Start, End: current.End})

	return clampWindows(windows, total)
}

// spanPiece 规划窗口用的语音片段。
//
// newWindow 为 true 表示该片段必须开启一个新的识别窗口：它来自对超长语音段的
// 强制切分，切断点未必是静音，因此不能再与前一个片段合并回同一个窗口。
type spanPiece struct {
	speechSpan
	newWindow bool
}

// uniformWindows 把音频均匀切成不超过 maxChunk 的窗口（无语音段信息时的退化方案）。
func uniformWindows(total, maxChunk int) []asrWindow {
	if total <= 0 {
		return nil
	}
	if maxChunk <= 0 {
		maxChunk = total
	}
	starts := chunkWindows(total, maxChunk, 0)
	windows := make([]asrWindow, 0, len(starts))
	for _, start := range starts {
		end := start + maxChunk
		if end > total {
			end = total
		}
		windows = append(windows, asrWindow{Start: start, End: end})
	}
	return clampWindows(windows, total)
}

// clampWindows 裁剪窗口到音频范围内，并合并重叠窗口保证互不重叠。
func clampWindows(windows []asrWindow, total int) []asrWindow {
	out := make([]asrWindow, 0, len(windows))
	for _, w := range windows {
		if w.Start < 0 {
			w.Start = 0
		}
		if w.End > total {
			w.End = total
		}
		if w.End <= w.Start {
			continue
		}
		if len(out) > 0 && w.Start < out[len(out)-1].End {
			if w.End > out[len(out)-1].End {
				out[len(out)-1].End = w.End
			}
			continue
		}
		out = append(out, w)
	}
	return out
}

// splitOversizedSpans 把超过 maxChunk 的语音段切小。
//
// 切割点优先选择段内真实存在的停顿（先找长停顿，再退而求其次找词间短停顿），
// 只有在整段都没有可辨识停顿（例如持续朗读）时才按上限硬切。
// 切分出来的第 2 段及以后都标记为「必须新开窗口」。
func splitOversizedSpans(spans []speechSpan, samples []float32, sampleRate, maxChunk, minCut int) []spanPiece {
	if maxChunk <= 0 {
		return toPieces(spans)
	}
	out := make([]spanPiece, 0, len(spans))
	for _, sp := range spans {
		out = append(out, splitOneSpan(sp, samples, sampleRate, maxChunk, minCut, 0)...)
	}
	return out
}

// toPieces 把语音段转换为片段（均不强制新开窗口）。
func toPieces(spans []speechSpan) []spanPiece {
	out := make([]spanPiece, 0, len(spans))
	for _, sp := range spans {
		out = append(out, spanPiece{speechSpan: sp})
	}
	return out
}

// forcedPieces 把切分结果标记为「必须新开窗口」（首段除外）。
func forcedPieces(spans []speechSpan) []spanPiece {
	out := make([]spanPiece, 0, len(spans))
	for i, sp := range spans {
		out = append(out, spanPiece{speechSpan: sp, newWindow: i > 0})
	}
	return out
}

// maxSplitDepth 切分递归深度上限，防止病态输入导致无限递归。
const maxSplitDepth = 8

// splitOneSpan 切分单个过长的语音段：优先在段内停顿处切，其次递归细分，
// 只有在完全没有可辨识停顿（例如持续朗读）时才按上限硬切。
func splitOneSpan(sp speechSpan, samples []float32, sampleRate, maxChunk, minCut, depth int) []spanPiece {
	if sp.End <= sp.Start {
		return nil
	}
	if sp.End-sp.Start <= maxChunk || depth >= maxSplitDepth {
		return []spanPiece{{speechSpan: sp}}
	}
	if sampleRate <= 0 || sp.Start < 0 || sp.End > len(samples) {
		return forcedPieces(hardSplitSpan(sp, maxChunk))
	}

	// minCut 为 0 时给一个保守默认值（400ms），确保降级尝试仍按“明显停顿”切分。
	longPause := minCut
	if longPause <= 0 {
		longPause = sampleRate * 2 / 5
	}
	for _, silenceSamples := range []int{longPause, sampleRate / 8} { // 先长停顿，再词间停顿(125ms)
		if silenceSamples <= 0 {
			continue
		}
		inner := detectEnergySpans(samples[sp.Start:sp.End], sampleRate, vadPostOptions{
			MinSilenceMs: samplesToMs(silenceSamples, sampleRate),
			MinSpeechMs:  1,
			PaddingMs:    0,
		})
		if len(inner) < 2 {
			continue
		}
		abs := make([]speechSpan, len(inner))
		for i, s := range inner {
			abs[i] = speechSpan{Start: sp.Start + s.Start, End: sp.Start + s.End}
		}
		if packed := packSpans(abs, samples, sampleRate, maxChunk, minCut, depth+1); len(packed) > 1 {
			return packed
		}
	}
	return forcedPieces(hardSplitSpan(sp, maxChunk))
}

// packSpans 贪心装箱：把相邻的小语音段合成尽量接近但不超过 maxChunk 的片段，
// 单个仍然超长的片段递归切分。第 2 片及以后都标记为「必须新开窗口」。
func packSpans(spans []speechSpan, samples []float32, sampleRate, maxChunk, minCut, depth int) []spanPiece {
	var out []spanPiece
	var current speechSpan
	has := false
	flush := func() {
		if has {
			out = append(out, spanPiece{speechSpan: current})
			has = false
		}
	}

	for _, sp := range spans {
		if sp.End <= sp.Start {
			continue
		}
		if sp.End-sp.Start > maxChunk {
			flush()
			out = append(out, splitOneSpan(sp, samples, sampleRate, maxChunk, minCut, depth)...)
			continue
		}
		if !has {
			current, has = sp, true
			continue
		}
		if sp.End-current.Start > maxChunk {
			out = append(out, spanPiece{speechSpan: current})
			current = sp
			continue
		}
		if sp.End > current.End {
			current.End = sp.End
		}
	}
	flush()

	for i := 1; i < len(out); i++ {
		out[i].newWindow = true
	}
	return out
}

// hardSplitSpan 按固定长度等分语音段（兜底方案，可能在词中间切开）。
func hardSplitSpan(sp speechSpan, maxChunk int) []speechSpan {
	if sp.End <= sp.Start {
		return nil
	}
	if maxChunk <= 0 {
		return []speechSpan{sp}
	}
	out := make([]speechSpan, 0, (sp.End-sp.Start)/maxChunk+1)
	for start := sp.Start; start < sp.End; start += maxChunk {
		end := start + maxChunk
		if end > sp.End {
			end = sp.End
		}
		out = append(out, speechSpan{Start: start, End: end})
	}
	return out
}
