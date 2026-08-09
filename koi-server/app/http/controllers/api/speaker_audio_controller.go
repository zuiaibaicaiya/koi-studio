package api

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/spf13/cast"

	"koi-server/app/facades"
	"koi-server/app/services"
)

// SpeakerAudioController 说话人声纹控制器
//
// 负责注册音频的上传与管理，以及基于 sherpa-onnx 的声纹识别（1:N）与校验（1:1）。
type SpeakerAudioController struct {
	BaseController
	speakerService    *services.SpeakerService
	voiceprintService *services.SpeakerVoiceprintService
}

func NewSpeakerAudioController() *SpeakerAudioController {
	return &SpeakerAudioController{
		speakerService:    services.NewSpeakerService(),
		voiceprintService: services.NewSpeakerVoiceprintService(),
	}
}

// ListAudios 说话人已注册的声纹音频列表
func (ctrl *SpeakerAudioController) ListAudios(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "说话人ID不正确")
	}

	speaker, err := ctrl.speakerService.GetSpeakerById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "说话人不存在")
	}

	audios, err := ctrl.speakerService.GetAudiosBySpeakerId(speaker.ID)
	if err != nil {
		facades.Log().WithContext(ctx).Error("获取声纹音频列表失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "获取声纹音频列表失败")
	}

	return ctrl.ApiSuccess(ctx, audios)
}

// UploadAudio 上传音频为说话人注册声纹
//
// 表单字段：file 为 wav 音频文件（必填），remark 为备注（选填）。
func (ctrl *SpeakerAudioController) UploadAudio(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "说话人ID不正确")
	}

	speaker, err := ctrl.speakerService.GetSpeakerById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "说话人不存在")
	}

	file, err := ctx.Request().File("file")
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "请选择需要上传的音频文件")
	}

	audio, err := ctrl.voiceprintService.RegisterAudio(&speaker, file, ctx.Request().Input("remark"))
	if err != nil {
		facades.Log().WithContext(ctx).Warning("注册说话人声纹失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "注册声纹失败: "+err.Error())
	}

	facades.Log().WithContext(ctx).Info("注册说话人声纹成功: " + speaker.Name)

	return ctrl.ApiSuccess(ctx, audio)
}

// DeleteAudio 删除说话人的某条声纹音频
func (ctrl *SpeakerAudioController) DeleteAudio(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "说话人ID不正确")
	}

	audioID := ctx.Request().RouteInt("audioId")
	if audioID <= 0 {
		return ctrl.ApiErrorMsg(ctx, "声纹音频ID不正确")
	}

	speaker, err := ctrl.speakerService.GetSpeakerById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "说话人不存在")
	}

	if err := ctrl.voiceprintService.RemoveAudio(&speaker, audioID); err != nil {
		facades.Log().WithContext(ctx).Error("删除声纹音频失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "删除声纹音频失败")
	}

	facades.Log().WithContext(ctx).Info("删除声纹音频成功: " + speaker.Name)

	return ctrl.ApiSuccess(ctx, map[string]string{})
}

// IdentifySpeaker 声纹识别（1:N），在声纹库中检索与上传音频最相似的说话人
//
// 表单字段：file 为 wav 音频文件（必填），threshold 为相似度阈值（选填，取值 0-1）。
func (ctrl *SpeakerAudioController) IdentifySpeaker(ctx http.Context) http.Response {
	file, err := ctx.Request().File("file")
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "请选择需要识别的音频文件")
	}

	match, speaker, err := ctrl.voiceprintService.Identify(file, cast.ToFloat32(ctx.Request().Input("threshold")))
	if err != nil {
		facades.Log().WithContext(ctx).Warning("声纹识别失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "声纹识别失败: "+err.Error())
	}

	return ctrl.ApiSuccess(ctx, map[string]any{
		"matched":   match.Matched,
		"score":     match.Score,
		"threshold": match.Threshold,
		"name":      match.Name,
		"speaker":   speaker,
	})
}

// VerifySpeaker 声纹校验（1:1），判断上传音频是否属于指定说话人
//
// 表单字段：file 为 wav 音频文件（必填），threshold 为相似度阈值（选填，取值 0-1）。
func (ctrl *SpeakerAudioController) VerifySpeaker(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "说话人ID不正确")
	}

	speaker, err := ctrl.speakerService.GetSpeakerById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "说话人不存在")
	}

	file, err := ctx.Request().File("file")
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "请选择需要校验的音频文件")
	}

	match, err := ctrl.voiceprintService.Verify(&speaker, file, cast.ToFloat32(ctx.Request().Input("threshold")))
	if err != nil {
		facades.Log().WithContext(ctx).Warning("声纹校验失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "声纹校验失败: "+err.Error())
	}

	return ctrl.ApiSuccess(ctx, map[string]any{
		"matched":   match.Matched,
		"score":     match.Score,
		"threshold": match.Threshold,
		"speaker":   speaker,
	})
}

// GetStatus 声纹模型状态，用于健康检查与前端能力探测
func (ctrl *SpeakerAudioController) GetStatus(ctx http.Context) http.Response {
	status := ctrl.voiceprintService.Status()

	return ctrl.ApiSuccess(ctx, map[string]any{
		"loaded":    status.Loaded,
		"dim":       status.Dim,
		"threshold": status.Threshold,
		"error":     status.Error,
		"speakers":  facades.Speaker().Speakers(),
	})
}
