package asr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"koi-server/app/models"
	offlinetranscribe "koi-server/app/services/offline_transcribe"
	"koi-server/app/services/transcript"
)

// 本包参考 https://github.com/k2-fsa/sherpa-onnx/tree/master/go-api-examples/non-streaming-decode-files
// （以及 streaming-decode-files）示例，验证离线转写与重新转写所使用的模型
// （models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20，一个流式 bilingual
// zipformer transducer 模型）能够被正确加载并解码。
//
// 该模型为“流式”模型，其 encoder 输入为带 chunk 上下文的 [N, 39, 80]，无法作为普通离线
// transducer 一次性整段喂入（会报 "Expected: 39" 维度错误）。因此线上与单测都使用
// OnlineRecognizer，在离线批量场景下一口气把整段音频喂入（AcceptWaveform + InputFinished），
// 由其在内部完成 chunk 化与缓存——效果等同离线转写，同时兼容流式模型并得到 token 级时间戳。
//
// 该模型使用 BPE 建模单元，离线解码时必须设置 ModelingUnit="bpe" 与 BpeVocab，否则输出为空。
//
// 运行方式（模型路径相对项目根，需先 cd 到项目根）：
//
//	cd koi-server && go test ./tests/asr/ -run TestBilingualOffline -v
//
// 模型文件较大，CI 若无模型会跳过（SKIP_ASR_TESTS=1 亦可强制跳过）。

const (
	bilingualModelDir   = "models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20"
	bilingualEncoder    = "encoder-epoch-99-avg-1.onnx"
	bilingualDecoder    = "decoder-epoch-99-avg-1.onnx"
	bilingualJoiner     = "joiner-epoch-99-avg-1.onnx"
	bilingualTokens     = "tokens.txt"
	bilingualBpeVocab   = "bpe.vocab"
	bilingualFeatureDim = 80 // 流式 zipformer 的 fbank 特征维度
	bilingualTestWavDir = "models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20/test_wavs"
)

// requireProjectRoot 确保当前工作目录为项目根（含 go.mod 与 models/），否则报错。
func requireProjectRoot(t *testing.T) {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	// 测试可能从 tests/asr 目录运行，向上查找项目根。
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, bilingualModelDir)); err == nil {
				require.NoError(t, os.Chdir(dir))
				return
			}
		}
		dir = filepath.Dir(dir)
	}
	t.Skipf("未在项目根目录找到模型 %s，跳过 ASR 解码测试", bilingualModelDir)
}

// newBilingualRecognizer 构造复用 bilingual 流式模型的在线识别器（与线上 offline_transcribe 一致）。
func newBilingualRecognizer(t *testing.T) *sherpa.OnlineRecognizer {
	t.Helper()
	cfg := sherpa.OnlineRecognizerConfig{
		FeatConfig: sherpa.FeatureConfig{
			SampleRate: 16000,
			FeatureDim: bilingualFeatureDim,
		},
		ModelConfig: sherpa.OnlineModelConfig{
			Transducer: sherpa.OnlineTransducerModelConfig{
				Encoder: filepath.Join(bilingualModelDir, bilingualEncoder),
				Decoder: filepath.Join(bilingualModelDir, bilingualDecoder),
				Joiner:  filepath.Join(bilingualModelDir, bilingualJoiner),
			},
			Tokens:       filepath.Join(bilingualModelDir, bilingualTokens),
			NumThreads:   4,
			ModelingUnit: "bpe",
			BpeVocab:     filepath.Join(bilingualModelDir, bilingualBpeVocab),
			Provider:     "",
		},
		DecodingMethod: "greedy_search",
		MaxActivePaths: 4,
		HotwordsScore:  2.0,
	}
	rec := sherpa.NewOnlineRecognizer(&cfg)
	require.NotNil(t, rec, "OnlineRecognizer 不应为 nil，请检查模型文件是否完整")
	return rec
}

// newBilingualRecognizerWith 构造可配置解码方式与热词文件的识别器，用于复现/验证
// 重新转写路径（热词 + modified_beam_search）。
func newBilingualRecognizerWith(t *testing.T, decodingMethod, hotwordsFile string) *sherpa.OnlineRecognizer {
	t.Helper()
	cfg := sherpa.OnlineRecognizerConfig{
		FeatConfig: sherpa.FeatureConfig{
			SampleRate: 16000,
			FeatureDim: bilingualFeatureDim, // 必须为 80，否则解码时 SIGABRT（index: 2 Got: 39 Expected: 80）
		},
		ModelConfig: sherpa.OnlineModelConfig{
			Transducer: sherpa.OnlineTransducerModelConfig{
				Encoder: filepath.Join(bilingualModelDir, bilingualEncoder),
				Decoder: filepath.Join(bilingualModelDir, bilingualDecoder),
				Joiner:  filepath.Join(bilingualModelDir, bilingualJoiner),
			},
			Tokens:       filepath.Join(bilingualModelDir, bilingualTokens),
			NumThreads:   4,
			ModelingUnit: "bpe",
			BpeVocab:     filepath.Join(bilingualModelDir, bilingualBpeVocab),
			Provider:     "",
		},
		DecodingMethod: decodingMethod,
		MaxActivePaths: 4,
		HotwordsScore:  2.0,
	}
	if hotwordsFile != "" {
		cfg.HotwordsFile = hotwordsFile
	}
	rec := sherpa.NewOnlineRecognizer(&cfg)
	require.NotNil(t, rec, "OnlineRecognizer 不应为 nil（热词需配合 modified_beam_search，否则配置非法）")
	return rec
}

// decodeWav 读取单声道 wav 并用在线识别器解码，返回文本与结果。
func decodeWav(t *testing.T, rec *sherpa.OnlineRecognizer, wavPath string) string {
	t.Helper()
	wave := sherpa.ReadWave(wavPath)
	require.NotNil(t, wave, "ReadWave 不应为 nil: %s", wavPath)

	stream := sherpa.NewOnlineStream(rec)
	require.NotNil(t, stream, "NewOnlineStream 不应为 nil")
	defer sherpa.DeleteOnlineStream(stream)

	stream.AcceptWaveform(wave.SampleRate, wave.Samples)
	stream.InputFinished()
	for rec.IsReady(stream) {
		rec.Decode(stream)
	}
	result := rec.GetResult(stream)
	require.NotNil(t, result, "GetResult 不应为 nil")

	return result.Text
}

// TestBilingualOfflineModelLoad 验证 bilingual 模型可被识别器加载且配置生效。
func TestBilingualOfflineModelLoad(t *testing.T) {
	if os.Getenv("SKIP_ASR_TESTS") == "1" {
		t.Skip("SKIP_ASR_TESTS=1，跳过 ASR 解码测试")
	}
	requireProjectRoot(t)

	rec := newBilingualRecognizer(t)
	defer sherpa.DeleteOnlineRecognizer(rec)
	t.Log("bilingual 离线模型加载成功")
}

// TestBilingualOfflineTranscribeTestWavs
// 离线转写场景：对模型自带的中文/英文测试音频逐条解码，断言得到非空文本。
// 这覆盖了“离线转写”和“重新转写”（后者复用同一识别器与模型）的核心能力。
func TestBilingualOfflineTranscribeTestWavs(t *testing.T) {
	if os.Getenv("SKIP_ASR_TESTS") == "1" {
		t.Skip("SKIP_ASR_TESTS=1，跳过 ASR 解码测试")
	}
	requireProjectRoot(t)

	rec := newBilingualRecognizer(t)
	defer sherpa.DeleteOnlineRecognizer(rec)

	entries, err := os.ReadDir(bilingualTestWavDir)
	require.NoError(t, err, "读取测试音频目录失败")

	var wavs []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".wav") {
			wavs = append(wavs, filepath.Join(bilingualTestWavDir, e.Name()))
		}
	}
	require.NotEmpty(t, wavs, "模型目录应至少包含一个测试 wav")

	for _, wav := range wavs {
		wav := wav
		t.Run(filepath.Base(wav), func(t *testing.T) {
			text := decodeWav(t, rec, wav)
			t.Logf("音频 %s -> 转写: %q", filepath.Base(wav), text)
			assert.NotEmpty(t, text, "离线转写结果不应为空（若为空请确认 BpeVocab 已正确设置）")
		})
	}
}

// TestBilingualOfflineReTranscribeSameRecognizer
// 重新转写场景：用同一个已加载的识别器对同一条音频重复解码，验证可稳定复现结果。
func TestBilingualOfflineReTranscribeSameRecognizer(t *testing.T) {
	if os.Getenv("SKIP_ASR_TESTS") == "1" {
		t.Skip("SKIP_ASR_TESTS=1，跳过 ASR 解码测试")
	}
	requireProjectRoot(t)

	rec := newBilingualRecognizer(t)
	defer sherpa.DeleteOnlineRecognizer(rec)

	const wav = bilingualTestWavDir + "/0.wav"
	require.FileExists(t, wav, "默认测试音频 0.wav 应存在")

	first := decodeWav(t, rec, wav)
	t.Logf("首次转写: %q", first)
	assert.NotEmpty(t, first, "首次转写结果不应为空")

	// 重新转写：复用识别器再解码一次，greedy_search 应得到一致结果。
	second := decodeWav(t, rec, wav)
	t.Logf("重新转写: %q", second)
	assert.Equal(t, first, second, "相同音频在相同识别器下重新转写结果应一致")
}

// TestBilingualOfflineConfigMatchesService
// 验证 offline_transcribe 服务的默认配置指向新 bilingual 模型，
// 保证“离线转写 / 重新转写”与单元测试使用同一模型。
func TestBilingualOfflineConfigMatchesService(t *testing.T) {
	requireProjectRoot(t)

	// 复用服务包内的归一化默认值做一致性校验。
	offlineDir := "models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20"
	assert.Equal(t, offlineDir, offlinetranscribe.DefaultModelDir(),
		"offline_transcribe 默认模型目录应指向 bilingual 流式模型")

	if os.Getenv("SKIP_ASR_TESTS") != "1" {
		// 进一步确认模型文件真实存在。
		for _, f := range []string{bilingualEncoder, bilingualDecoder, bilingualJoiner, bilingualTokens, bilingualBpeVocab} {
			assert.FileExists(t, filepath.Join(offlineDir, f), "离线模型文件应存在: %s", f)
		}
	}
}

// TestBilingualOfflineHotwordsBeamSearch
// 回归测试：复现“前端调用重新转写导致后端崩溃”。
//
// 崩溃日志关键两行：
//   online-recognizer.cc:Validate ... Please use --decoding-method=modified_beam_search
//     if you provide --hotwords-file. Given --decoding-method=greedy_search
//   Ort::Exception: Got invalid dimensions for input: x ... index: 2 Got: 39 Expected: 80
//
// 根因：
//   1) 离线模型特征维度被误设为 39，而 bilingual 流式 zipformer 需要 80，解码时 SIGABRT；
//   2) 提供热词文件时必须使用 modified_beam_search，否则配置非法、识别器创建失败。
//
// 本测试用 modified_beam_search + 热词文件构造识别器并解码，验证不再崩溃且能正常转写。
func TestBilingualOfflineHotwordsBeamSearch(t *testing.T) {
	if os.Getenv("SKIP_ASR_TESTS") == "1" {
		t.Skip("SKIP_ASR_TESTS=1，跳过 ASR 解码测试")
	}
	requireProjectRoot(t)

	// 写一个小热词文件（中英文混合，验证 BPE 热词可用）。
	hw := "会议\nMONDAY\n星期三\n"
	hwFile := filepath.Join(t.TempDir(), "hotwords.txt")
	require.NoError(t, os.WriteFile(hwFile, []byte(hw), 0644))

	rec := newBilingualRecognizerWith(t, "modified_beam_search", hwFile)
	defer sherpa.DeleteOnlineRecognizer(rec)

	const wav = bilingualTestWavDir + "/0.wav"
	require.FileExists(t, wav, "默认测试音频 0.wav 应存在")

	text := decodeWav(t, rec, wav)
	t.Logf("热词+beam 转写: %q", text)
	assert.NotEmpty(t, text, "热词+beam 解码结果不应为空（否则可能又触发维度/SIGABRT 崩溃）")
}

// TestBilingualOfflineFeatureDimIsEighty 锁定关键约束：bilingual 流式 zipformer 的
// fbank 特征维度必须是 80（与实时流一致）。归一化默认值保证即使配置缺失也不会回退到 39，
// 避免解码时出现 “index: 2 Got: 39 Expected: 80” 的维度不匹配导致 SIGABRT。
func TestBilingualOfflineFeatureDimIsEighty(t *testing.T) {
	assert.Equal(t, 80, offlinetranscribe.DefaultFeatureDim(), "离线模型特征维度必须固定为 80")
}

// decodeWavDetailed 读取单声道 wav 并用在线识别器解码，返回文本、token 级时间戳
// 与音频元信息（采样率、总时长秒），供详细转写输出使用。
func decodeWavDetailed(t *testing.T, rec *sherpa.OnlineRecognizer, wavPath string) (text string, tokens []string, timestamps []float32, sampleRate int, durationSec float64) {
	t.Helper()
	wave := sherpa.ReadWave(wavPath)
	require.NotNil(t, wave, "ReadWave 不应为 nil: %s", wavPath)

	stream := sherpa.NewOnlineStream(rec)
	require.NotNil(t, stream, "NewOnlineStream 不应为 nil")
	defer sherpa.DeleteOnlineStream(stream)

	stream.AcceptWaveform(wave.SampleRate, wave.Samples)
	stream.InputFinished()
	for rec.IsReady(stream) {
		rec.Decode(stream)
	}
	result := rec.GetResult(stream)
	require.NotNil(t, result, "GetResult 不应为 nil")

	return result.Text, result.Tokens, result.Timestamps,
		wave.SampleRate, float64(len(wave.Samples)) / float64(wave.SampleRate)
}

// formatMs 把毫秒格式化为 mm:ss.mmm 便于阅读。
func formatMs(ms int64) string {
	return fmt.Sprintf("%02d:%02d.%03d", ms/60000, (ms%60000)/1000, ms%1000)
}

// hasAnyValidTimestamp 判断模型产出的 token 级时间戳是否真实可用
// （与线上 offline_transcribe 的 hasValidTimestamps 规则一致：全 0 视为无效）。
func hasAnyValidTimestamp(ts []float32) bool {
	for _, t := range ts {
		if t > 0 {
			return true
		}
	}
	return false
}

// TestBilingualOfflineTranscribeLiDyeDetailed
// 对项目根目录的 李大爷.wav 执行离线转写，输出“详细的转写内容”：
//
//	1) 音频元信息（采样率、总时长）；
//	2) 完整转写文本；
//	3) token 级时间戳（模型若产出；bilingual 离线 transducer 可能为全 0，此时退化按音频时长近似）；
//	4) 按标点/长度切分的句子分段，每句带起止时间戳。
//
// 分句与时间戳策略与线上 offline_transcribe（splitSentencesWithTimestamps）保持一致：
// 优先使用模型 token 级时间戳，无效时退化为按整段音频时长在句间按字符比例分配。
func TestBilingualOfflineTranscribeLiDyeDetailed(t *testing.T) {
	if os.Getenv("SKIP_ASR_TESTS") == "1" {
		t.Skip("SKIP_ASR_TESTS=1，跳过 ASR 解码测试")
	}
	requireProjectRoot(t)

	const wav = "李大爷.wav"
	require.FileExists(t, wav, "李大爷.wav 应位于项目根目录")

	rec := newBilingualRecognizer(t)
	defer sherpa.DeleteOnlineRecognizer(rec)

	text, tokens, timestamps, sampleRate, durationSec := decodeWavDetailed(t, rec, wav)

	// 1) 音频元信息
	t.Logf("===== 离线转写详情: %s =====", wav)
	t.Logf("音频: 采样率=%d Hz, 总时长=%.3f 秒", sampleRate, durationSec)

	// 2) 完整文本
	t.Logf("完整文本: %q", text)
	assert.NotEmpty(t, text, "离线转写结果不应为空（请确认 BpeVocab 已正确设置）")
	assert.Len(t, tokens, len(timestamps), "tokens 与 timestamps 应等长")

	// 3) token 级时间戳
	if hasAnyValidTimestamp(timestamps) {
		t.Logf("token 级时间戳（模型产出）:")
		for i := range tokens {
			start := int64(timestamps[i] * 1000)
			end := start
			if i+1 < len(timestamps) {
				end = int64(timestamps[i+1] * 1000)
			}
			t.Logf("  [%s - %s] %q", formatMs(start), formatMs(end), tokens[i])
		}
	} else {
		t.Logf("模型未产出有效 token 级时间戳（全 0），句子时间戳按音频时长近似分配")
	}

	// 4) 句子分段（含起止时间）
	segs := splitSentencesWithApproxTime(text, timestamps, sampleRate, durationSec)
	t.Logf("句子分段 (%d 句):", len(segs))
	for _, seg := range segs {
		t.Logf("  [%s - %s] %s", formatMs(seg.startMs), formatMs(seg.endMs), seg.text)
	}
	assert.NotEmpty(t, segs, "应至少切分出一句")
	for i := 1; i < len(segs); i++ {
		assert.GreaterOrEqual(t, segs[i].startMs, segs[i-1].startMs,
			"句子时间戳应单调不减（防止时间轴错乱）")
	}
}

// TestBilingualOfflineTranscribeLiDyeWordTimestamps
// 对项目根目录的 李大爷.wav 执行离线转写，输出“文字级别”的时间戳：
// 每个字（中文）/词（英文）对应 [起始时间, 结束时间]，毫秒精度。
//
// 时间戳来源与线上 offline_transcribe（splitSentencesWithTimestamps）一致：
//   - 优先：模型 token 级时间戳 -> transcript.AlignCharTimes 展开为逐字时间戳
//     -> transcript.WordsFromCharTimesIntervals 聚合成带时间区间的字/词级时间戳；
//   - 退化：模型未产出有效时间戳时，按整段音频时长对逐字线性近似后再聚合。
//
// 可用 -run TestBilingualOfflineTranscribeLiDye 同时运行详细版与文字级版本。
func TestBilingualOfflineTranscribeLiDyeWordTimestamps(t *testing.T) {
	if os.Getenv("SKIP_ASR_TESTS") == "1" {
		t.Skip("SKIP_ASR_TESTS=1，跳过 ASR 解码测试")
	}
	requireProjectRoot(t)

	const wav = "李大爷.wav"
	require.FileExists(t, wav, "李大爷.wav 应位于项目根目录")

	rec := newBilingualRecognizer(t)
	defer sherpa.DeleteOnlineRecognizer(rec)

	text, tokens, timestamps, sampleRate, durationSec := decodeWavDetailed(t, rec, wav)

	t.Logf("===== 离线转写 文字级时间戳: %s =====", wav)
	t.Logf("音频: 采样率=%d Hz, 总时长=%.3f 秒", sampleRate, durationSec)
	t.Logf("完整文本: %q", text)
	assert.NotEmpty(t, text, "离线转写结果不应为空（请确认 BpeVocab 已正确设置）")

	// 与线上 splitSentencesWithTimestamps 相同的两条路径：模型时间戳优先，否则按音频时长近似。
	words, ok := wordTimestampsFromModel(text, tokens, timestamps)
	if !ok {
		words = approxWordTimestamps(text, durationSec)
	}
	require.NotEmpty(t, words, "文字级时间戳不应为空")

	t.Logf("文字级时间戳 (%d 个，中文按字、英文按词):", len(words))
	for _, w := range words {
		t.Logf("  [%s - %s] %s", formatMs(w.StartMs), formatMs(w.EndMs), w.Word)
	}

	// 时间必须单调不减，与音频时间轴一致。
	last := int64(0)
	for _, w := range words {
		assert.GreaterOrEqual(t, w.StartMs, last, "文字时间戳应单调不减: %q", w.Word)
		assert.GreaterOrEqual(t, w.EndMs, w.StartMs, "结束时间不应早于开始时间: %q", w.Word)
		last = w.EndMs
	}

	// 回归断言：每个字的时长必须被限制在合理范围内，语音停顿/静音
	// 不得整段归到前一个字上（根因：WordsFromCharTimesIntervals 把字 i 的结束
	// 时间直接设为字 i+1 的开始时间，导致静音前一个字被拉长到数秒）。
	// 本音频全文为中文单字，单字最长估计时长为 500ms；留出余量取 1000ms 判定。
	for _, w := range words {
		dur := w.EndMs - w.StartMs
		assert.LessOrEqual(t, dur, int64(1000),
			"字 %q 时长异常 (%dms)：语音停顿/静音被错误归到该字上", w.Word, dur)
	}
}

// wordTimestampsFromModel 复现线上 offline_transcribe 的文字级时间戳生成策略：
// 模型 token 级时间戳有效时用 transcript.AlignCharTimes + WordsFromCharTimesIntervals，
// 返回带时间区间的字/词级时间戳；模型时间戳无效时 ok=false。
func wordTimestampsFromModel(text string, tokens []string, timestamps []float32) ([]models.WordTimestamp, bool) {
	if !hasAnyValidTimestamp(timestamps) || len(timestamps) != len(tokens) {
		return nil, false
	}
	tokenTimes := make([]transcript.TokenTimestamp, len(tokens))
	for i := range tokens {
		tokenTimes[i] = transcript.TokenTimestamp{Token: tokens[i], TimeSec: timestamps[i]}
	}
	charTimes, ok := transcript.AlignCharTimes(text, tokenTimes)
	if !ok {
		return nil, false
	}
	return transcript.WordsFromCharTimesIntervals(text, charTimes), true
}

// approxWordTimestamps 退化方案：没有模型时间戳时，把整段音频时长按字符数
// 均匀铺开得到逐字时间戳，再聚合为带区间的字/词级时间戳（仅用于展示，非线上逻辑）。
func approxWordTimestamps(text string, durationSec float64) []models.WordTimestamp {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	charTimes := make([]float32, len(runes))
	for i := range runes {
		charTimes[i] = float32(durationSec * float64(i) / float64(len(runes)))
	}
	return transcript.WordsFromCharTimesIntervals(text, charTimes)
}

// detailSentence 测试内的句子分段（仅用于详细输出展示）。
type detailSentence struct {
	text    string
	startMs int64
	endMs   int64
}

// splitSentencesWithApproxTime 在测试内复现线上 splitSentencesWithTimestamps 的分句与
// 时间戳策略：
//   - 分句：按标点（。！？.!?；;，,）切句（不足 4 字不切），超 20 字强制切句；
//   - 时间戳：模型 token 级时间戳有效则按 token 对齐；否则按整段音频时长、
//     在句间按字符比例线性近似（简化版：不做语音段检测，适合短音频展示）。
func splitSentencesWithApproxTime(text string, timestamps []float32, sampleRate int, durationSec float64) []detailSentence {
	src := []rune(text)
	if len(src) == 0 {
		return nil
	}
	// 分句（与 simpleSplitSentences 相同的规则）
	var spans []struct{ text string; start, end int }
	curStart := 0
	var cur []rune
	for i, r := range src {
		cur = append(cur, r)
		switch r {
		case '。', '！', '？', '.', '!', '?', '；', ';', '，', ',':
			if len(cur) >= 4 {
				spans = append(spans, struct {
					text         string
					start, end int
				}{string(cur), curStart, i + 1})
				cur = nil
				curStart = i + 1
			}
		default:
			if len(cur) >= 20 {
				spans = append(spans, struct {
					text         string
					start, end int
				}{string(cur), curStart, i + 1})
				cur = nil
				curStart = i + 1
			}
		}
	}
	if len(cur) > 0 {
		spans = append(spans, struct {
			text         string
			start, end int
		}{string(cur), curStart, len(src)})
	}
	if len(spans) == 0 {
		spans = append(spans, struct {
			text         string
			start, end int
		}{text, 0, len(src)})
	}

	totalRunes := 0
	for _, sp := range spans {
		totalRunes += sp.end - sp.start
	}
	if totalRunes == 0 {
		totalRunes = 1
	}

	// 模型 token 级时间戳有效：按 token 序号映射字符位置（token 序近似字符序）。
	if hasAnyValidTimestamp(timestamps) && len(timestamps) == len([]rune(text)) {
		_ = sampleRate
		var out []detailSentence
		for _, sp := range spans {
			startMs := int64(timestamps[sp.start] * 1000)
			endMs := int64(timestamps[sp.end-1] * 1000)
			if endMs < startMs {
				endMs = startMs
			}
			out = append(out, detailSentence{text: sp.text, startMs: startMs, endMs: endMs})
		}
		return out
	}

	// 退化：按整段音频时长、字符比例近似分配。
	totalMs := int64(durationSec * 1000)
	var out []detailSentence
	for _, sp := range spans {
		startMs := int64(float64(totalMs) * float64(sp.start) / float64(totalRunes))
		endMs := int64(float64(totalMs) * float64(sp.end) / float64(totalRunes))
		if endMs < startMs {
			endMs = startMs
		}
		out = append(out, detailSentence{text: sp.text, startMs: startMs, endMs: endMs})
	}
	return out
}
