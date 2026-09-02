package offlinetranscribe

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

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
