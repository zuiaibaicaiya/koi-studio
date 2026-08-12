package api

import (
	"github.com/goravel/framework/contracts/http"

	"koi-server/app/facades"
	"koi-server/app/http/requests/speakers"
	"koi-server/app/models"
	"koi-server/app/services"
)

// SpeakerController 说话人管理控制器
//
// 提供说话人的增删改查接口。声纹音频的注册与识别见 SpeakerAudioController。
type SpeakerController struct {
	BaseController
	speakerService    *services.SpeakerService
	voiceprintService *services.SpeakerVoiceprintService
}

func NewSpeakerController() *SpeakerController {
	return &SpeakerController{
		speakerService:    services.NewSpeakerService(),
		voiceprintService: services.NewSpeakerVoiceprintService(),
	}
}

// ListSpeakers 说话人列表，支持分页与关键词筛选
func (ctrl *SpeakerController) ListSpeakers(ctx http.Context) http.Response {
	var listReq speakers.SpeakerListRequest
	errors, err := ctx.Request().ValidateRequest(&listReq)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	page, pageSize := normalizePagination(listReq.Page, listReq.PageSize)

	list, total, err := ctrl.speakerService.GetSpeakerList(page, pageSize, listReq.Keyword)
	if err != nil {
		facades.Log().WithContext(ctx).Error("获取说话人列表失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "获取说话人列表失败")
	}

	return ctrl.ApiPaginate(ctx, list, total, page, pageSize)
}

// GetSpeaker 说话人详情，返回其名下已注册的声纹音频列表
func (ctrl *SpeakerController) GetSpeaker(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "说话人ID不正确")
	}

	speaker, err := ctrl.speakerService.GetSpeakerDetail(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "说话人不存在")
	}

	return ctrl.ApiSuccess(ctx, speaker)
}

// CreateSpeaker 新增说话人
func (ctrl *SpeakerController) CreateSpeaker(ctx http.Context) http.Response {
	var speakerPost speakers.SpeakerPostRequest
	errors, err := ctx.Request().ValidateRequest(&speakerPost)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	exists, err := ctrl.speakerService.IsSpeakerNameExists(speakerPost.Name, 0)
	if err != nil {
		facades.Log().WithContext(ctx).Error("校验说话人名称失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "创建说话人失败")
	}
	if exists {
		return ctrl.ApiErrorMsg(ctx, "说话人已经存在")
	}

	speaker := models.Speaker{
		Name:        speakerPost.Name,
		Description: speakerPost.Description,
	}

	if err := ctrl.speakerService.AddSpeaker(&speaker); err != nil {
		facades.Log().WithContext(ctx).Error("创建说话人失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "创建说话人失败")
	}

	// 音频为选填：表单以 multipart/form-data 提交时可携带 file 字段，创建后立即注册声纹
	if file, fileErr := ctx.Request().File("file"); fileErr == nil && file != nil {
		audio, err := ctrl.voiceprintService.RegisterAudio(&speaker, file, ctx.Request().Input("remark"))
		if err != nil {
			// 声纹注册失败则回滚说话人，避免留下没有声纹的空档案
			if _, delErr := ctrl.speakerService.DeleteSpeakerById(int(speaker.ID)); delErr != nil {
				facades.Log().WithContext(ctx).Error("回滚说话人失败: " + delErr.Error())
			}
			facades.Log().WithContext(ctx).Warning("注册声纹失败: " + err.Error())

			return ctrl.ApiErrorMsg(ctx, "注册声纹失败: "+err.Error())
		}

		speaker.Audios = append(speaker.Audios, audio)
		speaker.AudioCount = len(speaker.Audios)
	}

	facades.Log().WithContext(ctx).Info("创建说话人成功: " + speaker.Name)

	return ctrl.ApiSuccess(ctx, speaker)
}

// UpdateSpeaker 更新说话人，仅更新传入的字段
//
// 名称或状态发生变化时会同步刷新内存声纹库：改名需要以旧名注销、新名重注册，
// 禁用则需要把该说话人从检索范围内移除。
func (ctrl *SpeakerController) UpdateSpeaker(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "说话人ID不正确")
	}

	var speakerUpdate speakers.SpeakerUpdateRequest
	errors, err := ctx.Request().ValidateRequest(&speakerUpdate)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	speaker, err := ctrl.speakerService.GetSpeakerById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "说话人不存在")
	}

	previousName := speaker.Name

	if speakerUpdate.Name != "" && speakerUpdate.Name != speaker.Name {
		exists, err := ctrl.speakerService.IsSpeakerNameExists(speakerUpdate.Name, speaker.ID)
		if err != nil {
			facades.Log().WithContext(ctx).Error("校验说话人名称失败: " + err.Error())
			return ctrl.ApiErrorMsg(ctx, "更新说话人失败")
		}
		if exists {
			return ctrl.ApiErrorMsg(ctx, "说话人已经存在")
		}

		speaker.Name = speakerUpdate.Name
	}

	if speakerUpdate.Description != "" {
		speaker.Description = speakerUpdate.Description
	}

	if err := ctrl.speakerService.UpdateSpeaker(&speaker); err != nil {
		facades.Log().WithContext(ctx).Error("更新说话人失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "更新说话人失败")
	}

	if speaker.Name != previousName {
		ctrl.voiceprintService.UnregisterSpeaker(previousName)
		if err := ctrl.voiceprintService.SyncSpeaker(&speaker); err != nil {
			facades.Log().WithContext(ctx).Warning("刷新内存声纹库失败: " + err.Error())
		}
	}

	facades.Log().WithContext(ctx).Info("更新说话人成功: " + speaker.Name)

	return ctrl.ApiSuccess(ctx, speaker)
}

// DeleteSpeaker 删除说话人（软删除），同时删除其名下所有声纹音频
func (ctrl *SpeakerController) DeleteSpeaker(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "说话人ID不正确")
	}

	speaker, err := ctrl.speakerService.GetSpeakerById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "说话人不存在")
	}

	// 先清理磁盘文件，避免记录被软删除后无法再定位到文件路径
	ctrl.voiceprintService.RemoveAllAudioFiles(speaker.ID)

	result, err := ctrl.speakerService.DeleteSpeakerById(id)
	if err != nil {
		facades.Log().WithContext(ctx).Error("删除说话人失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "删除说话人失败")
	}
	if result == nil || result.RowsAffected == 0 {
		return ctrl.ApiErrorMsg(ctx, "删除失败")
	}

	ctrl.voiceprintService.UnregisterSpeaker(speaker.Name)

	facades.Log().WithContext(ctx).Info("删除说话人成功: " + speaker.Name)

	return ctrl.ApiSuccess(ctx, map[string]string{})
}
