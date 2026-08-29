package asr

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"github.com/stretchr/testify/require"
)

// TestSynthOnsetSemantics 用「精确时间已知」的合成音频锁定模型 token 时间戳的语义：
// 它到底是字的【起始】时刻还是【结束】时刻。
//
// 构造方式：从 李大爷.wav 中切出边界已知（20ms 能量包络人工确认）的三个汉字音节，
// 首尾相接拼成连续语音，得到每个字真实 [start,end] 已知的合成 wav。
//
// 结论（transcript.WordsFromCharTimesIntervals 的时间回溯逻辑依赖此结论）：
// 模型时间戳约等于该字发音的【结束】时刻。若把它当作起始时刻，每个字的时间
// 都会整体推后约一个字——点击第 i 个字播放到的是第 i+1 个字的音频。
//
// 运行：cd koi-server && go test ./tests/asr/ -run TestSynthOnsetSemantics -v
func TestSynthOnsetSemantics(t *testing.T) {
	if os.Getenv("SKIP_ASR_TESTS") == "1" {
		t.Skip("SKIP_ASR_TESTS=1，跳过 ASR 解码测试")
	}
	requireProjectRoot(t)

	wave := sherpa.ReadWave("李大爷.wav")
	require.NotNil(t, wave, "李大爷.wav 应位于项目根目录")
	sr := wave.SampleRate

	cut := func(from, to float64) []float32 {
		a, b := int(from*float64(sr)), int(to*float64(sr))
		return wave.Samples[a:b]
	}
	// 三个连续音节，拼接后真实区间分别为 [2.00,2.38] [2.38,2.76] [2.76,3.06]。
	var pcm []float32
	pcm = append(pcm, make([]float32, 2*sr)...)
	truths := [][2]float64{}
	for _, seg := range [][]float32{cut(0.93, 1.31), cut(1.37, 1.75), cut(1.77, 2.07)} {
		truths = append(truths, [2]float64{float64(len(pcm)) / float64(sr), (float64(len(pcm)) + float64(len(seg))) / float64(sr)})
		pcm = append(pcm, seg...)
	}
	pcm = append(pcm, make([]float32, 2*sr)...)

	out := filepath.Join(t.TempDir(), "synth.wav")
	require.NoError(t, writeWav(out, pcm, sr))

	rec := newBilingualRecognizer(t)
	defer sherpa.DeleteOnlineRecognizer(rec)

	w2 := sherpa.ReadWave(out)
	require.NotNil(t, w2)
	s := sherpa.NewOnlineStream(rec)
	s.AcceptWaveform(w2.SampleRate, w2.Samples)
	s.InputFinished()
	for rec.IsReady(s) {
		rec.Decode(s)
	}
	r := rec.GetResult(s)
	sherpa.DeleteOnlineStream(s)

	require.Equal(t, "一二三", r.Text, "合成音频应被识别为 一二三")
	require.Len(t, r.Timestamps, len(truths))

	fmt.Printf("%-6s %-12s %-20s %-12s %-12s\n", "字", "模型时间戳", "真实发音区间", "距起点", "距终点")
	for i, tr := range truths {
		ts := float64(r.Timestamps[i])
		fmt.Printf("%-6q %-12.3f [%6.3f, %6.3f]     %-12.3f %-12.3f\n",
			r.Tokens[i], ts, tr[0], tr[1], ts-tr[0], ts-tr[1])
		// 时间戳应显著更接近字发音的结束时刻，而非起始时刻。
		require.Less(t, abs(ts-tr[1]), 0.2, "模型时间戳应落在字 %q 发音结束时刻附近", r.Tokens[i])
		require.Less(t, abs(ts-tr[1]), abs(ts-tr[0]),
			"模型时间戳应更接近字 %q 的发音结束时刻而非起始时刻", r.Tokens[i])
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func writeWav(path string, samples []float32, sampleRate int) error {
	var data []byte
	for _, v := range samples {
		i := int32(v * 32767)
		if i > 32767 {
			i = 32767
		}
		if i < -32768 {
			i = -32768
		}
		data = append(data, byte(i), byte(i>>8))
	}
	buf := make([]byte, 0, 44+len(data))
	put := func(s string) { buf = append(buf, []byte(s)...) }
	put32 := func(v uint32) {
		buf = append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
	}
	put16 := func(v uint16) { buf = append(buf, byte(v), byte(v>>8)) }
	put("RIFF")
	put32(uint32(36 + len(data)))
	put("WAVEfmt ")
	put32(16)
	put16(1)
	put16(1)
	put32(uint32(sampleRate))
	put32(uint32(sampleRate * 2))
	put16(2)
	put16(16)
	put("data")
	put32(uint32(len(data)))
	buf = append(buf, data...)
	return os.WriteFile(path, buf, 0o644)
}
