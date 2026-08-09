package config

import (
	"koi-server/app/facades"
)

func init() {
	config := facades.Config()
	config.Add("socketio", map[string]any{
		// Socket.IO 服务器配置
		"server": map[string]any{
			// 是否启用 Socket.IO 服务器
			"enabled": config.Env("SOCKETIO_ENABLED", true),
			// 路径
			"path": config.Env("SOCKETIO_PATH", "/socket.io"),
			// 允许的 origins
			"cors": map[string]any{
				"origins":         []string{"*"},
				"methods":         []string{"GET", "POST"},
				"allowed_headers": []string{"Origin", "Content-Type", "Accept", "Authorization"},
			},
			// 传输方式
			"transports": []string{"websocket", "polling"},
		},
		// 连接管理配置
		"connection": map[string]any{
			// 最大连接数
			"max_connections": config.Env("SOCKETIO_MAX_CONNECTIONS", 1000),
			// 心跳间隔（毫秒）
			"ping_interval": config.Env("SOCKETIO_PING_INTERVAL", 25000),
			// 心跳超时（毫秒）
			"ping_timeout": config.Env("SOCKETIO_PING_TIMEOUT", 5000),
			// 连接超时（毫秒）
			"connect_timeout": config.Env("SOCKETIO_CONNECT_TIMEOUT", 45000),
		},
		// 消息配置
		"message": map[string]any{
			// 最大消息大小（字节）
			"max_message_size": config.Env("SOCKETIO_MAX_MESSAGE_SIZE", 1048576),
			// 是否允许二进制消息
			"allow_binary": config.Env("SOCKETIO_ALLOW_BINARY", true),
		},
	})
}
