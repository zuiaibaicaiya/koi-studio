package socketio

import (
	"koi-server/packages/socketio/contracts"

	"github.com/goravel/framework/contracts/binding"
	"github.com/goravel/framework/contracts/config"
	"github.com/goravel/framework/contracts/foundation"
	"github.com/goravel/framework/facades"
)

const Binding = "socketio"

type ServiceProvider struct {
}

func (r *ServiceProvider) Relationship() binding.Relationship {
	return binding.Relationship{
		Bindings:     []string{Binding},
		Dependencies: []string{binding.Config},
		ProvideFor:   []string{},
	}
}

func (r *ServiceProvider) Register(app foundation.Application) {
	app.Singleton(Binding, func(app foundation.Application) (any, error) {
		return NewSocketio(app.MakeConfig()), nil
	})
	facades.Log().Info("Socket.IO service provider registered, version: " + PackageVersion)
}

func (r *ServiceProvider) Boot(app foundation.Application) {
	facades.Log().Info("Socket.IO service provider booted")
}

func (r *ServiceProvider) Runners(app foundation.Application) []foundation.Runner {
	socketioInstance, err := app.Make(Binding)
	if err != nil {
		facades.Log().Error("Failed to make socketio instance: " + err.Error())
		return []foundation.Runner{}
	}
	return []foundation.Runner{
		NewSocketioRunner(app.MakeConfig(), socketioInstance.(contracts.Socketio)),
	}
}

type SocketioRunner struct {
	config   config.Config
	socketio contracts.Socketio
}

func NewSocketioRunner(config config.Config, socketio contracts.Socketio) *SocketioRunner {
	return &SocketioRunner{
		config:   config,
		socketio: socketio,
	}
}

func (r *SocketioRunner) Signature() string {
	return "socketio.runner"
}

func (r *SocketioRunner) ShouldRun() bool {
	return r.config.GetBool("socketio.server.enabled", true)
}

func (r *SocketioRunner) Run() error {
	facades.Log().Info("Socket.IO runner started")
	return nil
}

func (r *SocketioRunner) Shutdown() error {
	facades.Log().Info("Shutting down Socket.IO server")
	if err := r.socketio.Close(); err != nil {
		facades.Log().Error("Error shutting down Socket.IO server: " + err.Error())
		return err
	}
	facades.Log().Info("Socket.IO server shut down successfully")
	return nil
}
