package services

import (
	"koi-server/app/facades"
	"koi-server/app/models"
)

// MeetingTranscriptService 会议转写记录服务
type MeetingTranscriptService struct{}

// NewMeetingTranscriptService 创建服务实例
func NewMeetingTranscriptService() *MeetingTranscriptService {
	return &MeetingTranscriptService{}
}

// Create 创建一条转写记录
func (s *MeetingTranscriptService) Create(transcript *models.MeetingTranscript) error {
	return facades.Orm().Query().Create(transcript)
}

// GetByMeetingID 分页查询指定会议的转写记录，按时间顺序排列
func (s *MeetingTranscriptService) GetByMeetingID(meetingID uint, page, pageSize int) ([]models.MeetingTranscript, int64, error) {
	var transcripts []models.MeetingTranscript
	var total int64

	err := facades.Orm().Query().
		Where("meeting_id = ?", meetingID).
		Where("is_final = ?", true).
		OrderBy("start_ms").
		Paginate(page, pageSize, &transcripts, &total)

	return transcripts, total, err
}

// GetByMeetingIDAll 获取指定会议的全部最终转写记录
func (s *MeetingTranscriptService) GetByMeetingIDAll(meetingID uint) ([]models.MeetingTranscript, error) {
	var transcripts []models.MeetingTranscript

	err := facades.Orm().Query().
		Where("meeting_id = ?", meetingID).
		Where("is_final = ?", true).
		OrderBy("start_ms").
		Find(&transcripts)

	return transcripts, err
}

// RelabelByTimeRange 把与 [startMs, endMs) 相交的最终转写记录重新归属到目标说话人。
//
// 用于「用户框选转写文字动态注册说话人」时回填历史归属：此前这些片段可能
// 被识别为其他说话人或未识别，注册完成后应按用户的框选结果统一纠正。
func (s *MeetingTranscriptService) RelabelByTimeRange(meetingID uint, startMs, endMs int64, speakerID uint, speakerName string) (int64, error) {
	if meetingID == 0 || speakerID == 0 || endMs <= startMs {
		return 0, nil
	}

	result, err := facades.Orm().Query().
		Model(&models.MeetingTranscript{}).
		Where("meeting_id = ?", meetingID).
		Where("is_final = ?", true).
		Where("start_ms < ?", endMs).
		Where("end_ms > ?", startMs).
		Update(map[string]any{
			"speaker_id":   speakerID,
			"speaker_name": speakerName,
		})
	if err != nil {
		return 0, err
	}
	if result == nil {
		return 0, nil
	}

	return result.RowsAffected, nil
}

// DeleteByMeetingID 软删除指定会议的全部转写记录
func (s *MeetingTranscriptService) DeleteByMeetingID(meetingID uint) error {
	_, err := facades.Orm().Query().
		Model(&models.MeetingTranscript{}).
		Where("meeting_id = ?", meetingID).
		Delete()

	return err
}
