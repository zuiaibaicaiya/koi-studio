package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"koi-server/app/facades"

	sherpa_onnx "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
	"github.com/zishang520/socket.io/servers/socket/v3"
)

// 单例：保证全局只预加载一次语音模型，避免重复构造 AudioService 造成多模型泄漏
var (
	defaultAudioService *AudioService
	audioServiceOnce    sync.Once
)

// GetAudioService 获取 AudioService 单例
func GetAudioService() *AudioService {
	audioServiceOnce.Do(func() {
		defaultAudioService = NewAudioService()
	})
	return defaultAudioService
}

// audioChunk 表示一个待处理的音频数据块（已从 Socket 缓冲复制，可安全异步处理）
type audioChunk struct {
	data []byte
	flag int
}

// AudioService handles audio data processing
type AudioService struct {
	// Mutex for thread safety（仅保护内部 map，不阻塞转写解码）
	mu sync.Mutex
	// Sherpa-ONNX recognizer configuration
	recognizerConfig *sherpa_onnx.OnlineRecognizerConfig
	// Shared recognizer instance (pre-loaded)
	sharedRecognizer *sherpa_onnx.OnlineRecognizer
	// Model loading status
	modelLoaded bool
	// Model loading error
	modelLoadError error
	// Hotwords for recognition enhancement
	hotwords string
	// Hotwords score for biasing
	hotwordsScore float32

	// Map to store audio data for each client
	clientBuffers map[string]*AudioBuffer
	// Map to store speech recognition contexts for each client
	clientRecognizers map[string]*ClientRecognizer
	// Map to store socket connections for each client
	clientSockets map[string]*socket.Socket
	// Map to store per-client async processing channel (避免阻塞 Socket.IO 事件循环)
	clientChans map[string]chan audioChunk
	// Map to store per-client stop signal
	clientStops map[string]chan struct{}
}

// AudioBuffer stores audio data for a client
type AudioBuffer struct {
	// PCM data buffer (used only for current processing, not accumulated)
	Data []byte
	// Last activity time
	LastActivity time.Time
	// Pre-allocated float32 buffer for audio samples
	SamplesBuffer []float32
	// Temporary file handle for persistent storage
	TempFile *os.File
	// Temporary file name
	TempFileName string
}

// ClientRecognizer stores speech recognition state for a client
type ClientRecognizer struct {
	// Sherpa-ONNX recognizer
	Recognizer *sherpa_onnx.OnlineRecognizer
	// Online stream
	Stream *sherpa_onnx.OnlineStream
	// Current transcript
	Transcript string
	// Start time of current utterance
	UtteranceStartTime time.Time
	// Whether we're in an active speech segment
	InSpeech bool
	// Last sent transcript (for real-time updates)
	LastSentTranscript string
	// Last send time (to throttle socket messages)
	LastSendTime time.Time
	// Batch counter for processing
	BatchCounter int
}

// NewAudioService creates a new audio service
func NewAudioService() *AudioService {
	_ = facades.Storage().Disk("audio").MakeDirectory(".")

	modelDir := "models/sherpa-onnx-streaming-zipformer-bilingual-zh-en-2023-02-20"
	recognizerConfig := &sherpa_onnx.OnlineRecognizerConfig{
		FeatConfig: sherpa_onnx.FeatureConfig{
			SampleRate: 16000,
			FeatureDim: 80,
		},
		ModelConfig: sherpa_onnx.OnlineModelConfig{
			Transducer: sherpa_onnx.OnlineTransducerModelConfig{
				Encoder: modelDir + "/encoder-epoch-99-avg-1.onnx",
				Decoder: modelDir + "/decoder-epoch-99-avg-1.onnx",
				Joiner:  modelDir + "/joiner-epoch-99-avg-1.onnx",
			},
			Tokens:     modelDir + "/tokens.txt",
			NumThreads: 2,
		},
		DecodingMethod:          "modified_beam_search",
		MaxActivePaths:          4,
		EnableEndpoint:          1,
		Rule1MinTrailingSilence: 0.5,
		Rule2MinTrailingSilence: 1.0,
		Rule3MinUtteranceLength: 15.0,
		HotwordsScore:           2.0,
		HotwordsFile:            modelDir + "/hotwords.txt",
	}

	service := &AudioService{
		clientBuffers:     make(map[string]*AudioBuffer),
		clientRecognizers: make(map[string]*ClientRecognizer),
		clientSockets:     make(map[string]*socket.Socket),
		clientChans:       make(map[string]chan audioChunk),
		clientStops:       make(map[string]chan struct{}),
		recognizerConfig:  recognizerConfig,
		modelLoaded:       false,
		hotwords:          "",
		hotwordsScore:     2.0,
	}

	go func() {
		startTime := time.Now()
		facades.Log().Info("开始预加载语音转写模型...")

		recognizer := sherpa_onnx.NewOnlineRecognizer(recognizerConfig)
		if recognizer == nil {
			service.modelLoadError = fmt.Errorf("failed to create shared recognizer")
			facades.Log().Error("预加载语音转写模型失败: " + service.modelLoadError.Error())
			return
		}

		service.sharedRecognizer = recognizer
		service.modelLoaded = true

		loadTime := time.Since(startTime)
		facades.Log().Info(fmt.Sprintf("语音转写模型预加载完成，耗时: %.2f秒", loadTime.Seconds()))
	}()

	service.cleanupOrphanedTempFiles()

	service.StartResourceCleanupScheduler(5*time.Minute, 10*time.Minute)

	return service
}

// IsModelReady 返回模型是否已加载完成
func (s *AudioService) IsModelReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.modelLoaded
}

// ProcessAudioData processes audio data from a client.
// 该方法只在 Socket.IO 事件协程中做「复制数据 + 非阻塞入队」后立即返回，
// 实际的音频解码与转写由每个客户端专属的工作协程异步完成，避免阻塞实时通信。
func (s *AudioService) ProcessAudioData(clientID string, data []byte, flag int) error {
	if clientID == "" {
		return fmt.Errorf("invalid client ID")
	}

	// 确保客户端资源（缓冲、识别流、工作协程）已就绪
	buffer, ch, err := s.ensureClient(clientID)
	if err != nil {
		return err
	}
	buffer.LastActivity = time.Now()

	// 复制音频数据，避免 Socket.IO 底层缓冲复用导致的数据竞争
	cp := make([]byte, len(data))
	copy(cp, data)

	// 非阻塞入队：客户端处理过慢时丢弃当前帧，避免阻塞 Socket.IO 事件循环
	select {
	case ch <- audioChunk{data: cp, flag: flag}:
	default:
		facades.Log().Debug(fmt.Sprintf("audio queue full, drop frame for client %s", clientID))
	}
	return nil
}

// ensureClient 获取（或创建）客户端的处理资源。客户端首次接入时创建独立的
// 临时文件、在线识别流以及专属的工作协程。模型未加载完成时会释放锁进行等待，
// 避免阻塞其它客户端。
func (s *AudioService) ensureClient(clientID string) (*AudioBuffer, chan audioChunk, error) {
	s.mu.Lock()
	if rec, ok := s.clientRecognizers[clientID]; ok {
		buf := s.clientBuffers[clientID]
		ch := s.clientChans[clientID]
		s.mu.Unlock()
		_ = rec
		return buf, ch, nil
	}
	s.mu.Unlock()

	// 等待模型加载完成（释放锁，避免阻塞其它客户端）
	waitStart := time.Now()
	for {
		s.mu.Lock()
		loaded := s.modelLoaded
		loadErr := s.modelLoadError
		s.mu.Unlock()
		if loaded {
			break
		}
		if loadErr != nil {
			return nil, nil, loadErr
		}
		if time.Since(waitStart) > 5*time.Second {
			return nil, nil, fmt.Errorf("model loading timeout")
		}
		time.Sleep(100 * time.Millisecond)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// 双重检查，防止并发重复创建
	if rec, ok := s.clientRecognizers[clientID]; ok {
		_ = rec
		return s.clientBuffers[clientID], s.clientChans[clientID], nil
	}

	tempFile, tempFileName, err := s.createTempFile(clientID)
	if err != nil {
		facades.Log().Warning(fmt.Sprintf("failed to create temp file for client %s: %v, falling back to memory mode", clientID, err))
	}

	buffer := &AudioBuffer{
		Data:          make([]byte, 0, 4096),
		LastActivity:  time.Now(),
		SamplesBuffer: make([]float32, 0, 2048),
		TempFile:      tempFile,
		TempFileName:  tempFileName,
	}
	stream := sherpa_onnx.NewOnlineStream(s.sharedRecognizer)
	if stream == nil {
		return nil, nil, fmt.Errorf("failed to create online stream")
	}
	recognizer := &ClientRecognizer{
		Recognizer:         s.sharedRecognizer, // Use shared recognizer
		Stream:             stream,
		Transcript:         "",
		UtteranceStartTime: time.Now(),
		InSpeech:           false,
		LastSentTranscript: "",
		LastSendTime:       time.Now(),
		BatchCounter:       0,
	}
	s.clientBuffers[clientID] = buffer
	s.clientRecognizers[clientID] = recognizer

	ch := make(chan audioChunk, 64)
	stop := make(chan struct{})
	s.clientChans[clientID] = ch
	s.clientStops[clientID] = stop

	go s.clientWorker(clientID, buffer, recognizer, ch, stop)
	facades.Log().Info(fmt.Sprintf("Created new speech recognizer for client: %s (using shared model)", clientID))
	return buffer, ch, nil
}

// clientWorker 每个客户端专属的异步处理协程，负责消费音频块并执行解码转写，
// 从而将耗时操作移出 Socket.IO 事件循环，多个客户端可真正并发转写。
func (s *AudioService) clientWorker(clientID string, buffer *AudioBuffer, recognizer *ClientRecognizer, ch chan audioChunk, stop chan struct{}) {
	for {
		select {
		case <-stop:
			// 客户端断开：收尾并清理资源
			s.finalizeClient(clientID, buffer, recognizer)
			return
		case chunk, ok := <-ch:
			if !ok {
				// 通道已关闭，资源已由 cleanup 释放
				return
			}
			if chunk.flag == 0 {
				// 最终帧：收尾并清理资源
				s.finalizeClient(clientID, buffer, recognizer)
				return
			}

			// 写入临时文件（不再逐帧 fsync，降低 I/O 开销）
			if buffer.TempFile != nil && len(chunk.data) > 0 {
				if err := s.appendToTempFile(buffer.TempFile, chunk.data); err != nil {
					facades.Log().Warning(fmt.Sprintf("failed to write to temp file for client %s: %v", clientID, err))
				}
			}

			// 转换为 float32 样本
			sampleCount := len(chunk.data) / 2
			if cap(buffer.SamplesBuffer) < sampleCount {
				buffer.SamplesBuffer = make([]float32, sampleCount, sampleCount*2)
			} else {
				buffer.SamplesBuffer = buffer.SamplesBuffer[:sampleCount]
			}
			for i := 0; i < sampleCount; i++ {
				buffer.SamplesBuffer[i] = float32(int16(binary.LittleEndian.Uint16(chunk.data[i*2:]))) / 32768.0
			}

			s.processAudioWithRecognizer(clientID, recognizer, buffer.SamplesBuffer)
		}
	}
}

// finalizeClient 完成一段音频的收尾工作：关闭临时文件、完成解码、保存 WAV、清理资源。
func (s *AudioService) finalizeClient(clientID string, buffer *AudioBuffer, recognizer *ClientRecognizer) {
	// 关闭临时文件（若存在）
	if buffer.TempFile != nil {
		buffer.TempFile.Close()
		buffer.TempFile = nil
	}

	// 完成识别
	recognizer.Stream.InputFinished()
	for recognizer.Recognizer.IsReady(recognizer.Stream) {
		recognizer.Recognizer.Decode(recognizer.Stream)
	}
	result := recognizer.Recognizer.GetResult(recognizer.Stream)
	if result.Text != "" {
		recognizer.Transcript += result.Text
	}

	// 保存 WAV 文件
	if err := s.processFinalFrame(clientID, buffer); err != nil {
		facades.Log().Error(fmt.Sprintf("failed to process final frame for client %s, temp file preserved: %v", clientID, err))
	}

	s.cleanupClientResources(clientID)
}

// processFinalFrame processes the final audio frame
func (s *AudioService) processFinalFrame(clientID string, buffer *AudioBuffer) error {
	var pcmData []byte
	var err error

	if buffer.TempFileName != "" {
		pcmData, err = s.readTempFile(buffer.TempFileName)
		if err != nil {
			facades.Log().Error(fmt.Sprintf("failed to read temp file for client %s: %v", clientID, err))
			return err
		}
	}

	if len(pcmData) == 0 {
		facades.Log().Warning(fmt.Sprintf("no PCM data for client %s", clientID))
		return nil
	}

	filename := fmt.Sprintf("%s.wav", clientID)

	wavData, err := s.pcmToWAV(pcmData)
	if err != nil {
		return err
	}

	if err := facades.Storage().Disk("audio").Put(filename, string(wavData)); err != nil {
		return err
	}

	facades.Log().Info(fmt.Sprintf("saved WAV file for client %s: %s", clientID, filename))

	if err := s.deleteTempFile(buffer.TempFileName); err != nil {
		facades.Log().Warning(fmt.Sprintf("failed to delete temp file for client %s: %v", clientID, err))
	}
	buffer.TempFileName = ""

	return nil
}

// pcmToWAV converts PCM data to WAV format
func (s *AudioService) pcmToWAV(pcmData []byte) ([]byte, error) {
	// Calculate WAV file size
	dataSize := len(pcmData)
	fileSize := dataSize + 44 // 44 bytes for WAV header

	// Create buffer for WAV data
	buf := new(bytes.Buffer)

	// Write RIFF header
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(fileSize-8))
	buf.WriteString("WAVE")

	// Write fmt chunk
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16))    // Subchunk1Size
	binary.Write(buf, binary.LittleEndian, uint16(1))     // AudioFormat (PCM)
	binary.Write(buf, binary.LittleEndian, uint16(1))     // NumChannels (Mono)
	binary.Write(buf, binary.LittleEndian, uint32(16000)) // SampleRate (16kHz)
	binary.Write(buf, binary.LittleEndian, uint32(32000)) // ByteRate (SampleRate * NumChannels * BitsPerSample/8)
	binary.Write(buf, binary.LittleEndian, uint16(2))     // BlockAlign (NumChannels * BitsPerSample/8)
	binary.Write(buf, binary.LittleEndian, uint16(16))    // BitsPerSample (16-bit)

	// Write data chunk
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(dataSize))
	buf.Write(pcmData)

	return buf.Bytes(), nil
}

func (s *AudioService) createTempFile(clientID string) (*os.File, string, error) {
	tempFileName := fmt.Sprintf("%s.pcm.tmp", clientID)
	tempFilePath := filepath.Join(facades.Storage().Disk("audio").Path(""), tempFileName)

	file, err := os.OpenFile(tempFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create temp file: %w", err)
	}

	return file, tempFileName, nil
}

func (s *AudioService) appendToTempFile(file *os.File, data []byte) error {
	if file == nil {
		return fmt.Errorf("temp file is nil")
	}

	// 移除逐帧 fsync：fsync 是昂贵的磁盘同步操作，改为仅在最终保存 WAV 时
	// 由操作系统统一刷盘，显著降低高频音频写入的 I/O 开销。
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write to temp file: %w", err)
	}
	return nil
}

func (s *AudioService) readTempFile(filename string) ([]byte, error) {
	filePath := filepath.Join(facades.Storage().Disk("audio").Path(""), filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read temp file: %w", err)
	}

	return data, nil
}

func (s *AudioService) deleteTempFile(filename string) error {
	if filename == "" {
		return nil
	}

	filePath := filepath.Join(facades.Storage().Disk("audio").Path(""), filename)

	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete temp file: %w", err)
	}

	return nil
}

func (s *AudioService) cleanupOrphanedTempFiles() {
	audioPath := facades.Storage().Disk("audio").Path("")

	entries, err := os.ReadDir(audioPath)
	if err != nil {
		facades.Log().Warning(fmt.Sprintf("failed to read audio directory: %v", err))
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if len(name) > 8 && name[len(name)-8:] == ".pcm.tmp" {
			filePath := filepath.Join(audioPath, name)
			if err := os.Remove(filePath); err != nil {
				facades.Log().Warning(fmt.Sprintf("failed to cleanup orphaned temp file %s: %v", name, err))
			} else {
				facades.Log().Info(fmt.Sprintf("cleaned up orphaned temp file: %s", name))
			}
		}
	}
}

// processAudioWithRecognizer processes audio through the online recognizer
func (s *AudioService) processAudioWithRecognizer(clientID string, recognizer *ClientRecognizer, samples []float32) {
	// Accept waveform from the stream
	recognizer.Stream.AcceptWaveform(16000, samples)

	// Increment batch counter
	recognizer.BatchCounter++

	// Process in batches to reduce CPU usage
	// Only process every 3rd batch or when endpoint is detected
	if recognizer.BatchCounter%3 == 0 || recognizer.Recognizer.IsEndpoint(recognizer.Stream) {
		// Decode while there are enough frames
		var currentTranscript string
		for recognizer.Recognizer.IsReady(recognizer.Stream) {
			recognizer.Recognizer.Decode(recognizer.Stream)

			// 获取当前转写结果
			result := recognizer.Recognizer.GetResult(recognizer.Stream)
			currentTranscript = result.Text
		}

		// Throttle socket messages to reduce network overhead
		if currentTranscript != "" && currentTranscript != recognizer.LastSentTranscript {
			// Only send if at least 200ms has passed since last send
			if time.Since(recognizer.LastSendTime) > 200*time.Millisecond {
				recognizer.LastSentTranscript = currentTranscript
				recognizer.LastSendTime = time.Now()
				// 发送实时转写结果到前端（isFinal=false表示中间结果）
				s.sendTranscriptToClient(clientID, currentTranscript, false)
			}
		}

		// Reset batch counter after processing
		recognizer.BatchCounter = 0
	}

	// Check for endpoint
	if recognizer.Recognizer.IsEndpoint(recognizer.Stream) {
		// Get the final result
		result := recognizer.Recognizer.GetResult(recognizer.Stream)
		transcript := result.Text
		if transcript != "" {
			recognizer.Transcript += transcript + " "
			// 发送最终结果到前端（isFinal=true表示最终结果）
			s.sendTranscriptToClient(clientID, transcript, true)
		}
		// Reset the stream for next utterance
		recognizer.Recognizer.Reset(recognizer.Stream)
		recognizer.UtteranceStartTime = time.Now()
		recognizer.LastSentTranscript = "" // 重置最后发送的转写结果
		recognizer.LastSendTime = time.Now()
	} else {
		// Check for maximum utterance length (20 seconds)
		if time.Since(recognizer.UtteranceStartTime) > 20*time.Second {
			// Force end of utterance due to maximum length
			recognizer.Stream.InputFinished()
			// Decode remaining frames
			for recognizer.Recognizer.IsReady(recognizer.Stream) {
				recognizer.Recognizer.Decode(recognizer.Stream)
			}
			// Get the result
			result := recognizer.Recognizer.GetResult(recognizer.Stream)
			transcript := result.Text
			if transcript != "" {
				recognizer.Transcript += transcript + " "
				// 发送最终结果到前端
				s.sendTranscriptToClient(clientID, transcript, true)
			}
			// Reset the stream for next utterance
			recognizer.Recognizer.Reset(recognizer.Stream)
			recognizer.UtteranceStartTime = time.Now()
			recognizer.LastSentTranscript = "" // 重置最后发送的转写结果
			recognizer.LastSendTime = time.Now()
		}
	}
}

// cleanupClientResources cleans up resources for a client
func (s *AudioService) cleanupClientResources(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clean up recognizer resources
	if recognizer, exists := s.clientRecognizers[clientID]; exists {
		sherpa_onnx.DeleteOnlineStream(recognizer.Stream)
		delete(s.clientRecognizers, clientID)
	}

	// Clean up buffer and temp file
	if buffer, exists := s.clientBuffers[clientID]; exists {
		if buffer.TempFile != nil {
			buffer.TempFile.Close()
		}
		if buffer.TempFileName != "" {
			s.deleteTempFile(buffer.TempFileName)
			facades.Log().Info(fmt.Sprintf("cleaned up unreferenced temp file for client %s", clientID))
		}
		delete(s.clientBuffers, clientID)
	}

	// Clean up socket
	if _, exists := s.clientSockets[clientID]; exists {
		delete(s.clientSockets, clientID)
	}

	// 通知工作协程退出并关闭通道，避免协程泄漏
	if stop, exists := s.clientStops[clientID]; exists {
		close(stop)
		delete(s.clientStops, clientID)
	}
	if ch, exists := s.clientChans[clientID]; exists {
		close(ch)
		delete(s.clientChans, clientID)
	}
}

// CleanupInactiveBuffers cleans up buffers for inactive clients
func (s *AudioService) CleanupInactiveBuffers(timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for clientID, buffer := range s.clientBuffers {
		if now.Sub(buffer.LastActivity) > timeout {
			s.cleanupClientResources(clientID)
		}
	}
}

// StartResourceCleanupScheduler starts a scheduler to clean up inactive resources
func (s *AudioService) StartResourceCleanupScheduler(interval time.Duration, timeout time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			<-ticker.C
			s.CleanupInactiveBuffers(timeout)
		}
	}()
}

// GetClientBufferSize returns the size of the buffer for a client
func (s *AudioService) GetClientBufferSize(clientID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if buffer, exists := s.clientBuffers[clientID]; exists {
		return len(buffer.Data)
	}
	return 0
}

// GetClientTranscript returns the current transcript for a client
func (s *AudioService) GetClientTranscript(clientID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if recognizer, exists := s.clientRecognizers[clientID]; exists {
		return recognizer.Transcript
	}
	return ""
}

// GetModelStatus returns the model loading status
func (s *AudioService) GetModelStatus() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := map[string]interface{}{
		"loaded": s.modelLoaded,
		"error":  nil,
	}

	if s.modelLoadError != nil {
		status["error"] = s.modelLoadError.Error()
	}

	return status
}

// RegisterClientSocket registers a client's socket connection
func (s *AudioService) RegisterClientSocket(clientID string, socket *socket.Socket) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clientSockets[clientID] = socket
}

// UnregisterClientSocket unregisters a client's socket connection.
// 通过关闭 stop 通道通知工作协程收尾退出，避免协程与资源泄漏。
func (s *AudioService) UnregisterClientSocket(clientID string) {
	s.mu.Lock()
	stop := s.clientStops[clientID]
	delete(s.clientStops, clientID)
	delete(s.clientSockets, clientID)
	s.mu.Unlock()

	if stop != nil {
		close(stop)
	}
}

// SetHotwords sets the hotwords for recognition enhancement
func (s *AudioService) SetHotwords(hotwords string, score float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.hotwords = hotwords
	s.hotwordsScore = score

	if s.modelLoaded && hotwords != "" {
		newConfig := *s.recognizerConfig
		newConfig.HotwordsBuf = hotwords
		newConfig.HotwordsBufSize = len(hotwords)
		newConfig.HotwordsScore = score

		newRecognizer := sherpa_onnx.NewOnlineRecognizer(&newConfig)
		if newRecognizer == nil {
			return fmt.Errorf("failed to create recognizer with hotwords")
		}

		oldRecognizer := s.sharedRecognizer
		s.sharedRecognizer = newRecognizer

		for clientID, clientRecognizer := range s.clientRecognizers {
			stream := sherpa_onnx.NewOnlineStream(s.sharedRecognizer)
			if stream == nil {
				facades.Log().Error(fmt.Sprintf("Failed to create stream for client %s during hotwords update", clientID))
				continue
			}
			sherpa_onnx.DeleteOnlineStream(clientRecognizer.Stream)
			clientRecognizer.Stream = stream
			clientRecognizer.Recognizer = s.sharedRecognizer
		}

		_ = oldRecognizer

		facades.Log().Info(fmt.Sprintf("Hotwords updated: %s, score: %.1f", hotwords, score))
	}

	return nil
}

// GetHotwords gets the current hotwords
func (s *AudioService) GetHotwords() (string, float32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.hotwords, s.hotwordsScore
}

// sendTranscriptToClient sends transcript to client via socket
func (s *AudioService) sendTranscriptToClient(clientID string, transcript string, isFinal bool) {
	s.mu.Lock()
	socket, exists := s.clientSockets[clientID]
	s.mu.Unlock()

	if exists {
		socket.Emit("transcript", map[string]interface{}{
			"text":    transcript,
			"isFinal": isFinal,
		})
	}
}
