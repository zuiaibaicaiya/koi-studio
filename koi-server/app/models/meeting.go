package models

import (
	"github.com/dromara/carbon/v2"
	"github.com/goravel/framework/database/orm"
)

// 会议状态取值
const (
	MeetingStatusCreated  = "created"  // 已创建，未开始
	MeetingStatusOngoing  = "ongoing"  // 进行中（实时转写中）
	MeetingStatusFinished = "finished" // 已结束
)

// Meeting 实时会议模型
type Meeting struct {
	orm.Model
	orm.SoftDeletes
	// Name 会议名称（纯文本）
	Name string `json:"name" gorm:"column:name" example:"产品周会"`
	// Participants 参会人员（纯文本）
	Participants string `json:"participants" gorm:"column:participants" example:"张三、李四、王五"`
	// SpeakerIds 说话人ID列表，逗号分隔，关联 speakers 表
	SpeakerIds string `json:"speaker_ids" gorm:"column:speaker_ids" example:"1,2,3"`
	// HotWordLibraryIds 关联的热词库ID列表，逗号分隔，可空
	HotWordLibraryIds string `json:"hot_word_library_ids" gorm:"column:hot_word_library_ids" example:"1,2,3"`
	// StartTime 开始时间
	StartTime carbon.DateTime `json:"start_time" gorm:"column:start_time" example:"2026-08-10 09:00:00"`
	// EndTime 结束时间
	EndTime carbon.DateTime `json:"end_time" gorm:"column:end_time" example:"2026-08-10 10:00:00"`
	// Status 会议状态：created-已创建，ongoing-进行中，finished-已结束
	Status string `json:"status" gorm:"column:status" example:"created"`
	// AudioFilePath 会议录音文件路径，会议结束后由归档任务写入
	AudioFilePath string `json:"audio_file_path" gorm:"column:audio_file_path"`
	// AudioURL 会议录音的可直接访问 URL，由 AudioFilePath 动态生成，不落库
	AudioURL string `json:"audio_url" gorm:"-" example:"http://localhost/audio/client123.wav"`
	// CreatedBy 创建人ID，关联 users 表
	CreatedBy uint `json:"created_by" gorm:"column:created_by" example:"1"`
}

// TableName 自定义表名
func (Meeting) TableName() string {
	return "meetings"
}
