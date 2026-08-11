package models

import (
	"encoding/json"

	"github.com/goravel/framework/database/orm"
)

// WordTimestamp 词级时间戳
type WordTimestamp struct {
	Word    string `json:"word"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
}

// MeetingTranscript 会议转写记录模型
//
// 每条记录对应实时转写中识别出的一个完整语句（由 sherpa-onnx 端点检测断句）。
// 说话人由声纹识别确定，未识别时 speaker_name 为"未知说话人"、speaker_id 为 null。
type MeetingTranscript struct {
	orm.Model
	orm.SoftDeletes
	// MeetingID 关联的会议ID
	MeetingID uint `json:"meeting_id" gorm:"column:meeting_id" example:"1"`
	// SpeakerID 识别的说话人ID，未识别时为nil
	SpeakerID *uint `json:"speaker_id" gorm:"column:speaker_id" example:"1"`
	// SpeakerName 说话人名称，未识别时为"未知说话人"
	SpeakerName string `json:"speaker_name" gorm:"column:speaker_name" example:"张三"`
	// Text 转写文本内容
	Text string `json:"text" gorm:"column:text" example:"今天的会议主要讨论以下几个议题"`
	// StartMs 语句起始相对时间（毫秒，相对于音频开始位置）
	StartMs int64 `json:"start_ms" gorm:"column:start_ms" example:"1200"`
	// EndMs 语句结束相对时间（毫秒，相对于音频开始位置）
	EndMs int64 `json:"end_ms" gorm:"column:end_ms" example:"5800"`
	// WordTimestamps 词级时间戳，JSON 数组文本
	WordTimestamps string `json:"word_timestamps" gorm:"column:word_timestamps" example:"[{\"word\":\"今天\",\"start_ms\":1200,\"end_ms\":1450}]"`
	// IsFinal 是否为最终结果
	IsFinal bool `json:"is_final" gorm:"column:is_final" example:"true"`
}

// TableName 自定义表名
func (MeetingTranscript) TableName() string {
	return "meeting_transcripts"
}

// SetWordTimestamps 将词级时间戳切片序列化后写入 WordTimestamps 字段
func (mt *MeetingTranscript) SetWordTimestamps(timestamps []WordTimestamp) error {
	encoded, err := json.Marshal(timestamps)
	if err != nil {
		return err
	}

	mt.WordTimestamps = string(encoded)

	return nil
}

// GetWordTimestamps 将 WordTimestamps 字段反序列化为词级时间戳切片
func (mt *MeetingTranscript) GetWordTimestamps() ([]WordTimestamp, error) {
	if mt.WordTimestamps == "" {
		return nil, nil
	}

	var timestamps []WordTimestamp
	if err := json.Unmarshal([]byte(mt.WordTimestamps), &timestamps); err != nil {
		return nil, err
	}

	return timestamps, nil
}
