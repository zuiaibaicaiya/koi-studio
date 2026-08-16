package services

import (
	"errors"
	"strings"

	"github.com/dromara/carbon/v2"
	"github.com/goravel/framework/contracts/database/db"

	"koi-server/app/facades"
	"koi-server/app/models"
)

// MeetingService 实时会议服务
type MeetingService struct{}

// NewMeetingService 创建会议服务实例
func NewMeetingService() *MeetingService {
	return &MeetingService{}
}

// meetingTimeLayouts 支持的时间字符串格式
var meetingTimeLayouts = []string{
	carbon.RFC3339Layout,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// ParseMeetingTime 解析时间字符串为 *carbon.DateTime
func ParseMeetingTime(value string) (*carbon.DateTime, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("时间不能为空")
	}

	for _, layout := range meetingTimeLayouts {
		if c := carbon.ParseByLayout(value, layout); !c.HasError() {
			return carbon.NewDateTime(c), nil
		}
	}

	return nil, errors.New("时间格式不正确，请使用 2006-01-02 15:04:05")
}

// GetMeetingList 分页查询会议列表，支持关键词、状态、模式、时间范围过滤
func (s *MeetingService) GetMeetingList(page, pageSize int, keyword, status, mode, startTime, endTime string) ([]models.Meeting, int64, error) {
	query := facades.Orm().Query()
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR participants LIKE ?", like, like)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if mode != "" {
		query = query.Where("mode = ?", mode)
	}
	// 时间段筛选：会议与所选区间有重叠即命中
	if startTime != "" && endTime != "" {
		query = query.Where("start_time <= ?", endTime).Where("end_time >= ?", startTime)
	} else if startTime != "" {
		query = query.Where("start_time >= ?", startTime)
	} else if endTime != "" {
		query = query.Where("end_time <= ?", endTime)
	}

	var meetings []models.Meeting
	var total int64
	err := query.OrderByDesc("id").Paginate(page, pageSize, &meetings, &total)
	return meetings, total, err
}

// GetMeetingById 根据ID获取会议
func (s *MeetingService) GetMeetingById(id int) (models.Meeting, error) {
	var meeting models.Meeting
	err := facades.Orm().Query().FindOrFail(&meeting, id)
	return meeting, err
}

// CreateMeeting 创建会议
func (s *MeetingService) CreateMeeting(meeting *models.Meeting) error {
	return facades.Orm().Query().Create(meeting)
}

// UpdateMeeting 更新会议
func (s *MeetingService) UpdateMeeting(meeting *models.Meeting) error {
	return facades.Orm().Query().Save(meeting)
}

// DeleteMeetingById 删除会议
func (s *MeetingService) DeleteMeetingById(id int) (*db.Result, error) {
	return facades.Orm().Query().Model(&models.Meeting{}).Where("id = ?", id).Delete()
}

// SetMeetingStatus 更新会议状态
func (s *MeetingService) SetMeetingStatus(id int, status string) error {
	_, err := facades.Orm().Query().Model(&models.Meeting{}).Where("id = ?", id).Update("status", status)
	return err
}

// GetAudioURL 根据音频文件路径生成可直接访问的 URL。
// audio_file_path 为空时返回空字符串；生成失败时记录日志并返回空字符串。
func (s *MeetingService) GetAudioURL(audioFilePath string) string {
	if audioFilePath == "" {
		return ""
	}

	diskName := facades.Config().GetString("audio.storage.disk", "audio")
	return facades.Storage().Disk(diskName).Url(audioFilePath)
}
