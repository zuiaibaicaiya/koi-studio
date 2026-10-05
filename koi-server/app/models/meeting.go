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

// 会议模式取值
const (
	MeetingModeLive  = "live"  // 实时会议（实时录音转写）
	MeetingModeAudio = "audio" // 音频转写（上传音频文件转写）
)

// 会议音频转码状态取值（未转码时为空字符串）
const (
	MeetingTranscodeStatusPending   = "pending"   // 等待转码
	MeetingTranscodeStatusRunning   = "running"   // 转码中
	MeetingTranscodeStatusCompleted = "completed" // 转码完成
	MeetingTranscodeStatusFailed    = "failed"    // 转码失败
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
	// Mode 会议模式：live-实时会议，audio-音频转写
	Mode string `json:"mode" gorm:"column:mode" example:"live"`
	// AudioFilePath 会议录音文件路径，会议结束后由归档任务写入
	AudioFilePath string `json:"audio_file_path" gorm:"column:audio_file_path"`
	// OriginalFilePath 原始上传音频文件路径（转码前文件，位于 audio disk 的 original/ 目录），
	// 转码完成后保留供溯源，未走转码流程时为空
	OriginalFilePath string `json:"original_file_path" gorm:"column:original_file_path"`
	// TranscodeStatus 转码状态：pending/running/completed/failed，未转码为空
	TranscodeStatus string `json:"transcode_status" gorm:"column:transcode_status"`
	// TranscodeProgress 转码进度 0-100
	TranscodeProgress int `json:"transcode_progress" gorm:"column:transcode_progress"`
	// TranscodeError 最近一次转码失败原因
	TranscodeError string `json:"transcode_error" gorm:"column:transcode_error"`
	// TranscodeAttempts 转码已尝试次数
	TranscodeAttempts int `json:"transcode_attempts" gorm:"column:transcode_attempts"`
	// AudioURL 会议录音的可直接访问 URL，由 AudioFilePath 动态生成，不落库
	AudioURL string `json:"audio_url" gorm:"-" example:"http://localhost/audio/client123.wav"`
	// CreatedBy 创建人ID，关联 users 表
	CreatedBy uint `json:"created_by" gorm:"column:created_by" example:"1"`
}

// TableName 自定义表名
func (Meeting) TableName() string {
	return "meetings"
}
