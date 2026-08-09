package models

import (
	"encoding/json"

	"github.com/goravel/framework/database/orm"
)

// SpeakerAudio 说话人注册音频模型
//
// 每条记录保存一份上传的音频文件及其对应的声纹特征向量。
// 特征向量以 JSON 数组的形式存放在 embedding 字段，避免为其单独建表，
// 同时便于服务启动时批量重建内存中的声纹库。
type SpeakerAudio struct {
	orm.Model
	orm.SoftDeletes
	// SpeakerID 所属说话人 ID
	SpeakerID uint `json:"speaker_id" example:"1"`
	// FileName 上传时的原始文件名
	FileName string `json:"file_name" example:"zhangsan-01.wav"`
	// FilePath 音频在存储磁盘中的相对路径
	FilePath string `json:"file_path" example:"speakers/1/20260809-153000-ab12cd.wav"`
	// FileSize 音频文件大小（字节）
	FileSize int64 `json:"file_size" example:"320044"`
	// SampleRate 提取声纹时实际使用的采样率
	SampleRate int `json:"sample_rate" example:"16000"`
	// Duration 音频时长（秒）
	Duration float64 `json:"duration" example:"10.5"`
	// Dim 声纹特征维度
	Dim int `json:"dim" example:"192"`
	// Embedding 声纹特征向量，序列化为 JSON 数组文本存储。
	// 该字段体积较大且属于内部数据，不对外输出。
	Embedding string `json:"-"`
	// Remark 备注
	Remark string `json:"remark" example:"安静环境录制"`
	// Speaker 关联的说话人
	Speaker *Speaker `json:"speaker,omitempty" gorm:"foreignKey:SpeakerID"`
}

// Vector 把存储的 JSON 文本反序列化为声纹特征向量。
func (audio *SpeakerAudio) Vector() ([]float32, error) {
	if audio.Embedding == "" {
		return nil, nil
	}

	var vector []float32
	if err := json.Unmarshal([]byte(audio.Embedding), &vector); err != nil {
		return nil, err
	}

	return vector, nil
}

// SetVector 把声纹特征向量序列化后写入 Embedding 字段，并同步维度。
func (audio *SpeakerAudio) SetVector(vector []float32) error {
	encoded, err := json.Marshal(vector)
	if err != nil {
		return err
	}

	audio.Embedding = string(encoded)
	audio.Dim = len(vector)

	return nil
}
