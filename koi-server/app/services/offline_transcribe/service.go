package offlinetranscribe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/goravel/framework/contracts/filesystem"
	"github.com/goravel/framework/contracts/log"
	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	contractsspeaker "koi-server/app/contracts/speaker"
	audiosvc "koi-server/app/services/audio"
	"koi-server/app/facades"
	"koi-server/app/models"
	"koi-server/app/services"
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

// Service 离线转写服务，单例持有一个 OfflineRecognizer。
type Service struct {
	cfg  Config
	deps Dependencies

	mu         sync.RWMutex
	recognizer *sherpa.OfflineRecognizer
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
		sherpa.DeleteOfflineRecognizer(s.recognizer)
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

	var (
		globalOffsetMs int64 = 0
		index          int   = 0
	)

	for start := 0; start < totalSamples; start += chunkSamples - overlapSamples {
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

		// 实际转写本段
		result, timestamps, perr := s.decodeChunk(chunkSamplesData, sampleRate)
		if perr != nil {
			s.deps.Log.Warning(fmt.Sprintf("offline: decode chunk %d failed: %v", index, perr))
			index++
			continue
		}

		if result != "" {
			// 按句子分段（基于简单标点和长度）
			sentences := splitSentencesWithTimestamps(result, timestamps, sampleRate, chunkSamplesData, start)
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

				// 写入数据库
				wt, _ := json.Marshal(seg.wordTimestamps)
				tr := &models.MeetingTranscript{
					MeetingID:      meetingID,
					SpeakerID:      speakerID,
					SpeakerName:    speakerName,
					Text:           seg.text,
					StartMs:        seg.startMs + globalOffsetMs,
					EndMs:          seg.endMs + globalOffsetMs,
					WordTimestamps: string(wt),
					IsFinal:        true,
				}
				if err := s.deps.TranscriptService.Create(tr); err != nil {
					s.deps.Log.Warning(fmt.Sprintf("offline: store transcript failed: %v", err))
				}
			}
		}

		// 更新全局时间偏移（减去重叠部分以免时间重复）
		if index > 0 {
			globalOffsetMs -= int64(float64(overlapSamples) / float64(sampleRate) * 1000)
			if globalOffsetMs < 0 {
				globalOffsetMs = 0
			}
		}
		chunkDurationMs := int64(float64(len(chunkSamplesData)) / float64(sampleRate) * 1000)
		globalOffsetMs += chunkDurationMs

		index++
	}

	// 7. 完成
	s.deps.Progress.Update(meetingID, 95, "正在写入最终结果")
	s.deps.Progress.Complete(meetingID)
	_ = s.deps.MeetingService.SetMeetingStatus(int(meetingID), models.MeetingStatusFinished)

	s.deps.Log.Info(fmt.Sprintf("offline: meeting %d transcription finished in %d chunks", meetingID, index))
}

// decodeChunk 对一段音频采样执行离线解码，返回文本与词级时间戳。
func (s *Service) decodeChunk(samples []float32, sampleRate int) (string, []sherpa.OfflineRecognizerResult, error) {
	s.mu.RLock()
	recognizer := s.recognizer
	s.mu.RUnlock()
	if recognizer == nil {
		return "", nil, ErrModelNotLoaded
	}

	stream := sherpa.NewOfflineStream(recognizer)
	if stream == nil {
		return "", nil, errors.New("offline: failed to create offline stream")
	}
	defer sherpa.DeleteOfflineStream(stream)

	stream.AcceptWaveform(sampleRate, samples)
	recognizer.Decode(stream)
	result := stream.GetResult()
	if result == nil {
		return "", nil, ErrNoTranscript
	}

	// 收集多条结果（尽管非流式通常是一条）
	var all []sherpa.OfflineRecognizerResult
	all = append(all, *result)

	return result.Text, all, nil
}

// sentenceSegment 分句后的片段，带时间戳
type sentenceSegment struct {
	text            string
	startMs         int64
	endMs           int64
	chunkStart      int // 在 chunk samples 中的起始样本下标
	chunkEnd        int // 在 chunk samples 中的结束样本下标
	wordTimestamps  []models.WordTimestamp
}

// splitSentencesWithTimestamps 将整段文本按标点和长度切分为句，并估算每句的起止时间。
func splitSentencesWithTimestamps(text string, results []sherpa.OfflineRecognizerResult, sampleRate int, samples []float32, globalSampleStart int) []sentenceSegment {
	if text == "" {
		return nil
	}

	// 使用第一个（通常也是唯一一个）结果的 Timestamps
	var ts []float32
	var tokens []string
	for _, r := range results {
		if len(r.Timestamps) > 0 {
			ts = r.Timestamps
			tokens = r.Tokens
			break
		}
	}

	// 如果模型没有返回时间戳，则按整段估算
	if len(ts) == 0 || len(ts) != len(tokens) {
		totalMs := int64(float64(len(samples)) / float64(sampleRate) * 1000)
		// 简单分句
		sents := simpleSplitSentences(text)
		var segs []sentenceSegment
		cursor := int64(0)
		for _, s := range sents {
			ratio := float64(len([]rune(s))) / float64(len([]rune(text)))
			dur := int64(float64(totalMs) * ratio)
			end := cursor + dur
			if end > totalMs {
				end = totalMs
			}
			segs = append(segs, sentenceSegment{
				text:           s,
				startMs:        cursor,
				endMs:          end,
				chunkStart:     int(float64(cursor) / 1000.0 * float64(sampleRate)),
				chunkEnd:       int(float64(end) / 1000.0 * float64(sampleRate)),
				wordTimestamps: computeWordTimestampsApprox(s, cursor, end),
			})
			cursor = end
		}
		return segs
	}

	// 有 token 级时间戳，先组合为字/词级，再分句
	type tokenInfo struct {
		tok  string
		tSec float32
	}
	tis := make([]tokenInfo, 0, len(tokens))
	for i, tok := range tokens {
		tis = append(tis, tokenInfo{tok: tok, tSec: ts[i]})
	}

	// 将 BPE tokens 组合成可读文本（去 ▁ 空格）
	// 这里简化处理：按标点断句，同时按 token 进度估算句时间
	type charInfo struct {
		r    rune
		tSec float32
	}
	var chars []charInfo
	for _, ti := range tis {
		tok := strings.TrimPrefix(ti.tok, "▁")
		if tok == "" {
			continue
		}
		for _, r := range tok {
			chars = append(chars, charInfo{r: r, tSec: ti.tSec})
		}
	}

	// 构建文本与逐字符时间
	fullRunes := make([]rune, 0, len(chars))
	charTimes := make([]float32, 0, len(chars))
	for _, c := range chars {
		fullRunes = append(fullRunes, c.r)
		charTimes = append(charTimes, c.tSec)
	}
	_ = fullRunes
	_ = charTimes

	// 回退到按文本+原始结果首尾时间戳估算
	startSec := float32(0.0)
	endSec := float32(float64(len(samples)) / float64(sampleRate))
	if len(ts) > 0 {
		startSec = ts[0]
		endSec = ts[len(ts)-1]
		if len(results) > 0 && len(results[0].Durations) > 0 {
			endSec = startSec + results[0].Durations[len(results[0].Durations)-1]
		}
	}
	startMs := int64(startSec * 1000)
	endMs := int64(endSec * 1000)
	if endMs <= startMs {
		endMs = startMs + int64(float64(len(samples))/float64(sampleRate)*1000)
	}

	sents := simpleSplitSentences(text)
	var segs []sentenceSegment
	totalRunes := len([]rune(text))
	cursor := startMs
	chunkSampleStart := int(float64(startSec) * float64(sampleRate))
	chunkSampleEnd := len(samples)
	if int(float64(endSec)*float64(sampleRate)) < chunkSampleEnd {
		chunkSampleEnd = int(float64(endSec) * float64(sampleRate))
	}

	for si, s := range sents {
		srunes := len([]rune(s))
		if totalRunes == 0 {
			continue
		}
		ratio := float64(srunes) / float64(totalRunes)
		dur := int64(float64(endMs-startMs) * ratio)
		sEnd := cursor + dur
		if si == len(sents)-1 {
			sEnd = endMs
		}

		relStart := float64(cursor-startMs) / float64(endMs-startMs)
		relEnd := float64(sEnd-startMs) / float64(endMs-startMs)
		cs := chunkSampleStart + int(float64(chunkSampleEnd-chunkSampleStart)*relStart)
		ce := chunkSampleStart + int(float64(chunkSampleEnd-chunkSampleStart)*relEnd)
		if cs < 0 {
			cs = 0
		}
		if ce > len(samples) {
			ce = len(samples)
		}

		segs = append(segs, sentenceSegment{
			text:           s,
			startMs:        cursor,
			endMs:          sEnd,
			chunkStart:     cs,
			chunkEnd:       ce,
			wordTimestamps: computeWordTimestampsApprox(s, cursor, sEnd),
		})
		cursor = sEnd
	}

	return segs
}

// simpleSplitSentences 按中英文标点和长度简单断句
func simpleSplitSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// 先按主要标点切
	var parts []string
	var cur strings.Builder
	runes := []rune(text)
	for i, r := range runes {
		cur.WriteRune(r)
		switch r {
		case '。', '！', '？', '.', '!', '?', '；', ';', '，', ',':
			// 在标点处切句，但至少保留 4 个字符
			if cur.Len() >= 4 {
				parts = append(parts, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
		default:
			// 超过 60 字也强制切句（避免超长）
			if cur.Len() >= 60 && (unicode.IsSpace(r) || i == len(runes)-1) {
				parts = append(parts, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, strings.TrimSpace(cur.String()))
	}

	// 过滤空
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = []string{text}
	}
	return out
}

// computeWordTimestampsApprox 近似计算词级时间戳（复用 audio 包思路）
func computeWordTimestampsApprox(text string, startMs, endMs int64) []models.WordTimestamp {
	// 直接复用 audiosvc 包的实现（它是公有的吗？不是，故复制一份简化版）
	if text == "" || endMs <= startMs {
		return nil
	}
	words := splitWordsLocal(text)
	if len(words) == 0 {
		return nil
	}
	totalDur := endMs - startMs
	charTotal := 0
	for _, w := range words {
		charTotal += charCountLocal(w)
	}
	if charTotal == 0 {
		return nil
	}
	timestamps := make([]models.WordTimestamp, 0, len(words))
	cursor := startMs
	for _, w := range words {
		wc := charCountLocal(w)
		if wc == 0 {
			continue
		}
		dur := int64(float64(totalDur) * float64(wc) / float64(charTotal))
		wEnd := cursor + dur
		if wEnd > endMs {
			wEnd = endMs
		}
		timestamps = append(timestamps, models.WordTimestamp{
			Word:    w,
			StartMs: cursor,
			EndMs:   wEnd,
		})
		cursor = wEnd
	}
	return timestamps
}

func splitWordsLocal(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	hasCJK := false
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			hasCJK = true
			break
		}
	}
	if hasCJK {
		var words []string
		var cur strings.Builder
		for _, r := range text {
			if unicode.Is(unicode.Han, r) {
				if cur.Len() > 0 {
					words = append(words, cur.String())
					cur.Reset()
				}
				words = append(words, string(r))
			} else if r == ' ' {
				if cur.Len() > 0 {
					words = append(words, cur.String())
					cur.Reset()
				}
			} else {
				cur.WriteRune(r)
			}
		}
		if cur.Len() > 0 {
			words = append(words, cur.String())
		}
		return words
	}
	return strings.Fields(text)
}

func charCountLocal(word string) int {
	c := 0
	for range word {
		c++
	}
	return c
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
				lines = append(lines, fmt.Sprintf("%s:%.2f", word, hw.Weight))
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
	} else {
		cfg.HotwordsFile = ""
		cfg.HotwordsScore = 0
	}

	newRec := sherpa.NewOfflineRecognizer(&cfg)
	if newRec == nil {
		_ = os.Remove(tmpPath)
		return ErrModelLoadFailed
	}

	// 替换旧识别器，删除旧热词文件
	if s.recognizer != nil {
		sherpa.DeleteOfflineRecognizer(s.recognizer)
	}
	s.recognizer = newRec
	if s.hotwordsFile != "" {
		_ = os.Remove(s.hotwordsFile)
	}
	s.hotwordsFile = tmpPath

	s.deps.Log.Info(fmt.Sprintf("offline: hotwords applied, %d lines", strings.Count(hotwords, "\n")+1))
	return nil
}

// buildRecognizerConfig 根据 Config 组装离线识别器参数
func (s *Service) buildRecognizerConfig() sherpa.OfflineRecognizerConfig {
	cfg := sherpa.OfflineRecognizerConfig{
		FeatConfig: sherpa.FeatureConfig{
			SampleRate: s.cfg.SampleRate,
			FeatureDim: s.cfg.FeatureDim,
		},
		ModelConfig: sherpa.OfflineModelConfig{
			Tokens:     s.cfg.modelPath(s.cfg.Tokens),
			NumThreads: s.cfg.NumThreads,
			Provider:   s.cfg.Provider,
		},
		DecodingMethod: s.cfg.DecodingMethod,
		MaxActivePaths: s.cfg.MaxActivePaths,
		HotwordsScore:  s.cfg.HotwordsScore,
	}

	switch strings.ToLower(s.cfg.ModelType) {
	case "transducer":
		cfg.ModelConfig.Transducer = sherpa.OfflineTransducerModelConfig{
			Encoder: s.cfg.modelPath(s.cfg.Encoder),
			Decoder: s.cfg.modelPath(s.cfg.Decoder),
			Joiner:  s.cfg.modelPath(s.cfg.Joiner),
		}
	case "paraformer":
		cfg.ModelConfig.Paraformer = sherpa.OfflineParaformerModelConfig{
			Model: s.cfg.modelPath(s.cfg.Model),
		}
	default:
		// zipformer_ctc（默认）
		cfg.ModelConfig.ZipformerCtc = sherpa.OfflineZipformerCtcModelConfig{
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
	s.deps.Log.Info("offline: preloading offline ASR model...")

	cfg := s.buildRecognizerConfig()
	rec := sherpa.NewOfflineRecognizer(&cfg)

	s.mu.Lock()
	defer s.mu.Unlock()

	if rec == nil {
		s.loadErr = ErrModelLoadFailed
		s.deps.Log.Error("offline: failed to create offline recognizer (check model files exist)")
		return
	}
	if s.closed {
		sherpa.DeleteOfflineRecognizer(rec)
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
	// data 块大小（在 40-43 之后：44 是 data 标识，44-47 是 size）
	// 重新读取完整 data 大小
	if _, err := f.Seek(40, 0); err != nil {
		return 0, err
	}
	buf2 := make([]byte, 8)
	if _, err := f.Read(buf2); err != nil {
		return 0, err
	}
	dataSize := uint32(buf2[4]) | uint32(buf2[5])<<8 | uint32(buf2[6])<<16 | uint32(buf2[7])<<24
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
