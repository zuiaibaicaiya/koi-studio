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

	NumThreads     int
	Provider       string
	DecodingMethod string
	MaxActivePaths int
	HotwordsScore  float32
	LoadTimeout    time.Duration

	// 音频
	SampleRate int
	FeatureDim int
}

// NewConfig 从 config/audio.go 的 offline_model 节读取配置并归一化。
func NewConfig(cfg config.Config) Config {
	c := Config{
		ModelType: cfg.GetString("audio.offline_model.model_type", "zipformer_ctc"),
		ModelDir:  cfg.GetString("audio.offline_model.dir"),
		Model:     cfg.GetString("audio.offline_model.model"),
		Encoder:   cfg.GetString("audio.offline_model.encoder"),
		Decoder:   cfg.GetString("audio.offline_model.decoder"),
		Joiner:    cfg.GetString("audio.offline_model.joiner"),
		Tokens:    cfg.GetString("audio.offline_model.tokens"),

		NumThreads:     cfg.GetInt("audio.offline_model.num_threads", 4),
		Provider:       cfg.GetString("audio.offline_model.provider", ""),
		DecodingMethod: cfg.GetString("audio.offline_model.decoding_method", "greedy_search"),
		MaxActivePaths: cfg.GetInt("audio.offline_model.max_active_paths", 4),
		HotwordsScore:  cast.ToFloat32(cfg.Get("audio.offline_model.hotwords_score", 2.0)),
		LoadTimeout:    time.Duration(cfg.GetInt("audio.offline_model.load_timeout", 15)) * time.Second,

		SampleRate: cfg.GetInt("audio.stream.sample_rate", 16000),
		FeatureDim: cfg.GetInt("audio.stream.feature_dim", 80),
	}

	return c.normalized()
}

func (c Config) normalized() Config {
	if c.ModelDir == "" {
		c.ModelDir = "models/sherpa-onnx-zipformer-zh-en-2023-11-22"
	}
	if c.ModelType == "" {
		c.ModelType = "transducer"
	}
	if c.Model == "" {
		c.Model = "model.onnx"
	}
	if c.Encoder == "" {
		c.Encoder = "encoder-epoch-34-avg-19.onnx"
	}
	if c.Decoder == "" {
		c.Decoder = "decoder-epoch-34-avg-19.onnx"
	}
	if c.Joiner == "" {
		c.Joiner = "joiner-epoch-34-avg-19.onnx"
	}
	if c.Tokens == "" {
		c.Tokens = "tokens.txt"
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
