// Package speaker 定义说话人声纹模块对外暴露的契约。
//
// 与 app/contracts/audio 保持一致：上层组件（控制器、Service）只依赖本包中的
// 接口，具体的 sherpa-onnx 实现由 app/services/speaker 提供并通过 IoC 容器绑定，
// 从而使业务逻辑可在无模型环境下用替身完成单元测试。
package speaker

// Binding 声纹服务在 IoC 容器中的绑定键。
const Binding = "koi.speaker.voiceprint"

// ModelStatus 描述声纹模型的加载状态，用于健康检查与前端提示。
type ModelStatus struct {
	// Error 为模型加载失败的原因，加载成功时为空字符串。
	Error string `json:"error,omitempty"`
	// Loaded 表示模型是否已就绪。
	Loaded bool `json:"loaded"`
	// Dim 声纹特征维度，模型未就绪时为 0。
	Dim int `json:"dim"`
	// Threshold 当前生效的检索相似度阈值。
	Threshold float32 `json:"threshold"`
}

// Feature 表示一次声纹提取的结果。
type Feature struct {
	// Vector 归一化前的声纹特征向量，维度由模型决定。
	Vector []float32 `json:"-"`
	// Dim 特征维度。
	Dim int `json:"dim"`
	// SampleRate 提取所使用的采样率。
	SampleRate int `json:"sample_rate"`
	// Duration 参与提取的音频时长（秒）。
	Duration float64 `json:"duration"`
	// ValidDuration 去除静音后的有效语音累计时长（秒）；
	// 未启用 VAD 时退化为整段音频时长。
	ValidDuration float64 `json:"valid_duration"`
}

// Match 表示一次声纹检索的命中结果。
type Match struct {
	// Name 命中的说话人标识，未命中时为空字符串。
	Name string `json:"name"`
	// Matched 是否命中。
	Matched bool `json:"matched"`
	// Score 与命中说话人的余弦相似度；未命中时为库中的最高相似度。
	Score float32 `json:"score"`
	// Threshold 本次检索使用的阈值。
	Threshold float32 `json:"threshold"`
}

// Voiceprint 声纹服务契约。
//
// 实现需保证所有方法并发安全：HTTP 请求会并发调用提取与检索。
type Voiceprint interface {
	// Ready 报告声纹模型是否已加载完成。
	Ready() bool

	// Status 返回模型加载状态。
	Status() ModelStatus

	// Dim 返回声纹特征维度，模型未就绪时返回 0。
	Dim() int

	// Threshold 返回默认的检索相似度阈值。
	Threshold() float32

	// Extract 从音频文件字节流中提取声纹特征。
	// 支持 WAV(PCM/IEEE Float) 容器，非 16kHz 单声道时自动混音并重采样。
	Extract(data []byte) (Feature, error)

	// Register 把一个说话人的若干条声纹注册进内存声纹库，覆盖同名旧数据。
	// vectors 为空时等价于 Unregister。
	Register(name string, vectors [][]float32) error

	// Unregister 从内存声纹库中移除指定说话人。
	Unregister(name string)

	// Reset 用给定的全量数据重建内存声纹库，用于服务启动或数据修复。
	Reset(all map[string][][]float32) error

	// Search 在声纹库中检索最相似的说话人。
	// threshold 小于等于 0 时使用配置的默认阈值。
	Search(vector []float32, threshold float32) (Match, error)

	// Verify 校验给定声纹是否属于指定说话人（1:1 比对）。
	Verify(name string, vector []float32, threshold float32) (Match, error)

	// Similarity 计算两个声纹向量的余弦相似度。
	Similarity(left, right []float32) (float32, error)

	// Speakers 返回当前内存声纹库中的全部说话人标识。
	Speakers() []string

	// Close 释放服务持有的全部资源，用于进程优雅退出。
	Close() error
}
