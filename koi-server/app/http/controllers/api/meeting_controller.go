package api

import (
	"github.com/dromara/carbon/v2"
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/facades"

	"koi-server/app/http/requests/meetings"
	"koi-server/app/models"
	"koi-server/app/services"
)

// MeetingController 实时会议控制器
type MeetingController struct {
	BaseController
	meetingService          *services.MeetingService
	transcriptService       *services.MeetingTranscriptService
	sessionMgr              *services.MeetingSessionManager
}

// NewMeetingController 创建控制器实例
func NewMeetingController(
	meetingService *services.MeetingService,
	transcriptService *services.MeetingTranscriptService,
	sessionMgr *services.MeetingSessionManager,
) *MeetingController {
	return &MeetingController{
		meetingService:    meetingService,
		transcriptService: transcriptService,
		sessionMgr:        sessionMgr,
	}
}

// ListMeetings 会议列表
// @Route GET /meeting
func (ctrl *MeetingController) ListMeetings(ctx http.Context) http.Response {
	var req meetings.MeetingListRequest
	errors, err := ctx.Request().ValidateRequest(&req)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	page, pageSize := normalizePagination(req.Page, req.PageSize)
	list, total, err := ctrl.meetingService.GetMeetingList(page, pageSize, req.Keyword, req.Status, req.Mode, req.StartTime, req.EndTime)
	if err != nil {
		facades.Log().WithContext(ctx).Error("查询会议列表失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "查询失败")
	}

	for i := range list {
		list[i].AudioURL = ctrl.meetingService.GetAudioURL(list[i].AudioFilePath)
	}

	return ctrl.ApiPaginate(ctx, list, total, page, pageSize)
}

// GetMeeting 会议详情
// @Route GET /meeting/{id}
func (ctrl *MeetingController) GetMeeting(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "会议ID不正确")
	}

	meeting, err := ctrl.meetingService.GetMeetingById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "会议不存在")
	}

	meeting.AudioURL = ctrl.meetingService.GetAudioURL(meeting.AudioFilePath)

	return ctrl.ApiSuccess(ctx, meeting)
}

// CreateMeeting 创建会议
// @Route POST /meeting
func (ctrl *MeetingController) CreateMeeting(ctx http.Context) http.Response {
	var req meetings.MeetingPostRequest
	errors, err := ctx.Request().ValidateRequest(&req)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	// carbon.Now() 返回 *Carbon 指针，且 AddHour 为指针接收者、原地修改。
	// 若写成 startTime:=now; endTime:=now.AddHour()，两者会指向同一对象，
	// endTime 等于 startTime，导致「结束时间必须晚于开始时间」误报。
	// 因此两次独立调用 carbon.Now()，确保 startTime 与 endTime 各自持有独立实例。
	startTime := carbon.Now()
	endTime := carbon.Now().AddHour()

	if req.StartTime != "" {
		parsed, perr := services.ParseMeetingTime(req.StartTime)
		if perr != nil {
			return ctrl.ApiErrorMsg(ctx, perr.Error())
		}
		startTime = parsed.Carbon
	}
	if req.EndTime != "" {
		parsed, perr := services.ParseMeetingTime(req.EndTime)
		if perr != nil {
			return ctrl.ApiErrorMsg(ctx, perr.Error())
		}
		endTime = parsed.Carbon
	}

	if !endTime.Gt(startTime) {
		return ctrl.ApiErrorMsg(ctx, "结束时间必须晚于开始时间")
	}

	// 模式：未指定时缺省为实时会议
	meetingMode := req.Mode
	if meetingMode == "" {
		meetingMode = models.MeetingModeLive
	}

	meeting := models.Meeting{
		Name:         req.Name,
		Participants: req.Participants,
		SpeakerIds:   req.SpeakerIds,
		StartTime:    *carbon.NewDateTime(startTime),
		EndTime:      *carbon.NewDateTime(endTime),
		Status:       models.MeetingStatusCreated,
		Mode:         meetingMode,
	}

	meeting.HotWordLibraryIds = req.HotWordLibraryIds

	// 记录创建人
	if user, uerr := ctrl.GetCurrentUser(ctx); uerr == nil {
		meeting.CreatedBy = user.ID
	}

	if err := ctrl.meetingService.CreateMeeting(&meeting); err != nil {
		facades.Log().WithContext(ctx).Error("创建会议失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "创建会议失败")
	}

	return ctrl.ApiSuccess(ctx, meeting)
}

// UpdateMeeting 更新会议
// @Route PUT /meeting/{id}
func (ctrl *MeetingController) UpdateMeeting(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "会议ID不正确")
	}

	var req meetings.MeetingUpdateRequest
	errors, err := ctx.Request().ValidateRequest(&req)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}
	if errors != nil {
		return ctrl.ApiErrorMsg(ctx, ctrl.GetFirstError(errors))
	}

	meeting, err := ctrl.meetingService.GetMeetingById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "会议不存在")
	}

	if req.Name != "" {
		meeting.Name = req.Name
	}
	if req.Participants != "" {
		meeting.Participants = req.Participants
	}
	if req.SpeakerIds != "" {
		meeting.SpeakerIds = req.SpeakerIds
	}
	if req.Status != "" {
		meeting.Status = req.Status
	}
	if req.Mode != "" {
		meeting.Mode = req.Mode
	}

	// 热词库：空字符串表示不修改
	if req.HotWordLibraryIds != "" {
		meeting.HotWordLibraryIds = req.HotWordLibraryIds
	}

	if req.StartTime != "" {
		t, perr := services.ParseMeetingTime(req.StartTime)
		if perr != nil {
			return ctrl.ApiErrorMsg(ctx, perr.Error())
		}
		meeting.StartTime = *t
	}
	if req.EndTime != "" {
		t, perr := services.ParseMeetingTime(req.EndTime)
		if perr != nil {
			return ctrl.ApiErrorMsg(ctx, perr.Error())
		}
		meeting.EndTime = *t
	}

	if !meeting.EndTime.Gt(meeting.StartTime.Carbon) {
		return ctrl.ApiErrorMsg(ctx, "结束时间必须晚于开始时间")
	}

	if err := ctrl.meetingService.UpdateMeeting(&meeting); err != nil {
		facades.Log().WithContext(ctx).Error("更新会议失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "更新会议失败")
	}

	return ctrl.ApiSuccess(ctx, meeting)
}

// DeleteMeeting 删除会议
// @Route DELETE /meeting/{id}
func (ctrl *MeetingController) DeleteMeeting(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "会议ID不正确")
	}

	if _, err := ctrl.meetingService.DeleteMeetingById(id); err != nil {
		facades.Log().WithContext(ctx).Error("删除会议失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "删除会议失败")
	}

	return ctrl.ApiSuccess(ctx, nil)
}

// StartMeeting 开始会议（标记为进行中）
// @Route POST /meeting/{id}/start
func (ctrl *MeetingController) StartMeeting(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "会议ID不正确")
	}

	if _, err := ctrl.meetingService.GetMeetingById(id); err != nil {
		return ctrl.ApiErrorMsg(ctx, "会议不存在")
	}

	if err := ctrl.meetingService.SetMeetingStatus(id, models.MeetingStatusOngoing); err != nil {
		facades.Log().WithContext(ctx).Error("开始会议失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "开始会议失败")
	}

	return ctrl.ApiSuccess(ctx, nil)
}

// FinishMeeting 结束会议（标记为已结束，释放所有活跃的转写会话）
// @Route POST /meeting/{id}/finish
func (ctrl *MeetingController) FinishMeeting(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "会议ID不正确")
	}

	if _, err := ctrl.meetingService.GetMeetingById(id); err != nil {
		return ctrl.ApiErrorMsg(ctx, "会议不存在")
	}

	// 查找该会议的所有活跃客户端并释放会话
	for _, clientID := range ctrl.sessionMgr.ClientsByMeetingID(uint(id)) {
		// 通过 facade 获取 transcriber 释放会话
		ctrl.sessionMgr.Unbind(clientID)
	}

	if err := ctrl.meetingService.SetMeetingStatus(id, models.MeetingStatusFinished); err != nil {
		facades.Log().WithContext(ctx).Error("结束会议失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "结束会议失败")
	}

	return ctrl.ApiSuccess(ctx, nil)
}

// GetMeetingTranscripts 分页查询会议转写记录
// @Route GET /meeting/{id}/transcripts
func (ctrl *MeetingController) GetMeetingTranscripts(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "会议ID不正确")
	}

	// 验证会议存在
	if _, err := ctrl.meetingService.GetMeetingById(id); err != nil {
		return ctrl.ApiErrorMsg(ctx, "会议不存在")
	}

	page := ctx.Request().QueryInt("page", 1)
	pageSize := ctx.Request().QueryInt("pageSize", 50)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	transcripts, total, err := ctrl.transcriptService.GetByMeetingID(uint(id), page, pageSize)
	if err != nil {
		facades.Log().WithContext(ctx).Error("查询转写记录失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "查询转写记录失败")
	}

	return ctrl.ApiPaginate(ctx, transcripts, total, page, pageSize)
}
