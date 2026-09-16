package offlinetranscribe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/log"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"github.com/stretchr/testify/require"
)

// TestRetranscribeConcurrentWithDecode
//
// 回归测试：修复“前端调用重新转写导致后端崩溃”。
//
// 崩溃根因：离线转写服务是进程级单例，识别器指针被实时转写（按句并发解码）与
// 离线/重新转写共享。重新转写的 doTranscribe 在步骤 2 调用 applyHotwords，它会
// DeleteOnlineRecognizer 并重建识别器；而 decodeChunk 此前在释放读锁后才使用
// s.recognizer，存在窗口：applyHotwords 的写锁删除旧识别器后，decodeChunk 仍持有旧
// 指针继续 Decode → use-after-free → C 库 SIGSEGV → 后端进程崩溃。
//
// 修复：decodeChunk 在解码全过程中持有读锁，applyHotwords/preloadModel 的写锁必须等待
// 当前解码结束后再替换识别器。
//
// 本测试在加载真实模型后并发执行：
//   - 多个 goroutine 持续调用 decodeChunk（模拟实时按句解码）
//   - 一个 goroutine 不断调用 applyHotwords（触发识别器重建，模拟重新转写）
//
// 若修复失效，会在运行期触发 SIGSEGV 直接杀死测试进程；通过则说明不再存在 use-after-free。
//
// 该测试需要真实模型与 dylib，默认跳过；用 OFFLINE_RETRANSCRIBE_STRESS=1 启用。
func TestRetranscribeConcurrentWithDecode(t *testing.T) {
	if os.Getenv("OFFLINE_RETRANSCRIBE_STRESS") != "1" {
		t.Skip("OFFLINE_RETRANSCRIBE_STRESS=1 时启用（需加载真实 ASR 模型）")
	}
	requireProjectRootForModel(t)

	svc := &Service{cfg: Config{}.normalized(), deps: Dependencies{Log: &noopLog{}}}
	svc.preloadModel()
	svc.mu.RLock()
	loaded := svc.loaded && svc.recognizer != nil
	loadErr := svc.loadErr
	svc.mu.RUnlock()
	require.NoError(t, loadErr)
	require.True(t, loaded, "模型应加载成功")

	// 准备一段测试音频（借用 bilingual 模型自带的测试 wav）。
	wavPath := filepath.Join(
		"models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/test_wavs/0.wav")
	require.FileExists(t, wavPath)

	wave := sherpa.ReadWave(wavPath)
	require.NotNil(t, wave)

	const decoders = 4
	var wg sync.WaitGroup

	// 模拟实时按句并发解码
	for i := 0; i < decoders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				// 任何错误（如模型未加载）都不应导致进程崩溃。
				_, _, _ = svc.decodeChunk(wave.Samples, wave.SampleRate)
			}
		}()
	}

	// 模拟重新转写：不断重建识别器（applyHotwords 内部 Delete+New）。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 30; j++ {
			_ = svc.applyHotwords("")
		}
	}()

	wg.Wait()

	// 收尾：确认服务仍可正常转写，识别器指针有效。
	text, _, err := svc.decodeChunk(wave.Samples, wave.SampleRate)
	require.NoError(t, err)
	require.NotEmpty(t, text, "收尾转写应得到非空结果")
}

func requireProjectRootForModel(t *testing.T) {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20")); err == nil {
				require.NoError(t, os.Chdir(dir))
				return
			}
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("未在项目根目录找到模型，跳过")
}

// TestApplyHotwordsReusesRecognizer
//
// 性能回归测试：热词未变化时必须复用当前识别器。
//
// doTranscribe 每次转写都会调用 applyHotwords，而重建识别器要重新加载
// encoder/decoder/joiner 三个 onnx（本项目模型约 4~5 秒）。若每次都重建，
// 「离线转写 / 重新转写」会白付这笔固定开销，也是此前重新转写偏慢的主因之一。
func TestApplyHotwordsReusesRecognizer(t *testing.T) {
	requireProjectRootForModel(t)

	svc := &Service{cfg: Config{}.normalized(), deps: Dependencies{Log: &noopLog{}}}
	svc.preloadModel()

	svc.mu.RLock()
	base := svc.recognizer
	svc.mu.RUnlock()
	require.NotNil(t, base, "模型应加载成功")

	// 热词变化：必须重建（否则热词不生效）。
	require.NoError(t, svc.applyHotwords("会议\nMONDAY"))
	svc.mu.RLock()
	first := svc.recognizer
	svc.mu.RUnlock()
	require.NotSame(t, base, first, "热词变化时应重建识别器")

	// 热词不变：复用识别器，且必须远快于一次模型加载。
	start := time.Now()
	require.NoError(t, svc.applyHotwords("会议\nMONDAY"))
	elapsed := time.Since(start)
	svc.mu.RLock()
	second := svc.recognizer
	svc.mu.RUnlock()
	require.Same(t, first, second, "热词未变化时应复用同一识别器")
	require.Less(t, elapsed, 100*time.Millisecond,
		"复用识别器不应重新加载模型（耗时 %s）", elapsed)

	// 热词再次变化：仍需重建。
	require.NoError(t, svc.applyHotwords("会议"))
	svc.mu.RLock()
	third := svc.recognizer
	svc.mu.RUnlock()
	require.NotSame(t, second, third, "热词变化时应重建识别器")
}

// TestDecodeWindowsParallelMatchesSerial
//
// 正确性测试：并行批量解码（DecodeStreams）与逐窗口串行解码结果必须一致。
// 并行只是把互不相关的识别窗口同时推进，不能改变每个窗口的解码结果。
func TestDecodeWindowsParallelMatchesSerial(t *testing.T) {
	requireProjectRootForModel(t)

	svc := &Service{
		cfg:  Config{MaxConcurrency: 4}.normalized(),
		deps: Dependencies{Log: &noopLog{}, Progress: NewProgressManager()},
	}
	svc.preloadModel()
	svc.mu.RLock()
	loaded := svc.loaded && svc.recognizer != nil
	svc.mu.RUnlock()
	require.True(t, loaded, "模型应加载成功")

	wavPath := filepath.Join(
		"models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/test_wavs/0.wav")
	require.FileExists(t, wavPath)
	wave := sherpa.ReadWave(wavPath)
	require.NotNil(t, wave)

	// 把音频切成若干段，模拟 VAD 规划出来的识别窗口。
	const n = 6
	step := len(wave.Samples) / n
	require.Greater(t, step, 0)
	windows := make([]asrWindow, 0, n)
	for i := 0; i < n; i++ {
		end := (i + 1) * step
		if i == n-1 {
			end = len(wave.Samples)
		}
		windows = append(windows, asrWindow{Start: i * step, End: end})
	}

	// 基准：逐窗口串行解码（等价于改动前的实现）。
	wantText := make([]string, len(windows))
	for i, win := range windows {
		text, _, err := svc.decodeChunk(wave.Samples[win.Start:win.End], wave.SampleRate)
		require.NoError(t, err, "窗口 %d 串行解码失败", i)
		wantText[i] = text
	}
	require.NotEmpty(t, strings.Join(wantText, ""), "测试音频应能转写出文本")

	for _, concurrency := range []int{1, 2, 4, 8} {
		svc.cfg.MaxConcurrency = concurrency
		got := svc.decodeWindows(wave.Samples, wave.SampleRate, windows, 1)
		require.Len(t, got, len(windows))
		for i := range windows {
			require.NoError(t, got[i].err, "并发度 %d：窗口 %d 解码失败", concurrency, i)
			require.Equal(t, wantText[i], got[i].text,
				"并发度 %d：窗口 %d 的并行解码结果应与串行一致", concurrency, i)
		}
	}
}

// noopLog 是离线转写 Dependencies.Log 的最小实现，避免引入框架日志依赖。
// 同时实现 contracts/log.Log 与内嵌的 Writer 接口。
type noopLog struct{}

func (noopLog) WithContext(ctx context.Context) log.Log      { return noopLog{} }
func (noopLog) Channel(channel string) log.Log               { return noopLog{} }
func (noopLog) Stack(channels []string) log.Log              { return noopLog{} }
func (noopLog) Debug(args ...any)                            {}
func (noopLog) Debugf(format string, args ...any)            {}
func (noopLog) Info(args ...any)                             {}
func (noopLog) Infof(format string, args ...any)             {}
func (noopLog) Warning(args ...any)                          {}
func (noopLog) Warningf(format string, args ...any)          {}
func (noopLog) Error(args ...any)                            {}
func (noopLog) Errorf(format string, args ...any)            {}
func (noopLog) Fatal(args ...any)                            {}
func (noopLog) Fatalf(format string, args ...any)            {}
func (noopLog) Panic(args ...any)                            {}
func (noopLog) Panicf(format string, args ...any)            {}
func (noopLog) Code(code string) log.Writer                  { return noopLog{} }
func (noopLog) Hint(hint string) log.Writer                  { return noopLog{} }
func (noopLog) In(domain string) log.Writer                  { return noopLog{} }
func (noopLog) Owner(owner any) log.Writer                   { return noopLog{} }
func (noopLog) Request(req http.ContextRequest) log.Writer   { return noopLog{} }
func (noopLog) Response(res http.ContextResponse) log.Writer { return noopLog{} }
func (noopLog) Tags(tags ...string) log.Writer               { return noopLog{} }
func (noopLog) User(user any) log.Writer                     { return noopLog{} }
func (noopLog) With(data map[string]any) log.Writer          { return noopLog{} }
func (noopLog) WithTrace() log.Writer                        { return noopLog{} }
