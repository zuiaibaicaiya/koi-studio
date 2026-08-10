package models

import (
	"time"

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
	StartTime time.Time `json:"start_time" gorm:"column:start_time" example:"2026-08-10 09:00:00"`
	// EndTime 结束时间
	EndTime time.Time `json:"end_time" gorm:"column:end_time" example:"2026-08-10 10:00:00"`
	// Status 会议状态：created-已创建，ongoing-进行中，finished-已结束
	Status string `json:"status" gorm:"column:status" example:"created"`
	// CreatedBy 创建人ID，关联 users 表
	CreatedBy uint `json:"created_by" gorm:"column:created_by" example:"1"`
}

// TableName 自定义表名
func (Meeting) TableName() string {
	return "meetings"
}
