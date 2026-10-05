// Package transcode 负责将上传的音频文件转码为离线转写所需的格式
// （16kHz / 16bit / 单声道 PCM WAV）。
//
// 实现要点：
//   - 调用外部 ffmpeg 二进制完成解码与重采样，格式覆盖 mp3/m4a/flac/ogg 等；
//   - ffmpeg 路径按「配置项 → 可执行文件同级目录 → PATH」解析，兼容
//     Electron 客户端捆绑（Resources/koi-server-bin/ffmpeg）与服务器部署；
//   - 转码以 goroutine 异步执行（项目队列默认 sync 连接会阻塞请求、
//     database 连接无独立 worker 消费，均不可用），状态与进度持久化到
//     meetings 表，服务重启后可识别中断任务；
//   - 失败后按 audio.transcode.max_attempts 自动重试，超过上限标记失败，
//     由 POST /meeting/{id}/retranscode 手动重试。
package transcode

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"koi-server/app/facades"
	"koi-server/app/models"
	audiosvc "koi-server/app/services/audio"
)

// TargetSampleRate 转码目标采样率，与离线转写模型要求一致。
const TargetSampleRate = 16000

// SupportedInputExtensions 允许上传的音频扩展名（含转码场景）。
var SupportedInputExtensions = map[string]struct{}{
	"wav":  {},
	"wave": {},
	"mp3":  {},
	"m4a":  {},
	"aac":  {},
	"flac": {},
	"ogg":  {},
	"oga":  {},
	"opus": {},
	"webm": {},
	"amr":  {},
	"wma":  {},
}

// Service 音频转码服务：进程内异步执行 + meetings 表持久化进度。
type Service struct {
	// mu 保护 running 集合。
	mu sync.Mutex
	// running 记录正在转码的会议 ID，用于去重与中断识别。
	running map[uint]struct{}
	// sem 限制并发转码数。
	sem chan struct{}
	// maxAttempts 失败自动重试上限（含首次执行）。
	maxAttempts int
	// retryDelay 自动重试前的等待时间。
	retryDelay time.Duration
	// onSuccess 转码成功后的回调（触发离线转写），由装配方注入。
	onSuccess func(meetingID uint) error
}

// NewService 创建转码服务，并发数与重试策略来自 audio.transcode 配置。
func NewService() *Service {
	concurrency := facades.Config().GetInt("audio.transcode.max_concurrency", 1)
	if concurrency < 1 {
		concurrency = 1
	}
	maxAttempts := facades.Config().GetInt("audio.transcode.max_attempts", 3)
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return &Service{
		running:     make(map[uint]struct{}),
		sem:         make(chan struct{}, concurrency),
		maxAttempts: maxAttempts,
		retryDelay:  3 * time.Second,
	}
}

// SetOnSuccess 注册转码成功后的回调（通常用于自动触发离线转写）。
func (s *Service) SetOnSuccess(fn func(meetingID uint) error) {
	s.onSuccess = fn
}

// IsRunning 指定会议是否正在转码。
func (s *Service) IsRunning(meetingID uint) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.running[meetingID]
	return ok
}

// RecoverStale 服务启动时把遗留的 running 状态标记为失败，
// 避免进程重启后进度查询永远停留在 running。
func (s *Service) RecoverStale() {
	result, err := facades.Orm().Query().
		Model(&models.Meeting{}).
		Where("transcode_status = ?", models.MeetingTranscodeStatusRunning).
		Update(map[string]any{
			"transcode_status": models.MeetingTranscodeStatusFailed,
			"transcode_error":  "服务重启导致转码中断，请手动重试",
		})
	if err != nil {
		facades.Log().Warning(fmt.Sprintf("transcode: recover stale running tasks failed: %v", err))
		return
	}
	if result != nil && result.RowsAffected > 0 {
		facades.Log().Info(fmt.Sprintf("transcode: recovered %d stale running task(s)", result.RowsAffected))
	}
}

// Submit 提交会议的异步转码任务（手动触发，重置尝试次数）。
func (s *Service) Submit(meetingID uint) error {
	if err := s.resetAndDispatch(meetingID); err != nil {
		return err
	}
	return nil
}

// resetAndDispatch 重置转码状态为 pending 并派发后台执行。
func (s *Service) resetAndDispatch(meetingID uint) error {
	s.mu.Lock()
	if _, ok := s.running[meetingID]; ok {
		s.mu.Unlock()
		return fmt.Errorf("会议 %d 已有转码任务进行中", meetingID)
	}
	s.mu.Unlock()

	// 检查原始文件是否就绪
	var meeting models.Meeting
	if err := facades.Orm().Query().FindOrFail(&meeting, meetingID); err != nil {
		return fmt.Errorf("会议 %d 不存在: %w", meetingID, err)
	}
	if meeting.OriginalFilePath == "" {
		return fmt.Errorf("会议 %d 没有待转码的原始音频文件", meetingID)
	}

	if err := s.updateMeeting(meetingID, map[string]any{
		"transcode_status":   models.MeetingTranscodeStatusPending,
		"transcode_progress": 0,
		"transcode_error":    "",
		"transcode_attempts": 0,
	}); err != nil {
		return fmt.Errorf("重置转码状态失败: %w", err)
	}

	go s.execute(meetingID, 0)
	return nil
}

// execute 执行一次转码（含自动重试循环）。attempt 从 0 开始。
func (s *Service) execute(meetingID uint, attempt int) {
	s.mu.Lock()
	if _, ok := s.running[meetingID]; ok {
		s.mu.Unlock()
		return
	}
	s.running[meetingID] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.running, meetingID)
		s.mu.Unlock()
	}()

	// 并发限制：超过上限时排队等待
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	_ = s.updateMeeting(meetingID, map[string]any{
		"transcode_status":   models.MeetingTranscodeStatusRunning,
		"transcode_progress": 0,
		"transcode_error":    "",
		"transcode_attempts": attempt + 1,
	})

	if err := s.runOnce(meetingID); err != nil {
		facades.Log().Warning(fmt.Sprintf("transcode: meeting %d attempt %d/%d failed: %v",
			meetingID, attempt+1, s.maxAttempts, err))

		if attempt+1 < s.maxAttempts {
			// 未达重试上限，延迟后自动重试
			_ = s.updateMeeting(meetingID, map[string]any{
				"transcode_status": models.MeetingTranscodeStatusPending,
				"transcode_error":  fmt.Sprintf("第 %d 次转码失败，将自动重试: %s", attempt+1, trimError(err)),
			})
			time.AfterFunc(s.retryDelay, func() {
				s.execute(meetingID, attempt+1)
			})
			return
		}

		_ = s.updateMeeting(meetingID, map[string]any{
			"transcode_status": models.MeetingTranscodeStatusFailed,
			"transcode_error":  trimError(err),
		})
		return
	}

	// 转码成功
	_ = s.updateMeeting(meetingID, map[string]any{
		"transcode_status":   models.MeetingTranscodeStatusCompleted,
		"transcode_progress": 100,
		"transcode_error":    "",
	})

	// 触发后续转写；失败不影响转码结果，可由用户手动重试转写
	if s.onSuccess != nil {
		if err := s.onSuccess(meetingID); err != nil {
			facades.Log().Warning(fmt.Sprintf("transcode: meeting %d trigger transcription failed: %v", meetingID, err))
		}
	}
}

// runOnce 单次转码：读取会议原始文件 → ffmpeg 转码 → 校验输出 → 回写会议。
func (s *Service) runOnce(meetingID uint) error {
	var meeting models.Meeting
	if err := facades.Orm().Query().FindOrFail(&meeting, meetingID); err != nil {
		return fmt.Errorf("load meeting: %w", err)
	}
	if meeting.OriginalFilePath == "" {
		return fmt.Errorf("original audio file is missing")
	}

	diskName := facades.Config().GetString("audio.storage.disk", "audio")
	disk := facades.Storage().Disk(diskName)
	srcPath := filepath.Join(disk.Path(""), meeting.OriginalFilePath)
	if _, err := os.Stat(srcPath); err != nil {
		return fmt.Errorf("source file not accessible: %w", err)
	}

	// 输出文件名：UUIDv7（去连字符 32 位 hex），与现有音频命名风格一致
	uuidV7, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate uuid: %w", err)
	}
	dstRel := strings.ReplaceAll(uuidV7.String(), "-", "") + ".wav"
	dstPath := filepath.Join(disk.Path(""), dstRel)

	// 清理残留的同名输出文件
	_ = os.Remove(dstPath)

	progress := newProgressReporter(meetingID, s.updateMeeting)
	if err := TranscodeFile(srcPath, dstPath, progress.report); err != nil {
		_ = os.Remove(dstPath)
		return err
	}

	// 校验输出文件头：确认是 16kHz 16bit PCM WAV
	head := make([]byte, 64)
	f, err := os.Open(dstPath)
	if err != nil {
		return fmt.Errorf("open output: %w", err)
	}
	n, _ := f.Read(head)
	_ = f.Close()
	info, err := audiosvc.ParseWAVHeader(head[:n])
	if err != nil {
		return fmt.Errorf("output is not a valid WAV file: %w", err)
	}
	if info.SampleRate != TargetSampleRate || info.BitsPerSample != 16 {
		return fmt.Errorf("unexpected output format: %dHz %dbit", info.SampleRate, info.BitsPerSample)
	}

	// 清理旧的转码输出（保留原始文件供溯源）
	if meeting.AudioFilePath != "" {
		_ = disk.Delete(meeting.AudioFilePath)
	}

	if err := s.updateMeeting(meetingID, map[string]any{
		"audio_file_path": dstRel,
	}); err != nil {
		return fmt.Errorf("update meeting audio_file_path: %w", err)
	}

	facades.Log().Info(fmt.Sprintf("transcode: meeting %d done, output %s (duration %.1fs)",
		meetingID, dstRel, info.DurationSec))
	return nil
}

// updateMeeting 更新会议记录的指定字段。
func (s *Service) updateMeeting(meetingID uint, values map[string]any) error {
	_, err := facades.Orm().Query().
		Model(&models.Meeting{}).
		Where("id = ?", meetingID).
		Update(values)
	return err
}

// =====================================================================
// ffmpeg 调用与进度解析
// =====================================================================

// durationPattern 匹配 ffmpeg 输出中的 "Duration: HH:MM:SS.ms"。
var durationPattern = regexp.MustCompile(`Duration:\s*(\d+):(\d+):([\d.]+)`)

// TranscodeFile 调用 ffmpeg 将 src 转码为 16kHz/16bit/单声道 PCM WAV。
//
// progress 非空时按 0-99 上报百分比（完成时由调用方置 100）。
func TranscodeFile(src, dst string, progress func(percent int)) error {
	ffmpeg, err := ResolveFFmpeg()
	if err != nil {
		return fmt.Errorf("未找到可用的 ffmpeg，请安装或通过 FFMPEG_PATH 指定: %w", err)
	}

	args := []string{
		"-y", "-nostdin", "-nostats",
		"-i", src,
		"-vn",      // 丢弃视频轨（webm 等容器可能带视频）
		"-ac", "1", // 单声道
		"-ar", strconv.Itoa(TargetSampleRate),
		"-c:a", "pcm_s16le", // 16bit PCM
		"-f", "wav",
	}

	if facades.Config().GetBool("audio.transcode.normalize", false) {
		// 单遍响度归一化：会议录音音量差异大，归一化有助于提升识别率
		args = append(args, "-af", "loudnorm=I=-16:TP=-1.5:LRA=11")
	}

	// 剥离 LIST 等元数据块，输出标准 44 字节头的 WAV：
	// 下游 estimateWavDuration 按 data chunk 的固定偏移读取，带元数据会算错时长
	args = append(args, "-map_metadata", "-1", "-fflags", "+bitexact")

	// -progress pipe:1 以机器可读格式输出进度（out_time_ms 实为微秒）
	args = append(args, "-progress", "pipe:1", dst)

	cmd := exec.Command(ffmpeg, args...)
	stdout, serr := cmd.StdoutPipe()
	if serr != nil {
		return fmt.Errorf("create stdout pipe: %w", serr)
	}

	// 总时长来自 stderr 的 Duration 行，须在运行期流式解析，进度才有分母
	var stderrBuf strings.Builder
	var totalMu sync.Mutex
	var totalSeconds float64
	stderrParser := newLineParser(func(line string) {
		if m := durationPattern.FindStringSubmatch(line); m != nil {
			h, _ := strconv.ParseFloat(m[1], 64)
			min, _ := strconv.ParseFloat(m[2], 64)
			sec, _ := strconv.ParseFloat(m[3], 64)
			totalMu.Lock()
			totalSeconds = h*3600 + min*60 + sec
			totalMu.Unlock()
		}
	})
	cmd.Stderr = io.MultiWriter(&stderrBuf, stderrParser)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "out_time_ms=") || progress == nil {
				continue
			}
			totalMu.Lock()
			total := totalSeconds
			totalMu.Unlock()
			if total <= 0 {
				continue
			}
			us, perr := strconv.ParseInt(strings.TrimPrefix(line, "out_time_ms="), 10, 64)
			if perr != nil || us < 0 {
				continue
			}
			percent := int(float64(us) / 1e6 / total * 100)
			if percent < 0 {
				percent = 0
			}
			if percent > 99 {
				percent = 99
			}
			progress(percent)
		}
		done <- scanner.Err()
	}()

	if werr := cmd.Wait(); werr != nil {
		msg := lastLines(stderrBuf.String(), 5)
		return fmt.Errorf("ffmpeg 转码失败: %s: %s", werr, msg)
	}

	// 输出文件非空校验
	if st, statErr := os.Stat(dst); statErr != nil || st.Size() == 0 {
		return fmt.Errorf("转码输出文件为空或不可读")
	}
	return nil
}

// lineParser 按行拆分流式写入的数据并回调（处理跨 Write 的半行）。
type lineParser struct {
	onLine func(line string)
	buf    []byte
}

func newLineParser(onLine func(line string)) *lineParser {
	return &lineParser{onLine: onLine}
}

func (p *lineParser) Write(data []byte) (int, error) {
	p.buf = append(p.buf, data...)
	for {
		idx := bytes.IndexByte(p.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSpace(string(p.buf[:idx]))
		p.buf = p.buf[idx+1:]
		if line != "" {
			p.onLine(line)
		}
	}
	return len(data), nil
}

// progressReporter 带节流的进度上报（最多每秒写一次库）。
type progressReporter struct {
	meetingID uint
	update    func(meetingID uint, values map[string]any) error
	mu        sync.Mutex
	lastWrite time.Time
	lastPct   int
}

func newProgressReporter(meetingID uint, update func(meetingID uint, values map[string]any) error) *progressReporter {
	return &progressReporter{meetingID: meetingID, update: update}
}

func (r *progressReporter) report(percent int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if percent == r.lastPct || now.Sub(r.lastWrite) < time.Second {
		return
	}
	r.lastPct = percent
	r.lastWrite = now
	_ = r.update(r.meetingID, map[string]any{"transcode_progress": percent})
}

// =====================================================================
// ffmpeg 二进制解析
// =====================================================================

// ResolveFFmpeg 解析 ffmpeg 可执行文件路径，优先级：
//  1. 配置项 audio.transcode.ffmpeg_path（环境变量 FFMPEG_PATH）
//  2. 可执行文件同级目录 koi-server-bin/ffmpeg（Electron 客户端捆绑）
//  3. 可执行文件同级目录 ffmpeg
//  4. $PATH（服务器安装了 ffmpeg 的场景）
func ResolveFFmpeg() (string, error) {
	if configured := facades.Config().GetString("audio.transcode.ffmpeg_path"); configured != "" {
		if isExecutableFile(configured) {
			return configured, nil
		}
		return "", fmt.Errorf("FFMPEG_PATH 指定的文件不可执行: %s", configured)
	}

	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		suffix := ""
		if runtime.GOOS == "windows" {
			suffix = ".exe"
		}
		candidates := []string{
			filepath.Join(dir, "koi-server-bin", "ffmpeg"+suffix),
			filepath.Join(dir, "ffmpeg"+suffix),
		}
		for _, cand := range candidates {
			if isExecutableFile(cand) {
				return cand, nil
			}
		}
	}

	return exec.LookPath("ffmpeg")
}

// isExecutableFile 判断路径是否存在且为可执行文件。
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

// lastLines 取文本最后 n 行（用于错误信息摘要）。
func lastLines(s string, n int) string {
	s = strings.TrimSpace(s)
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := strings.Join(lines, " | ")
	if len(out) > 500 {
		out = out[len(out)-500:]
	}
	return out
}

// trimError 压缩错误信息，避免超长写库。
func trimError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return msg
}
