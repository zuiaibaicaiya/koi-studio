package services

import (
	"github.com/goravel/framework/contracts/database/db"
	"github.com/goravel/framework/contracts/database/orm"

	"koi-server/app/facades"
	"koi-server/app/models"
)

// SpeakerService 说话人服务，封装说话人及其声纹音频的数据访问逻辑
type SpeakerService struct {
}

func NewSpeakerService() *SpeakerService {
	return &SpeakerService{}
}

// GetSpeakerList 分页获取说话人列表，支持关键词筛选
func (speakerService *SpeakerService) GetSpeakerList(page int, pageSize int, keyword string) (speakers []models.Speaker, total int64, err error) {
	query := facades.Orm().Query()

	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR description LIKE ?", like, like)
	}

	err = query.OrderByDesc("id").Paginate(page, pageSize, &speakers, &total)
	if err != nil {
		return speakers, total, err
	}

	// 实时统计声纹音频数量，避免依赖容易失准的冗余 audio_count 字段
	for i := range speakers {
		count, cerr := facades.Orm().Query().
			Model(&models.SpeakerAudio{}).
			Where("speaker_id = ?", speakers[i].ID).
			Count()
		if cerr != nil {
			facades.Log().Warning("统计说话人声纹数量失败: " + cerr.Error())
			continue
		}
		speakers[i].AudioCount = int(count)
	}

	return speakers, total, err
}

// AddSpeaker 创建说话人
func (speakerService *SpeakerService) AddSpeaker(speaker *models.Speaker) error {
	return facades.Orm().Query().Create(speaker)
}

// GetSpeakerById 根据 ID 查询说话人，不存在时返回错误
func (speakerService *SpeakerService) GetSpeakerById(id int) (speaker models.Speaker, err error) {
	err = facades.Orm().Query().FindOrFail(&speaker, id)
	if err != nil {
		return speaker, err
	}

	// 实时统计声纹数量，保证详情与列表一致
	if count, cerr := facades.Orm().Query().
		Model(&models.SpeakerAudio{}).
		Where("speaker_id = ?", speaker.ID).
		Count(); cerr == nil {
		speaker.AudioCount = int(count)
	}

	return speaker, err
}

// GetSpeakerDetail 查询说话人详情，并附带其名下的声纹音频列表
func (speakerService *SpeakerService) GetSpeakerDetail(id int) (speaker models.Speaker, err error) {
	speaker, err = speakerService.GetSpeakerById(id)
	if err != nil {
		return speaker, err
	}

	audios, err := speakerService.GetAudiosBySpeakerId(speaker.ID)
	if err != nil {
		return speaker, err
	}
	speaker.Audios = audios

	return speaker, nil
}

// UpdateSpeaker 更新说话人
func (speakerService *SpeakerService) UpdateSpeaker(speaker *models.Speaker) error {
	return facades.Orm().Query().Save(speaker)
}

// DeleteSpeakerById 根据 ID 软删除说话人，同时软删除其名下所有声纹音频
func (speakerService *SpeakerService) DeleteSpeakerById(id int) (*db.Result, error) {
	var result *db.Result

	err := facades.Orm().Transaction(func(tx orm.Query) error {
		if _, err := tx.Model(&models.SpeakerAudio{}).Where("speaker_id = ?", id).Delete(); err != nil {
			return err
		}

		deleted, err := tx.Model(&models.Speaker{}).Where("id = ?", id).Delete()
		if err != nil {
			return err
		}

		result = deleted

		return nil
	})

	return result, err
}

// IsSpeakerNameExists 判断说话人名称是否已存在，excludeID 大于 0 时排除该说话人（用于更新场景）
func (speakerService *SpeakerService) IsSpeakerNameExists(name string, excludeID uint) (bool, error) {
	query := facades.Orm().Query().Model(&models.Speaker{}).Where("name = ?", name)

	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}

	count, err := query.Count()

	return count > 0, err
}

// AddAudio 新增一条声纹音频，并同步说话人的声纹数量与特征维度
func (speakerService *SpeakerService) AddAudio(audio *models.SpeakerAudio) error {
	return facades.Orm().Transaction(func(tx orm.Query) error {
		if err := tx.Create(audio); err != nil {
			return err
		}

		count, err := tx.Model(&models.SpeakerAudio{}).Where("speaker_id = ?", audio.SpeakerID).Count()
		if err != nil {
			return err
		}

		_, err = tx.Model(&models.Speaker{}).Where("id = ?", audio.SpeakerID).Update(map[string]any{
			"audio_count":   count,
			"embedding_dim": audio.Dim,
		})

		return err
	})
}

// GetAudiosBySpeakerId 获取指定说话人的全部声纹音频，按创建时间倒序
func (speakerService *SpeakerService) GetAudiosBySpeakerId(speakerID uint) (audios []models.SpeakerAudio, err error) {
	err = facades.Orm().Query().
		Where("speaker_id = ?", speakerID).
		OrderByDesc("id").
		Find(&audios)

	return audios, err
}

// GetAudioById 查询指定说话人名下的某条声纹音频
func (speakerService *SpeakerService) GetAudioById(speakerID uint, audioID int) (audio models.SpeakerAudio, err error) {
	err = facades.Orm().Query().
		Where("speaker_id = ?", speakerID).
		Where("id = ?", audioID).
		FirstOrFail(&audio)

	return audio, err
}

// DeleteAudioById 软删除一条声纹音频，并同步说话人的声纹数量
func (speakerService *SpeakerService) DeleteAudioById(speakerID uint, audioID int) (*db.Result, error) {
	var result *db.Result

	err := facades.Orm().Transaction(func(tx orm.Query) error {
		deleted, err := tx.Model(&models.SpeakerAudio{}).
			Where("speaker_id = ?", speakerID).
			Where("id = ?", audioID).
			Delete()
		if err != nil {
			return err
		}
		result = deleted

		count, err := tx.Model(&models.SpeakerAudio{}).Where("speaker_id = ?", speakerID).Count()
		if err != nil {
			return err
		}

		_, err = tx.Model(&models.Speaker{}).Where("id = ?", speakerID).Update("audio_count", count)

		return err
	})

	return result, err
}

// GetActiveVoiceprints 读取所有说话人的声纹向量，用于重建内存声纹库。
//
// 返回值以说话人名称为键，与 sherpa-onnx 声纹库中的检索标识保持一致。
func (speakerService *SpeakerService) GetActiveVoiceprints() (map[string][][]float32, error) {
	var speakers []models.Speaker
	if err := facades.Orm().Query().
		Find(&speakers); err != nil {
		return nil, err
	}

	voiceprints := make(map[string][][]float32, len(speakers))
	for _, speaker := range speakers {
		vectors, err := speakerService.GetVectorsBySpeakerId(speaker.ID)
		if err != nil {
			return nil, err
		}
		if len(vectors) == 0 {
			continue
		}
		voiceprints[speaker.Name] = vectors
	}

	return voiceprints, nil
}

// GetVectorsBySpeakerId 读取指定说话人的全部声纹向量，跳过无法解析的脏数据
func (speakerService *SpeakerService) GetVectorsBySpeakerId(speakerID uint) ([][]float32, error) {
	audios, err := speakerService.GetAudiosBySpeakerId(speakerID)
	if err != nil {
		return nil, err
	}

	vectors := make([][]float32, 0, len(audios))
	for i := range audios {
		vector, verr := audios[i].Vector()
		if verr != nil {
			facades.Log().Warning("解析声纹特征失败: " + verr.Error())
			continue
		}
		if len(vector) == 0 {
			continue
		}
		vectors = append(vectors, vector)
	}

	return vectors, nil
}
