package routes

import (
	"github.com/goravel/framework/contracts/route"

	"koi-server/app/facades"
	"koi-server/app/http/controllers/api"
	"koi-server/app/http/middleware"
)

// Api 注册 API 与实时通信路由。
//
// 这里同时充当组合根：从容器解析各项依赖并显式注入控制器，
// 使控制器本身不依赖 facades，便于单元测试。
func Api() {
	socketioController := api.NewSocketioController(
		facades.Socketio(),
		facades.Audio(),
		facades.Log(),
	)

	// 注册 Socket.IO 事件处理器。
	socketioController.RegisterHandlers()

	// Socket.IO 端点由自身握手协议完成鉴权，不挂载 JWT 中间件。
	facades.Route().Any("/socket.io/*any", socketioController.ServeSocketio)

	userController := api.NewUserController()

	// 登录接口，无需认证。
	facades.Route().Post("/api/user/login", userController.Login)

	// 用户管理接口，统一挂载 JWT 认证中间件。
	facades.Route().Prefix("api").Middleware(middleware.JWT()).Group(func(router route.Router) {
		router.Post("/user/logout", userController.Logout)
		router.Get("/user/current", userController.GetCurrentUserInfo)

		router.Get("/user", userController.ListUsers)
		router.Post("/user", userController.CreateUser)
		router.Get("/user/{id}", userController.GetUser)
		router.Put("/user/{id}", userController.UpdateUser)
		router.Delete("/user/{id}", userController.DeleteUser)
	})
}
