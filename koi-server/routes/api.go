package routes

import (
	"github.com/goravel/framework/contracts/route"

	"koi-server/app/facades"
	"koi-server/app/http/controllers/api"
	"koi-server/app/http/middleware"
	"koi-server/app/services"
)

// Api 注册 API 与实时通信路由。
//
// 这里同时充当组合根：从容器解析各项依赖并显式注入控制器，
// 使控制器本身不依赖 facades，便于单元测试。
func Api() {
	// 解析共享依赖
	sessionMgrRaw, err := facades.App().Make("meeting.session_manager")
	if err != nil {
		panic("Failed to resolve meeting session manager: " + err.Error())
	}
	sessionMgr := sessionMgrRaw.(*services.MeetingSessionManager)

	meetingService := services.NewMeetingService()
	transcriptService := services.NewMeetingTranscriptService()
	hotWordService := services.NewHotWordService()
	hotWordLibService := services.NewHotWordLibraryService()
	speakerVoiceprintService := services.NewSpeakerVoiceprintService()

	socketioController := api.NewSocketioController(
		facades.Socketio(),
		facades.Audio(),
		facades.Log(),
		meetingService,
		sessionMgr,
		speakerVoiceprintService,
		hotWordService,
		hotWordLibService,
	)

	// 注册 Socket.IO 事件处理器。
	socketioController.RegisterHandlers()

	// Socket.IO 端点由自身握手协议完成鉴权，不挂载 JWT 中间件。
	facades.Route().Any("/socket.io/*any", socketioController.ServeSocketio)

	userController := api.NewUserController()
	hotWordLibraryController := api.NewHotWordLibraryController()
	hotWordController := api.NewHotWordController()
	speakerController := api.NewSpeakerController()
	speakerAudioController := api.NewSpeakerAudioController()
	meetingController := api.NewMeetingController(meetingService, transcriptService, sessionMgr)

	// 登录和注册接口，无需认证。
	facades.Route().Post("/api/user/login", userController.Login)
	facades.Route().Post("/api/user/register", userController.Register)

	// 用户管理接口，统一挂载 JWT 认证中间件。
	facades.Route().Prefix("api").Middleware(middleware.JWT()).Group(func(router route.Router) {
		router.Post("/user/logout", userController.Logout)
		router.Get("/user/current", userController.GetCurrentUserInfo)
		router.Post("/user/refresh", userController.RefreshToken)
		router.Put("/user/profile", userController.UpdateProfile)
		router.Put("/user/password", userController.ChangePassword)

		router.Get("/user", userController.ListUsers)
		router.Post("/user", userController.CreateUser)
		router.Get("/user/{id}", userController.GetUser)
		router.Put("/user/{id}", userController.UpdateUser)
		router.Delete("/user/{id}", userController.DeleteUser)
		router.Put("/user/{id}/status", userController.ToggleUserStatus)

		// 热词库管理接口。
		router.Get("/hot-word-library", hotWordLibraryController.ListLibraries)
		router.Post("/hot-word-library", hotWordLibraryController.CreateLibrary)
		router.Post("/hot-word-library/import", hotWordLibraryController.ImportLibrary)
		router.Get("/hot-word-library/{id}", hotWordLibraryController.GetLibrary)
		router.Put("/hot-word-library/{id}", hotWordLibraryController.UpdateLibrary)
		router.Delete("/hot-word-library/{id}", hotWordLibraryController.DeleteLibrary)

		// 热词管理接口，归属于指定热词库。
		router.Get("/hot-word-library/{id}/word", hotWordController.ListHotWords)
		router.Post("/hot-word-library/{id}/word", hotWordController.CreateHotWord)
		router.Get("/hot-word-library/{id}/word/{wordId}", hotWordController.GetHotWord)
		router.Put("/hot-word-library/{id}/word/{wordId}", hotWordController.UpdateHotWord)
		router.Delete("/hot-word-library/{id}/word/{wordId}", hotWordController.DeleteHotWord)

		// 说话人管理接口。
		router.Get("/speaker", speakerController.ListSpeakers)
		router.Post("/speaker", speakerController.CreateSpeaker)
		router.Get("/speaker/status", speakerAudioController.GetStatus)
		router.Post("/speaker/identify", speakerAudioController.IdentifySpeaker)
		router.Get("/speaker/{id}", speakerController.GetSpeaker)
		router.Put("/speaker/{id}", speakerController.UpdateSpeaker)
		router.Delete("/speaker/{id}", speakerController.DeleteSpeaker)

		// 说话人声纹音频接口，归属于指定说话人。
		router.Get("/speaker/{id}/audio", speakerAudioController.ListAudios)
		router.Post("/speaker/{id}/audio", speakerAudioController.UploadAudio)
		router.Delete("/speaker/{id}/audio/{audioId}", speakerAudioController.DeleteAudio)
		router.Post("/speaker/{id}/verify", speakerAudioController.VerifySpeaker)

		// 实时会议管理接口。
		router.Get("/meeting", meetingController.ListMeetings)
		router.Post("/meeting", meetingController.CreateMeeting)
		router.Get("/meeting/{id}", meetingController.GetMeeting)
		router.Put("/meeting/{id}", meetingController.UpdateMeeting)
		router.Delete("/meeting/{id}", meetingController.DeleteMeeting)
		router.Post("/meeting/{id}/start", meetingController.StartMeeting)
		router.Post("/meeting/{id}/finish", meetingController.FinishMeeting)
		// 会议转写记录查询。
		router.Get("/meeting/{id}/transcripts", meetingController.GetMeetingTranscripts)

		// 音频文件离线转写接口（mode=audio）。
		router.Post("/meeting/{id}/audio", meetingController.UploadAudio)
		router.Post("/meeting/{id}/transcribe", meetingController.StartTranscription)
		router.Post("/meeting/{id}/retranscribe", meetingController.Retranscribe)
		router.Get("/meeting/{id}/progress", meetingController.GetTranscriptionProgress)
	})
}
