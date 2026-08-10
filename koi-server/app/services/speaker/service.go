// Package speaker 实现基于 sherpa-onnx 的说话人声纹提取与检索服务。
//
// 设计要点：
//   - 服务只依赖 app/contracts/speaker 中的接口与注入的依赖，不感知 HTTP 与 ORM；
//   - 模型在构造时异步加载，避免阻塞应用启动，首次调用最多等待 LoadTimeout；
//   - 声纹检索委托给 sherpa-onnx 的 SpeakerEmbeddingManager，
//     同时在本地保留一份向量副本用于计算并返回相似度分值。
package speaker

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/goravel/framework/contracts/log"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	contracts "koi-server/app/contracts/speaker"
)

// 服务可能返回的错误。
var (
	// ErrServiceClosed 服务已关闭。
	ErrServiceClosed = errors.New("speaker: service is closed")
	// ErrModelTimeout 等待声纹模型加载超时。
	ErrModelTimeout = errors.New("speaker: model loading timeout")
	// ErrStreamCreate 创建声纹提取流失败。
	ErrStreamCreate = errors.New("speaker: failed to create embedding stream")
	// ErrExtractFailed 声纹特征计算失败。
	ErrExtractFailed = errors.New("speaker: failed to compute embedding")
	// ErrEmptyName 说话人标识为空。
	ErrEmptyName = errors.New("speaker: empty speaker name")
	// ErrEmptyVector 声纹特征向量为空。
	ErrEmptyVector = errors.New("speaker: empty embedding vector")
	// ErrDimMismatch 声纹特征维度与模型不一致。
	ErrDimMismatch = errors.New("speaker: embedding dimension mismatch")
	// ErrAudioTooShort 有效音频时长不足。
	ErrAudioTooShort = errors.New("speaker: audio is too short")
	// ErrValidSpeechTooShort 有效语音（去除静音）时长不足。
	ErrValidSpeechTooShort = errors.New("speaker: valid speech is too short")
)

// VADSampleRate 是 Silero VAD 模型固定要求的采样率。
const VADSampleRate = 16000

// Dependencies 声纹服务的外部依赖，以接口注入，便于替换与单元测试。
type Dependencies struct {
	Log log.Log
}

// validate 校验依赖完整性。
func (d Dependencies) validate() error {
	if d.Log == nil {
		return errors.New("speaker: log dependency is required")
	}

	return nil
}

// Service 是 contracts.Voiceprint 的 sherpa-onnx 实现。
type Service struct {
	cfg Config
	log log.Log

	// loaded 在模型加载流程结束（无论成败）后关闭，作为就绪信号。
	loaded  chan struct{}
	loadErr error
	dim     int

	// mu 保护 extractor / manager / vectors / closed，
	// sherpa 的 C 对象不保证并发安全，统一串行化访问。
	mu        sync.RWMutex
	extractor *sherpa.SpeakerEmbeddingExtractor
	manager   *sherpa.SpeakerEmbeddingManager
	// vad 语音活动检测器，用于统计有效语音时长；为 nil 表示未启用。
	vad *sherpa.VoiceActivityDetector
	// vectors 保存已注册说话人的向量副本，用于计算相似度分值。
	vectors map[string][][]float32
	closed  bool
}

// 编译期确认实现满足契约。
var _ contracts.Voiceprint = (*Service)(nil)

// NewService 构造声纹服务并异步加载模型。
func NewService(cfg Config, deps Dependencies) (*Service, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}

	s := &Service{
		cfg:     cfg,
		log:     deps.Log,
		loaded:  make(chan struct{}),
		vectors: make(map[string][][]float32),
	}

	go s.load()

	return s, nil
}

// load 加载声纹模型并初始化声纹库，仅在构造时执行一次。
func (s *Service) load() {
	defer close(s.loaded)

	modelPath := s.cfg.ModelPath()

	extractor := sherpa.NewSpeakerEmbeddingExtractor(&sherpa.SpeakerEmbeddingExtractorConfig{
		Model:      modelPath,
		NumThreads: s.cfg.NumThreads,
		Debug:      boolToInt(s.cfg.Debug),
		Provider:   s.cfg.Provider,
	})
	if extractor == nil {
		s.loadErr = fmt.Errorf("speaker: failed to load embedding model %q", modelPath)
		s.log.Error(s.loadErr.Error())

		return
	}

	dim := extractor.Dim()
	if dim <= 0 {
		sherpa.DeleteSpeakerEmbeddingExtractor(extractor)
		s.loadErr = fmt.Errorf("speaker: invalid embedding dim %d from model %q", dim, modelPath)
		s.log.Error(s.loadErr.Error())

		return
	}

	manager := sherpa.NewSpeakerEmbeddingManager(dim)
	if manager == nil {
		sherpa.DeleteSpeakerEmbeddingExtractor(extractor)
		s.loadErr = errors.New("speaker: failed to create embedding manager")
		s.log.Error(s.loadErr.Error())

		return
	}

	s.mu.Lock()
	// 服务在模型加载期间被关闭时，立即回收刚创建的资源。
	if s.closed {
		s.mu.Unlock()
		sherpa.DeleteSpeakerEmbeddingManager(manager)
		sherpa.DeleteSpeakerEmbeddingExtractor(extractor)

		return
	}
	s.extractor = extractor
	s.manager = manager
	s.dim = dim
	s.mu.Unlock()

	s.log.Info(fmt.Sprintf("speaker: embedding model loaded from %s, dim=%d", modelPath, dim))

	// 语音活动检测模型为可选项：配置缺失或加载失败时退化为“整段音频时长”，
	// 不影响声纹提取本身，仅会失去有效语音时长的精确统计。
	if s.cfg.VadModel != "" {
		vad := sherpa.NewVoiceActivityDetector(&sherpa.VadModelConfig{
			SileroVad: sherpa.SileroVadModelConfig{
				Model:              s.cfg.VadModel,
				Threshold:          s.cfg.VadThreshold,
				MinSilenceDuration: 0.5,
				MinSpeechDuration:  0.25,
				WindowSize:         512,
			},
			SampleRate: VADSampleRate,
			NumThreads: s.cfg.NumThreads,
			Provider:   s.cfg.Provider,
			Debug:      boolToInt(s.cfg.Debug),
		}, 60)
		if vad == nil {
			s.log.Warning(fmt.Sprintf("speaker: failed to load VAD model %q, valid-speech detection disabled", s.cfg.VadModel))

			return
		}

		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			sherpa.DeleteVoiceActivityDetector(vad)

			return
		}
		s.vad = vad
		s.mu.Unlock()

		s.log.Info(fmt.Sprintf("speaker: VAD model loaded from %s", s.cfg.VadModel))
	}
}

// wait 阻塞等待模型加载完成，超时或加载失败时返回错误。
func (s *Service) wait() error {
	select {
	case <-s.loaded:
	case <-time.After(s.cfg.LoadTimeout):
		return ErrModelTimeout
	}

	if s.loadErr != nil {
		return s.loadErr
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return ErrServiceClosed
	}

	return nil
}

// Ready 报告声纹模型是否已加载完成。
func (s *Service) Ready() bool {
	select {
	case <-s.loaded:
	default:
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.loadErr == nil && !s.closed && s.extractor != nil
}

// Status 返回模型加载状态。
func (s *Service) Status() contracts.ModelStatus {
	status := contracts.ModelStatus{
		Loaded:    s.Ready(),
		Threshold: s.cfg.Threshold,
	}

	s.mu.RLock()
	status.Dim = s.dim
	s.mu.RUnlock()

	// loadErr 仅在 loaded 关闭前写入，确认信号后读取才是安全的。
	select {
	case <-s.loaded:
		if s.loadErr != nil {
			status.Error = s.loadErr.Error()
		}
	default:
	}

	return status
}

// Dim 返回声纹特征维度，模型未就绪时返回 0。
func (s *Service) Dim() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.dim
}

// Threshold 返回默认的检索相似度阈值。
func (s *Service) Threshold() float32 {
	return s.cfg.Threshold
}

// Extract 从音频文件字节流中提取声纹特征。
func (s *Service) Extract(data []byte) (contracts.Feature, error) {
	if err := s.wait(); err != nil {
		return contracts.Feature{}, err
	}

	pcm, err := DecodeWAV(data)
	if err != nil {
		return contracts.Feature{}, err
	}

	pcm = Resample(pcm, s.cfg.SampleRate)

	duration := pcm.Duration()
	if duration < s.cfg.MinDuration {
		return contracts.Feature{}, fmt.Errorf("%w: %.2fs < %.2fs", ErrAudioTooShort, duration, s.cfg.MinDuration)
	}

	// 超长音频只取前 MaxDuration 秒，避免单次请求占用过多 CPU。
	if duration > s.cfg.MaxDuration {
		limit := int(s.cfg.MaxDuration * float64(pcm.SampleRate))
		pcm.Samples = pcm.Samples[:limit]
		duration = pcm.Duration()
	}

	// 检测有效语音（去除静音）累计时长，用于注册时校验语音是否充足。
	validDuration := s.detectSpeechDuration(pcm)

	vector, err := s.compute(pcm)
	if err != nil {
		return contracts.Feature{}, err
	}

	return contracts.Feature{
		Vector:        vector,
		Dim:           len(vector),
		SampleRate:    pcm.SampleRate,
		Duration:      duration,
		ValidDuration: validDuration,
	}, nil
}

// detectSpeechDuration 返回音频中有效语音（去除静音段）的累计时长（秒）。
//
// 在锁保护下运行 VAD，因此与本服务的其他 C 对象访问串行化；若未配置 VAD
// 模型或服务已关闭，则退化为整段音频时长，保证调用方始终能拿到一个数值。
func (s *Service) detectSpeechDuration(pcm PCM) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.vad == nil {
		return pcm.Duration()
	}

	s.vad.Reset()

	// VAD 模型固定要求 16k 单声道输入，先重采样到目标采样率。
	samples := Resample(pcm, VADSampleRate).Samples
	if len(samples) == 0 {
		return pcm.Duration()
	}

	// 以 10ms 为帧送入检测器，每送一帧就取走已完成的语音段。
	frameSize := VADSampleRate / 100
	var total float64
	for i := 0; i < len(samples); i += frameSize {
		end := i + frameSize
		var frame []float32
		if end > len(samples) {
			frame = samples[i:]
		} else {
			frame = samples[i:end]
		}

		s.vad.AcceptWaveform(frame)
		for !s.vad.IsEmpty() {
			seg := s.vad.Front()
			total += float64(len(seg.Samples)) / float64(VADSampleRate)
			s.vad.Pop()
		}
	}

	// 收尾，把缓冲区里最后一段语音也取出来。
	s.vad.Flush()
	for !s.vad.IsEmpty() {
		seg := s.vad.Front()
		total += float64(len(seg.Samples)) / float64(VADSampleRate)
		s.vad.Pop()
	}

	return total
}

// compute 调用 sherpa-onnx 提取声纹特征。
func (s *Service) compute(pcm PCM) ([]float32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.extractor == nil {
		return nil, ErrServiceClosed
	}

	stream := s.extractor.CreateStream()
	if stream == nil {
		return nil, ErrStreamCreate
	}
	defer sherpa.DeleteOnlineStream(stream)

	stream.AcceptWaveform(pcm.SampleRate, pcm.Samples)
	stream.InputFinished()

	if !s.extractor.IsReady(stream) {
		return nil, ErrAudioTooShort
	}

	vector := s.extractor.Compute(stream)
	if len(vector) == 0 {
		return nil, ErrExtractFailed
	}

	return vector, nil
}

// Register 把一个说话人的若干条声纹注册进内存声纹库，覆盖同名旧数据。
func (s *Service) Register(name string, vectors [][]float32) error {
	if name == "" {
		return ErrEmptyName
	}
	if err := s.wait(); err != nil {
		return err
	}

	dim := s.Dim()
	valid := make([][]float32, 0, len(vectors))
	for _, vector := range vectors {
		if len(vector) == 0 {
			continue
		}
		if len(vector) != dim {
			return fmt.Errorf("%w: got %d, want %d", ErrDimMismatch, len(vector), dim)
		}
		valid = append(valid, vector)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.manager == nil {
		return ErrServiceClosed
	}

	// 先移除旧数据，保证重复注册时是覆盖而非追加。
	if s.manager.Contains(name) {
		s.manager.Remove(name)
	}
	delete(s.vectors, name)

	if len(valid) == 0 {
		return nil
	}

	if !s.manager.RegisterV(name, valid) {
		return fmt.Errorf("speaker: failed to register voiceprint for %q", name)
	}
	s.vectors[name] = valid

	return nil
}

// Unregister 从内存声纹库中移除指定说话人。
func (s *Service) Unregister(name string) {
	if name == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.manager == nil {
		return
	}

	if s.manager.Contains(name) {
		s.manager.Remove(name)
	}
	delete(s.vectors, name)
}

// Reset 用给定的全量数据重建内存声纹库。
func (s *Service) Reset(all map[string][][]float32) error {
	if err := s.wait(); err != nil {
		return err
	}

	s.mu.Lock()
	if s.closed || s.manager == nil {
		s.mu.Unlock()

		return ErrServiceClosed
	}

	for _, name := range s.manager.AllSpeakers() {
		s.manager.Remove(name)
	}
	s.vectors = make(map[string][][]float32, len(all))
	s.mu.Unlock()

	for name, vectors := range all {
		if err := s.Register(name, vectors); err != nil {
			return err
		}
	}

	return nil
}

// Search 在声纹库中检索最相似的说话人。
func (s *Service) Search(vector []float32, threshold float32) (contracts.Match, error) {
	if len(vector) == 0 {
		return contracts.Match{}, ErrEmptyVector
	}
	if err := s.wait(); err != nil {
		return contracts.Match{}, err
	}
	if dim := s.Dim(); len(vector) != dim {
		return contracts.Match{}, fmt.Errorf("%w: got %d, want %d", ErrDimMismatch, len(vector), dim)
	}

	threshold = s.resolveThreshold(threshold)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.manager == nil {
		return contracts.Match{}, ErrServiceClosed
	}

	name := s.manager.Search(vector, threshold)

	match := contracts.Match{
		Name:      name,
		Matched:   name != "",
		Threshold: threshold,
	}

	if name != "" {
		match.Score = bestScore(s.vectors[name], vector)

		return match, nil
	}

	// 未命中时返回库中的最高相似度，便于调用方判断阈值是否需要调整。
	for _, registered := range s.vectors {
		if score := bestScore(registered, vector); score > match.Score {
			match.Score = score
		}
	}

	return match, nil
}

// Verify 校验给定声纹是否属于指定说话人（1:1 比对）。
func (s *Service) Verify(name string, vector []float32, threshold float32) (contracts.Match, error) {
	if name == "" {
		return contracts.Match{}, ErrEmptyName
	}
	if len(vector) == 0 {
		return contracts.Match{}, ErrEmptyVector
	}
	if err := s.wait(); err != nil {
		return contracts.Match{}, err
	}
	if dim := s.Dim(); len(vector) != dim {
		return contracts.Match{}, fmt.Errorf("%w: got %d, want %d", ErrDimMismatch, len(vector), dim)
	}

	threshold = s.resolveThreshold(threshold)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed || s.manager == nil {
		return contracts.Match{}, ErrServiceClosed
	}

	matched := s.manager.Verify(name, vector, threshold)

	return contracts.Match{
		Name:      name,
		Matched:   matched,
		Score:     bestScore(s.vectors[name], vector),
		Threshold: threshold,
	}, nil
}

// Similarity 计算两个声纹向量的余弦相似度。
func (s *Service) Similarity(left, right []float32) (float32, error) {
	if len(left) == 0 || len(right) == 0 {
		return 0, ErrEmptyVector
	}
	if len(left) != len(right) {
		return 0, fmt.Errorf("%w: %d vs %d", ErrDimMismatch, len(left), len(right))
	}

	return cosine(left, right), nil
}

// Speakers 返回当前内存声纹库中的全部说话人标识。
func (s *Service) Speakers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed || s.manager == nil {
		return nil
	}

	names := s.manager.AllSpeakers()
	sort.Strings(names)

	return names
}

// Close 释放服务持有的全部资源。
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.manager != nil {
		sherpa.DeleteSpeakerEmbeddingManager(s.manager)
		s.manager = nil
	}
	if s.extractor != nil {
		sherpa.DeleteSpeakerEmbeddingExtractor(s.extractor)
		s.extractor = nil
	}
	if s.vad != nil {
		sherpa.DeleteVoiceActivityDetector(s.vad)
		s.vad = nil
	}
	s.vectors = nil

	return nil
}

// resolveThreshold 归一化阈值：非法输入回落到配置的默认值。
func (s *Service) resolveThreshold(threshold float32) float32 {
	if threshold <= 0 || threshold >= 1 {
		return s.cfg.Threshold
	}

	return threshold
}

// bestScore 返回目标向量与一组已注册向量之间的最高余弦相似度。
func bestScore(registered [][]float32, target []float32) float32 {
	var best float32
	for _, vector := range registered {
		if len(vector) != len(target) {
			continue
		}
		if score := cosine(vector, target); score > best {
			best = score
		}
	}

	return best
}

// cosine 计算两个等长向量的余弦相似度。
func cosine(left, right []float32) float32 {
	var dot, leftNorm, rightNorm float64
	for i := range left {
		l := float64(left[i])
		r := float64(right[i])
		dot += l * r
		leftNorm += l * l
		rightNorm += r * r
	}

	denominator := math.Sqrt(leftNorm) * math.Sqrt(rightNorm)
	if denominator == 0 {
		return 0
	}

	return float32(dot / denominator)
}

// boolToInt 把布尔值转换为 sherpa 配置所需的 0/1。
func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}
