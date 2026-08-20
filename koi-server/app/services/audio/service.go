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
)

// 服务可能返回的错误。
var (
	ErrEmptyClientID = errors.New("audio: empty client id")
	ErrServiceClosed = errors.New("audio: service is closed")
	ErrModelTimeout  = errors.New("audio: model loading timeout")
	ErrStreamCreate  = errors.New("audio: failed to create online stream")
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
func (s *Service) buildRecognizerConfig() sherpa.OnlineRecognizerConfig {
	cfg := sherpa.OnlineRecognizerConfig{
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
		DecodingMethod:          s.cfg.DecodingMethod,
		MaxActivePaths:          s.cfg.MaxActivePaths,
		Rule1MinTrailingSilence: s.cfg.Rule1Silence,
		Rule2MinTrailingSilence: s.cfg.Rule2Silence,
		Rule3MinUtteranceLength: s.cfg.Rule3Utterance,
		HotwordsScore:           s.cfg.HotwordsScore,
		HotwordsFile:            s.cfg.modelPath(s.cfg.HotwordsFile),
	}
	if s.cfg.EnableEndpoint {
		cfg.EnableEndpoint = 1
	}
	return cfg
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
	sess.resetUtterance()
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

// decode 把采样点送入识别流，按批解码并下发中间/最终结果。
func (s *Service) decode(sess *session) {
	// 记录本次解码窗口（当前帧）的起点采样位置，并初始化当前语音段的流起点。
	// 流式模型固定 token 需要前置上下文，token 时间戳以流起点为基准，
	// 可避免解码延迟把时间戳整体往后推。
	sess.windowStartSample = sess.totalSamples - int64(len(sess.samples))
	if sess.utteranceStreamStart < 0 {
		sess.utteranceStreamStart = sess.windowStartSample
	}
	sess.stream.AcceptWaveform(s.cfg.SampleRate, sess.samples)
	sess.batch++

	// 攒批解码以降低 CPU 占用；检测到端点时立即解码保证响应速度。
	if sess.batch%s.cfg.DecodeBatch == 0 || sess.recognizer.IsEndpoint(sess.stream) {
		var partial string
		var result *sherpa.OnlineRecognizerResult
		for sess.recognizer.IsReady(sess.stream) {
			sess.recognizer.Decode(sess.stream)
			result = sess.recognizer.GetResult(sess.stream)
			partial = result.Text
		}
		// 记录 token 对应的真实音频采样位置，供字级时间戳对齐音频。
		// 优先使用模型产出的 token 级时间戳，消除「token 被解码发现晚于
		// 其实际发音」带来的系统性时间漂移。
		if result != nil && len(result.Tokens) > 0 {
			sess.trackTokens(result)
		}

		// 文本出现时标记语音段起始。
		if partial != "" {
			sess.markUtteranceStart()
		}

		// 限流下发：文本无变化或距上次下发不足间隔时跳过。
		if partial != "" && partial != sess.lastSentText && time.Since(sess.lastSentAt) > s.cfg.EmitInterval {
			sess.lastSentText = partial
			sess.lastSentAt = time.Now()
			s.publishIntermediate(sess, partial)
		}
		sess.batch = 0
	}

	if sess.recognizer.IsEndpoint(sess.stream) {
		s.commitUtterance(sess)
		return
	}

	// 长时间未检测到端点时强制断句，防止结果迟迟不下发。
	if time.Since(sess.utteranceAt) > s.cfg.MaxUtterance {
		sess.stream.InputFinished()
		for sess.recognizer.IsReady(sess.stream) {
			sess.recognizer.Decode(sess.stream)
			if result := sess.recognizer.GetResult(sess.stream); len(result.Tokens) > 0 {
				sess.trackTokens(result)
			}
		}
		s.commitUtterance(sess)
	}
}

// commitUtterance 输出一句已确定的文本，同时执行说话人识别、入库存储、发布增强结果。
func (s *Service) commitUtterance(sess *session) {
	text := sess.recognizer.GetResult(sess.stream).Text
	if text == "" {
		sess.recognizer.Reset(sess.stream)
		sess.resetUtterance()
		sess.resetUtteranceTracking()
		return
	}

	sess.markUtteranceStart()

	// 1. 计算时间戳
	startMs := sess.utteranceStartMs()
	endMs := sess.commitEndMs()

	// 2. 计算词级时间戳（优先基于 token 发射时的真实采样位置对齐音频，
	//    流模型不产出 token 时间戳时退化为按音频时长的近似对齐）。
	var wordTimestamps []models.WordTimestamp
	if len(sess.emittedTokens) > 0 {
		wordTimestamps = buildRealtimeWordTimestamps(text, sess.emittedTokens, s.cfg.SampleRate, startMs, endMs)
	}
	if len(wordTimestamps) == 0 {
		wordTimestamps = computeWordTimestamps(text, startMs, endMs)
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

	// 7. 更新累积文本并重置状态
	sess.appendTranscript(text + " ")
	sess.recognizer.Reset(sess.stream)
	sess.resetUtterance()
	sess.resetUtteranceTracking()
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
	if ctx == nil || len(ctx.SpeakerIDs) == 0 {
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

	// 1:N 检索
	match, err := s.deps.Voiceprint.Search(feature.Vector, 0)
	if err != nil || !match.Matched {
		return "未知说话人", nil, nil
	}

	// 检查命中者是否在会议选择的说话人列表中
	speaker, found := s.findSpeakerByName(match.Name, ctx.SpeakerIDs)
	if !found {
		return "未知说话人", nil, nil
	}

	return speaker.Name, &speaker.ID, &speaker
}

// findSpeakerByName 在会议选择的说话人列表中按名称查找说话人，返回完整模型。
func (s *Service) findSpeakerByName(name string, speakerIDs []uint) (models.Speaker, bool) {
	if s.deps.SpeakerService == nil {
		return models.Speaker{}, false
	}

	for _, id := range speakerIDs {
		speaker, err := s.deps.SpeakerService.GetSpeakerById(int(id))
		if err != nil {
			continue
		}
		if speaker.Name == name {
			return speaker, true
		}
	}

	return models.Speaker{}, false
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
	if len(sess.emittedTokens) > 0 {
		wordTimestamps = buildRealtimeWordTimestamps(text, sess.emittedTokens, s.cfg.SampleRate, startMs, endMs)
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
	if text := sess.recognizer.GetResult(sess.stream).Text; text != "" {
		sess.appendTranscript(text)
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
func (s *Service) createTempFile(clientID string) (*os.File, string, error) {
	name := clientID + tempFileSuffix

	file, err := os.OpenFile(s.tempFilePath(name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
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
func buildRealtimeWordTimestamps(text string, emitted []tokenEmit, sampleRate int, startMs, endMs int64) []models.WordTimestamp {
	if len(emitted) == 0 || sampleRate <= 0 || endMs <= startMs {
		return nil
	}
	type tokTime struct {
		tok  string
		tSec float32
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
	words := transcript.WordsFromCharTimes(text, charTimes)
	return clampWords(words, startMs, endMs)
}

// clampWords 将词级时间戳裁剪到 [startMs, endMs] 并强制单调递增。
//
// 裁剪后始终保证 StartMs <= EndMs：当词的起点本身超出 endMs（理论/近似时间戳
// 超前）时，起点与终点一并钳制到 endMs，避免出现时间倒挂。
func clampWords(words []models.WordTimestamp, startMs, endMs int64) []models.WordTimestamp {
	if len(words) == 0 {
		return nil
	}
	out := make([]models.WordTimestamp, 0, len(words))
	last := startMs
	for _, w := range words {
		s := w.StartMs
		if s < startMs {
			s = startMs
		}
		if s < last {
			s = last
		}
		e := w.EndMs
		if e < s {
			e = s
		}
		if e > endMs {
			e = endMs
		}
		if s > e {
			// 起点超出 endMs：随终点一起钳制，保证不倒挂。
			s = e
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
