package models

import "github.com/goravel/framework/database/orm"

// 说话人性别取值。
const (
	SpeakerGenderUnknown = "unknown"
	SpeakerGenderMale    = "male"
	SpeakerGenderFemale  = "female"
)

// 说话人状态取值。
const (
	SpeakerStatusActive   = "active"
	SpeakerStatusInactive = "inactive"
)

// Speaker 说话人模型
//
// 一个说话人可关联多条注册音频，每条音频对应一份声纹特征向量；
// 检索时以说话人为单位，把其名下所有声纹一并注册进 sherpa-onnx 的
// SpeakerEmbeddingManager，从而提升识别的鲁棒性。
type Speaker struct {
	orm.Model
	orm.SoftDeletes
	// Name 说话人名称，全局唯一，同时作为声纹库中的检索标识
	Name string `json:"name" example:"张三"`
	// Gender 性别：unknown-未知，male-男，female-女
	Gender string `json:"gender" example:"male"`
	// Description 说话人描述
	Description string `json:"description" example:"技术部产品经理"`
	// Status 状态：active-启用，inactive-禁用。禁用后不参与声纹检索
	Status string `json:"status" example:"active"`
	// EmbeddingDim 声纹特征维度，由模型决定，注册首条音频后写入
	EmbeddingDim int `json:"embedding_dim" example:"192"`
	// AudioCount 已注册的声纹音频数量
	AudioCount int `json:"audio_count" example:"3"`
	// Audios 关联的注册音频列表（一对多）
	Audios []SpeakerAudio `json:"audios,omitempty" gorm:"foreignKey:SpeakerID"`
}
