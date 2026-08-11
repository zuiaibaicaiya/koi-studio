// Package jobs 定义可入队执行的后台任务。
package jobs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"koi-server/app/facades"
	"koi-server/app/models"
	"koi-server/app/services/audio"
)

// ArchiveRecording 把会话录音的临时 PCM 文件转码为 WAV 并落盘。
//
// 该任务由转写服务在会话结束时派发。放入队列执行有两个目的：
//  1. 把可能较大的文件读写移出转写工作协程，避免影响下一次会话的实时性；
//  2. 失败后可依赖队列重试，临时文件在成功前不会被删除。
type ArchiveRecording struct{}

// Signature 任务唯一标识。
func (r *ArchiveRecording) Signature() string {
	return "audio:archive_recording"
}

// Handle 执行归档。
//
// 参数约定：args[0] string 客户端 ID，args[1] string 临时文件名，args[2] uint meetingID（可选，0 表示不关联会议）。
func (r *ArchiveRecording) Handle(args ...any) error {
	if len(args) < 2 {
		return fmt.Errorf("archive recording: expected at least 2 arguments, got %d", len(args))
	}

	clientID, ok := args[0].(string)
	if !ok || clientID == "" {
		return fmt.Errorf("archive recording: invalid client id %v", args[0])
	}
	tempName, ok := args[1].(string)
	if !ok {
		return fmt.Errorf("archive recording: invalid temp file %v", args[1])
	}
	if err := validTempName(tempName); err != nil {
		return err
	}

	// 可选参数：会议ID
	var meetingID uint
	if len(args) >= 3 {
		switch v := args[2].(type) {
		case uint:
			meetingID = v
		case int:
			meetingID = uint(v)
		case int64:
			meetingID = uint(v)
		}
	}

	config := facades.Config()
	disk := facades.Storage().Disk(config.GetString("audio.storage.disk", "audio"))
	tempPath := filepath.Join(disk.Path(""), tempName)

	pcm, err := os.ReadFile(tempPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件已被清理（例如重复投递），视为已完成，避免无意义重试。
			facades.Log().Warning(fmt.Sprintf("archive recording: temp file %s no longer exists", tempName))
			return nil
		}
		return fmt.Errorf("archive recording: read temp file: %w", err)
	}

	if len(pcm) == 0 {
		facades.Log().Warning(fmt.Sprintf("archive recording: no audio captured for client %s", clientID))
		return removeTemp(tempPath)
	}

	wav, err := audio.PCMToWAV(pcm, config.GetInt("audio.stream.sample_rate", 16000))
	if err != nil {
		return fmt.Errorf("archive recording: encode wav: %w", err)
	}

	filename := filepath.Base(clientID) + ".wav"
	if err := disk.Put(filename, string(wav)); err != nil {
		return fmt.Errorf("archive recording: persist wav: %w", err)
	}

	facades.Log().Info(fmt.Sprintf("archive recording: saved %s (%d bytes)", filename, len(wav)))

	// 关联会议：若提供了 meetingID，更新会议记录中的音频文件路径
	if meetingID > 0 {
		if _, err := facades.Orm().Query().
			Model(&models.Meeting{}).
			Where("id = ?", meetingID).
			Update("audio_file_path", filename); err != nil {
			facades.Log().Warning(fmt.Sprintf("archive recording: failed to update meeting %d audio_file_path: %v", meetingID, err))
		} else {
			facades.Log().Info(fmt.Sprintf("archive recording: linked %s to meeting %d", filename, meetingID))
		}
	}

	return removeTemp(tempPath)
}

// validTempName 拒绝带路径分隔符或非法后缀的文件名，防止越权读写。
func validTempName(name string) error {
	if name == "" {
		return fmt.Errorf("archive recording: empty temp file name")
	}
	if name != filepath.Base(name) || strings.ContainsRune(name, os.PathSeparator) {
		return fmt.Errorf("archive recording: illegal temp file name %q", name)
	}
	if !strings.HasSuffix(name, ".pcm.tmp") {
		return fmt.Errorf("archive recording: unexpected temp file name %q", name)
	}
	return nil
}

// removeTemp 删除临时文件，缺失视为成功。
func removeTemp(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		facades.Log().Warning(fmt.Sprintf("archive recording: failed to delete temp file %s: %v", path, err))
	}
	return nil
}
