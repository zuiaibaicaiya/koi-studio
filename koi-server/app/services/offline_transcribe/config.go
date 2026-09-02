// Package offlinetranscribe 实现基于 sherpa-onnx 非流式（离线）模型的音频文件转写服务。
package offlinetranscribe

import (
	"path/filepath"
	"time"

	"github.com/goravel/framework/contracts/config"
	"github.com/spf13/cast"
)

// Config 离线转写服务的运行参数集合。
type Config struct {
	// 模型类型：zipformer_ctc | transducer | paraformer
	ModelType string

	// 模型目录与文件名
	ModelDir string
	Model    string // zipformer_ctc / paraformer 使用的单模型文件
	Encoder  string // transducer 三文件模型
	Decoder  string
	Joiner   string
	Tokens   string

	// bilingual 等模型使用 BPE 建模单元时需要。
	ModelingUnit string // cjkchar | bpe，留空表示不指定
	BpeVocab     string // bpe.vocab 路径（ModelingUnit=bpe 时必填）

	NumThreads     int
	Provider       string
	DecodingMethod string
	MaxActivePaths int
	HotwordsScore  float32
	LoadTimeout    time.Duration

	// 音频
	SampleRate int
	FeatureDim int // 特征维度，bilingual 流式 zipformer 为 39

	// 语音活动检测（VAD）：用于按静音切分音频，使句子不被从中间切开
	VadEnabled            bool
	VadModel              string  // silero_vad.onnx 路径；文件不存在时自动退化为能量检测
	VadThreshold          float32 // Silero VAD 判定阈值
	VadMinSilenceDuration float32 // 段内静音超过该秒数才结束当前语音段
	VadMinSpeechDuration  float32 // 短于该秒数的语音段丢弃
	VadWindowSize         int     // 每次送入 VAD 的采样数（512 对应 32ms@16k）
	VadMaxSpeechDuration  float32 // 单段语音上限（秒），超过后强制切分
	VadNumThreads         int
	VadProvider           string
	VadBufferSeconds      float32 // VAD 内部缓冲容量（秒）
	VadMinSilenceMs       int     // 后处理：静音间隔不超过该毫秒时合并相邻语音段
	VadMinSpeechMs        int     // 后处理：短于该毫秒的语音段视为噪声丢弃
	VadPaddingMs          int     // 后处理：语音段首尾各扩展该毫秒，避免切掉首尾音节

	// 音频切分
	MaxChunkSeconds      float64 // 单个识别窗口的最大时长（秒）
	MinSilenceCutSeconds float64 // 窗口之间允许切分的最小静音时长（秒）

	// 断句
	SentenceMinRunes     int // 单句最少字符数，低于该值不切句
	SentenceTargetRunes  int // 达到该长度后允许在句内标点/停顿处断句
	SentenceHardMaxRunes int // 硬上限，超过后强制断句
	SentencePauseMs      int // 字间静音超过该毫秒视为可断句的停顿
	SentenceMergeGapMs   int // 跨窗口合并碎片时允许的最大静音间隙（毫秒）
}

// NewConfig 从 config/audio.go 的 offline_model 节读取配置并归一化。
func NewConfig(cfg config.Config) Config {
	c := Config{
		ModelType:    cfg.GetString("audio.offline_model.model_type", "transducer"),
		ModelDir:     cfg.GetString("audio.offline_model.dir"),
		Model:        cfg.GetString("audio.offline_model.model"),
		Encoder:      cfg.GetString("audio.offline_model.encoder"),
		Decoder:      cfg.GetString("audio.offline_model.decoder"),
		Joiner:       cfg.GetString("audio.offline_model.joiner"),
		Tokens:       cfg.GetString("audio.offline_model.tokens"),
		ModelingUnit: cfg.GetString("audio.offline_model.modeling_unit", ""),
		BpeVocab:     cfg.GetString("audio.offline_model.bpe_vocab", ""),

		NumThreads:     cfg.GetInt("audio.offline_model.num_threads", 4),
		Provider:       cfg.GetString("audio.offline_model.provider", ""),
		DecodingMethod: cfg.GetString("audio.offline_model.decoding_method", "greedy_search"),
		MaxActivePaths: cfg.GetInt("audio.offline_model.max_active_paths", 4),
		HotwordsScore:  cast.ToFloat32(cfg.Get("audio.offline_model.hotwords_score", 2.0)),
		LoadTimeout:    time.Duration(cfg.GetInt("audio.offline_model.load_timeout", 15)) * time.Second,

		SampleRate: cfg.GetInt("audio.stream.sample_rate", 16000),
		// bilingual 流式 zipformer 的 fbank 特征维度为 80（与实时流一致），不可为 39。
		FeatureDim: cfg.GetInt("audio.offline_model.feature_dim", 80),

		VadEnabled:            cfg.GetBool("audio.offline_vad.enabled", true),
		VadModel:              cfg.GetString("audio.offline_vad.model", "models/silero_vad.onnx"),
		VadThreshold:          cast.ToFloat32(cfg.Get("audio.offline_vad.threshold", 0.4)),
		VadMinSilenceDuration: cast.ToFloat32(cfg.Get("audio.offline_vad.min_silence_duration", 0.4)),
		VadMinSpeechDuration:  cast.ToFloat32(cfg.Get("audio.offline_vad.min_speech_duration", 0.25)),
		VadWindowSize:         cfg.GetInt("audio.offline_vad.window_size", 512),
		VadMaxSpeechDuration:  cast.ToFloat32(cfg.Get("audio.offline_vad.max_speech_duration", 30)),
		VadNumThreads:         cfg.GetInt("audio.offline_vad.num_threads", 1),
		VadProvider:           cfg.GetString("audio.offline_vad.provider", ""),
		VadBufferSeconds:      cast.ToFloat32(cfg.Get("audio.offline_vad.buffer_seconds", 60)),
		VadMinSilenceMs:       cfg.GetInt("audio.offline_vad.min_silence_ms", 300),
		VadMinSpeechMs:        cfg.GetInt("audio.offline_vad.min_speech_ms", 150),
		VadPaddingMs:          cfg.GetInt("audio.offline_vad.padding_ms", 80),

		MaxChunkSeconds:      cast.ToFloat64(cfg.Get("audio.offline_segment.max_chunk_seconds", 30)),
		MinSilenceCutSeconds: cast.ToFloat64(cfg.Get("audio.offline_segment.min_silence_cut_seconds", 0.4)),

		SentenceMinRunes:     cfg.GetInt("audio.offline_segment.sentence_min_runes", 8),
		SentenceTargetRunes:  cfg.GetInt("audio.offline_segment.sentence_target_runes", 30),
		SentenceHardMaxRunes: cfg.GetInt("audio.offline_segment.sentence_hard_max_runes", 50),
		SentencePauseMs:      cfg.GetInt("audio.offline_segment.sentence_pause_ms", 500),
		SentenceMergeGapMs:   cfg.GetInt("audio.offline_segment.sentence_merge_gap_ms", 250),
	}

	return c.normalized()
}

// vadPostOptions 返回 VAD 后处理参数。
func (c Config) vadPostOptions() vadPostOptions {
	return vadPostOptions{
		MinSilenceMs: c.VadMinSilenceMs,
		MinSpeechMs:  c.VadMinSpeechMs,
		PaddingMs:    c.VadPaddingMs,
	}
}

// chunkOptions 返回音频切分参数。
func (c Config) chunkOptions() chunkOptions {
	return chunkOptions{
		MaxChunkSeconds:      c.MaxChunkSeconds,
		MinSilenceCutSeconds: c.MinSilenceCutSeconds,
	}
}

// sentenceOptions 返回断句参数。
func (c Config) sentenceOptions() sentenceOptions {
	return sentenceOptions{
		MinRunes:     c.SentenceMinRunes,
		TargetRunes:  c.SentenceTargetRunes,
		HardMaxRunes: c.SentenceHardMaxRunes,
		PauseMs:      c.SentencePauseMs,
		MergeGapMs:   c.SentenceMergeGapMs,
	}.normalized()
}

// DefaultModelDir 返回离线转写模型目录的归一化默认值。
// 供测试等场景在不依赖框架配置的情况下获取当前默认模型路径。
func DefaultModelDir() string {
	return Config{}.normalized().ModelDir
}

// DefaultFeatureDim 返回离线转写特征维度的归一化默认值（bilingual 流式模型固定为 80）。
func DefaultFeatureDim() int {
	return Config{}.normalized().FeatureDim
}

func (c Config) normalized() Config {
	if c.ModelDir == "" {
		c.ModelDir = "models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20"
	}
	if c.ModelType == "" {
		c.ModelType = "transducer"
	}
	if c.Model == "" {
		c.Model = "model.onnx"
	}
	if c.Encoder == "" {
		c.Encoder = "encoder-epoch-99-avg-1.onnx"
	}
	if c.Decoder == "" {
		c.Decoder = "decoder-epoch-99-avg-1.onnx"
	}
	if c.Joiner == "" {
		c.Joiner = "joiner-epoch-99-avg-1.onnx"
	}
	if c.Tokens == "" {
		c.Tokens = "tokens.txt"
	}
	if c.ModelingUnit == "" {
		c.ModelingUnit = "bpe"
	}
	if c.BpeVocab == "" {
		c.BpeVocab = "bpe.vocab"
	}
	if c.FeatureDim <= 0 {
		c.FeatureDim = 80
	}
	if c.NumThreads <= 0 {
		c.NumThreads = 4
	}
	if c.MaxActivePaths <= 0 {
		c.MaxActivePaths = 4
	}
	if c.LoadTimeout <= 0 {
		c.LoadTimeout = 60 * time.Second
	}
	if c.SampleRate <= 0 {
		c.SampleRate = 16000
	}
	if c.FeatureDim <= 0 {
		c.FeatureDim = 80
	}
	if c.HotwordsScore <= 0 {
		c.HotwordsScore = 2.0
	}
	if c.VadModel == "" {
		c.VadModel = "models/silero_vad.onnx"
	}
	if c.VadThreshold <= 0 {
		c.VadThreshold = 0.4
	}
	if c.VadMinSilenceDuration <= 0 {
		c.VadMinSilenceDuration = 0.4
	}
	if c.VadMinSpeechDuration <= 0 {
		c.VadMinSpeechDuration = 0.25
	}
	if c.VadWindowSize <= 0 {
		c.VadWindowSize = 512
	}
	if c.VadMaxSpeechDuration <= 0 {
		c.VadMaxSpeechDuration = 30
	}
	if c.VadNumThreads <= 0 {
		c.VadNumThreads = 1
	}
	if c.VadBufferSeconds <= 0 {
		c.VadBufferSeconds = 60
	}
	if c.VadMinSilenceMs < 0 {
		c.VadMinSilenceMs = 0
	}
	if c.VadMinSpeechMs < 0 {
		c.VadMinSpeechMs = 0
	}
	if c.VadPaddingMs < 0 {
		c.VadPaddingMs = 0
	}
	if c.MaxChunkSeconds <= 0 {
		c.MaxChunkSeconds = 30
	}
	if c.MinSilenceCutSeconds < 0 {
		c.MinSilenceCutSeconds = 0
	}
	return c
}

func (c Config) modelPath(name string) string {
	return filepath.Join(c.ModelDir, name)
}

// VadModelPath 返回 Silero VAD 模型文件的完整路径。
//
// VAD 模型与 ASR 模型分开放置（默认在项目根目录的 models/silero_vad.onnx），
// 因此这里不做 modelPath 拼接，而是直接返回配置值。
func (c Config) VadModelPath() string {
	return c.VadModel
}

// chunkOptions 音频切分参数（时长单位：秒）。
type chunkOptions struct {
	// MaxChunkSeconds 单个识别窗口的最大时长。
	MaxChunkSeconds float64
	// MinSilenceCutSeconds 允许在两个窗口之间切分的最小静音时长。
	// 静音短于该值时宁可让窗口超长，也不在语音中间切开。
	MinSilenceCutSeconds float64
}

// normalized 兜底非法切分参数。
func (o chunkOptions) normalized() chunkOptions {
	if o.MaxChunkSeconds <= 0 {
		o.MaxChunkSeconds = 30
	}
	if o.MinSilenceCutSeconds < 0 {
		o.MinSilenceCutSeconds = 0
	}
	return o
}
