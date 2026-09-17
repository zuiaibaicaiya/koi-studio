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

	// 断句（实时）
	//
	// 实时转写不使用 sherpa-onnx 的内置端点检测（Reset 会污染下一句的
	// 时间戳原点与首字识别质量），而是在一条连续识别流上按尾部静音自行断句。
	// SegmentSilenceMs 为「距最后一个已发射 token 的音频时长」阈值：
	// 该值包含模型发射 token 的固有延迟（约 0.2~0.4s），因此需要明显大于
	// 期望的真实静音时长，避免正常说话中的换气停顿被误判为句尾。
	SegmentSilenceMs int
	// MaxUtterance 单句最长时长，超过后强制断句，避免长时间无停顿导致结果迟迟不下发。
	MaxUtterance time.Duration

	// 热词
	HotwordsScore float32

	// 存储
	Disk string

	// 说话人识别
	SpeakerIdentifyEnabled     bool
	SpeakerIdentifyMinDuration float64
	SpeakerIdentifyMaxBuffer   float64
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

		SegmentSilenceMs: cfg.GetInt("audio.segment.silence_ms", 1500),
		MaxUtterance:     time.Duration(cfg.GetInt("audio.segment.max_utterance_seconds", 30)) * time.Second,

		HotwordsScore: cast.ToFloat32(cfg.Get("audio.hotwords.score", 2.0)),

		Disk: cfg.GetString("audio.storage.disk", "audio"),

		SpeakerIdentifyEnabled:     cfg.GetBool("audio.speaker_identify.enabled", true),
		SpeakerIdentifyMinDuration: cast.ToFloat64(cfg.Get("audio.speaker_identify.min_duration", 1.0)),
		SpeakerIdentifyMaxBuffer:   cast.ToFloat64(cfg.Get("audio.speaker_identify.max_buffer_duration", 30)),
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
	if c.DecodingMethod == "" {
		// 识别器始终带有热词文件，而 sherpa-onnx 要求提供热词文件时必须使用
		// modified_beam_search，否则配置非法、识别器创建失败（返回 nil）。
		c.DecodingMethod = "modified_beam_search"
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
	if c.SegmentSilenceMs <= 0 {
		c.SegmentSilenceMs = 1500
	}
	if c.MaxUtterance <= 0 {
		c.MaxUtterance = 30 * time.Second
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
