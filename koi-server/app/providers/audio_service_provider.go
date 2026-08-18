// Package providers 存放应用自身的服务提供者，负责把实现绑定到 IoC 容器。
package providers

import (
	"fmt"

	"github.com/goravel/framework/contracts/event"
	"github.com/goravel/framework/contracts/foundation"
	"github.com/goravel/framework/contracts/queue"

	contractsaudio "koi-server/app/contracts/audio"
	"koi-server/app/events"
	"koi-server/app/facades"
	"koi-server/app/jobs"
	"koi-server/app/services"
	"koi-server/app/services/audio"
	offlinetranscribe "koi-server/app/services/offline_transcribe"
)

// 容器绑定键
const (
	OfflineTranscribeBinding       = "koi.audio.offline_transcriber"
	OfflineProgressManagerBinding  = "koi.audio.offline_progress"
)

// AudioServiceProvider 注册音频实时转写与离线转写服务。
type AudioServiceProvider struct{}

// 编译期确认满足框架的服务提供者契约。
var _ foundation.ServiceProvider = (*AudioServiceProvider)(nil)

// Register 把转写服务以单例形式绑定到容器。
//
// 单例保证全局只加载一份语音模型；具体实现通过构造函数注入日志、存储、
// 结果发布器与录音归档器，任一依赖都可在测试中替换为替身。
func (r *AudioServiceProvider) Register(app foundation.Application) {
	// --- 实时转写服务 ---
	app.Singleton(contractsaudio.Binding, func(app foundation.Application) (any, error) {
		config := audio.NewConfig(app.MakeConfig())

		sessionMgrRaw, err := app.Make("meeting.session_manager")
		if err != nil {
			return nil, fmt.Errorf("audio: failed to resolve session manager: %w", err)
		}

		return audio.NewService(config, audio.Dependencies{
			Log:               app.MakeLog(),
			Storage:           app.MakeStorage().Disk(config.Disk),
			Publisher:         &eventPublisher{},
			Archiver:          &queueArchiver{},
			SessionMgr:        sessionMgrRaw.(*services.MeetingSessionManager),
			TranscriptService: services.NewMeetingTranscriptService(),
			SpeakerService:    services.NewSpeakerService(),
			Voiceprint:        facades.Speaker(),
		})
	})

	// 注册 MeetingSessionManager 为单例，使转写服务与控制器共享同一实例
	app.Singleton("meeting.session_manager", func(app foundation.Application) (any, error) {
		return services.NewMeetingSessionManager(), nil
	})

	// --- 离线转写进度管理器 ---
	app.Singleton(OfflineProgressManagerBinding, func(app foundation.Application) (any, error) {
		return offlinetranscribe.NewProgressManager(), nil
	})

	// --- 离线转写服务 ---
	app.Singleton(OfflineTranscribeBinding, func(app foundation.Application) (any, error) {
		offlineCfg := offlinetranscribe.NewConfig(app.MakeConfig())
		progressRaw, err := app.Make(OfflineProgressManagerBinding)
		if err != nil {
			return nil, fmt.Errorf("offline: failed to resolve progress manager: %w", err)
		}
		diskName := app.MakeConfig().GetString("audio.storage.disk", "audio")
		return offlinetranscribe.NewService(offlineCfg, offlinetranscribe.Dependencies{
			Log:               app.MakeLog(),
			Storage:           app.MakeStorage().Disk(diskName),
			Progress:          progressRaw.(*offlinetranscribe.ProgressManager),
			TranscriptService: services.NewMeetingTranscriptService(),
			MeetingService:    services.NewMeetingService(),
			HotWordLibService: services.NewHotWordLibraryService(),
			HotWordService:    services.NewHotWordService(),
			SpeakerService:    services.NewSpeakerService(),
			Voiceprint:        facades.Speaker(),
			SpeakerVoiceprint: services.NewSpeakerVoiceprintService(),
		})
	})
}

// Boot 提前实例化服务，使模型在应用启动阶段就开始异步预加载，
// 而不是等到第一个客户端接入时才触发。
func (r *AudioServiceProvider) Boot(app foundation.Application) {
	if _, err := app.Make(contractsaudio.Binding); err != nil {
		app.MakeLog().Error(fmt.Sprintf("audio: failed to boot transcriber: %v", err))
	}
	if _, err := app.Make(OfflineTranscribeBinding); err != nil {
		app.MakeLog().Error(fmt.Sprintf("offline: failed to boot offline transcriber: %v", err))
	}
}

// eventPublisher 通过框架事件系统发布转写结果。
//
// 转写服务因此不依赖 Socket.IO：广播、落库等副作用都由事件的监听器承担。
type eventPublisher struct{}

var _ contractsaudio.Publisher = (*eventPublisher)(nil)

// Publish 派发 TranscriptGenerated 事件（含说话人与时间戳）。
func (p *eventPublisher) Publish(result contractsaudio.Result) {
	args := []event.Arg{
		{Type: "string", Value: result.ClientID},
		{Type: "string", Value: result.Text},
		{Type: "bool", Value: result.IsFinal},
		{Type: "int64", Value: result.StartMs},
		{Type: "int64", Value: result.EndMs},
		{Type: "string", Value: result.SpeakerName},
		{Type: "uintptr", Value: result.SpeakerID},
		{Type: "uint", Value: result.MeetingID},
		{Type: "string", Value: result.WordTimestamps},
		{Type: "string", Value: result.SpeakerDescription},
	}
	err := facades.Event().Job(&events.TranscriptGenerated{}, args).Dispatch()
	if err != nil {
		facades.Log().Error(fmt.Sprintf("audio: failed to dispatch transcript event for client %s: %v", result.ClientID, err))
	}
}

// queueArchiver 通过队列异步归档录音。
type queueArchiver struct{}

var _ contractsaudio.RecordingArchiver = (*queueArchiver)(nil)

// Archive 派发 ArchiveRecording 任务，携带可选的 meetingID。
//
// 连接与队列名可通过 audio.storage.connection / audio.storage.queue 配置，
// 留空时使用 queue.default 指定的默认连接。
func (a *queueArchiver) Archive(clientID, tempFile string, meetingID uint) error {
	config := facades.Config()

	pending := facades.Queue().Job(&jobs.ArchiveRecording{}, []queue.Arg{
		{Type: "string", Value: clientID},
		{Type: "string", Value: tempFile},
		{Type: "uint", Value: meetingID},
	})

	if connection := config.GetString("audio.storage.connection"); connection != "" {
		pending = pending.OnConnection(connection)
	}
	if name := config.GetString("audio.storage.queue"); name != "" {
		pending = pending.OnQueue(name)
	}

	return pending.Dispatch()
}
