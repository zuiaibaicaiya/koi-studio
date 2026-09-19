package contracts

import (
	"context"
	"net/http"

	socketio "github.com/zishang520/socket.io/servers/socket/v3"
)

type Socketio interface {
	On(event string, handler EventHandler)
	OnNamespace(namespace, event string, handler EventHandler)
	Emit(event string, args ...any)
	EmitToNamespace(namespace, event string, args ...any)
	EmitToRoom(namespace, room, event string, args ...any)
	JoinRoom(socket *socketio.Socket, room string) error
	LeaveRoom(socket *socketio.Socket, room string) error
	Close() error
	Server() *socketio.Server
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	GetConnectionManager() ConnectionManager
	Use(middleware Middleware)
	UseNamespace(namespace string, middleware Middleware)
	Shutdown(ctx context.Context) error
}

type EventHandler func(*socketio.Socket, ...any) error

type Middleware interface {
	Handle(socket *socketio.Socket, next func() error) error
}

type MiddlewareFunc func(*socketio.Socket, func() error) error

func (f MiddlewareFunc) Handle(socket *socketio.Socket, next func() error) error {
	return f(socket, next)
}

// ConnectionManager 连接登记表：只负责在线连接的登记与查询；
// 房间/命名空间拓扑以 socket.io 服务器内部状态为权威。
type ConnectionManager interface {
	RegisterConnection(socket *socketio.Socket, namespace string)
	RemoveConnection(socketID string)
	GetConnection(socketID string) *socketio.Socket
	GetAllConnections() []*socketio.Socket
	GetConnectionCount() int
	ClearConnections()
}
