package asr

import (
	"fmt"
	"math"
	"testing"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"github.com/stretchr/testify/require"

	"koi-server/app/services/transcript"
)

// TestVerifyOnset 对照真实音频的能量包络，验证修复后每个字的时间区间是否
// 覆盖该字的真实发音。李大爷.wav 前几个字边界清晰（音节之间有静音）：
//
//	一 [0.93, 1.30]  二 [1.38, 1.73]  三 [1.78, 2.06]
func TestVerifyOnset(t *testing.T) {
	requireProjectRoot(t)
	rec := newBilingualRecognizer(t)
	defer sherpa.DeleteOnlineRecognizer(rec)

	wave := sherpa.ReadWave("李大爷.wav")
	require.NotNil(t, wave)
	sr := wave.SampleRate

	s := sherpa.NewOnlineStream(rec)
	s.AcceptWaveform(sr, wave.Samples)
	s.InputFinished()
	for rec.IsReady(s) {
		rec.Decode(s)
	}
	r := rec.GetResult(s)
	sherpa.DeleteOnlineStream(s)

	tt := make([]transcript.TokenTimestamp, len(r.Tokens))
	for i := range r.Tokens {
		tt[i] = transcript.TokenTimestamp{Token: r.Tokens[i], TimeSec: r.Timestamps[i]}
	}
	charTimes, ok := transcript.AlignCharTimes(r.Text, tt)
	require.True(t, ok)
	words := transcript.WordsWithSpansFromCharTimes(r.Text, charTimes)
	require.NotEmpty(t, words)

	// 李大爷.wav 开头「一二三四五六」的真实发音区间（由 20ms 能量包络人工确认）。
	truth := [][2]float64{
		{0.93, 1.30}, {1.38, 1.73}, {1.78, 2.06},
		{2.24, 2.47}, {2.48, 2.85}, {2.86, 3.10},
	}
	require.GreaterOrEqual(t, len(words), len(truth))

	fmt.Printf("%-6s %-22s %-22s %-10s\n", "字", "字时间区间(s)", "真实发音区间(s)", "起点误差")
	for i, tr := range truth {
		w := words[i]
		got := [2]float64{float64(w.StartMs) / 1000, float64(w.EndMs) / 1000}
		fmt.Printf("%-6q [%6.3f, %6.3f]       [%6.3f, %6.3f]       %+6.3f\n",
			w.Word, got[0], got[1], tr[0], tr[1], got[0]-tr[0])
		// 区间必须与真实发音重叠：起点不得晚于发音结束、终点不得早于发音开始。
		require.Less(t, got[0], tr[1], "字 %q 的起点不得晚于其真实发音结束", w.Word)
		require.Greater(t, got[1], tr[0], "字 %q 的终点不得早于其真实发音开始", w.Word)
		// 回归：若把模型时间戳误当作起始时刻，每个字的起点会晚约一个字（>0.25s）。
		require.Less(t, math.Abs(got[0]-tr[0]), 0.25,
			"字 %q 起点与真实发音相差过大（时间戳是否被当作起始时刻？）", w.Word)
	}

	// 能量包络对照：每个字的时间区间内平均能量应显著高于静音底噪。
	rms := func(a, b int) float64 {
		if a < 0 {
			a = 0
		}
		if b > len(wave.Samples) {
			b = len(wave.Samples)
		}
		if b <= a {
			return 0
		}
		var sum float64
		for _, v := range wave.Samples[a:b] {
			sum += float64(v) * float64(v)
		}
		return math.Sqrt(sum / float64(b-a))
	}
	fmt.Println("\n区间内平均能量（应显著高于静音底噪 ~0.005）：")
	for i := range truth {
		w := words[i]
		a := int(float64(w.StartMs) / 1000 * float64(sr))
		b := int(float64(w.EndMs) / 1000 * float64(sr))
		fmt.Printf("  %q [%6.3f, %6.3f] rms=%.4f\n", w.Word,
			float64(w.StartMs)/1000, float64(w.EndMs)/1000, rms(a, b))
		require.Greater(t, rms(a, b), 0.02, "字 %q 的区间应落在语音上", w.Word)
	}
}
