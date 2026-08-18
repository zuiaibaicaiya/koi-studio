package api

import (
	"fmt"
	"os"
	"strings"

	"github.com/dromara/carbon/v2"
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/facades"
	"github.com/google/uuid"

	"koi-server/app/http/requests/meetings"
	"koi-server/app/models"
	"koi-server/app/providers"
	"koi-server/app/services"
	audiosvc "koi-server/app/services/audio"
	offlinetranscribe "koi-server/app/services/offline_transcribe"
)

// 允许上传的音频扩展名
var supportedAudioExtensions = map[string]struct{}{
	"wav":  {},
	"wave": {},
}

// MeetingController 会议控制器（实时会议 + 音频离线转写）
type MeetingController struct {
	BaseController
	meetingService    *services.MeetingService
	transcriptService *services.MeetingTranscriptService
	sessionMgr        *services.MeetingSessionManager
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

// resolveOfflineServices 从 IoC 容器解析离线转写服务（避免构造函数循环依赖）
func (ctrl *MeetingController) resolveOfflineServices() (*offlinetranscribe.Service, *offlinetranscribe.ProgressManager, error) {
	svcRaw, err := facades.App().Make(providers.OfflineTranscribeBinding)
	if err != nil {
		return nil, nil, fmt.Errorf("离线转写服务未就绪: %w", err)
	}
	progRaw, err := facades.App().Make(providers.OfflineProgressManagerBinding)
	if err != nil {
		return nil, nil, fmt.Errorf("进度管理器未就绪: %w", err)
	}
	return svcRaw.(*offlinetranscribe.Service), progRaw.(*offlinetranscribe.ProgressManager), nil
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

// =====================================================================
// 以下为「音频文件离线转写」模式专用接口
// =====================================================================

// UploadAudio 为音频转写会议上传音频文件（仅 mode=audio 的会议可用）
//
// 请求体为 multipart/form-data，字段名 audio。
// @Route POST /meeting/{id}/audio
func (ctrl *MeetingController) UploadAudio(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "会议ID不正确")
	}

	meeting, err := ctrl.meetingService.GetMeetingById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "会议不存在")
	}
	if meeting.Mode != models.MeetingModeAudio {
		return ctrl.ApiErrorMsg(ctx, "仅音频转写模式的会议支持上传音频文件")
	}

	// 读取上传的文件
	file, err := ctx.Request().File("audio")
	if err != nil {
		facades.Log().WithContext(ctx).Warning("读取上传音频失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "请选择要上传的音频文件")
	}
	ext := strings.ToLower(file.GetClientOriginalExtension())
	if _, ok := supportedAudioExtensions[ext]; !ok {
		return ctrl.ApiErrorMsg(ctx, "仅支持 wav 格式的音频文件")
	}

	// 校验文件大小（默认上限 500MB，可通过配置覆盖）
	maxSize := facades.Config().GetInt("audio.upload.max_size", 500*1024*1024)
	if size, serr := file.Size(); serr == nil && maxSize > 0 && size > int64(maxSize) {
		return ctrl.ApiErrorMsg(ctx, fmt.Sprintf("音频文件过大（上限 %d MB）", maxSize/1024/1024))
	}

	// 读取文件内容用于持久化和格式校验
	data, rerr := os.ReadFile(file.File())
	if rerr != nil {
		facades.Log().WithContext(ctx).Error("读取上传音频内容失败: " + rerr.Error())
		return ctrl.ApiErrorMsg(ctx, "读取音频文件失败")
	}
	if len(data) == 0 {
		return ctrl.ApiErrorMsg(ctx, "音频文件内容为空")
	}

	// 校验 WAV 文件头：拒绝非 WAV 文件或格式不兼容的音频
	wavInfo, werr := audiosvc.ParseWAVHeader(data)
	if werr != nil {
		facades.Log().WithContext(ctx).Warning("上传音频格式校验失败: " + werr.Error())
		return ctrl.ApiErrorMsg(ctx, "音频文件格式无效，请上传 16kHz 16bit PCM WAV 文件")
	}

	compatible, compatMsg := wavInfo.IsCompatibleWithTranscription()
	if !compatible {
		return ctrl.ApiErrorMsg(ctx, compatMsg)
	}

	// 写入 audio 存储磁盘
	//
	// 路径规则对齐实时会议归档：直接存放于 audio disk 根目录。
	// 文件名采用 UUIDv7（去掉连字符的 32 字符 hex），时间有序、数据库索引友好。
	diskName := facades.Config().GetString("audio.storage.disk", "audio")
	disk := facades.Storage().Disk(diskName)

	// UUIDv7：前 48 位为毫秒时间戳，保证时间有序；后 80 位为随机位，避免碰撞。
	// 去掉连字符（ReplaceAll("-", "")）生成 32 字符 hex，与 Socket.IO 连接 ID 同为扁平根目录命名风格。
	uuidV7, err := uuid.NewV7()
	if err != nil {
		facades.Log().WithContext(ctx).Error("生成 UUID 失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "生成文件标识失败")
	}
	relPath := strings.ReplaceAll(uuidV7.String(), "-", "") + "." + ext

	if err := disk.Put(relPath, string(data)); err != nil {
		facades.Log().WithContext(ctx).Error("保存音频文件失败: " + err.Error())
		return ctrl.ApiErrorMsg(ctx, "保存音频文件失败")
	}

	// 校验落盘文件完整性：重新读取确认大小一致
	if saved, serr := disk.Get(relPath); serr != nil || len(saved) != len(data) {
		_ = disk.Delete(relPath)
		facades.Log().WithContext(ctx).Error("音频文件落盘校验失败")
		return ctrl.ApiErrorMsg(ctx, "音频文件保存不完整，请重试")
	}

	// 如果之前已有音频文件，优先删除旧的，避免浪费磁盘
	if meeting.AudioFilePath != "" && meeting.AudioFilePath != relPath {
		_ = disk.Delete(meeting.AudioFilePath)
	}

	// 更新会议记录：写入音频文件路径，保留会议为 created 状态（尚未开始转写）
	meeting.AudioFilePath = relPath
	if uerr := ctrl.meetingService.UpdateMeeting(&meeting); uerr != nil {
		facades.Log().WithContext(ctx).Error("更新会议音频路径失败: " + uerr.Error())
		_ = disk.Delete(relPath)
		return ctrl.ApiErrorMsg(ctx, "更新会议信息失败")
	}
	meeting.AudioURL = ctrl.meetingService.GetAudioURL(relPath)

	// 如果该会议已有历史转写记录（例如重新上传覆盖），清理旧记录，
	// 避免新转写结果与旧结果混杂。
	if cerr := ctrl.transcriptService.DeleteByMeetingID(uint(id)); cerr != nil {
		facades.Log().WithContext(ctx).Warning("清理旧转写记录失败: " + cerr.Error())
	}

	// 音频上传成功后，自动触发后端异步离线转写。
	// 转写结果将写入 meeting_transcripts 表；进度通过 /meeting/{id}/progress 查询。
	transcribeMsg := ""
	if terr := ctrl.triggerOfflineTranscription(ctx, uint(id)); terr != nil {
		transcribeMsg = "音频已上传，但触发转写失败: " + terr.Error()
		facades.Log().WithContext(ctx).Warning(transcribeMsg)
	}

	respData := map[string]any{
		"meeting_id":        meeting.ID,
		"audio_file_path":   meeting.AudioFilePath,
		"audio_url":         meeting.AudioURL,
		"file_size":         len(data),
		"original_filename": file.GetClientOriginalName(),
		"sample_rate":       wavInfo.SampleRate,
		"channels":          wavInfo.Channels,
		"bits_per_sample":   wavInfo.BitsPerSample,
		"duration":          wavInfo.DurationSec,
		"transcription":     "started",
	}
	if compatMsg != "" {
		respData["warning"] = compatMsg
	}
	if transcribeMsg != "" {
		respData["transcription_error"] = transcribeMsg
	}
	return ctrl.ApiSuccess(ctx, respData)
}

// triggerOfflineTranscription 触发会议的异步离线转写：
// 设置会议状态为 ongoing、清理旧记录、调用 OfflineTranscribeService.TranscribeMeeting。
// 返回的错误会被 UploadAudio 记录到日志，但不阻断音频上传响应。
func (ctrl *MeetingController) triggerOfflineTranscription(ctx http.Context, meetingID uint) error {
	offlineSvc, _, err := ctrl.resolveOfflineServices()
	if err != nil {
		return err
	}
	// 模型加载失败时直接返回错误，避免无效触发
	if offlineSvc.Status().Error != "" {
		return fmt.Errorf("离线转写模型加载失败: %s", offlineSvc.Status().Error)
	}
	if serr := ctrl.meetingService.SetMeetingStatus(int(meetingID), models.MeetingStatusOngoing); serr != nil {
		facades.Log().WithContext(ctx).Warning("设置会议状态失败: " + serr.Error())
	}
	if terr := offlineSvc.TranscribeMeeting(meetingID); terr != nil {
		_ = ctrl.meetingService.SetMeetingStatus(int(meetingID), models.MeetingStatusCreated)
		return fmt.Errorf("触发转写失败: %w", terr)
	}
	return nil
}

// StartTranscription 对会议已上传的音频手动触发离线转写（mode=audio）
//
// 通常由 UploadAudio 自动触发，此接口保留用于失败后重试。
// 转写在后台异步执行，进度可通过 GET /meeting/{id}/progress 查询。
// @Route POST /meeting/{id}/transcribe
func (ctrl *MeetingController) StartTranscription(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "会议ID不正确")
	}

	meeting, err := ctrl.meetingService.GetMeetingById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "会议不存在")
	}
	if meeting.Mode != models.MeetingModeAudio {
		return ctrl.ApiErrorMsg(ctx, "仅音频转写模式的会议支持触发转写")
	}
	if meeting.AudioFilePath == "" {
		return ctrl.ApiErrorMsg(ctx, "请先上传音频文件")
	}

	// 清理可能残留的旧转写记录
	if cerr := ctrl.transcriptService.DeleteByMeetingID(uint(id)); cerr != nil {
		facades.Log().WithContext(ctx).Warning("清理旧转写记录失败: " + cerr.Error())
	}

	if terr := ctrl.triggerOfflineTranscription(ctx, uint(id)); terr != nil {
		facades.Log().WithContext(ctx).Error("触发转写失败: " + terr.Error())
		return ctrl.ApiErrorMsg(ctx, "触发转写失败: "+terr.Error())
	}

	return ctrl.ApiSuccess(ctx, map[string]any{
		"meeting_id": meeting.ID,
		"status":     "started",
		"message":    "转写任务已提交，可通过 progress 接口查询进度",
	})
}

// GetTranscriptionProgress 查询离线转写的进度
//
// 返回：状态（pending/running/completed/failed）、百分比、当前步骤、错误信息等。
// @Route GET /meeting/{id}/progress
func (ctrl *MeetingController) GetTranscriptionProgress(ctx http.Context) http.Response {
	id := ctx.Request().RouteInt("id")
	if id <= 0 {
		return ctrl.ApiErrorMsg(ctx, "会议ID不正确")
	}

	meeting, err := ctrl.meetingService.GetMeetingById(id)
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, "会议不存在")
	}
	if meeting.Mode != models.MeetingModeAudio {
		return ctrl.ApiErrorMsg(ctx, "仅音频转写模式的会议支持进度查询")
	}

	_, progressMgr, err := ctrl.resolveOfflineServices()
	if err != nil {
		return ctrl.ApiErrorMsg(ctx, err.Error())
	}

	progress, perr := progressMgr.Get(uint(id))
	if perr != nil {
		// 若尚未创建进度记录，但会议已结束，给出已完成的兜底结果
		if meeting.Status == models.MeetingStatusFinished {
			return ctrl.ApiSuccess(ctx, map[string]any{
				"meeting_id":    meeting.ID,
				"status":        offlinetranscribe.StatusCompleted,
				"progress":      100,
				"current_step":  "转写完成",
				"total_seconds": 0,
			})
		}
		// 否则返回等待状态
		return ctrl.ApiSuccess(ctx, map[string]any{
			"meeting_id":   meeting.ID,
			"status":       offlinetranscribe.StatusPending,
			"progress":     0,
			"current_step": "尚未提交转写任务",
		})
	}

	return ctrl.ApiSuccess(ctx, progress)
}
