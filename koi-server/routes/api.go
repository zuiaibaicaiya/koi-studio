package routes

import (
	"koi-server/app/facades"
	"koi-server/app/http/controllers/api"
)

func Api() {
	socketioController := api.NewSocketioController()

	// 注册Socket.IO事件处理器
	socketioController.RegisterHandlers()

	// socket.io路由，不需要JWT中间件
	facades.Route().Any("/socket.io/*any", socketioController.ServeSocketio)
}
