package config

import (
	"github.com/goravel/framework/contracts/config"
)

type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Connection ConnectionConfig `mapstructure:"connection"`
	Message    MessageConfig    `mapstructure:"message"`
}

type ServerConfig struct {
	Enabled bool       `mapstructure:"enabled" default:"true"`
	Path    string     `mapstructure:"path" default:"/socket.io"`
	CORS    CORSConfig `mapstructure:"cors"`
}

type CORSConfig struct {
	Origins []string `mapstructure:"origins" default:"*"`
}

type ConnectionConfig struct {
	MaxConnections int `mapstructure:"max_connections" default:"1000"`
	PingInterval   int `mapstructure:"ping_interval" default:"25000"`
	PingTimeout    int `mapstructure:"ping_timeout" default:"5000"`
	ConnectTimeout int `mapstructure:"connect_timeout" default:"45000"`
}

type MessageConfig struct {
	MaxMessageSize int `mapstructure:"max_message_size" default:"1048576"`
}

func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Enabled: true,
			Path:    "/socket.io",
			CORS: CORSConfig{
				Origins: []string{"*"},
			},
		},
		Connection: ConnectionConfig{
			MaxConnections: 1000,
			PingInterval:   25000,
			PingTimeout:    5000,
			ConnectTimeout: 45000,
		},
		Message: MessageConfig{
			MaxMessageSize: 1048576,
		},
	}
}

func FromConfig(cfg config.Config) Config {
	socketioConfig := DefaultConfig()

	socketioConfig.Server.Enabled = cfg.GetBool("socketio.server.enabled", socketioConfig.Server.Enabled)
	socketioConfig.Server.Path = cfg.GetString("socketio.server.path", socketioConfig.Server.Path)

	if origins, ok := cfg.Get("socketio.server.cors.origins").([]string); ok {
		socketioConfig.Server.CORS.Origins = origins
	}

	socketioConfig.Connection.MaxConnections = cfg.GetInt("socketio.connection.max_connections", socketioConfig.Connection.MaxConnections)
	socketioConfig.Connection.PingInterval = cfg.GetInt("socketio.connection.ping_interval", socketioConfig.Connection.PingInterval)
	socketioConfig.Connection.PingTimeout = cfg.GetInt("socketio.connection.ping_timeout", socketioConfig.Connection.PingTimeout)
	socketioConfig.Connection.ConnectTimeout = cfg.GetInt("socketio.connection.connect_timeout", socketioConfig.Connection.ConnectTimeout)
	socketioConfig.Message.MaxMessageSize = cfg.GetInt("socketio.message.max_message_size", socketioConfig.Message.MaxMessageSize)

	return socketioConfig
}

func (c *Config) Validate() error {
	if c.Server.Path == "" {
		c.Server.Path = "/socket.io"
	}
	if c.Connection.PingInterval <= 0 {
		c.Connection.PingInterval = 25000
	}
	if c.Connection.PingTimeout <= 0 {
		c.Connection.PingTimeout = 5000
	}
	if c.Connection.ConnectTimeout <= 0 {
		c.Connection.ConnectTimeout = 45000
	}
	return nil
}
