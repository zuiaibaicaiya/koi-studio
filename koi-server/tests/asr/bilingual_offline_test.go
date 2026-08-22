package asr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	offlinetranscribe "koi-server/app/services/offline_transcribe"
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
