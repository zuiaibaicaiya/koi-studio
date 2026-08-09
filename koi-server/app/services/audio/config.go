package audio

import (
	"path/filepath"
	"time"

	"github.com/goravel/framework/contracts/config"
	"github.com/spf13/cast"
)

// Config 是转写服务的运行参数集合。
//
// 服务只依赖这个纯数据结构，不直接读取 facades.Config，
// 因此可以在单元测试中构造任意参数而无需启动框架。
type Config struct {
	// 模型
	ModelDir       string
	Encoder        string
	Decoder        string
	Joiner         string
	Tokens         string
	HotwordsFile   string
	NumThreads     int
	Provider       string
	DecodingMethod string
	MaxActivePaths int
	LoadTimeout    time.Duration

	// 音频流
	SampleRate   int
	FeatureDim   int
	QueueSize    int
	DecodeBatch  int
	EmitInterval time.Duration
	MaxUtterance time.Duration

	// 端点检测
	EnableEndpoint bool
	Rule1Silence   float32
	Rule2Silence   float32
	Rule3Utterance float32

	// 热词
	HotwordsScore float32

	// 存储
	Disk string
}

// NewConfig 从 config/audio.go 读取配置并归一化。
func NewConfig(cfg config.Config) Config {
	c := Config{
		ModelDir:       cfg.GetString("audio.model.dir"),
		Encoder:        cfg.GetString("audio.model.encoder"),
		Decoder:        cfg.GetString("audio.model.decoder"),
		Joiner:         cfg.GetString("audio.model.joiner"),
		Tokens:         cfg.GetString("audio.model.tokens"),
		HotwordsFile:   cfg.GetString("audio.model.hotwords_file"),
		NumThreads:     cfg.GetInt("audio.model.num_threads", 2),
		Provider:       cfg.GetString("audio.model.provider"),
		DecodingMethod: cfg.GetString("audio.model.decoding_method", "modified_beam_search"),
		MaxActivePaths: cfg.GetInt("audio.model.max_active_paths", 4),
		LoadTimeout:    time.Duration(cfg.GetInt("audio.model.load_timeout", 5)) * time.Second,

		SampleRate:   cfg.GetInt("audio.stream.sample_rate", 16000),
		FeatureDim:   cfg.GetInt("audio.stream.feature_dim", 80),
		QueueSize:    cfg.GetInt("audio.stream.queue_size", 64),
		DecodeBatch:  cfg.GetInt("audio.stream.decode_batch", 3),
		EmitInterval: time.Duration(cfg.GetInt("audio.stream.emit_interval", 200)) * time.Millisecond,
		MaxUtterance: time.Duration(cfg.GetInt("audio.stream.max_utterance", 20)) * time.Second,

		EnableEndpoint: cfg.GetBool("audio.endpoint.enable", true),
		Rule1Silence:   cast.ToFloat32(cfg.Get("audio.endpoint.rule1_min_trailing_silence", 0.5)),
		Rule2Silence:   cast.ToFloat32(cfg.Get("audio.endpoint.rule2_min_trailing_silence", 1.0)),
		Rule3Utterance: cast.ToFloat32(cfg.Get("audio.endpoint.rule3_min_utterance_length", 15.0)),

		HotwordsScore: cast.ToFloat32(cfg.Get("audio.hotwords.score", 2.0)),

		Disk: cfg.GetString("audio.storage.disk", "audio"),
	}

	return c.normalized()
}

// normalized 兜底非法配置，保证服务在配置缺失时仍能以安全默认值运行。
func (c Config) normalized() Config {
	if c.ModelDir == "" {
		c.ModelDir = "models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20"
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
	if c.HotwordsFile == "" {
		c.HotwordsFile = "hotwords.txt"
	}
	if c.NumThreads <= 0 {
		c.NumThreads = 2
	}
	if c.MaxActivePaths <= 0 {
		c.MaxActivePaths = 4
	}
	if c.LoadTimeout <= 0 {
		c.LoadTimeout = 5 * time.Second
	}
	if c.SampleRate <= 0 {
		c.SampleRate = 16000
	}
	if c.FeatureDim <= 0 {
		c.FeatureDim = 80
	}
	if c.QueueSize <= 0 {
		c.QueueSize = 64
	}
	if c.DecodeBatch <= 0 {
		c.DecodeBatch = 3
	}
	if c.EmitInterval <= 0 {
		c.EmitInterval = 200 * time.Millisecond
	}
	if c.MaxUtterance <= 0 {
		c.MaxUtterance = 20 * time.Second
	}
	if c.HotwordsScore <= 0 {
		c.HotwordsScore = 2.0
	}
	if c.Disk == "" {
		c.Disk = "audio"
	}
	return c
}

// modelPath 返回模型目录下某个文件的完整路径。
func (c Config) modelPath(name string) string {
	return filepath.Join(c.ModelDir, name)
}
