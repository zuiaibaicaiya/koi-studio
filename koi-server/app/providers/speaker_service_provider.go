package providers

import (
	"fmt"

	"github.com/goravel/framework/contracts/foundation"

	contractsspeaker "koi-server/app/contracts/speaker"
	"koi-server/app/services/speaker"
)

// SpeakerServiceProvider 注册说话人声纹服务。
type SpeakerServiceProvider struct{}

// 编译期确认满足框架的服务提供者契约。
var _ foundation.ServiceProvider = (*SpeakerServiceProvider)(nil)

// Register 把声纹服务以单例形式绑定到容器。
//
// 单例保证全局只加载一份声纹模型，并让内存声纹库在所有请求间共享。
func (r *SpeakerServiceProvider) Register(app foundation.Application) {
	app.Singleton(contractsspeaker.Binding, func(app foundation.Application) (any, error) {
		config := speaker.NewConfig(app.MakeConfig())

		return speaker.NewService(config, speaker.Dependencies{
			Log: app.MakeLog(),
		})
	})
}

// Boot 提前实例化服务，使声纹模型在应用启动阶段就开始异步预加载。
func (r *SpeakerServiceProvider) Boot(app foundation.Application) {
	if _, err := app.Make(contractsspeaker.Binding); err != nil {
		app.MakeLog().Error(fmt.Sprintf("speaker: failed to boot voiceprint service: %v", err))
	}
}
