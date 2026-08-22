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
}

// NewConfig 从 config/audio.go 的 offline_model 节读取配置并归一化。
func NewConfig(cfg config.Config) Config {
	c := Config{
		ModelType: cfg.GetString("audio.offline_model.model_type", "transducer"),
		ModelDir:  cfg.GetString("audio.offline_model.dir"),
		Model:     cfg.GetString("audio.offline_model.model"),
		Encoder:   cfg.GetString("audio.offline_model.encoder"),
		Decoder:   cfg.GetString("audio.offline_model.decoder"),
		Joiner:    cfg.GetString("audio.offline_model.joiner"),
		Tokens:    cfg.GetString("audio.offline_model.tokens"),
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
	}

	return c.normalized()
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
	return c
}

func (c Config) modelPath(name string) string {
	return filepath.Join(c.ModelDir, name)
}
