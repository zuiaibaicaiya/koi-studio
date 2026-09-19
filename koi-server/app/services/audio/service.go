// Package audio 实现基于 sherpa-onnx 流式 Zipformer 的实时语音转写服务。
//
// 设计要点：
//   - 服务只依赖 app/contracts/audio 中的接口与注入的依赖，不感知 Socket.IO；
//   - Socket.IO 事件协程只做「复制 + 入队」，解码在每个客户端专属协程中进行；
//   - 转写结果通过 Publisher 发布，录音归档通过 RecordingArchiver 交给队列；
//   - 每句结束后立即入库（meeting_transcripts），含相对时间戳与说话人信息。
package audio

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/goravel/framework/contracts/filesystem"
	"github.com/goravel/framework/contracts/log"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	contracts "koi-server/app/contracts/audio"
	contractsspeaker "koi-server/app/contracts/speaker"
	"koi-server/app/models"
	"koi-server/app/services"
	"koi-server/app/services/transcript"
)

const (
	// tempFileSuffix 会话录音临时文件后缀。
	tempFileSuffix = ".pcm.tmp"
	// modelPollInterval 等待模型加载时的轮询间隔。
	modelPollInterval = 100 * time.Millisecond
	// finalPushTimeout 结束帧入队的最长等待时间。
	finalPushTimeout = time.Second
	// shutdownTimeout 关闭服务时等待单个会话收尾的最长时间。
	shutdownTimeout = 5 * time.Second
	// maxSegmentDurationMs 单次声纹注册可截取的最长音频时长（毫秒）。
	maxSegmentDurationMs = 60 * 1000
)

// 服务可能返回的错误。
var (
	ErrEmptyClientID = errors.New("audio: empty client id")
	ErrServiceClosed = errors.New("audio: service is closed")
	ErrModelTimeout  = errors.New("audio: model loading timeout")
	ErrStreamCreate  = errors.New("audio: failed to create online stream")
	// ErrSessionNotFound 客户端当前没有活跃的转写会话。
	ErrSessionNotFound = errors.New("audio: session not found")
	// ErrRecordingUnavailable 会话录音不可用（临时文件缺失或尚未写入）。
	ErrRecordingUnavailable = errors.New("audio: recording buffer unavailable")
	// ErrInvalidSegmentRange 请求读取的音频时间段非法。
	ErrInvalidSegmentRange = errors.New("audio: invalid segment range")
)

// Dependencies 转写服务的外部依赖，全部以接口注入，便于替换与单元测试。
type Dependencies struct {
	Log       log.Log
	Storage   filesystem.Driver
	Publisher contracts.Publisher
	Archiver  contracts.RecordingArchiver
	// --- 新增依赖 ---
	SessionMgr        *services.MeetingSessionManager
	TranscriptService *services.MeetingTranscriptService
	SpeakerService    *services.SpeakerService
	Voiceprint        contractsspeaker.Voiceprint
	// SpeakerVoiceprint 说话人声纹业务服务，用于动态注册说话人时写入声纹并刷新内存库。
	SpeakerVoiceprint *services.SpeakerVoiceprintService
	// MeetingService 会议服务，用于把动态注册的说话人写回会议的说话人列表。
	MeetingService *services.MeetingService
}

// validate 校验依赖完整性。
func (d Dependencies) validate() error {
	switch {
	case d.Log == nil:
		return errors.New("audio: log dependency is required")
	case d.Storage == nil:
		return errors.New("audio: storage dependency is required")
	case d.Publisher == nil:
		return errors.New("audio: publisher dependency is required")
	case d.Archiver == nil:
		return errors.New("audio: archiver dependency is required")
	}
	return nil
}

// Service 是 contracts/audio.Transcriber 的默认实现。
type Service struct {
	cfg  Config
	deps Dependencies

	mu         sync.RWMutex
	baseConfig sherpa.OnlineRecognizerConfig
	shared     *sherpa.OnlineRecognizer
	retired    []*sherpa.OnlineRecognizer
	loaded     bool
	loadErr    error
	hotwords   string
	score      float32
	sessions   map[string]*session
	closed     bool
}

// 编译期确认实现满足契约。
var _ contracts.Transcriber = (*Service)(nil)

// NewService 构造转写服务，并在后台异步预加载语音模型。
func NewService(cfg Config, deps Dependencies) (*Service, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}

	cfg = cfg.normalized()
	s := &Service{
		cfg:      cfg,
		deps:     deps,
		score:    cfg.HotwordsScore,
		sessions: make(map[string]*session),
	}
	s.baseConfig = s.buildRecognizerConfig()

	if err := deps.Storage.MakeDirectory("."); err != nil {
		deps.Log.Warning(fmt.Sprintf("audio: failed to prepare storage directory: %v", err))
	}
	s.cleanupOrphanedTempFiles()

	go s.preloadModel()

	return s, nil
}

// Ready 报告模型是否已加载完成。
func (s *Service) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loaded
}

// Status 返回模型加载状态。
func (s *Service) Status() contracts.ModelStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := contracts.ModelStatus{Loaded: s.loaded}
	if s.loadErr != nil {
		status.Error = s.loadErr.Error()
	}
	return status
}

// Push 接收一帧 PCM 数据，flag 为 0 表示结束帧。
func (s *Service) Push(clientID string, pcm []byte, flag int) error {
	if clientID == "" {
		return ErrEmptyClientID
	}

	sess, err := s.acquire(clientID)
	if err != nil {
		return err
	}
	sess.touch()

	// Socket.IO 底层缓冲可能被复用，异步处理必须持有独立副本。
	data := make([]byte, len(pcm))
	copy(data, pcm)
	frame := chunk{data: data, last: flag == 0}

	if !frame.last {
		// 常规帧允许丢弃：队列积压说明解码已跟不上，丢帧优于阻塞事件循环。
		select {
		case sess.chunks <- frame:
		default:
			s.deps.Log.Debug(fmt.Sprintf("audio: queue full, dropping frame for client %s", clientID))
		}
		return nil
	}

	// 结束帧不可丢弃，否则会话无法收尾。短暂等待后退化为直接触发收尾。
	select {
	case sess.chunks <- frame:
	case <-sess.done:
	case <-time.After(finalPushTimeout):
		s.deps.Log.Warning(fmt.Sprintf("audio: enqueue final frame timed out for client %s, stopping session", clientID))
		sess.requestStop()
	}
	return nil
}

// Release 释放客户端会话。
func (s *Service) Release(clientID string) {
	s.mu.RLock()
	sess, ok := s.sessions[clientID]
	s.mu.RUnlock()

	if ok {
		sess.requestStop()
	}
}

// Transcript 返回客户端当前累积的完整转写文本。
func (s *Service) Transcript(clientID string) string {
	s.mu.RLock()
	sess, ok := s.sessions[clientID]
	s.mu.RUnlock()

	if !ok {
		return ""
	}
	return sess.text()
}

// SetHotwords 设置热词并热替换识别器。
//
// 传空字符串表示清空热词：重建一个不带热词的识别器，确保「没有热词库的会议
// 不残留上一位会议的热词」。热词未变化时跳过重建，避免不必要的开销。
func (s *Service) SetHotwords(hotwords string, score float32) error {
	if score <= 0 {
		score = s.cfg.HotwordsScore
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrServiceClosed
	}

	// 热词与权重均未变化时无需重建识别器。
	if hotwords == s.hotwords && score == s.score {
		return nil
	}

	s.hotwords = hotwords
	s.score = score

	// 模型尚未加载完成时仅记录热词，preloadModel 会用 s.hotwords 应用。
	if !s.loaded {
		return nil
	}

	cfg := s.baseConfig
	cfg.HotwordsBuf = hotwords
	cfg.HotwordsBufSize = len(hotwords)
	cfg.HotwordsScore = score

	recognizer := sherpa.NewOnlineRecognizer(&cfg)
	if recognizer == nil {
		return errors.New("audio: failed to create recognizer with hotwords")
	}

	// 旧识别器可能仍被在途解码引用，推迟到 Close 时统一释放。
	if s.shared != nil {
		s.retired = append(s.retired, s.shared)
	}
	s.shared = recognizer

	// 交由各会话的工作协程在两帧之间切换，避免与正在进行的解码竞争。
	for _, sess := range s.sessions {
		sess.setPending(recognizer)
	}

	if hotwords == "" {
		s.deps.Log.Info(fmt.Sprintf("audio: hotwords cleared for %d session(s)", len(s.sessions)))
	} else {
		s.deps.Log.Info(fmt.Sprintf("audio: hotwords updated for %d session(s), score %.1f", len(s.sessions), score))
	}
	return nil
}

// Hotwords 返回当前热词及权重。
func (s *Service) Hotwords() (string, float32) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hotwords, s.score
}

// CleanupInactive 回收空闲超时的会话。
func (s *Service) CleanupInactive(timeout time.Duration) int {
	if timeout <= 0 {
		return 0
	}

	now := time.Now()
	var stale []*session

	s.mu.RLock()
	for _, sess := range s.sessions {
		if now.Sub(sess.activityAt()) > timeout {
			stale = append(stale, sess)
		}
	}
	s.mu.RUnlock()

	for _, sess := range stale {
		s.deps.Log.Info(fmt.Sprintf("audio: reclaiming idle session %s", sess.clientID))
		sess.requestStop()
	}
	return len(stale)
}

// Close 停止所有会话并释放模型资源。
func (s *Service) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	sessions := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()

	for _, sess := range sessions {
		sess.requestStop()
	}
	for _, sess := range sessions {
		select {
		case <-sess.done:
		case <-time.After(shutdownTimeout):
			s.deps.Log.Warning(fmt.Sprintf("audio: session %s did not finish within %s", sess.clientID, shutdownTimeout))
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, recognizer := range s.retired {
		sherpa.DeleteOnlineRecognizer(recognizer)
	}
	s.retired = nil
	if s.shared != nil {
		sherpa.DeleteOnlineRecognizer(s.shared)
		s.shared = nil
	}
	s.loaded = false
	return nil
}

// buildRecognizerConfig 依据配置组装识别器参数。
//
// 显式关闭 sherpa-onnx 内置端点检测：其端点必须靠 OnlineStream.Reset 重新武装，
// 而 Reset 会保留上一句尾部尚未被 chunk 消费的音频，污染下一句的特征上下文
// （首字识别质量下降 + 逐字时间戳整体偏晚）。断句改由本服务在连续识别流上
// 按尾部静音自行判定，见 decode 与 Config.SegmentSilenceMs。
func (s *Service) buildRecognizerConfig() sherpa.OnlineRecognizerConfig {
	return sherpa.OnlineRecognizerConfig{
		FeatConfig: sherpa.FeatureConfig{
			SampleRate: s.cfg.SampleRate,
			FeatureDim: s.cfg.FeatureDim,
		},
		ModelConfig: sherpa.OnlineModelConfig{
			Transducer: sherpa.OnlineTransducerModelConfig{
				Encoder: s.cfg.modelPath(s.cfg.Encoder),
				Decoder: s.cfg.modelPath(s.cfg.Decoder),
				Joiner:  s.cfg.modelPath(s.cfg.Joiner),
			},
			Tokens:     s.cfg.modelPath(s.cfg.Tokens),
			NumThreads: s.cfg.NumThreads,
			Provider:   s.cfg.Provider,
		},
		DecodingMethod: s.cfg.DecodingMethod,
		MaxActivePaths: s.cfg.MaxActivePaths,
		HotwordsScore:  s.cfg.HotwordsScore,
		HotwordsFile:   s.cfg.modelPath(s.cfg.HotwordsFile),
		EnableEndpoint: 0,
	}
}

// preloadModel 异步预加载模型，避免首个客户端接入时长时间等待。
func (s *Service) preloadModel() {
	defer func() {
		if r := recover(); r != nil {
			s.mu.Lock()
			s.loadErr = fmt.Errorf("audio: model preload panicked: %v", r)
			s.mu.Unlock()
			s.deps.Log.Error(fmt.Sprintf("audio: model preload panicked: %v\n%s", r, debug.Stack()))
		}
	}()

	start := time.Now()
	s.deps.Log.Info("audio: preloading speech recognition model...")

	// 读取已在 SetHotwords 中登记的热词（可能在模型加载完成前被 join-meeting 设置）。
	s.mu.RLock()
	hotwords := s.hotwords
	score := s.score
	s.mu.RUnlock()

	cfg := s.baseConfig
	cfg.HotwordsBuf = hotwords
	cfg.HotwordsBufSize = len(hotwords)
	cfg.HotwordsScore = score
	recognizer := sherpa.NewOnlineRecognizer(&cfg)

	s.mu.Lock()
	defer s.mu.Unlock()

	if recognizer == nil {
		s.loadErr = errors.New("audio: failed to create shared recognizer")
		s.deps.Log.Error(s.loadErr.Error())
		return
	}
	if s.closed {
		sherpa.DeleteOnlineRecognizer(recognizer)
		return
	}

	s.shared = recognizer
	s.loaded = true
	s.deps.Log.Info(fmt.Sprintf("audio: model preloaded in %.2fs", time.Since(start).Seconds()))
}

// waitModel 等待模型就绪，超时或加载失败时返回错误。
func (s *Service) waitModel() error {
	deadline := time.Now().Add(s.cfg.LoadTimeout)
	for {
		s.mu.RLock()
		loaded, loadErr := s.loaded, s.loadErr
		s.mu.RUnlock()

		if loaded {
			return nil
		}
		if loadErr != nil {
			return loadErr
		}
		if time.Now().After(deadline) {
			return ErrModelTimeout
		}
		time.Sleep(modelPollInterval)
	}
}

// acquire 获取（必要时创建）客户端会话。
func (s *Service) acquire(clientID string) (*session, error) {
	s.mu.RLock()
	closed := s.closed
	sess, ok := s.sessions[clientID]
	s.mu.RUnlock()

	if closed {
		return nil, ErrServiceClosed
	}
	if ok {
		return sess, nil
	}

	// 在锁外等待模型加载，避免阻塞其它客户端。
	if err := s.waitModel(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, ErrServiceClosed
	}
	// 双重检查，防止并发重复创建。
	if sess, ok := s.sessions[clientID]; ok {
		return sess, nil
	}

	stream := sherpa.NewOnlineStream(s.shared)
	if stream == nil {
		return nil, ErrStreamCreate
	}

	// 临时文件创建失败不影响转写，仅退化为不落盘模式。
	tempFile, tempName, err := s.createTempFile(clientID)
	if err != nil {
		s.deps.Log.Warning(fmt.Sprintf("audio: temp file unavailable for client %s, recording disabled: %v", clientID, err))
	}

	sess = newSession(clientID, s.cfg.QueueSize, s.shared, stream, tempFile, tempName, s.cfg.SampleRate)
	s.sessions[clientID] = sess

	go s.work(sess)
	s.deps.Log.Info(fmt.Sprintf("audio: session started for client %s", clientID))

	return sess, nil
}

// SegmentPCM 读取客户端会话中 [startMs, endMs) 区间的原始 PCM（16bit 小端单声道）。
//
// 用于实时转写中「框选文字 → 动态注册说话人」：会话音频从第一帧起按序写入
// 临时文件，写入顺序与转写结果的时间戳（相对会话音频起点）严格一致，因此
// 可直接按时间偏移定位字节区间。读取使用 ReadAt，不移动写游标，与工作协程并发安全。
func (s *Service) SegmentPCM(clientID string, startMs, endMs int64) ([]byte, error) {
	if clientID == "" {
		return nil, ErrEmptyClientID
	}
	if startMs < 0 {
		startMs = 0
	}
	if endMs <= startMs {
		return nil, ErrInvalidSegmentRange
	}

	s.mu.RLock()
	sess, ok := s.sessions[clientID]
	s.mu.RUnlock()

	if !ok || sess == nil {
		return nil, ErrSessionNotFound
	}
	if sess.tempFile == nil {
		return nil, ErrRecordingUnavailable
	}

	// 单次截取长度设上限：声纹提取无需超长音频，同时避免大内存分配。
	if endMs-startMs > maxSegmentDurationMs {
		endMs = startMs + maxSegmentDurationMs
	}

	startByte := startMs * int64(s.cfg.SampleRate) / 1000 * bytesPerSample
	endByte := endMs * int64(s.cfg.SampleRate) / 1000 * bytesPerSample
	size := endByte - startByte
	if size <= 0 {
		return nil, ErrInvalidSegmentRange
	}

	buf := make([]byte, size)
	n, err := sess.tempFile.ReadAt(buf, startByte)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("audio: failed to read segment for client %s: %w", clientID, err)
	}
	if n <= 0 {
		return nil, ErrRecordingUnavailable
	}

	return buf[:n], nil
}

// work 是客户端专属的转写工作协程，把解码开销移出 Socket.IO 事件循环。
func (s *Service) work(sess *session) {
	defer func() {
		if r := recover(); r != nil {
			s.deps.Log.Error(fmt.Sprintf("audio: worker panicked for client %s: %v\n%s", sess.clientID, r, debug.Stack()))
			s.discard(sess)
		}
		close(sess.done)
	}()

	for {
		select {
		case <-sess.stop:
			s.finalize(sess)
			return
		case frame := <-sess.chunks:
			s.applyPending(sess)
			if frame.last {
				// 结束帧通常不带音频（客户端发空 buffer 作为收尾标记），
				// 但若带音频也一并消费，避免丢掉最后一段。
				if len(frame.data) > 0 {
					s.consume(sess, frame.data)
				}
				s.finalize(sess)
				return
			}
			s.consume(sess, frame.data)
		}
	}
}

// applyPending 在两帧之间安全切换到新的识别器（热词变更）。
func (s *Service) applyPending(sess *session) {
	recognizer := sess.takePending()
	if recognizer == nil {
		return
	}

	// 热词切换需要重建识别流，累积文本会从头开始：先把当前未提交的文本
	// 按旧识别器定稿提交，避免这一段文本随流重建而丢失。
	s.commitUtterance(sess)

	stream := sherpa.NewOnlineStream(recognizer)
	if stream == nil {
		s.deps.Log.Error(fmt.Sprintf("audio: failed to rebuild stream for client %s during hotwords update", sess.clientID))
		return
	}

	if sess.stream != nil {
		sherpa.DeleteOnlineStream(sess.stream)
	}
	sess.stream = stream
	sess.recognizer = recognizer
	// 新识别器上是一条全新的识别流：累积文本、token 时间轴都从头开始。
	sess.resetStreamTracking()
}

// consume 落盘并解码一帧音频。
func (s *Service) consume(sess *session, data []byte) {
	if len(data) == 0 {
		return
	}

	// 累计采样计数，用于相对时间戳计算。16bit PCM = 2 bytes per sample
	sess.addSamples(len(data) / 2)

	if sess.tempFile != nil {
		// 不逐帧 fsync：由操作系统统一刷盘，显著降低高频写入的 I/O 开销。
		if _, err := sess.tempFile.Write(data); err != nil {
			s.deps.Log.Warning(fmt.Sprintf("audio: failed to write temp file for client %s: %v", sess.clientID, err))
		}
	}

	// 缓冲当前语音段的 PCM，用于说话人识别。
	sess.collectUtterancePCM(data)

	sess.samples = PCMToSamples(data, sess.samples)
	// 语音段尚未开始时用帧能量检测真实语音起点，供时间戳对齐音频实际时间。
	sess.detectVoiceStart()
	s.decode(sess)
}

// decode 把采样点送入识别流，按批解码并下发中间结果，并在合适的时机断句。
//
// 断句（句子边界）由本服务自行判定，而不是使用 sherpa-onnx 的端点检测：
// 整场会话只使用一条连续识别流，任何 Reset 都会保留上一句尾部音频、破坏
// 下一句的时间戳原点与首字识别质量（详见 session.emittedTokens 的说明）。
//
// 断句条件（两者之一）：
//  1. 尾部静音：距最后一个已发射 token 的音频时长达到 SegmentSilenceMs；
//  2. 单句过长：当前语音段时长超过 MaxUtterance，强制断句避免结果迟迟不下发。
func (s *Service) decode(sess *session) {
	// 记录本次解码窗口（当前帧）的起点采样位置，并初始化识别流起点。
	// 流式模型需要前置上下文，token 时间戳以流起点为基准，
	// 据此可把 token 位置还原为会话时间轴上的绝对采样位置。
	sess.windowStartSample = sess.totalSamples - int64(len(sess.samples))
	if sess.utteranceStreamStart < 0 {
		sess.utteranceStreamStart = sess.windowStartSample
	}
	sess.stream.AcceptWaveform(s.cfg.SampleRate, sess.samples)
	sess.batch++

	// 攒批解码以降低 CPU 占用。
	if sess.batch%s.cfg.DecodeBatch == 0 {
		s.decodeBatch(sess)
		sess.batch = 0
	}

	if !sess.hasPendingText() {
		return
	}
	if sess.trailingSilenceMs() >= int64(s.cfg.SegmentSilenceMs) ||
		sess.currentOffsetMs()-sess.utteranceStartMs() > s.cfg.MaxUtterance.Milliseconds() {
		s.commitUtterance(sess)
	}
}

// decodeBatch 推进一次解码：增量记录 token 时间戳，并限流下发中间结果。
func (s *Service) decodeBatch(sess *session) {
	var result *sherpa.OnlineRecognizerResult
	for sess.recognizer.IsReady(sess.stream) {
		sess.recognizer.Decode(sess.stream)
		result = sess.recognizer.GetResult(sess.stream)
	}
	if result == nil {
		return
	}

	// 记录 token 对应的真实音频采样位置，供字级时间戳对齐音频。
	// 优先使用模型产出的 token 级时间戳，消除「token 被解码发现晚于
	// 其实际发音」带来的系统性时间漂移。
	if len(result.Tokens) > 0 {
		sess.trackTokens(result)
	}
	if result.Text == "" {
		return
	}
	sess.pendingRunes = len([]rune(result.Text))
	if !sess.hasPendingText() {
		// 本批没有新文本（累积文本已全部提交），不标记语句起始。
		return
	}

	// 文本出现时标记语音段起始。
	sess.markUtteranceStart()

	// 限流下发：文本无变化或距上次下发不足间隔时跳过。
	partial := sess.textSuffix(result.Text)
	if partial != "" && partial != sess.lastSentText && time.Since(sess.lastSentAt) > s.cfg.EmitInterval {
		sess.lastSentText = partial
		sess.lastSentAt = time.Now()
		s.publishIntermediate(sess, partial)
	}
}

// commitUtterance 输出一句已确定的文本，同时执行说话人识别、入库存储、发布增强结果。
//
// 文本与 token 都取自同一条连续识别流：文本 = 累积文本中尚未提交的后缀，
// token = emittedTokens 中本句的部分。两者共享同一时间轴，因此逐字时间戳
// 与音频严格对齐（与整段离线解码结果逐字一致），不会出现整体偏移或首字零宽。
func (s *Service) commitUtterance(sess *session) {
	result := sess.recognizer.GetResult(sess.stream)
	if result == nil {
		return
	}
	fullRunes := len([]rune(result.Text))
	text := sess.textSuffix(result.Text)
	tokens := sess.utteranceTokens()

	if text == "" {
		// 没有新文本：只推进游标并清理语句级状态，识别流与时间轴保持不变。
		sess.markCommitted(fullRunes)
		sess.resetUtterance()
		return
	}

	sess.markUtteranceStart()

	// 1. 计算时间戳
	endMs := sess.commitEndMs()
	// 2. 计算词级时间戳（基于 token 发射时的真实采样位置对齐音频，
	//    流模型不产出 token 时间戳时退化为按音频时长的近似对齐）。
	var wordTimestamps []models.WordTimestamp
	if len(tokens) > 0 {
		wordTimestamps = buildRealtimeWordTimestamps(text, tokens, s.cfg.SampleRate, endMs)
	}
	if len(wordTimestamps) == 0 {
		startMs := sess.utteranceStartMs()
		wordTimestamps = computeWordTimestamps(text, startMs, endMs)
	}
	// 句子起点不允许晚于第一个字的起点：能量检测出的语音起点可能因噪声或
	// 检测滞后而偏晚，若直接用它裁剪会把首字压成零宽区间（前端表现为点不中、
	// 高亮串字）。此处取两者的较早值，保证 startMs <= 首字起点。
	startMs := sess.utteranceStartMs()
	if len(wordTimestamps) > 0 && wordTimestamps[0].StartMs < startMs {
		startMs = wordTimestamps[0].StartMs
	}
	wordTimestampsJSON, _ := json.Marshal(wordTimestamps)

	// 3. 说话人识别
	speakerName, speakerID, speaker := s.identifySpeaker(sess)

	// 4. 入库存储
	s.storeTranscript(sess, text, startMs, endMs, wordTimestamps, speakerName, speakerID)

	// 5. 发布增强结果
	s.publishFinalResult(sess, text, startMs, endMs, speakerName, speakerID, speaker, string(wordTimestampsJSON))

	// 6. 单独推送说话人识别事件（客户端可据此更新说话人指示器）
	s.publishSpeakerIdentified(sess, speakerName, speakerID, speaker)

	// 7. 推进游标、累积文本并重置语句级状态。
	//    注意：这里**不做** recognizer.Reset —— 整场会话保持同一条连续识别流。
	sess.markCommitted(fullRunes)
	sess.appendTranscript(text + " ")
	sess.resetUtterance()
}

// identifySpeaker 从当前语音段提取声纹并在会议选择的说话人中检索。
//
// 返回说话人名称、ID与完整模型；语音段过短、未绑定会议或无匹配时返回"未知说话人"。
func (s *Service) identifySpeaker(sess *session) (string, *uint, *models.Speaker) {
	if !s.cfg.SpeakerIdentifyEnabled {
		return "未知说话人", nil, nil
	}

	if s.deps.Voiceprint == nil || !s.deps.Voiceprint.Ready() {
		return "未知说话人", nil, nil
	}

	// 获取会议上下文
	if s.deps.SessionMgr == nil {
		return "未知说话人", nil, nil
	}
	ctx := s.deps.SessionMgr.Context(sess.clientID)
	if ctx == nil {
		return "未知说话人", nil, nil
	}
	// 说话人列表可能在转写过程中被动态追加（框选文字注册新说话人），
	// 因此通过加锁的取值方法读取，避免与追加写入产生数据竞争。
	speakerIDs := ctx.SpeakerIDList()
	if len(speakerIDs) == 0 {
		return "未知说话人", nil, nil
	}

	// 检查语音段时长是否足够
	minDuration := s.cfg.SpeakerIdentifyMinDuration
	if sess.utteranceDuration() < minDuration {
		return "未知说话人", nil, nil
	}

	// 检查 PCM 缓冲是否超限
	maxBufferDuration := s.cfg.SpeakerIdentifyMaxBuffer
	if maxBufferDuration > 0 && sess.utteranceDuration() > maxBufferDuration {
		return "未知说话人", nil, nil
	}

	// 解析会议候选说话人：候选集即「已选择并关联到当前会议」的说话人，
	// 已删除或无效的 ID 会被跳过，不会进入比对范围。
	candidates := s.resolveMeetingSpeakers(speakerIDs)
	if len(candidates) == 0 {
		return "未知说话人", nil, nil
	}

	// 取出当前语音段的 PCM 数据
	pcmData := sess.flushUtterancePCM()
	if len(pcmData) < 1600 { // < 100ms at 16kHz 16bit
		return "未知说话人", nil, nil
	}

	// 转为 WAV 字节流
	wavData, err := PCMToWAV(pcmData, s.cfg.SampleRate)
	if err != nil {
		s.deps.Log.Warning(fmt.Sprintf("audio: failed to convert utterance to WAV for speaker ID: %v", err))
		return "未知说话人", nil, nil
	}

	// 提取声纹
	feature, err := s.deps.Voiceprint.Extract(wavData)
	if err != nil {
		s.deps.Log.Debug(fmt.Sprintf("audio: voiceprint extraction failed for client %s: %v", sess.clientID, err))
		return "未知说话人", nil, nil
	}

	// 1:N 检索：仅在会议候选说话人范围内检索。库中未关联本会议的说话人
	// 不参与比对——既不会被识别出来，也不会因相似度更高而挤掉正确候选，
	// 从根本上避免跨会议误识别。
	names := make([]string, len(candidates))
	for i := range candidates {
		names[i] = candidates[i].Name
	}
	match, err := s.deps.Voiceprint.SearchIn(feature.Vector, 0, names)
	if err != nil || !match.Matched {
		return "未知说话人", nil, nil
	}

	for i := range candidates {
		if candidates[i].Name == match.Name {
			speaker := candidates[i]
			return speaker.Name, &speaker.ID, &speaker
		}
	}

	return "未知说话人", nil, nil
}

// resolveMeetingSpeakers 把会议说话人ID列表解析为候选说话人模型。
// 已被删除或数据库中不存在的 ID 会被跳过并记录日志，保证候选集只包含有效说话人。
func (s *Service) resolveMeetingSpeakers(speakerIDs []uint) []models.Speaker {
	if s.deps.SpeakerService == nil {
		return nil
	}

	speakers := make([]models.Speaker, 0, len(speakerIDs))
	for _, id := range speakerIDs {
		speaker, err := s.deps.SpeakerService.GetSpeakerById(int(id))
		if err != nil {
			s.deps.Log.Debug(fmt.Sprintf("audio: meeting speaker %d unavailable, excluded from candidates: %v", id, err))
			continue
		}
		speakers = append(speakers, speaker)
	}

	return speakers
}

// storeTranscript 将一条转写记录写入数据库。
func (s *Service) storeTranscript(sess *session, text string, startMs, endMs int64, wordTimestamps []models.WordTimestamp, speakerName string, speakerID *uint) {
	if s.deps.TranscriptService == nil {
		return
	}

	var meetingID uint
	if s.deps.SessionMgr != nil {
		if ctx := s.deps.SessionMgr.Context(sess.clientID); ctx != nil {
			meetingID = ctx.MeetingID
		}
	}

	transcript := &models.MeetingTranscript{
		MeetingID:      meetingID,
		SpeakerID:      speakerID,
		SpeakerName:    speakerName,
		Text:           text,
		StartMs:        startMs,
		EndMs:          endMs,
		WordTimestamps: wordTimestamps,
		IsFinal:        true,
	}

	if err := s.deps.TranscriptService.Create(transcript); err != nil {
		s.deps.Log.Warning(fmt.Sprintf("audio: failed to store transcript for client %s: %v", sess.clientID, err))
	}
}

// publishFinalResult 发布最终转写结果（含说话人与时间戳）。
func (s *Service) publishFinalResult(sess *session, text string, startMs, endMs int64, speakerName string, speakerID *uint, speaker *models.Speaker, wordTimestampsJSON string) {
	var meetingID uint
	if s.deps.SessionMgr != nil {
		if ctx := s.deps.SessionMgr.Context(sess.clientID); ctx != nil {
			meetingID = ctx.MeetingID
		}
	}

	result := contracts.Result{
		ClientID:       sess.clientID,
		Text:           text,
		IsFinal:        true,
		StartMs:        startMs,
		EndMs:          endMs,
		SpeakerName:    speakerName,
		SpeakerID:      speakerID,
		MeetingID:      meetingID,
		WordTimestamps: wordTimestampsJSON,
	}

	// 附上说话人详细信息（描述等），供前端渲染说话人标签。
	if speaker != nil {
		result.SpeakerDescription = speaker.Description
	}

	s.deps.Publisher.Publish(result)
}

// publishSpeakerIdentified 单独推送说话人识别事件，用于前端更新说话人指示器。
func (s *Service) publishSpeakerIdentified(sess *session, speakerName string, speakerID *uint, speaker *models.Speaker) {
	if speakerName == "未知说话人" || speakerID == nil {
		return
	}

	var meetingID uint
	if s.deps.SessionMgr != nil {
		if ctx := s.deps.SessionMgr.Context(sess.clientID); ctx != nil {
			meetingID = ctx.MeetingID
		}
	}

	result := contracts.Result{
		ClientID:    sess.clientID,
		SpeakerName: speakerName,
		SpeakerID:   speakerID,
		MeetingID:   meetingID,
	}
	if speaker != nil {
		result.SpeakerDescription = speaker.Description
	}

	s.deps.Publisher.Publish(result)
}

// publishIntermediate 发布中间（仍可能变化）的转写结果，带时间戳与词级时间戳。
//
// 中间结果与最终结果共享同一语音段起点（StartMs 稳定不跳变），EndMs 基于当前
// 已发射 token 的末尾时间且单调递增（绝不回退），保证流式时间戳稳定、连续、无跳变。
func (s *Service) publishIntermediate(sess *session, text string) {
	startMs := sess.utteranceStartMs()
	endMs := sess.currentTokenEndMs()
	if endMs < startMs {
		endMs = startMs
	}
	if endMs < sess.lastSentEndMs {
		endMs = sess.lastSentEndMs
	}
	sess.lastSentEndMs = endMs

	var wordTimestamps []models.WordTimestamp
	if tokens := sess.utteranceTokens(); len(tokens) > 0 {
		wordTimestamps = buildRealtimeWordTimestamps(text, tokens, s.cfg.SampleRate, endMs)
	}
	wordTimestampsJSON, _ := json.Marshal(wordTimestamps)

	var meetingID uint
	if s.deps.SessionMgr != nil {
		if ctx := s.deps.SessionMgr.Context(sess.clientID); ctx != nil {
			meetingID = ctx.MeetingID
		}
	}

	s.deps.Publisher.Publish(contracts.Result{
		ClientID:       sess.clientID,
		Text:           text,
		IsFinal:        false,
		StartMs:        startMs,
		EndMs:          endMs,
		MeetingID:      meetingID,
		WordTimestamps: string(wordTimestampsJSON),
	})
}

// finalize 完成会话收尾：冲刷解码残留、移交录音归档、释放资源。
func (s *Service) finalize(sess *session) {
	if sess.tempFile != nil {
		if err := sess.tempFile.Close(); err != nil {
			s.deps.Log.Warning(fmt.Sprintf("audio: failed to close temp file for client %s: %v", sess.clientID, err))
		}
		sess.tempFile = nil
	}

	sess.stream.InputFinished()
	for sess.recognizer.IsReady(sess.stream) {
		sess.recognizer.Decode(sess.stream)
	}
	// 冲刷出的尾部 token 同样要计入时间轴，否则最后一句的时间戳会缺失尾字。
	if result := sess.recognizer.GetResult(sess.stream); result != nil {
		if len(result.Tokens) > 0 {
			sess.trackTokens(result)
		}
		sess.pendingRunes = len([]rune(result.Text))
		// 会议结束前最后一段语音未必等到断句条件（尾部静音/单句上限），
		// 这里补做一次提交：否则最后一句既不落库也不下发，会议详情里直接丢失。
		s.commitUtterance(sess)
	}

	// 获取 meetingID 用于归档
	var meetingID uint
	if s.deps.SessionMgr != nil {
		if ctx := s.deps.SessionMgr.Context(sess.clientID); ctx != nil {
			meetingID = ctx.MeetingID
		}
	}

	// 先移交临时文件所有权再释放会话，避免归档任务与资源回收争抢同一文件。
	tempName := sess.tempName
	sess.tempName = ""
	s.discard(sess)

	if tempName == "" {
		return
	}
	if err := s.deps.Archiver.Archive(sess.clientID, tempName, meetingID); err != nil {
		s.deps.Log.Error(fmt.Sprintf("audio: failed to archive recording for client %s, temp file preserved: %v", sess.clientID, err))
	}
}

// discard 从会话表摘除并释放会话持有的全部资源，可安全重复调用。
func (s *Service) discard(sess *session) {
	s.mu.Lock()
	if current, ok := s.sessions[sess.clientID]; ok && current == sess {
		delete(s.sessions, sess.clientID)
	}
	s.mu.Unlock()

	if sess.tempFile != nil {
		if err := sess.tempFile.Close(); err != nil {
			s.deps.Log.Warning(fmt.Sprintf("audio: failed to close temp file for client %s: %v", sess.clientID, err))
		}
		sess.tempFile = nil
	}
	if sess.stream != nil {
		sherpa.DeleteOnlineStream(sess.stream)
		sess.stream = nil
	}
	if sess.tempName != "" {
		if err := s.removeTempFile(sess.tempName); err != nil {
			s.deps.Log.Warning(fmt.Sprintf("audio: failed to delete temp file for client %s: %v", sess.clientID, err))
		}
		sess.tempName = ""
	}
}

// publish 发布一条转写结果（保留用于现有代码兼容）。
func (s *Service) publish(clientID, text string, isFinal bool) {
	s.deps.Publisher.Publish(contracts.Result{
		ClientID: clientID,
		Text:     text,
		IsFinal:  isFinal,
	})
}

// createTempFile 为客户端创建录音临时文件。
//
// 以 O_RDWR 打开：实时转写期间需要从同一文件回读指定时间段的音频
// （框选文字动态注册说话人时截取声纹样本），只写打开会导致读取被拒绝。
func (s *Service) createTempFile(clientID string) (*os.File, string, error) {
	name := clientID + tempFileSuffix

	file, err := os.OpenFile(s.tempFilePath(name), os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, "", fmt.Errorf("audio: failed to create temp file: %w", err)
	}
	return file, name, nil
}

// removeTempFile 删除录音临时文件，文件不存在视为成功。
func (s *Service) removeTempFile(name string) error {
	if name == "" {
		return nil
	}
	if err := os.Remove(s.tempFilePath(name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("audio: failed to delete temp file: %w", err)
	}
	return nil
}

// tempFilePath 返回临时文件在音频磁盘上的绝对路径。
func (s *Service) tempFilePath(name string) string {
	return filepath.Join(s.deps.Storage.Path(""), name)
}

// cleanupOrphanedTempFiles 清理上次进程异常退出遗留的临时文件。
func (s *Service) cleanupOrphanedTempFiles() {
	root := s.deps.Storage.Path("")

	entries, err := os.ReadDir(root)
	if err != nil {
		s.deps.Log.Warning(fmt.Sprintf("audio: failed to scan storage directory: %v", err))
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), tempFileSuffix) {
			continue
		}
		if err := os.Remove(filepath.Join(root, entry.Name())); err != nil {
			s.deps.Log.Warning(fmt.Sprintf("audio: failed to remove orphaned temp file %s: %v", entry.Name(), err))
			continue
		}
		s.deps.Log.Info(fmt.Sprintf("audio: removed orphaned temp file %s", entry.Name()))
	}
}

// --- 词级时间戳计算 ---

// computeWordTimestamps 根据文本和起止毫秒偏移，估算每个词/字的起止时间。
//
// 中文按字均分，英文按空格分词均分。这是近似方案，精度 ±100ms。
func computeWordTimestamps(text string, startMs, endMs int64) []models.WordTimestamp {
	if text == "" || endMs <= startMs {
		return nil
	}

	// 按规则分词
	words := splitWords(text)
	if len(words) == 0 {
		return nil
	}

	totalDur := endMs - startMs
	charTotal := 0
	for _, w := range words {
		charTotal += charCount(w)
	}
	if charTotal == 0 {
		return nil
	}

	timestamps := make([]models.WordTimestamp, 0, len(words))
	cursor := startMs

	for _, w := range words {
		wc := charCount(w)
		if wc == 0 {
			continue
		}
		dur := int64(float64(totalDur) * float64(wc) / float64(charTotal))
		wordEnd := cursor + dur
		if wordEnd > endMs {
			wordEnd = endMs
		}

		timestamps = append(timestamps, models.WordTimestamp{
			Word:    w,
			StartMs: cursor,
			EndMs:   wordEnd,
		})
		cursor = wordEnd
	}

	return timestamps
}

// buildRealtimeWordTimestamps 基于实时解码时记录的 token 真实音频采样位置（优先
// 模型产出的 token 级时间戳，其次为窗口起点估计），将字/词级时间戳对齐到音频
// 实际时间。tokenTimes 提供逐字（去掉 ▁ 前缀后的字符）时间戳，再由
// transcript.WordsFromCharTimes 切分为中文字/英文词；若字符无法对齐，返回 nil，
// 由调用方退化为近似方案。
//
// emitted 中记录的 samplePos 是 token 在整场会话音频中的**绝对**位置（连续识别流），
// 因此 AlignCharTimes 得到的逐字时刻就是音频真实时刻，不需要再按句起点平移；
// endMs 只用于兜底裁剪（词尾不得超出句子结束时间）。
func buildRealtimeWordTimestamps(text string, emitted []tokenEmit, sampleRate int, endMs int64) []models.WordTimestamp {
	if len(emitted) == 0 || sampleRate <= 0 {
		return nil
	}
	tts := make([]transcript.TokenTimestamp, 0, len(emitted))
	for _, e := range emitted {
		tts = append(tts, transcript.TokenTimestamp{
			Token:   e.token,
			TimeSec: float32(e.samplePos) / float32(sampleRate),
		})
	}
	charTimes, ok := transcript.AlignCharTimes(text, tts)
	if !ok {
		return nil
	}
	// 使用区间语义（WordsFromCharTimesIntervals）而非逐点语义（WordsFromCharTimes）：
	// 前者为每个字/词生成首尾相接的区间，并把语音停顿/静音从词区间中截断，
	// 避免前端按“下一个字的开始时间”推算结束时刻时把静音整段归到前一个字上，
	// 导致点击某字播放的内容与实际发音位置不符。
	words := transcript.WordsFromCharTimesIntervals(text, charTimes)
	return clampWordEnds(words, endMs)
}

// clampWordEnds 把词级时间戳裁剪到 endMs 以内，并保证 StartMs <= EndMs、
// 相邻词单调不减且互不重叠（给前端逐字高亮与点击定位提供可靠区间）。
//
// 注意：这里**不**以句子的 startMs 作为下界裁剪。能量检测得到的语音起点可能
// 因噪声或检测滞后而偏晚，若用它裁剪，首字会被压成 [startMs, startMs] 的零宽区间：
// 前端既无法把该字高亮为「正在播放」（零宽区间永远不含播放头），又会因自行补足
// 时长而与后一个字的高亮区间重叠（历史的“高亮串字/点不中”问题）。
// 句子起点由调用方按「语音起点与首字起点的较早值」决定（见 commitUtterance）。
func clampWordEnds(words []models.WordTimestamp, endMs int64) []models.WordTimestamp {
	if len(words) == 0 {
		return nil
	}
	out := make([]models.WordTimestamp, 0, len(words))
	last := int64(-1)
	for _, w := range words {
		s, e := w.StartMs, w.EndMs
		if s < 0 {
			s = 0
		}
		if last >= 0 && s < last {
			s = last
		}
		if e < s {
			e = s
		}
		// 仅当区间与 endMs 有交集时才裁剪；整段落在句子结束之后的词
		// （理论上不会出现，属于时间戳异常）保留原区间，避免信息被裁掉。
		if endMs > 0 && e > endMs && s < endMs {
			e = endMs
		}
		out = append(out, models.WordTimestamp{Word: w.Word, StartMs: s, EndMs: e})
		last = e
	}
	return out
}

// splitWords 将文本分词（中文按字，英文按空格分词）。
func splitWords(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// 检测是否包含中文字符
	hasCJK := false
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			hasCJK = true
			break
		}
	}

	if hasCJK {
		// 中文为主的文本，按字切分，英文单词作为整体
		var words []string
		var current strings.Builder
		for _, r := range text {
			if unicode.Is(unicode.Han, r) {
				if current.Len() > 0 {
					words = append(words, current.String())
					current.Reset()
				}
				words = append(words, string(r))
			} else if r == ' ' {
				if current.Len() > 0 {
					words = append(words, current.String())
					current.Reset()
				}
				continue
			} else {
				current.WriteRune(r)
			}
		}
		if current.Len() > 0 {
			words = append(words, current.String())
		}
		return words
	}

	// 纯英文/数字文本，按空格分词
	parts := strings.Fields(text)
	return parts
}

// charCount 计算词中字符数。
func charCount(word string) int {
	count := 0
	for range word {
		count++
	}
	return count
}
