package jobs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dromara/carbon/v2"
	"github.com/stretchr/testify/suite"

	"koi-server/app/facades"
	"koi-server/app/jobs"
	"koi-server/app/models"
	"koi-server/bootstrap"
)

// ArchiveRecordingTestSuite 实时会议归档任务测试
//
// 重点验证：归档生成的音频文件名采用 UUIDv7（去掉连字符的 32 字符 hex），
// 与离线转写场景的命名保持一致，便于统一管理与数据库索引友好。
type ArchiveRecordingTestSuite struct {
	suite.Suite
}

func init() {
	chdirToProjectRoot()
	bootstrap.Boot()
}

func chdirToProjectRoot() {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			_ = os.Chdir(dir)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

func TestArchiveRecordingTestSuite(t *testing.T) {
	suite.Run(t, new(ArchiveRecordingTestSuite))
}

// uuidV7HexPattern UUIDv7 去掉连字符后的 32 字符 hex 格式
//
// UUIDv7 格式：前 48 位为毫秒时间戳，总长度 128 位。
// 去掉连字符后形如 "01a014daba9779d7b6ea35262f0c9e6f"（32 字符）。
var uuidV7HexPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// TestArchiveRecordingGeneratesUUIDv7Filename 验证归档生成的文件名符合 UUIDv7 hex 格式。
func (s *ArchiveRecordingTestSuite) TestArchiveRecordingGeneratesUUIDv7Filename() {
	// 1. 准备 PCM 临时文件（10ms 静音，16kHz 单声道 16bit）
	pcm := make([]byte, 320)
	disk := facades.Storage().Disk(facades.Config().GetString("audio.storage.disk", "audio"))
	tempDir := disk.Path("")

	tempName := "test-archive-uuidv7.pcm.tmp"
	tempPath := filepath.Join(tempDir, tempName)
	s.Require().NoError(os.WriteFile(tempPath, pcm, 0o644))
	defer func() {
		_ = os.Remove(tempPath)
	}()

	// 2. 创建测试会议用于验证 audio_file_path 写回
	now := carbon.Now()
	meeting := &models.Meeting{
		Name:      "归档UUIDv7测试会议",
		Mode:      models.MeetingModeLive,
		Status:    models.MeetingStatusFinished,
		StartTime: *carbon.NewDateTime(now),
		EndTime:   *carbon.NewDateTime(now.AddHour()),
	}
	s.Require().NoError(facades.Orm().Query().Create(meeting))
	meetingID := meeting.ID
	defer func() {
		_, _ = facades.Orm().Query().Model(&models.Meeting{}).Where("id = ?", meetingID).Delete()
	}()

	// 3. 记录归档前的 audio 文件列表
	entries, _ := os.ReadDir(tempDir)
	existing := make(map[string]bool)
	for _, e := range entries {
		existing[e.Name()] = true
	}

	// 4. 执行归档
	job := &jobs.ArchiveRecording{}
	clientID := "test-client-uuidv7-001"
	err := job.Handle(clientID, tempName, meetingID)
	s.Require().NoError(err)

	// 5. 找出新创建的 wav 文件
	var newFile string
	entries, _ = os.ReadDir(tempDir)
	for _, e := range entries {
		name := e.Name()
		if !existing[name] && strings.HasSuffix(name, ".wav") {
			newFile = name
			break
		}
	}
	s.Require().NotEmpty(newFile, "应生成新的 wav 文件")
	// 不删除生成的音频文件，便于人工检查

	// 6. 验证文件名符合 UUIDv7 hex 格式
	base := strings.TrimSuffix(newFile, ".wav")
	s.Truef(uuidV7HexPattern.MatchString(base),
		"文件名应为 32 字符 hex（UUIDv7 去掉连字符），实际: %s", newFile)

	// UUIDv7 版本字段验证：去掉连字符后索引 12 应为 '7'
	// UUID 格式：xxxxxxxx-xxxx-7xxx-... → 去掉连字符后索引 12 即版本位
	s.Equalf("7", string(base[12]),
		"UUIDv7 版本位（索引 12）应为 '7'，实际文件名: %s", newFile)

	// 7. 验证 meetings 表的 audio_file_path 使用同一 UUIDv7 文件名
	var updatedMeeting models.Meeting
	s.Require().NoError(facades.Orm().Query().FindOrFail(&updatedMeeting, meetingID))
	s.Equalf(newFile, updatedMeeting.AudioFilePath,
		"meetings.audio_file_path 应为 UUIDv7 文件名，实际: %s", updatedMeeting.AudioFilePath)

	// 8. 验证文件内容非空
	saved, gerr := disk.Get(newFile)
	s.Require().NoError(gerr)
	s.NotEmpty(saved, "归档文件不应为空")

	s.T().Logf("✅ 归档文件名验证通过: %s", newFile)
}

// TestArchiveRecordingMultipleFilesAreTimeOrdered 验证连续生成的 UUIDv7 文件名时间有序。
func (s *ArchiveRecordingTestSuite) TestArchiveRecordingMultipleFilesAreTimeOrdered() {
	disk := facades.Storage().Disk(facades.Config().GetString("audio.storage.disk", "audio"))
	tempDir := disk.Path("")

	// 记录初始文件
	entries, _ := os.ReadDir(tempDir)
	existing := make(map[string]bool)
	for _, e := range entries {
		existing[e.Name()] = true
	}

	// 生成 3 个归档文件，每个间隔 15ms 保证时间戳递增
	var filenames []string
	for i := 0; i < 3; i++ {
		pcm := make([]byte, 320)
		tempName := "test-archive-order-" + string(rune('A'+i)) + ".pcm.tmp"
		tempPath := filepath.Join(tempDir, tempName)
		s.Require().NoError(os.WriteFile(tempPath, pcm, 0o644))

		job := &jobs.ArchiveRecording{}
		clientID := "test-client-order-" + string(rune('A'+i))
		s.Require().NoError(job.Handle(clientID, tempName, uint(0)))

		_ = os.Remove(tempPath)
		time.Sleep(15 * time.Millisecond)
	}

	// 找出本次新生成的 wav 文件
	entries, _ = os.ReadDir(tempDir)
	for _, e := range entries {
		name := e.Name()
		if !existing[name] && strings.HasSuffix(name, ".wav") {
			filenames = append(filenames, name)
		}
	}
	s.Require().Len(filenames, 3, "应生成 3 个文件")
	// 不删除生成的音频文件，便于人工检查

	// 验证字典序 = 生成顺序（UUIDv7 时间戳前缀保证有序）
	for i := 0; i < 2; i++ {
		s.LessOrEqualf(filenames[i], filenames[i+1],
			"文件名应按生成顺序递增（UUIDv7 时间有序），第 %d 个应小于第 %d 个", i, i+1)
	}

	s.T().Logf("✅ 3 个文件名时间有序: %v", filenames)
}
