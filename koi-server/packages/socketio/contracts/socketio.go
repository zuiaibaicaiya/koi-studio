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
	GetClientsInRoom(namespace, room string) []*socketio.Socket
	GetAllClients(namespace string) []*socketio.Socket
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

type ConnectionManager interface {
	RegisterConnection(socket *socketio.Socket, namespace string)
	RemoveConnection(socketID string)
	GetConnection(socketID string) *socketio.Socket
	GetAllConnections() []*socketio.Socket
	GetConnectionCount() int
	GetClientsInRoom(namespace, room string) []*socketio.Socket
	GetClientsInNamespace(namespace string) []*socketio.Socket
	BroadcastToAll(event string, args ...any)
	BroadcastToNamespace(namespace string, event string, args ...any)
	ClearConnections()
}
