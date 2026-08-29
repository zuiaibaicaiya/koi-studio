package offlinetranscribe

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goravel/framework/contracts/filesystem"
	"github.com/goravel/framework/contracts/log"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	contractsspeaker "koi-server/app/contracts/speaker"
	"koi-server/app/facades"
	"koi-server/app/models"
	"koi-server/app/services"
	audiosvc "koi-server/app/services/audio"
	"koi-server/app/services/transcript"
)

const (
	// maxChunkSeconds 每个分块的最大秒数，用于处理长音频，避免一次性加载占用过多内存。
	maxChunkSeconds = 30.0
	// chunkOverlapSeconds 分块之间的重叠秒数，避免在切分点丢失语句。
	chunkOverlapSeconds = 2.0
)

// 错误定义
var (
	ErrModelNotLoaded   = errors.New("offline: model not loaded")
	ErrServiceClosed    = errors.New("offline: service is closed")
	ErrInvalidAudioFile = errors.New("offline: invalid audio file")
	ErrModelLoadFailed  = errors.New("offline: failed to load offline recognizer")
	ErrNoTranscript     = errors.New("offline: no transcript produced")
)

// Dependencies 离线转写服务的外部依赖
type Dependencies struct {
	Log               log.Log
	Storage           filesystem.Driver
	Progress          *ProgressManager
	TranscriptService *services.MeetingTranscriptService
	MeetingService    *services.MeetingService
	HotWordLibService *services.HotWordLibraryService
	HotWordService    *services.HotWordService
	SpeakerService    *services.SpeakerService
	Voiceprint        contractsspeaker.Voiceprint
	SpeakerVoiceprint *services.SpeakerVoiceprintService
}

func (d Dependencies) validate() error {
	switch {
	case d.Log == nil:
		return errors.New("offline: log dependency is required")
	case d.Storage == nil:
		return errors.New("offline: storage dependency is required")
	case d.Progress == nil:
		return errors.New("offline: progress manager is required")
	case d.TranscriptService == nil:
		return errors.New("offline: transcript service is required")
	case d.MeetingService == nil:
		return errors.New("offline: meeting service is required")
	}
	return nil
}

// Service 离线转写服务，单例持有一个 OnlineRecognizer（用于离线批量转写）。
//
// 注意：所采用的模型是流式 zipformer（如 bilingual zh-en 模型），其 encoder 输入为
// 带 chunk 上下文的 [N, 39, 80]，无法作为普通离线 transducer 一次性整段喂入
// （会报 "Expected: 39" 维度错误）。因此这里统一使用 OnlineRecognizer，在离线批处理时
// 把整段音频一次性喂入（AcceptWaveform + InputFinished），由其在内部完成 chunk 化与缓存，
// 效果等同于离线转写，同时兼容流式模型并提供 token 级时间戳。
type Service struct {
	cfg  Config
	deps Dependencies

	mu         sync.RWMutex
	recognizer *sherpa.OnlineRecognizer
	loaded     bool
	loadErr    error
	closed     bool
	// 保存热词文件路径（临时文件，每次设置热词时重写）
	hotwordsFile string
}

// NewService 构造离线转写服务，后台异步预加载模型。
func NewService(cfg Config, deps Dependencies) (*Service, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	s := &Service{
		cfg:  cfg,
		deps: deps,
	}
	go s.preloadModel()
	return s, nil
}

// Ready 报告模型是否已加载完成。
func (s *Service) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loaded
}

// Status 返回模型加载状态
type Status struct {
	Loaded bool   `json:"loaded"`
	Error  string `json:"error,omitempty"`
}

func (s *Service) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := Status{Loaded: s.loaded}
	if s.loadErr != nil {
		st.Error = s.loadErr.Error()
	}
	return st
}

// Close 释放模型资源
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.recognizer != nil {
		sherpa.DeleteOnlineRecognizer(s.recognizer)
		s.recognizer = nil
	}
	s.loaded = false
	// 清理热词临时文件
	if s.hotwordsFile != "" {
		_ = os.Remove(s.hotwordsFile)
		s.hotwordsFile = ""
	}
	return nil
}

// TranscribeMeeting 对指定会议的音频文件执行离线转写，结果写入数据库。
//
// 该方法异步执行，立即返回；进度可通过 ProgressManager 查询。
func (s *Service) TranscribeMeeting(meetingID uint) error {
	// 防重复提交：已有进行中的任务时直接拒绝。
	// 否则 UploadAudio 的自动触发 + /transcribe 手动触发会并发执行两个转写，
	// 向数据库写入重复的转写记录（同一句子出现两条相同时间戳）。
	if p, perr := s.deps.Progress.Get(meetingID); perr == nil {
		if p.Status == StatusPending || p.Status == StatusRunning {
			return fmt.Errorf("meeting %d 已有转写任务进行中", meetingID)
		}
		// 已完成/失败：移除旧进度，允许重新触发
		s.deps.Progress.Remove(meetingID)
	}

	meeting, err := s.deps.MeetingService.GetMeetingById(int(meetingID))
	if err != nil {
		return fmt.Errorf("offline: meeting not found: %w", err)
	}
	if meeting.AudioFilePath == "" {
		return fmt.Errorf("offline: meeting has no audio file")
	}

	// 构造音频文件的绝对路径
	audioAbsPath := s.deps.Storage.Path(meeting.AudioFilePath)
	if _, err := os.Stat(audioAbsPath); err != nil {
		return fmt.Errorf("offline: audio file not found at %s: %w", audioAbsPath, err)
	}

	// 计算音频总时长（通过读取wav头）
	totalSeconds, err := s.estimateWavDuration(audioAbsPath)
	if err != nil {
		s.deps.Log.Warning(fmt.Sprintf("offline: failed to estimate duration for meeting %d: %v, default 60s", meetingID, err))
		totalSeconds = 60.0
	}

	// 初始化进度
	s.deps.Progress.Init(meetingID, totalSeconds)

	// 异步执行转写
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.deps.Log.Error(fmt.Sprintf("offline: transcribe panicked for meeting %d: %v\n%s", meetingID, r, debug.Stack()))
				s.deps.Progress.Fail(meetingID, fmt.Errorf("转写内部错误: %v", r))
				_ = s.deps.MeetingService.SetMeetingStatus(int(meetingID), models.MeetingStatusFinished)
			}
		}()
		s.doTranscribe(meetingID, audioAbsPath)
	}()

	return nil
}

// RetranscribeMeeting 重新转写指定会议：无论会议模式（实时或离线），
// 都基于会议已归档的音频文件调用离线转写流程，并清空旧的转写记录。
//
// 与 TranscribeMeeting 的区别：
//   - 不受 meeting.Mode 限制，实时会议（归档后已有音频）与离线会议均可调用；
//   - 触发前强制清理旧的转写记录与进度，确保从空白重新转写；
//   - 若已有进行中的任务则直接复用，避免重复提交。
func (s *Service) RetranscribeMeeting(meetingID uint) error {
	meeting, err := s.deps.MeetingService.GetMeetingById(int(meetingID))
	if err != nil {
		return errors.New("会议不存在: " + err.Error())
	}

	if meeting.AudioFilePath == "" {
		return errors.New("会议暂无可重新转写的音频文件（实时会议需先结束并归档）")
	}

	// 清理旧的转写记录，避免新旧转写混合。
	if err := s.deps.TranscriptService.DeleteByMeetingID(meetingID); err != nil {
		return errors.New("清理旧转写记录失败: " + err.Error())
	}

	// 移除可能残留的进度记录，使得下方 TranscribeMeeting 可重新提交任务。
	s.deps.Progress.Remove(meetingID)

	return s.TranscribeMeeting(meetingID)
}

// doTranscribe 实际执行转写逻辑（在独立协程中运行）
func (s *Service) doTranscribe(meetingID uint, audioPath string) {
	s.deps.Progress.Start(meetingID)

	// 1. 等待模型就绪
	if err := s.waitModel(); err != nil {
		s.deps.Log.Error(fmt.Sprintf("offline: model wait failed for meeting %d: %v", meetingID, err))
		s.deps.Progress.Fail(meetingID, fmt.Errorf("模型加载失败: %w", err))
		return
	}
	s.deps.Progress.Update(meetingID, 10, "正在加载热词")

	// 2. 加载会议关联的热词
	meeting, _ := s.deps.MeetingService.GetMeetingById(int(meetingID))
	hotwordsStr, err := s.buildHotwordsForMeeting(&meeting)
	if err != nil {
		s.deps.Log.Warning(fmt.Sprintf("offline: build hotwords failed for meeting %d: %v", meetingID, err))
	}
	if err := s.applyHotwords(hotwordsStr); err != nil {
		s.deps.Log.Warning(fmt.Sprintf("offline: apply hotwords failed for meeting %d: %v", meetingID, err))
	}

	s.deps.Progress.Update(meetingID, 15, "正在读取音频文件")

	// 3. 读取音频文件
	wave := sherpa.ReadWave(audioPath)
	if wave == nil || len(wave.Samples) == 0 {
		s.deps.Progress.Fail(meetingID, ErrInvalidAudioFile)
		return
	}

	sampleRate := wave.SampleRate
	if sampleRate <= 0 {
		sampleRate = s.cfg.SampleRate
	}
	totalSamples := len(wave.Samples)
	totalDurationSec := float64(totalSamples) / float64(sampleRate)

	s.deps.Progress.Update(meetingID, 20, fmt.Sprintf("音频总时长 %.1f 秒，开始转写", totalDurationSec))

	// 4. 声纹库预热（用于说话人识别）
	if s.deps.SpeakerVoiceprint != nil {
		if err := s.deps.SpeakerVoiceprint.Warmup(); err != nil {
			s.deps.Log.Warning(fmt.Sprintf("offline: voiceprint warmup failed: %v", err))
		}
	}

	// 5. 解析会议的说话人列表
	speakerIDs := parseIDList(meeting.SpeakerIds)
	speakerNameByID := make(map[uint]string)
	for _, sid := range speakerIDs {
		if s.deps.SpeakerService != nil {
			if sp, err := s.deps.SpeakerService.GetSpeakerById(int(sid)); err == nil {
				speakerNameByID[sid] = sp.Name
			}
		}
	}

	// 6. 分块转写
	chunkSamples := int(float64(sampleRate) * maxChunkSeconds)
	overlapSamples := int(float64(sampleRate) * chunkOverlapSeconds)
	if chunkSamples <= 0 {
		chunkSamples = totalSamples
	}
	windows := chunkWindows(totalSamples, chunkSamples, overlapSamples)

	index := 0

	for _, start := range windows {
		if s.isClosed() {
			break
		}
		end := start + chunkSamples
		if end > totalSamples {
			end = totalSamples
		}
		chunkSamplesData := wave.Samples[start:end]

		// 进度
		chunkProgress := 20 + int(float64(start)/float64(totalSamples)*70)
		s.deps.Progress.Update(meetingID, chunkProgress,
			fmt.Sprintf("正在转写第 %d 段（%.0f%%）", index+1, float64(start)/float64(totalSamples)*100))

		// 本块在全局时间轴上的偏移（毫秒）。直接用块起点采样位置换算，
		// 取代旧的逐块累加（globalOffsetMs）方式——旧方式把重叠时长当作
		// 偏移增量，使每个后续分块的时间戳整体偏晚、在块边界不连续。
		offsetMs := chunkOffsetMs(start, sampleRate)

		// 实际转写本段
		result, timestamps, perr := s.decodeChunk(chunkSamplesData, sampleRate)
		if perr != nil {
			s.deps.Log.Warning(fmt.Sprintf("offline: decode chunk %d failed: %v", index, perr))
			index++
			continue
		}

		if result != "" {
			// 按句子分段（基于简单标点和长度）
			sentences := splitSentencesWithTimestamps(result, timestamps, sampleRate, chunkSamplesData)
			for _, seg := range sentences {
				// 说话人识别
				speakerName, speakerID := "未知说话人", (*uint)(nil)
				if s.deps.Voiceprint != nil && len(speakerIDs) > 0 {
					// 从本段 PCM 中切出语句部分并做识别
					if spName, spID, ok := s.identifyInChunk(
						chunkSamplesData, sampleRate, seg.chunkStart, seg.chunkEnd, speakerIDs, speakerNameByID,
					); ok {
						speakerName = spName
						speakerID = spID
					}
				}

				// 写入数据库（词级时间戳同样对齐到整段音频的全局时间）
				tr := &models.MeetingTranscript{
					MeetingID:      meetingID,
					SpeakerID:      speakerID,
					SpeakerName:    speakerName,
					Text:           seg.text,
					StartMs:        seg.startMs + offsetMs,
					EndMs:          seg.endMs + offsetMs,
					WordTimestamps: offsetWordTimestamps(seg.wordTimestamps, offsetMs),
					IsFinal:        true,
				}
				if err := s.deps.TranscriptService.Create(tr); err != nil {
					s.deps.Log.Warning(fmt.Sprintf("offline: store transcript failed: %v", err))
				}
			}
		}

		index++
	}

	// 7. 完成
	s.deps.Progress.Update(meetingID, 95, "正在写入最终结果")
	s.deps.Progress.Complete(meetingID)
	_ = s.deps.MeetingService.SetMeetingStatus(int(meetingID), models.MeetingStatusFinished)

	s.deps.Log.Info(fmt.Sprintf("offline: meeting %d transcription finished in %d chunks", meetingID, index))
}

// chunkOffsetMs 返回以 start 采样位置开头的音频块在全局时间轴上的偏移（毫秒）。
// 分块使用带重叠的滑动窗口，块的全局起点即其首个采样对应的时间，
// 直接用采样位置换算，避免逐块累加重叠时长造成时间戳逐块漂移。
func chunkOffsetMs(start, sampleRate int) int64 {
	if sampleRate <= 0 {
		return 0
	}
	return int64(start) * 1000 / int64(sampleRate)
}

// chunkWindows 计算分块转写的滑动窗口起始采样位置序列。
// 窗口步进 = 块长 - 重叠（带重叠的滑窗，避免在切分点丢失语句）；
// 音频不足一块时只产生单个窗口，避免对末尾重叠区重复转写。
// 非法参数返回 nil。
func chunkWindows(totalSamples, chunkSamples, overlapSamples int) []int {
	if totalSamples <= 0 || chunkSamples <= 0 {
		return nil
	}
	step := chunkSamples - overlapSamples
	if totalSamples <= chunkSamples || step <= 0 {
		return []int{0}
	}
	var starts []int
	for start := 0; start < totalSamples; start += step {
		starts = append(starts, start)
	}
	return starts
}

// decodeChunk 对一段音频采样执行离线解码，返回文本与词级时间戳。
//
// 底层使用 OnlineRecognizer：把整段音频一次性喂入（AcceptWaveform + InputFinished），
// 再通过 IsReady/Decode 循环完成 chunk 化解码，最后取 GetResult。这与流式模型的
// 输入要求一致，同时能得到 token 级时间戳用于句子对齐。
//
// 重要：解码全程持有 s.mu 的读锁，保证 applyHotwords / preloadModel 在替换（删除并重建）
// 识别器时必须等待当前解码结束，避免对已删除的 C 识别器指针产生 use-after-free 导致
// 后端崩溃（SIGSEGV）。
func (s *Service) decodeChunk(samples []float32, sampleRate int) (string, []sherpa.OnlineRecognizerResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	recognizer := s.recognizer
	if recognizer == nil {
		return "", nil, ErrModelNotLoaded
	}

	stream := sherpa.NewOnlineStream(recognizer)
	if stream == nil {
		return "", nil, errors.New("offline: failed to create online stream")
	}
	defer sherpa.DeleteOnlineStream(stream)

	stream.AcceptWaveform(sampleRate, samples)
	stream.InputFinished()
	for recognizer.IsReady(stream) {
		recognizer.Decode(stream)
	}
	result := recognizer.GetResult(stream)
	if result == nil {
		return "", nil, ErrNoTranscript
	}

	// 收集多条结果（流式模型每次 Decode 出一个片段，循环已消费完，这里取最终结果）。
	var all []sherpa.OnlineRecognizerResult
	all = append(all, *result)

	return result.Text, all, nil
}

// sentenceSegment 分句后的片段，带时间戳
type sentenceSegment struct {
	text           string
	startMs        int64
	endMs          int64
	chunkStart     int // 在 chunk samples 中的起始样本下标
	chunkEnd       int // 在 chunk samples 中的结束样本下标
	wordTimestamps []models.WordTimestamp
}

// splitSentencesWithTimestamps 将整段文本按标点和长度切分为句，并为每个字生成
// 与音频真实时间对齐的时间戳。优先使用模型返回的 token 级时间戳（精确到字），
// 若无时间戳则退化为按音频时长在句间、句内按比例估算（仍为毫秒，区间与音频对齐）。
func splitSentencesWithTimestamps(text string, results []sherpa.OnlineRecognizerResult, sampleRate int, samples []float32) []sentenceSegment {
	if text == "" {
		return nil
	}

	// 合并所有结果的 token 与 token 级时间戳（离线解码可能返回多个结果段）。
	var ts []float32
	var tokens []string
	for _, r := range results {
		ts = append(ts, r.Timestamps...)
		tokens = append(tokens, r.Tokens...)
	}

	// 优先：模型产出 token 级时间戳，逐字对齐到音频真实时间。
	if len(ts) > 0 && len(ts) == len(tokens) && hasValidTimestamps(ts) {
		tokenTimes := make([]transcript.TokenTimestamp, len(tokens))
		for i := range tokens {
			tokenTimes[i] = transcript.TokenTimestamp{Token: tokens[i], TimeSec: ts[i]}
		}
		if charTimes, ok := transcript.AlignCharTimes(text, tokenTimes); ok {
			return buildSentenceSegments(text, charTimes, samples, sampleRate)
		}
	}

	// 退化：无 token 时间戳时，按整段音频时长在句间、句内按比例分配（毫秒）。
	return buildSentenceSegmentsApprox(text, samples, sampleRate)
}

// hasValidTimestamps 判断模型产出的 token 级时间戳是否真实可用。
// sherpa-onnx 的离线 transducer 模型不产生时间戳：其 Timestamps 是与 Tokens
// 等长的全 0 数组（长度匹配但数值无意义），若不作区分会被误当作有效时间戳，
// 导致全部句子时间戳为 0（重新转写后时间戳全 0 的根因）。
func hasValidTimestamps(ts []float32) bool {
	for _, t := range ts {
		if t > 0 {
			return true
		}
	}
	return false
}

// buildSentenceSegments 基于逐字时间戳（秒）生成句子分段与字级词时间戳。
func buildSentenceSegments(text string, charTimes []float32, samples []float32, sampleRate int) []sentenceSegment {
	runes := []rune(text)
	if len(charTimes) != len(runes) {
		return buildSentenceSegmentsApprox(text, samples, sampleRate)
	}
	// 先基于整段文本一次性算出字/词区间，再按句切分：避免逐句独立计算时
	// 每句首字都向前回退一个常规发音时长，造成相邻句子时间区间重叠。
	allWords := transcript.WordsWithSpansFromCharTimes(text, charTimes)
	spans := simpleSplitSentences(text)
	var out []sentenceSegment
	for _, sp := range spans {
		a, b := sp.start, sp.end
		if b > len(runes) {
			b = len(runes)
		}
		if b <= a {
			continue
		}
		// 取起点落在 [a, b) 内的字/词：按起点归句，保证每个字/词只属于一句。
		var words []models.WordTimestamp
		for _, iv := range allWords {
			if iv.Start < a {
				continue
			}
			if iv.Start >= b {
				break
			}
			words = append(words, models.WordTimestamp{Word: iv.Word, StartMs: iv.StartMs, EndMs: iv.EndMs})
		}
		// 逐字时间戳是“该字发音结束时刻”，句子的起止时间必须取回溯后的
		// 字/词区间端点，否则整句时间会比真实语音晚约一个字。
		startMs := int64(charTimes[a] * 1000)
		endMs := int64(charTimes[b-1] * 1000)
		if len(words) > 0 {
			startMs = words[0].StartMs
			endMs = words[len(words)-1].EndMs
		}
		out = append(out, sentenceSegment{
			text:           sp.text,
			startMs:        startMs,
			endMs:          endMs,
			chunkStart:     int(float64(startMs) / 1000.0 * float64(sampleRate)),
			chunkEnd:       int(float64(endMs) / 1000.0 * float64(sampleRate)),
			wordTimestamps: words,
		})
	}
	return out
}

// buildSentenceSegmentsApprox 在没有 token 时间戳时，按音频时长在句子与字词间
// 近似分配时间戳（仍为毫秒，区间与音频对齐），保证前端能正确定位。
// 近似时间轴只覆盖有语音的区间：先检测音频中的所有语音段，文本按比例铺在
// 各语音段内部，静音段不分配任何文字时间。这样中间的静音不会被压缩/跳过，
// 文字时间戳与音频实际发音位置一一对应。
func buildSentenceSegmentsApprox(text string, samples []float32, sampleRate int) []sentenceSegment {
	spans := simpleSplitSentences(text)
	totalRunes := 0
	for _, sp := range spans {
		totalRunes += sp.end - sp.start
	}
	if totalRunes == 0 {
		totalRunes = 1
	}

	// 检测所有语音段；全静音时退化为整段音频（与旧行为一致）。
	segs := detectSpeechSegments(samples, sampleRate)
	if len(segs) == 0 {
		segs = [][2]int{{0, len(samples)}}
	}
	totalSpeech := 0
	for _, sg := range segs {
		totalSpeech += sg[1] - sg[0]
	}
	if totalSpeech <= 0 {
		totalSpeech = 1
	}

	// pref[i] = 前 i 段累计语音时长（样本数），用于把字符比例位置定位到语音段。
	pref := make([]int, len(segs)+1)
	for i, sg := range segs {
		pref[i+1] = pref[i] + (sg[1] - sg[0])
	}

	// charToSample：把字符序号（0..totalRunes）按比例落在"总语音时长"轴上，
	// 再映射回真实音频采样位置（只落在语音段内，静音段不产生任何文字时间）。
	charToSample := func(runeIdx int) int {
		p := int(math.Round(float64(totalSpeech) * float64(runeIdx) / float64(totalRunes)))
		if p < 0 {
			p = 0
		}
		if p >= totalSpeech {
			return segs[len(segs)-1][1]
		}
		lo, hi := 0, len(segs)
		for lo+1 < hi {
			mid := (lo + hi) / 2
			if pref[mid] <= p {
				lo = mid
			} else {
				hi = mid
			}
		}
		return segs[lo][0] + (p - pref[lo])
	}

	var out []sentenceSegment
	for _, sp := range spans {
		cs := charToSample(sp.start)
		ce := charToSample(sp.end)
		if ce <= cs {
			ce = cs + 1 // 至少 1 个采样，保证区间非空
		}
		segStartMs := int64(float64(cs) / float64(sampleRate) * 1000)
		segEndMs := int64(float64(ce) / float64(sampleRate) * 1000)
		if segEndMs < segStartMs {
			segEndMs = segStartMs
		}
		// 在句内按字符数线性铺开逐字时间（退化的近似对齐）。逐字时间戳语义是
		// “该字发音结束时刻”，故第 i 个字取区间内的第 (i+1)/n 个等分点，
		// 使末字结束时刻对齐句末、首字起始时刻对齐句首。
		runes := []rune(sp.text)
		sub := make([]float32, len(runes))
		span := float64(segEndMs - segStartMs)
		for i := range runes {
			sub[i] = float32(segStartMs+int64(span*float64(i+1)/float64(len(runes)))) / 1000.0
		}
		words := transcript.WordsFromCharTimesIntervals(sp.text, sub)
		if len(words) > 0 {
			// 首字没有前驱时间戳，会按常规发音时长向前回退，可能越出本句区间，收敛回来。
			if words[0].StartMs < segStartMs {
				words[0].StartMs = segStartMs
			}
			if last := words[len(words)-1]; last.EndMs > segEndMs {
				words[len(words)-1].EndMs = segEndMs
			}
		}
		out = append(out, sentenceSegment{
			text:           sp.text,
			startMs:        segStartMs,
			endMs:          segEndMs,
			chunkStart:     cs,
			chunkEnd:       ce,
			wordTimestamps: words,
		})
	}
	return out
}

// detectSpeechSegments 检测音频中所有有语音的采样区间（[start, end)，样本序号）。
// 以 100ms 帧的短时 RMS 能量超过阈值判断，阈值与实时转写保持一致（0.03）。
// 相邻语音段之间的静音间隙不超过 300ms 时合并为同一段（说话中的短暂停顿），
// 避免单帧噪声或极短停顿造成过多碎片段。全静音时返回空切片。
func detectSpeechSegments(samples []float32, sampleRate int) [][2]int {
	if len(samples) == 0 || sampleRate <= 0 {
		return nil
	}
	frame := sampleRate / 10 // 100ms 一帧
	if frame <= 0 {
		frame = 1
	}
	minGap := frame * 3 // 300ms

	var segs [][2]int
	for start := 0; start < len(samples); start += frame {
		end := start + frame
		if end > len(samples) {
			end = len(samples)
		}
		var sum float64
		for _, v := range samples[start:end] {
			sum += float64(v) * float64(v)
		}
		if end <= start || math.Sqrt(sum/float64(end-start)) <= 0.03 {
			continue // 静音帧
		}
		if len(segs) > 0 && start-segs[len(segs)-1][1] <= minGap {
			// 与前一段的静音间隙 ≤ 300ms，视为同一语音段，扩展其末尾。
			segs[len(segs)-1][1] = end
		} else {
			segs = append(segs, [2]int{start, end})
		}
	}
	return segs
}

// detectSpeechStart 返回音频中第一个有语音的采样位置（毫秒对齐到 100ms 帧）。
// 复用 detectSpeechSegments 的结果，找不到语音（全静音）时返回 0。
func detectSpeechStart(samples []float32, sampleRate int) int {
	segs := detectSpeechSegments(samples, sampleRate)
	if len(segs) == 0 {
		return 0
	}
	return segs[0][0]
}

// offsetWordTimestamps 将词级时间戳整体偏移 offsetMs（用于对齐到整段音频的全局时间）。
func offsetWordTimestamps(wts []models.WordTimestamp, offsetMs int64) []models.WordTimestamp {
	if offsetMs == 0 || len(wts) == 0 {
		return wts
	}
	out := make([]models.WordTimestamp, len(wts))
	for i, w := range wts {
		out[i] = models.WordTimestamp{Word: w.Word, StartMs: w.StartMs + offsetMs, EndMs: w.EndMs + offsetMs}
	}
	return out
}

// textSpan 表示 text 中的一个句子片段及其在原文中的 rune 区间（含标点）。
// 片段区间可直接用于从逐字时间戳切片，不依赖重新拼接。
type textSpan struct {
	text  string
	start int // 原文 rune 起始下标（含）
	end   int // 原文 rune 结束下标（不含）
}

// simpleSplitSentences 按中英文标点和长度简单断句，返回的每个片段保留其原始标点，
// 且携带在原文中的 rune 区间，便于从逐字时间戳精确切片。
func simpleSplitSentences(text string) []textSpan {
	src := []rune(text)
	if len(src) == 0 {
		return nil
	}
	var out []textSpan
	curStart := 0
	var cur []rune
	for i, r := range src {
		cur = append(cur, r)
		switch r {
		case '。', '！', '？', '.', '!', '?', '；', ';', '，', ',':
			// 在标点处切句，但至少保留 4 个字符
			if len(cur) >= 4 {
				out = append(out, textSpan{text: string(cur), start: curStart, end: i + 1})
				cur = nil
				curStart = i + 1
			}
		default:
			// 超过 20 字也强制切句：离线模型（如 transducer）输出常无标点，
			// 若只在空格/结尾处切，一整块音频会变成一条超长记录，无法精确定位。
			if len(cur) >= 20 {
				out = append(out, textSpan{text: string(cur), start: curStart, end: i + 1})
				cur = nil
				curStart = i + 1
			}
		}
	}
	if len(cur) > 0 {
		out = append(out, textSpan{text: string(cur), start: curStart, end: len(src)})
	}
	if len(out) == 0 {
		out = []textSpan{{text: text, start: 0, end: len(src)}}
	}
	return out
}

// identifyInChunk 在 chunk 样本中的指定区间，提取声纹并做说话人识别。
func (s *Service) identifyInChunk(
	chunkSamples []float32, sampleRate int,
	chunkStart, chunkEnd int,
	speakerIDs []uint, nameByID map[uint]string,
) (string, *uint, bool) {
	if s.deps.Voiceprint == nil || !s.deps.Voiceprint.Ready() || len(speakerIDs) == 0 {
		return "", nil, false
	}
	if chunkEnd <= chunkStart {
		return "", nil, false
	}
	segSamples := chunkSamples[chunkStart:chunkEnd]
	durSec := float64(len(segSamples)) / float64(sampleRate)
	if durSec < 1.0 {
		return "", nil, false // 太短，不稳定
	}
	// float32 samples -> 16bit PCM bytes -> WAV
	pcm := make([]byte, 0, len(segSamples)*2)
	for _, s := range segSamples {
		v := int16(s * 32767.0)
		if s > 1.0 {
			v = 32767
		} else if s < -1.0 {
			v = -32768
		}
		pcm = append(pcm, byte(v), byte(v>>8))
	}
	wavBytes, err := audiosvc.PCMToWAV(pcm, sampleRate)
	if err != nil {
		return "", nil, false
	}
	feat, err := s.deps.Voiceprint.Extract(wavBytes)
	if err != nil {
		return "", nil, false
	}
	match, err := s.deps.Voiceprint.Search(feat.Vector, 0)
	if err != nil || !match.Matched {
		return "", nil, false
	}
	// 检查命中者是否在本会议的 speakerIDs 列表中
	for _, sid := range speakerIDs {
		if name, ok := nameByID[sid]; ok && name == match.Name {
			cp := sid
			return match.Name, &cp, true
		}
	}
	return "", nil, false
}

// buildHotwordsForMeeting 根据会议关联的热词库 ID 列表构建热词字符串。
// 格式：每行一个词（可选加冒号和权重），与 sherpa-onnx 要求一致。
func (s *Service) buildHotwordsForMeeting(meeting *models.Meeting) (string, error) {
	ids := parseIDList(meeting.HotWordLibraryIds)
	if len(ids) == 0 {
		return "", nil
	}

	if s.deps.HotWordService == nil || s.deps.HotWordLibService == nil {
		return "", nil
	}

	var lines []string
	for _, libID := range ids {
		// 简单获取该库下全部热词（库通常不大，一次取 500 条足够）
		var hotwords []models.HotWord
		if err := facades.Orm().Query().
			Where("library_id = ?", libID).
			Limit(500).
			Find(&hotwords); err != nil {
			continue
		}
		for _, hw := range hotwords {
			word := strings.TrimSpace(hw.Word)
			if word == "" {
				continue
			}
			if hw.Weight > 0 {
				lines = append(lines, fmt.Sprintf("%s:%d", word, hw.Weight))
			} else {
				lines = append(lines, word)
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}

// applyHotwords 把热词字符串写入临时文件，并替换当前识别器。
func (s *Service) applyHotwords(hotwords string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrServiceClosed
	}
	if !s.loaded || s.recognizer == nil {
		return ErrModelNotLoaded
	}

	// 写入临时文件（空字符串时创建空文件，等效于清除热词）
	tmp, err := os.CreateTemp("", "offline-hotwords-*.txt")
	if err != nil {
		return fmt.Errorf("offline: create hotwords temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(hotwords); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("offline: write hotwords temp file: %w", err)
	}
	_ = tmp.Close()

	// 重建识别器配置
	cfg := s.buildRecognizerConfig()
	if hotwords != "" {
		cfg.HotwordsFile = tmpPath
		cfg.HotwordsScore = s.cfg.HotwordsScore
		// sherpa 要求提供热词文件时必须使用 modified_beam_search，
		// 否则配置非法、识别器创建失败（返回 nil），进而在解码时崩溃。
		cfg.DecodingMethod = "modified_beam_search"
		if cfg.MaxActivePaths <= 0 {
			cfg.MaxActivePaths = 4
		}
	} else {
		cfg.HotwordsFile = ""
		cfg.HotwordsScore = 0
	}

	newRec := sherpa.NewOnlineRecognizer(&cfg)
	if newRec == nil {
		_ = os.Remove(tmpPath)
		return ErrModelLoadFailed
	}

	// 替换旧识别器，删除旧热词文件
	if s.recognizer != nil {
		sherpa.DeleteOnlineRecognizer(s.recognizer)
	}
	s.recognizer = newRec
	if s.hotwordsFile != "" {
		_ = os.Remove(s.hotwordsFile)
	}
	s.hotwordsFile = tmpPath

	s.deps.Log.Info(fmt.Sprintf("offline: hotwords applied, %d lines", strings.Count(hotwords, "\n")+1))
	return nil
}

// buildRecognizerConfig 根据 Config 组装识别器参数（OnlineRecognizer，用于离线批量转写）。
//
// 采用 OnlineRecognizer 以兼容流式 zipformer 模型：流式模型的 encoder 需要 chunk 化的
// 输入（[N, 39, 80]），离线 transducer 一次性整段喂入会报维度错误。
func (s *Service) buildRecognizerConfig() sherpa.OnlineRecognizerConfig {
	cfg := sherpa.OnlineRecognizerConfig{
		FeatConfig: sherpa.FeatureConfig{
			SampleRate: s.cfg.SampleRate,
			FeatureDim: s.cfg.FeatureDim,
		},
		ModelConfig: sherpa.OnlineModelConfig{
			Tokens:        s.cfg.modelPath(s.cfg.Tokens),
			NumThreads:    s.cfg.NumThreads,
			Provider:      s.cfg.Provider,
			ModelingUnit:  s.cfg.ModelingUnit,
			BpeVocab:      s.cfg.modelPath(s.cfg.BpeVocab),
		},
		DecodingMethod: s.cfg.DecodingMethod,
		MaxActivePaths: s.cfg.MaxActivePaths,
		HotwordsScore:  s.cfg.HotwordsScore,
	}

	switch strings.ToLower(s.cfg.ModelType) {
	case "transducer":
		cfg.ModelConfig.Transducer = sherpa.OnlineTransducerModelConfig{
			Encoder: s.cfg.modelPath(s.cfg.Encoder),
			Decoder: s.cfg.modelPath(s.cfg.Decoder),
			Joiner:  s.cfg.modelPath(s.cfg.Joiner),
		}
	case "paraformer":
		cfg.ModelConfig.Paraformer = sherpa.OnlineParaformerModelConfig{
			Encoder: s.cfg.modelPath(s.cfg.Model),
			Decoder: s.cfg.modelPath(s.cfg.Model),
		}
	default:
		// zipformer_ctc（默认）
		cfg.ModelConfig.Zipformer2Ctc = sherpa.OnlineZipformer2CtcModelConfig{
			Model: s.cfg.modelPath(s.cfg.Model),
		}
	}

	return cfg
}

// preloadModel 异步预加载模型
func (s *Service) preloadModel() {
	defer func() {
		if r := recover(); r != nil {
			s.mu.Lock()
			s.loadErr = fmt.Errorf("offline: model preload panicked: %v", r)
			s.mu.Unlock()
			s.deps.Log.Error(fmt.Sprintf("offline: model preload panicked: %v\n%s", r, debug.Stack()))
		}
	}()

	start := time.Now()
	s.deps.Log.Info("offline: preloading offline ASR model (online recognizer)...")

	cfg := s.buildRecognizerConfig()
	rec := sherpa.NewOnlineRecognizer(&cfg)

	s.mu.Lock()
	defer s.mu.Unlock()

	if rec == nil {
		s.loadErr = ErrModelLoadFailed
		s.deps.Log.Error("offline: failed to create offline recognizer (check model files exist)")
		return
	}
	if s.closed {
		sherpa.DeleteOnlineRecognizer(rec)
		return
	}
	s.recognizer = rec
	s.loaded = true
	s.deps.Log.Info(fmt.Sprintf("offline: model loaded in %.2fs (type=%s)", time.Since(start).Seconds(), s.cfg.ModelType))
}

// waitModel 等待模型就绪
func (s *Service) waitModel() error {
	deadline := time.Now().Add(s.cfg.LoadTimeout)
	for {
		s.mu.RLock()
		loaded, loadErr, closed := s.loaded, s.loadErr, s.closed
		s.mu.RUnlock()
		if closed {
			return ErrServiceClosed
		}
		if loaded {
			return nil
		}
		if loadErr != nil {
			return loadErr
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("offline: model load timeout after %s", s.cfg.LoadTimeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (s *Service) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// parseIDList 把 "1,2,3" 形式的字符串解析为 uint 切片
func parseIDList(s string) []uint {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]uint, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if v, err := strconv.ParseUint(p, 10, 64); err == nil {
			out = append(out, uint(v))
		}
	}
	return out
}

// estimateWavDuration 通过解析 wav 头估算音频时长（秒）
func (s *Service) estimateWavDuration(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	header := make([]byte, 44)
	if _, err := f.Read(header); err != nil {
		return 0, err
	}
	// 简单校验 RIFF/WAVE
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not a wav file")
	}
	// 从 fmt 块读取
	sampleRate := uint32(header[24]) | uint32(header[25])<<8 | uint32(header[26])<<16 | uint32(header[27])<<24
	byteRate := uint32(header[28]) | uint32(header[29])<<8 | uint32(header[30])<<16 | uint32(header[31])<<24
	if sampleRate == 0 || byteRate == 0 {
		return 0, fmt.Errorf("invalid wav header")
	}
	// data 块大小：标准 44 字节头中偏移 36-39 是 "data" 标识，40-43 是 data size。
	// （旧代码误读了 44-47，即 PCM 数据区开头 4 个字节，导致时长估算错误。）
	if _, err := f.Seek(40, 0); err != nil {
		return 0, err
	}
	buf2 := make([]byte, 4)
	if _, err := f.Read(buf2); err != nil {
		return 0, err
	}
	dataSize := uint32(buf2[0]) | uint32(buf2[1])<<8 | uint32(buf2[2])<<16 | uint32(buf2[3])<<24
	if dataSize == 0 {
		// 退化：读文件总大小
		info, err := f.Stat()
		if err != nil {
			return 0, err
		}
		dataSize = uint32(info.Size()) - 44
	}
	return float64(dataSize) / float64(byteRate), nil
}

// Ensure helper used but not unused
var _ = filepath.Base
