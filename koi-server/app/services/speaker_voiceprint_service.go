package services

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/goravel/framework/contracts/filesystem"
	"github.com/goravel/framework/support/str"

	contractsspeaker "koi-server/app/contracts/speaker"
	"koi-server/app/facades"
	"koi-server/app/models"
)

// 声纹注册与识别过程中可能返回的业务错误。
var (
	// ErrUnsupportedAudioFormat 上传的音频格式不受支持。
	ErrUnsupportedAudioFormat = errors.New("仅支持 wav 格式的音频文件")
	// ErrAudioTooLarge 上传的音频文件超出大小限制。
	ErrAudioTooLarge = errors.New("音频文件超出大小限制")
	// ErrSpeakerInactive 说话人已禁用，无法参与声纹检索。
	ErrSpeakerInactive = errors.New("说话人已禁用")
)

// supportedAudioExtensions 允许上传的音频扩展名。
//
// 声纹提取只接受 PCM/IEEE Float 编码的 WAV，其他容器需先行转码，
// 在此显式限制可以给出明确的错误提示，而不是在解码阶段才失败。
var supportedAudioExtensions = map[string]struct{}{
	"wav":  {},
	"wave": {},
}

// SpeakerVoiceprintService 声纹业务服务
//
// 负责串联三方：上传的音频文件、sherpa-onnx 声纹提取器与数据库，
// 并维护内存声纹库与数据库之间的一致性。
type SpeakerVoiceprintService struct {
	speakerService *SpeakerService

	// warmup 保证内存声纹库在进程内只从数据库重建一次。
	warmup sync.Once
}

func NewSpeakerVoiceprintService() *SpeakerVoiceprintService {
	return &SpeakerVoiceprintService{
		speakerService: NewSpeakerService(),
	}
}

// Status 返回声纹模型的加载状态。
func (voiceprintService *SpeakerVoiceprintService) Status() contractsspeaker.ModelStatus {
	return facades.Speaker().Status()
}

// RegisterAudio 为指定说话人注册一条声纹音频。
//
// 流程：校验文件 -> 提取声纹 -> 落盘归档 -> 写入数据库 -> 刷新内存声纹库。
// 任一环节失败都不会留下半成品：落盘后写库失败时会回收已保存的文件。
func (voiceprintService *SpeakerVoiceprintService) RegisterAudio(speaker *models.Speaker, file filesystem.File, remark string) (models.SpeakerAudio, error) {
	var audio models.SpeakerAudio

	data, err := voiceprintService.readUpload(file)
	if err != nil {
		return audio, err
	}

	feature, err := facades.Speaker().Extract(data)
	if err != nil {
		return audio, err
	}

	filePath, err := voiceprintService.store(speaker.ID, file, data)
	if err != nil {
		return audio, err
	}

	audio = models.SpeakerAudio{
		SpeakerID:  speaker.ID,
		FileName:   file.GetClientOriginalName(),
		FilePath:   filePath,
		FileSize:   int64(len(data)),
		SampleRate: feature.SampleRate,
		Duration:   feature.Duration,
		Remark:     remark,
	}
	if err := audio.SetVector(feature.Vector); err != nil {
		voiceprintService.removeFile(filePath)

		return models.SpeakerAudio{}, err
	}

	if err := voiceprintService.speakerService.AddAudio(&audio); err != nil {
		voiceprintService.removeFile(filePath)

		return models.SpeakerAudio{}, err
	}

	speaker.EmbeddingDim = audio.Dim
	if err := voiceprintService.SyncSpeaker(speaker); err != nil {
		// 内存声纹库刷新失败不影响已持久化的数据，下次预热时会自动补齐。
		facades.Log().Warning("刷新内存声纹库失败: " + err.Error())
	}

	return audio, nil
}

// RemoveAudio 删除一条声纹音频，并同步内存声纹库与磁盘文件。
func (voiceprintService *SpeakerVoiceprintService) RemoveAudio(speaker *models.Speaker, audioID int) error {
	audio, err := voiceprintService.speakerService.GetAudioById(speaker.ID, audioID)
	if err != nil {
		return err
	}

	result, err := voiceprintService.speakerService.DeleteAudioById(speaker.ID, audioID)
	if err != nil {
		return err
	}
	if result == nil || result.RowsAffected == 0 {
		return errors.New("删除失败")
	}

	voiceprintService.removeFile(audio.FilePath)

	if err := voiceprintService.SyncSpeaker(speaker); err != nil {
		facades.Log().Warning("刷新内存声纹库失败: " + err.Error())
	}

	return nil
}

// SyncSpeaker 用数据库中的最新数据刷新该说话人在内存声纹库中的声纹。
//
// 说话人被禁用或名下没有有效声纹时，将其从内存声纹库中移除。
func (voiceprintService *SpeakerVoiceprintService) SyncSpeaker(speaker *models.Speaker) error {
	if speaker.Status != models.SpeakerStatusActive {
		facades.Speaker().Unregister(speaker.Name)

		return nil
	}

	vectors, err := voiceprintService.speakerService.GetVectorsBySpeakerId(speaker.ID)
	if err != nil {
		return err
	}

	return facades.Speaker().Register(speaker.Name, vectors)
}

// UnregisterSpeaker 把说话人从内存声纹库中移除，用于删除或改名场景。
func (voiceprintService *SpeakerVoiceprintService) UnregisterSpeaker(name string) {
	facades.Speaker().Unregister(name)
}

// RemoveAllAudioFiles 删除说话人名下所有音频文件，用于删除说话人时清理磁盘。
func (voiceprintService *SpeakerVoiceprintService) RemoveAllAudioFiles(speakerID uint) {
	audios, err := voiceprintService.speakerService.GetAudiosBySpeakerId(speakerID)
	if err != nil {
		facades.Log().Warning("读取说话人音频列表失败: " + err.Error())

		return
	}

	for i := range audios {
		voiceprintService.removeFile(audios[i].FilePath)
	}
}

// Identify 从上传音频中提取声纹，并在声纹库中检索最相似的说话人（1:N）。
func (voiceprintService *SpeakerVoiceprintService) Identify(file filesystem.File, threshold float32) (contractsspeaker.Match, *models.Speaker, error) {
	if err := voiceprintService.Warmup(); err != nil {
		return contractsspeaker.Match{}, nil, err
	}

	data, err := voiceprintService.readUpload(file)
	if err != nil {
		return contractsspeaker.Match{}, nil, err
	}

	feature, err := facades.Speaker().Extract(data)
	if err != nil {
		return contractsspeaker.Match{}, nil, err
	}

	match, err := facades.Speaker().Search(feature.Vector, threshold)
	if err != nil {
		return contractsspeaker.Match{}, nil, err
	}
	if !match.Matched {
		return match, nil, nil
	}

	var speaker models.Speaker
	if err := facades.Orm().Query().Where("name = ?", match.Name).FirstOrFail(&speaker); err != nil {
		// 内存声纹库与数据库不一致时，仅返回命中标识而不阻断请求。
		facades.Log().Warning("命中的说话人已不存在: " + match.Name)

		return match, nil, nil
	}

	return match, &speaker, nil
}

// Verify 校验上传音频是否属于指定说话人（1:1）。
func (voiceprintService *SpeakerVoiceprintService) Verify(speaker *models.Speaker, file filesystem.File, threshold float32) (contractsspeaker.Match, error) {
	if speaker.Status != models.SpeakerStatusActive {
		return contractsspeaker.Match{}, ErrSpeakerInactive
	}

	// 1:1 比对只需目标说话人的声纹在库中，无需整库预热。
	if err := voiceprintService.SyncSpeaker(speaker); err != nil {
		return contractsspeaker.Match{}, err
	}

	data, err := voiceprintService.readUpload(file)
	if err != nil {
		return contractsspeaker.Match{}, err
	}

	feature, err := facades.Speaker().Extract(data)
	if err != nil {
		return contractsspeaker.Match{}, err
	}

	return facades.Speaker().Verify(speaker.Name, feature.Vector, threshold)
}

// Warmup 首次调用时用数据库中的全部启用说话人重建内存声纹库。
//
// 采用懒加载而非启动时加载，避免应用启动阶段阻塞在数据库与模型上。
func (voiceprintService *SpeakerVoiceprintService) Warmup() error {
	var err error

	voiceprintService.warmup.Do(func() {
		var voiceprints map[string][][]float32
		voiceprints, err = voiceprintService.speakerService.GetActiveVoiceprints()
		if err != nil {
			return
		}

		if err = facades.Speaker().Reset(voiceprints); err != nil {
			return
		}

		facades.Log().Info(fmt.Sprintf("speaker: voiceprint library warmed up with %d speakers", len(voiceprints)))
	})

	return err
}

// readUpload 校验并读取上传音频的完整内容。
func (voiceprintService *SpeakerVoiceprintService) readUpload(file filesystem.File) ([]byte, error) {
	extension := strings.ToLower(file.GetClientOriginalExtension())
	if _, ok := supportedAudioExtensions[extension]; !ok {
		return nil, ErrUnsupportedAudioFormat
	}

	maxSize := facades.Config().GetInt("speaker.audio.max_file_size", 20*1024*1024)
	if size, err := file.Size(); err == nil && maxSize > 0 && size > int64(maxSize) {
		return nil, fmt.Errorf("%w（最大 %d MB）", ErrAudioTooLarge, maxSize/1024/1024)
	}

	data, err := os.ReadFile(file.File())
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("音频文件内容为空")
	}

	return data, nil
}

// store 把音频归档到配置的存储磁盘，返回磁盘内的相对路径。
func (voiceprintService *SpeakerVoiceprintService) store(speakerID uint, file filesystem.File, data []byte) (string, error) {
	config := facades.Config()

	extension := strings.ToLower(file.GetClientOriginalExtension())
	if extension == "" {
		extension = "wav"
	}

	// 文件名带上时间戳与随机串，避免同一说话人重复上传同名文件时相互覆盖。
	name := fmt.Sprintf("%s-%s.%s", time.Now().Format("20060102150405"), str.Random(8), extension)
	filePath := path.Join(config.GetString("speaker.storage.dir", "speakers"), fmt.Sprint(speakerID), name)

	disk := config.GetString("speaker.storage.disk", "speaker")
	if err := facades.Storage().Disk(disk).Put(filePath, string(data)); err != nil {
		return "", err
	}

	return filePath, nil
}

// removeFile 尽力删除磁盘上的音频文件，失败仅记录日志，不影响主流程。
func (voiceprintService *SpeakerVoiceprintService) removeFile(filePath string) {
	if filePath == "" {
		return
	}

	disk := facades.Config().GetString("speaker.storage.disk", "speaker")
	if err := facades.Storage().Disk(disk).Delete(filePath); err != nil {
		facades.Log().Warning("删除说话人音频文件失败: " + err.Error())
	}
}
