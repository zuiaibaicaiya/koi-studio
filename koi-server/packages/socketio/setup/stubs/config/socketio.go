package config

import (
	"github.com/goravel/framework/facades"
)

func init() {
	facades.Config().Add("socketio", map[string]any{
		"host":          "localhost",
		"port":          3000,
		"path":          "/socket.io",
		"ping_interval": 25000,
		"ping_timeout":  5000,
	})
}
