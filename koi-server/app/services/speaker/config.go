package speaker

import (
	"path/filepath"
	"time"

	"github.com/goravel/framework/contracts/config"
	"github.com/spf13/cast"
)

// Config 是声纹服务的运行参数集合。
//
// 与 audio.Config 的设计一致：服务只依赖这个纯数据结构，
// 不直接读取 facades.Config，便于在单元测试中构造任意参数。
type Config struct {
	// 模型
	ModelDir    string
	Model       string
	NumThreads  int
	Provider    string
	Debug       bool
	LoadTimeout time.Duration

	// 检索
	Threshold float32

	// 音频
	SampleRate  int
	MinDuration float64
	MaxDuration float64
	MaxFileSize int64

	// 有效语音
	MinValidDuration float64

	// 语音活动检测（VAD）
	VadModel     string
	VadThreshold float32

	// 存储
	Disk string
	Dir  string
}

// NewConfig 从 config/speaker.go 读取配置并归一化。
func NewConfig(cfg config.Config) Config {
	c := Config{
		ModelDir:    cfg.GetString("speaker.model.dir"),
		Model:       cfg.GetString("speaker.model.file"),
		NumThreads:  cfg.GetInt("speaker.model.num_threads", 2),
		Provider:    cfg.GetString("speaker.model.provider"),
		Debug:       cfg.GetBool("speaker.model.debug", false),
		LoadTimeout: time.Duration(cfg.GetInt("speaker.model.load_timeout", 10)) * time.Second,

		Threshold: cast.ToFloat32(cfg.Get("speaker.search.threshold", 0.5)),

		SampleRate:  cfg.GetInt("speaker.audio.sample_rate", 16000),
		MinDuration: cast.ToFloat64(cfg.Get("speaker.audio.min_duration", 0.5)),
		MaxDuration: cast.ToFloat64(cfg.Get("speaker.audio.max_duration", 60.0)),
		MaxFileSize: cast.ToInt64(cfg.Get("speaker.audio.max_file_size", 20*1024*1024)),

		MinValidDuration: cast.ToFloat64(cfg.Get("speaker.min_valid_duration", 5.0)),

		VadModel:     cfg.GetString("speaker.vad.model"),
		VadThreshold: cast.ToFloat32(cfg.Get("speaker.vad.threshold", 0.5)),

		Disk: cfg.GetString("speaker.storage.disk", "speaker"),
		Dir:  cfg.GetString("speaker.storage.dir", "speakers"),
	}

	return c.normalized()
}

// normalized 兜底非法配置，保证服务在配置缺失时仍能以安全默认值运行。
func (c Config) normalized() Config {
	if c.ModelDir == "" {
		c.ModelDir = "models/speaker"
	}
	if c.Model == "" {
		c.Model = "3dspeaker_speech_campplus_sv_zh-cn_16k-common.onnx"
	}
	if c.NumThreads <= 0 {
		c.NumThreads = 2
	}
	if c.LoadTimeout <= 0 {
		c.LoadTimeout = 10 * time.Second
	}
	// 余弦相似度取值区间为 [-1, 1]，阈值超出该范围没有意义。
	if c.Threshold <= 0 || c.Threshold >= 1 {
		c.Threshold = 0.5
	}
	if c.SampleRate <= 0 {
		c.SampleRate = 16000
	}
	if c.MinDuration <= 0 {
		c.MinDuration = 0.5
	}
	if c.MaxDuration <= c.MinDuration {
		c.MaxDuration = 60.0
	}
	if c.MaxFileSize <= 0 {
		c.MaxFileSize = 20 * 1024 * 1024
	}
	// 注册所需的有效语音最短时长，至少 1 秒才有意义。
	if c.MinValidDuration <= 0 {
		c.MinValidDuration = 5.0
	}
	// 余弦阈值取值区间 (0, 1]，超过该范围按默认值处理。
	if c.VadThreshold <= 0 || c.VadThreshold > 1 {
		c.VadThreshold = 0.5
	}
	if c.Disk == "" {
		c.Disk = "speaker"
	}
	if c.Dir == "" {
		c.Dir = "speakers"
	}

	return c
}

// ModelPath 返回声纹模型文件的完整路径。
func (c Config) ModelPath() string {
	return filepath.Join(c.ModelDir, c.Model)
}
