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
	"koi-server/app/services/audio"
)

// AudioServiceProvider 注册音频实时转写服务。
type AudioServiceProvider struct{}

// 编译期确认满足框架的服务提供者契约。
var _ foundation.ServiceProvider = (*AudioServiceProvider)(nil)

// Register 把转写服务以单例形式绑定到容器。
//
// 单例保证全局只加载一份语音模型；具体实现通过构造函数注入日志、存储、
// 结果发布器与录音归档器，任一依赖都可在测试中替换为替身。
func (r *AudioServiceProvider) Register(app foundation.Application) {
	app.Singleton(contractsaudio.Binding, func(app foundation.Application) (any, error) {
		config := audio.NewConfig(app.MakeConfig())

		return audio.NewService(config, audio.Dependencies{
			Log:       app.MakeLog(),
			Storage:   app.MakeStorage().Disk(config.Disk),
			Publisher: &eventPublisher{},
			Archiver:  &queueArchiver{},
		})
	})
}

// Boot 提前实例化服务，使模型在应用启动阶段就开始异步预加载，
// 而不是等到第一个客户端接入时才触发。
func (r *AudioServiceProvider) Boot(app foundation.Application) {
	if _, err := app.Make(contractsaudio.Binding); err != nil {
		app.MakeLog().Error(fmt.Sprintf("audio: failed to boot transcriber: %v", err))
	}
}

// eventPublisher 通过框架事件系统发布转写结果。
//
// 转写服务因此不依赖 Socket.IO：广播、落库等副作用都由事件的监听器承担。
type eventPublisher struct{}

var _ contractsaudio.Publisher = (*eventPublisher)(nil)

// Publish 派发 TranscriptGenerated 事件。
func (p *eventPublisher) Publish(result contractsaudio.Result) {
	err := facades.Event().Job(&events.TranscriptGenerated{}, []event.Arg{
		{Type: "string", Value: result.ClientID},
		{Type: "string", Value: result.Text},
		{Type: "bool", Value: result.IsFinal},
	}).Dispatch()
	if err != nil {
		facades.Log().Error(fmt.Sprintf("audio: failed to dispatch transcript event for client %s: %v", result.ClientID, err))
	}
}

// queueArchiver 通过队列异步归档录音。
type queueArchiver struct{}

var _ contractsaudio.RecordingArchiver = (*queueArchiver)(nil)

// Archive 派发 ArchiveRecording 任务。
//
// 连接与队列名可通过 audio.storage.connection / audio.storage.queue 配置，
// 留空时使用 queue.default 指定的默认连接。
func (a *queueArchiver) Archive(clientID, tempFile string) error {
	config := facades.Config()

	pending := facades.Queue().Job(&jobs.ArchiveRecording{}, []queue.Arg{
		{Type: "string", Value: clientID},
		{Type: "string", Value: tempFile},
	})

	if connection := config.GetString("audio.storage.connection"); connection != "" {
		pending = pending.OnConnection(connection)
	}
	if name := config.GetString("audio.storage.queue"); name != "" {
		pending = pending.OnQueue(name)
	}

	return pending.Dispatch()
}
