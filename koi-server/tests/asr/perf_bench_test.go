package asr

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"github.com/stretchr/testify/require"
)

// numCPU 返回可用 CPU 核数，用于并发度参考。
func numCPU() int { return runtime.NumCPU() }

// requireProjectRootB 与 requireProjectRoot 相同，但适用于 *testing.B。
func requireProjectRootB(tb testing.TB) {
	tb.Helper()
	dir, err := os.Getwd()
	require.NoError(tb, err)
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, bilingualModelDir)); err == nil {
				require.NoError(tb, os.Chdir(dir))
				return
			}
		}
		dir = filepath.Dir(dir)
	}
	tb.Skipf("未在项目根目录找到模型 %s，跳过基准测试", bilingualModelDir)
}

// 本文件是离线转写性能基准，用于评估「串行单窗口」与「多窗口并行(DecodeStreams)」
// 在不同线程数 / 量化模型下的实际吞吐，为 offline_transcribe 的并发参数提供依据。
//
// 运行方式（需完整模型文件）：
//
//	cd koi-server && go test ./tests/asr/ -run '^$' -bench BenchmarkOfflinePerf -benchtime 1x -v
//
// 可用环境变量：
//
//	PERF_WAV     基准音频（默认 李大爷.wav）
//	PERF_ENCODER 编码器文件名（默认 encoder-epoch-99-avg-1.onnx，可换 int8 版本）
//	PERF_THREADS 每个识别器的 intra-op 线程数（默认 4）

func perfEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func perfIntEnv(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// perfModelFiles 返回要使用的模型文件（PERF_INT8=1 时使用量化版本）。
func perfModelFiles() (encoder, decoder, joiner string) {
	if perfEnv("PERF_INT8", "") == "1" {
		return "encoder-epoch-99-avg-1.int8.onnx",
			"decoder-epoch-99-avg-1.int8.onnx",
			"joiner-epoch-99-avg-1.int8.onnx"
	}
	return perfEnv("PERF_ENCODER", bilingualEncoder), bilingualDecoder, bilingualJoiner
}

// perfRecognizer 构造指定线程数与模型的识别器。
func perfRecognizer(tb testing.TB, numThreads int, encoder, decoder, joiner string) *sherpa.OnlineRecognizer {
	tb.Helper()
	cfg := sherpa.OnlineRecognizerConfig{
		FeatConfig: sherpa.FeatureConfig{SampleRate: 16000, FeatureDim: bilingualFeatureDim},
		ModelConfig: sherpa.OnlineModelConfig{
			Transducer: sherpa.OnlineTransducerModelConfig{
				Encoder: filepath.Join(bilingualModelDir, encoder),
				Decoder: filepath.Join(bilingualModelDir, decoder),
				Joiner:  filepath.Join(bilingualModelDir, joiner),
			},
			Tokens:       filepath.Join(bilingualModelDir, bilingualTokens),
			NumThreads:   numThreads,
			ModelingUnit: "bpe",
			BpeVocab:     filepath.Join(bilingualModelDir, bilingualBpeVocab),
		},
		DecodingMethod: "greedy_search",
		MaxActivePaths: 4,
	}
	rec := sherpa.NewOnlineRecognizer(&cfg)
	require.NotNil(tb, rec, "识别器构造失败：请确认模型文件完整（%s）", encoder)
	return rec
}

// perfDecodeSerial 串行解码各窗口（等价于当前线上实现），返回耗时与各窗口文本。
func perfDecodeSerial(rec *sherpa.OnlineRecognizer, windows [][]float32) (time.Duration, []string) {
	start := time.Now()
	texts := make([]string, len(windows))
	for i, win := range windows {
		stream := sherpa.NewOnlineStream(rec)
		stream.AcceptWaveform(16000, win)
		stream.InputFinished()
		for rec.IsReady(stream) {
			rec.Decode(stream)
		}
		if r := rec.GetResult(stream); r != nil {
			texts[i] = r.Text
		}
		sherpa.DeleteOnlineStream(stream)
	}
	return time.Since(start), texts
}

// perfDecodeParallel 用官方 DecodeStreams（内部 OpenMP 并行）批量解码各窗口。
func perfDecodeParallel(rec *sherpa.OnlineRecognizer, windows [][]float32) time.Duration {
	start := time.Now()
	streams := make([]*sherpa.OnlineStream, 0, len(windows))
	for _, win := range windows {
		s := sherpa.NewOnlineStream(rec)
		s.AcceptWaveform(16000, win)
		s.InputFinished()
		streams = append(streams, s)
	}
	for {
		var ready []*sherpa.OnlineStream
		for _, s := range streams {
			if rec.IsReady(s) {
				ready = append(ready, s)
			}
		}
		if len(ready) == 0 {
			break
		}
		rec.DecodeStreams(ready)
	}
	for _, s := range streams {
		_ = rec.GetResult(s)
		sherpa.DeleteOnlineStream(s)
	}
	return time.Since(start)
}

// perfSplit 把采样均分为 n 段（模拟 n 个识别窗口）。
func perfSplit(samples []float32, n int) [][]float32 {
	if n <= 1 || len(samples) < n {
		return [][]float32{samples}
	}
	out := make([][]float32, 0, n)
	step := len(samples) / n
	for i := 0; i < n; i++ {
		end := (i + 1) * step
		if i == n-1 {
			end = len(samples)
		}
		out = append(out, samples[i*step:end])
	}
	return out
}

func BenchmarkOfflinePerf(b *testing.B) {
	requireProjectRootB(b)
	wav := perfEnv("PERF_WAV", "李大爷.wav")
	wave := sherpa.ReadWave(wav)
	require.NotNil(b, wave, "读取音频失败: %s", wav)
	audioSec := float64(len(wave.Samples)) / float64(wave.SampleRate)

	encoder, decoder, joiner := perfModelFiles()
	threads := perfIntEnv("PERF_THREADS", 4)

	b.Logf("音频 %s: %.1fs, 编码器 %s, threads=%d, cpu=%d", wav, audioSec, encoder, threads, numCPU())

	type variant struct {
		name    string
		windows int
		serial  bool
	}
	variants := []variant{
		{"serial-1", 1, true},
		{"serial-2", 2, true},
		{"serial-4", 4, true},
		{"serial-8", 8, true},
		{"parallel-2", 2, false},
		{"parallel-4", 4, false},
		{"parallel-8", 8, false},
	}
	// PERF_VARIANTS 支持只跑部分场景（逗号分隔，例如 "serial-1,parallel-8"），便于快速对比参数。
	if filter := perfEnv("PERF_VARIANTS", ""); filter != "" {
		want := map[string]bool{}
		for _, name := range strings.Split(filter, ",") {
			want[strings.TrimSpace(name)] = true
		}
		kept := variants[:0]
		for _, v := range variants {
			if want[v.name] {
				kept = append(kept, v)
			}
		}
		variants = kept
		require.NotEmpty(b, variants, "PERF_VARIANTS 未匹配到任何场景: %s", filter)
	}

	for _, v := range variants {
		v := v
		b.Run(v.name, func(b *testing.B) {
			rec := perfRecognizer(b, threads, encoder, decoder, joiner)
			defer sherpa.DeleteOnlineRecognizer(rec)
			wins := perfSplit(wave.Samples, v.windows)
			// 预热一次，避免把首次运行的缓存效应算进结果。
			if v.serial {
				_, _ = perfDecodeSerial(rec, wins)
			} else {
				perfDecodeParallel(rec, wins)
			}
			b.ResetTimer()
			var d time.Duration
			for i := 0; i < b.N; i++ {
				if v.serial {
					d, _ = perfDecodeSerial(rec, wins)
				} else {
					d = perfDecodeParallel(rec, wins)
				}
			}
			b.ReportMetric(float64(d.Milliseconds()), "ms")
			b.ReportMetric(audioSec/float64(d.Seconds()), "x-realtime")
			b.Logf("%s: %.1fs 音频耗时 %.0fms (RTF %.2f)",
				v.name, audioSec, float64(d.Milliseconds()), float64(d.Seconds())/audioSec)
		})
	}
}
