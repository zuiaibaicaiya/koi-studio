package models

import "github.com/goravel/framework/database/orm"

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
	// Description 说话人描述
	Description string `json:"description" example:"技术部产品经理"`
	// EmbeddingDim 声纹特征维度，由模型决定，注册首条音频后写入
	EmbeddingDim int `json:"embedding_dim" example:"192"`
	// AudioCount 已注册的声纹音频数量
	AudioCount int `json:"audio_count" example:"3"`
	// Audios 关联的注册音频列表（一对多）
	Audios []SpeakerAudio `json:"audios,omitempty" gorm:"foreignKey:SpeakerID"`
}
