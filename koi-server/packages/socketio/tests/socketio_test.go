package tests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	socket "github.com/zishang520/socket.io/servers/socket/v3"

	socketiopkg "koi-server/packages/socketio"
	"koi-server/packages/socketio/contracts"
)

func TestSocketio_On(t *testing.T) {
	sio := socketiopkg.NewSocketio(newMockConfig())

	sio.On("test", func(socket *socket.Socket, args ...interface{}) error {
		if len(args) > 0 {
			assert.Equal(t, "test message", args[0])
		}
		return nil
	})

	assert.NotNil(t, sio)
}

func TestSocketio_OnNamespace(t *testing.T) {
	sio := socketiopkg.NewSocketio(newMockConfig())

	sio.OnNamespace("/chat", "message", func(socket *socket.Socket, args ...interface{}) error {
		if len(args) > 0 {
			assert.Equal(t, "chat message", args[0])
		}
		return nil
	})

	assert.NotNil(t, sio)
}

func TestSocketio_Emit(t *testing.T) {
	sio := socketiopkg.NewSocketio(newMockConfig())
	sio.Emit("broadcast", "hello everyone")
	assert.NotNil(t, sio)
}

func TestSocketio_EmitToNamespace(t *testing.T) {
	sio := socketiopkg.NewSocketio(newMockConfig())
	sio.EmitToNamespace("/chat", "announcement", "new chat message")
	assert.NotNil(t, sio)
}

func TestSocketio_EmitToRoom(t *testing.T) {
	sio := socketiopkg.NewSocketio(newMockConfig())
	sio.EmitToRoom("/chat", "room1", "message", "hello room1")
	assert.NotNil(t, sio)
}

func TestSocketio_Server(t *testing.T) {
	sio := socketiopkg.NewSocketio(newMockConfig())
	server := sio.Server()
	assert.NotNil(t, server)
}

func TestSocketio_Integration(t *testing.T) {
	sio := socketiopkg.NewSocketio(newMockConfig())

	sio.On("ping", func(socket *socket.Socket, args ...interface{}) error {
		return nil
	})

	sio.Emit("broadcast", "test broadcast")
	assert.NotNil(t, sio)
}

func TestSocketio_ConnectionManager(t *testing.T) {
	sio := socketiopkg.NewSocketio(newMockConfig())
	cm := sio.GetConnectionManager()

	count := cm.GetConnectionCount()
	assert.Equal(t, 0, count)

	connections := cm.GetAllConnections()
	assert.Len(t, connections, 0)

	cm.ClearConnections()
	count = cm.GetConnectionCount()
	assert.Equal(t, 0, count)
}

func TestSocketio_Use(t *testing.T) {
	sio := socketiopkg.NewSocketio(newMockConfig())

	sio.Use(contracts.MiddlewareFunc(func(socket *socket.Socket, next func() error) error {
		return next()
	}))

	assert.NotNil(t, sio)
}

func TestSocketio_UseNamespace(t *testing.T) {
	sio := socketiopkg.NewSocketio(newMockConfig())

	sio.UseNamespace("/chat", contracts.MiddlewareFunc(func(socket *socket.Socket, next func() error) error {
		return next()
	}))

	assert.NotNil(t, sio)
}
