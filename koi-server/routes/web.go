package routes

import (
	"github.com/goravel/framework/contracts/http"
	"github.com/goravel/framework/support"

	"koi-server/app/facades"
	"koi-server/app/http/controllers/api"
)

func Web() {
	facades.Route().Get("/", func(ctx http.Context) http.Response {
		return ctx.Response().View().Make("welcome.tmpl", map[string]any{
			"version": support.Version,
		})
	})

	facades.Route().Static("public", "./public")

	// 会议录音等音频文件通过 /audio/{file} 直接访问
	facades.Route().Get("/audio/{file}", api.NewAudioController().Serve)
}
