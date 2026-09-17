package audio

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/contracts/log"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	contracts "koi-server/app/contracts/audio"
	"koi-server/app/models"
	"koi-server/app/services/transcript"
)

// 本文件是实时转写时间轴的端到端回归测试：
// 以「客户端 32ms PCM 帧 → 会话工作协程 → 断句提交 → 发布结果」的真实链路
// 喂入一段已知内容的音频，并与「整段喂入同一条识别流」的参考结果逐字比对。
//
// 覆盖的历史问题（均已实测复现）：
//  1. 每句 Reset 识别流会让时间戳整体偏晚 0.1~0.2s（模型 token 时间戳原点
//     落在上一句尾部残留音频上），并使首字被压成零宽区间；
//  2. 首字零宽区间在前端无法高亮「正在播放」，且会与后一个字的高亮区间重叠；
//  3. 会话结束时最后一句（尚未达到断句条件）既不落库也不下发，直接丢失。
//
// 运行：cd koi-server && go test ./app/services/audio/ -run TestRealtimeStream -v
// 无模型环境下自动跳过（也可用 SKIP_ASR_TESTS=1 强制跳过）。

// nopLog 满足 log.Log 契约的空实现（测试路径中不会真正写日志）。
type nopLog struct{}

func (nopLog) WithContext(context.Context) log.Log { return nopLog{} }
func (nopLog) Channel(string) log.Log              { return nopLog{} }
func (nopLog) Stack([]string) log.Log              { return nopLog{} }
func (nopLog) Debug(...any)                        {}
func (nopLog) Debugf(string, ...any)               {}
func (nopLog) Info(...any)                         {}
func (nopLog) Infof(string, ...any)                {}
func (nopLog) Warning(...any)                      {}
func (nopLog) Warningf(string, ...any)             {}
func (nopLog) Error(...any)                        {}
func (nopLog) Errorf(string, ...any)               {}
func (nopLog) Fatal(...any)                        {}
func (nopLog) Fatalf(string, ...any)               {}
func (nopLog) Panic(...any)                        {}
func (nopLog) Panicf(string, ...any)               {}
func (nopLog) Code(string) log.Writer              { return nopLog{} }
func (nopLog) Hint(string) log.Writer              { return nopLog{} }
func (nopLog) In(string) log.Writer                { return nopLog{} }
func (nopLog) Owner(any) log.Writer                { return nopLog{} }
func (nopLog) Request(http.ContextRequest) log.Writer {
	return nopLog{}
}
func (nopLog) Response(http.ContextResponse) log.Writer {
	return nopLog{}
}
func (nopLog) Tags(...string) log.Writer { return nopLog{} }
func (nopLog) User(any) log.Writer       { return nopLog{} }
func (nopLog) With(map[string]any) log.Writer {
	return nopLog{}
}
func (nopLog) WithTrace() log.Writer { return nopLog{} }

// recordingPublisher 记录转写服务发布的结果（替身）。
type recordingPublisher struct {
	mu      sync.Mutex
	results []contracts.Result
}

func (p *recordingPublisher) Publish(r contracts.Result) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.results = append(p.results, r)
}

// finals 返回全部最终（定稿）结果。
func (p *recordingPublisher) finals() []contracts.Result {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]contracts.Result, 0, len(p.results))
	for _, r := range p.results {
		if r.IsFinal {
			out = append(out, r)
		}
	}
	return out
}

// asrModelDir 实时转写使用的模型目录（相对项目根）。
const asrModelDir = "models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20"

// projectRoot 向上查找项目根（含 go.mod 与模型目录），并把工作目录切过去。
func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, asrModelDir)); err == nil {
				require.NoError(t, os.Chdir(dir))
				return dir
			}
		}
		dir = filepath.Dir(dir)
	}
	t.Skipf("未找到模型目录 %s，跳过实时转写回归测试", asrModelDir)
	return ""
}

// TestRealtimeStreamTimestampsMatchOffline 锁定实时转写的逐字时间戳：
// 与「同一段音频整段喂入」的参考结果逐字一致（偏差 0ms），且每个字都是
// 长度 > 0、互不重叠的区间；会话结束时尚未断句的最后一句也必须被提交。
func TestRealtimeStreamTimestampsMatchOffline(t *testing.T) {
	if os.Getenv("SKIP_ASR_TESTS") == "1" {
		t.Skip("SKIP_ASR_TESTS=1，跳过 ASR 解码测试")
	}
	projectRoot(t)

	wave := sherpa.ReadWave("李大爷.wav")
	require.NotNil(t, wave, "李大爷.wav 应位于项目根目录")
	require.Equal(t, 16000, wave.SampleRate)
	sr := wave.SampleRate

	// 只喂到最后一个字结束之后（不含文件末尾 4 秒静音）：
	// 这样最后一句不会因尾部静音被断句，从而校验收尾提交逻辑。
	feed := wave.Samples[:int(34.95*float64(sr))]

	// ---- 服务与识别器（配置与线上完全一致） ----
	cfg := Config{}.normalized()
	pub := &recordingPublisher{}
	svc := &Service{
		cfg:      cfg,
		deps:     Dependencies{Log: nopLog{}, Publisher: pub},
		sessions: map[string]*session{},
	}
	svc.baseConfig = svc.buildRecognizerConfig()
	svc.shared = sherpa.NewOnlineRecognizer(&svc.baseConfig)
	require.NotNil(t, svc.shared, "识别器创建失败，请检查模型文件是否完整")
	svc.loaded = true

	// ---- 参考：同一识别器整段喂入 ----
	refStream := sherpa.NewOnlineStream(svc.shared)
	refStream.AcceptWaveform(sr, feed)
	refStream.InputFinished()
	for svc.shared.IsReady(refStream) {
		svc.shared.Decode(refStream)
	}
	refResult := svc.shared.GetResult(refStream)
	sherpa.DeleteOnlineStream(refStream)
	require.NotEmpty(t, refResult.Text, "参考转写结果不应为空")

	refTimes := alignRefCharTimes(t, refResult)

	// ---- 实时：按客户端节奏（512 采样点 / 帧）喂入 ----
	sess := newSession("test-client", cfg.QueueSize, svc.shared, sherpa.NewOnlineStream(svc.shared), nil, "", sr)
	svc.sessions[sess.clientID] = sess
	go svc.work(sess)

	pcm := samplesToPCM(feed)
	const frameBytes = 512 * 2 // 512 采样点 = 1024 字节
	for off := 0; off < len(pcm); off += frameBytes {
		end := off + frameBytes
		if end > len(pcm) {
			end = len(pcm)
		}
		sess.touch()
		sess.chunks <- chunk{data: append([]byte(nil), pcm[off:end]...)}
	}
	// 结束帧：客户端点「结束转写」时发送的最后一帧。
	sess.chunks <- chunk{last: true}

	select {
	case <-sess.done:
	case <-time.After(120 * time.Second):
		t.Fatal("会话未在预期时间内收尾")
	}

	// ---- 校验 ----
	finals := pub.finals()
	require.NotEmpty(t, finals, "至少应发布一句最终结果")

	var (
		gotText strings.Builder
		words   []models.WordTimestamp
	)
	for _, r := range finals {
		gotText.WriteString(r.Text)
		words = append(words, parseWordTimestamps(t, r.WordTimestamps)...)
	}
	require.Equal(t, refResult.Text, gotText.String(),
		"实时链路拼接文本应与整段解码一致（含最后一句，不能丢尾句）")

	// 逐字比对：中文逐字，因此 word 序列与参考 rune 序列一一对应。
	refRunes := []rune(refResult.Text)
	require.Equal(t, len(refRunes), len(words), "逐字结果数量应与参考文本一致")

	refIdx := 0
	prevEnd := int64(-1)
	var maxDelta int64
	for i, w := range words {
		require.NotEmpty(t, w.Word)
		// 区间必须有效：长度 > 0，否则前端无法高亮该字（历史 bug：首字零宽）。
		assert.Greater(t, w.EndMs, w.StartMs, "第 %d 个字 %q 的区间长度必须 > 0", i, w.Word)
		// 相邻字不得重叠。
		if prevEnd >= 0 {
			assert.GreaterOrEqual(t, w.StartMs, prevEnd, "第 %d 个字 %q 与前一个字重叠", i, w.Word)
		}
		prevEnd = w.EndMs

		// 逐字核对字的结束时刻（模型时间戳语义为字发音结束时刻）。
		for _, r := range w.Word {
			require.Less(t, refIdx, len(refRunes), "实时结果比参考文本多出字符 %q", string(r))
			require.Equal(t, string(refRunes[refIdx]), string(r),
				"第 %d 个字的字符与参考文本不一致", i)
			refIdx++
		}
		refMs := int64(refTimes[refIdx-1] * 1000)
		delta := w.EndMs - refMs
		if delta < 0 {
			delta = -delta
		}
		if delta > maxDelta {
			maxDelta = delta
		}
		assert.LessOrEqual(t, delta, int64(1),
			"字 %q 的结束时刻 %dms 与整段解码参考 %dms 偏差过大", w.Word, w.EndMs, refMs)
	}
	assert.Equal(t, len(refRunes), refIdx, "实时结果与参考文本的字符数应一致")
	t.Logf("实时链路 %d 句 / %d 字，逐字时间戳与整段解码参考的最大偏差 %dms",
		len(finals), len(words), maxDelta)
}

// alignRefCharTimes 把参考结果的 token 时间戳展开为逐字时间（秒）。
func alignRefCharTimes(t *testing.T, result *sherpa.OnlineRecognizerResult) []float32 {
	t.Helper()
	require.Len(t, result.Timestamps, len(result.Tokens), "tokens 与 timestamps 应等长")
	tts := make([]transcript.TokenTimestamp, len(result.Tokens))
	for i := range result.Tokens {
		tts[i] = transcript.TokenTimestamp{Token: result.Tokens[i], TimeSec: result.Timestamps[i]}
	}
	charTimes, ok := transcript.AlignCharTimes(result.Text, tts)
	require.True(t, ok, "参考结果的 token 时间戳应可与文本对齐")
	require.Len(t, charTimes, len([]rune(result.Text)))
	return charTimes
}

// parseWordTimestamps 解析服务下发的词级时间戳 JSON。
func parseWordTimestamps(t *testing.T, raw string) []models.WordTimestamp {
	t.Helper()
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []models.WordTimestamp
	require.NoError(t, json.Unmarshal([]byte(raw), &out), "词级时间戳应为合法 JSON")
	return out
}

// samplesToPCM 把浮点样本量化为 16bit 小端 PCM，模拟客户端上行数据。
func samplesToPCM(samples []float32) []byte {
	out := make([]byte, 0, len(samples)*2)
	for _, v := range samples {
		i := int32(v * 32767)
		if i > 32767 {
			i = 32767
		} else if i < -32768 {
			i = -32768
		}
		out = append(out, byte(i), byte(uint16(i)>>8))
	}
	return out
}
