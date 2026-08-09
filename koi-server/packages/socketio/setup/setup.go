package main

import (
	"os"

	"github.com/goravel/framework/packages"
	"github.com/goravel/framework/packages/modify"
	"github.com/goravel/framework/support/file"
	"github.com/goravel/framework/support/path"
)

func main() {
	// 初始化 setup 以获取路径
	setup := packages.Setup(os.Args)

	// 通过这种方式安装时，配置文件将自动发布到项目的配置目录。
	// 你也可以手动发布此配置文件：./artisan vendor:publish --package=github.com/koi-electron/koi-server/packages/socketio
	config, err := file.GetPackageContent(setup.Paths().Module().String(), "setup/stubs/config/socketio.go")
	if err != nil {
		panic(err)
	}

	serviceProvider := "&socketio.ServiceProvider{}"
	moduleImport := setup.Paths().Module().Import()

	setup.Install(
		// 将服务提供者注册到 bootstrap/providers.go 中的 providers 切片
		modify.RegisterProvider(moduleImport, serviceProvider),

		// 将配置文件添加到配置目录
		modify.File(path.Config("socketio.go")).Overwrite(config),
	).Uninstall(
		// 从配置目录中移除配置文件
		modify.File(path.Config("socketio.go")).Remove(),

		// 从 bootstrap/providers.go 中的 providers 切片中注销服务提供者
		modify.UnregisterProvider(moduleImport, serviceProvider),
	).Execute()
}
