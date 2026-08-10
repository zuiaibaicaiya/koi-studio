package services

import (
	"errors"
	"strings"
	"time"

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
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// ParseMeetingTime 解析时间字符串为 time.Time
func ParseMeetingTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("时间不能为空")
	}

	for _, layout := range meetingTimeLayouts {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t, nil
		}
	}

	return time.Time{}, errors.New("时间格式不正确，请使用 2006-01-02 15:04:05")
}

// GetMeetingList 分页查询会议列表
func (s *MeetingService) GetMeetingList(page, pageSize int, keyword, status string) ([]models.Meeting, int64, error) {
	query := facades.Orm().Query()
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR participants LIKE ?", like, like)
	}
	if status != "" {
		query = query.Where("status = ?", status)
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
