package socketio

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	sioconfig "koi-server/packages/socketio/config"
	"koi-server/packages/socketio/contracts"

	goravelconfig "github.com/goravel/framework/contracts/config"
	"github.com/goravel/framework/facades"
	socketio "github.com/zishang520/socket.io/servers/socket/v3"
)

type Socketio struct {
	server      *socketio.Server
	config      goravelconfig.Config
	namespaces  map[string]*Namespace
	connections *ConnectionManager
	initOnce    sync.Once
	closeOnce   sync.Once
	closed      bool
}

type Namespace struct {
	name        string
	handlers    map[string]contracts.EventHandler
	middlewares []contracts.Middleware
}

func NewSocketio(config goravelconfig.Config) *Socketio {
	sio := &Socketio{
		config:      config,
		namespaces:  make(map[string]*Namespace),
		connections: NewConnectionManager(),
	}

	sio.initOnce.Do(func() {
		socketioConfig := sio.loadConfig()

		server := socketio.NewServer(nil, socketio.DefaultServerOptions())

		if socketioConfig.Server.Path != "" {
			server.SetPath(socketioConfig.Server.Path)
		}

		sio.server = server
		sio.setupServer()
	})

	return sio
}

func (s *Socketio) loadConfig() sioconfig.Config {
	var socketioConfig sioconfig.Config

	socketioConfig.Server.Enabled = s.config.GetBool("socketio.server.enabled", true)
	socketioConfig.Server.Path = s.config.GetString("socketio.server.path", "/socket.io")

	if origins, ok := s.config.Get("socketio.server.cors.origins").([]string); ok {
		socketioConfig.Server.CORS.Origins = origins
	}

	socketioConfig.Connection.MaxConnections = s.config.GetInt("socketio.connection.max_connections", 1000)
	socketioConfig.Connection.PingInterval = s.config.GetInt("socketio.connection.ping_interval", 25000)
	socketioConfig.Connection.PingTimeout = s.config.GetInt("socketio.connection.ping_timeout", 5000)

	return socketioConfig
}

func (s *Socketio) setupServer() {
	s.server.On("connection", func(args ...interface{}) {
		defer func() {
			if r := recover(); r != nil {
				facades.Log().Error(fmt.Sprintf("Connection event panic: %v", r))
			}
		}()
		if len(args) > 0 {
			socket := args[0].(*socketio.Socket)
			socketID := string(socket.Id())

			facades.Log().Info("Client connected: " + socketID)
			s.connections.RegisterConnection(socket, DefaultNamespace)

			socket.On("disconnect", func(reason ...interface{}) {
				defer func() {
					if r := recover(); r != nil {
						facades.Log().Error(fmt.Sprintf("Disconnect event panic: %v", r))
					}
				}()
				disconnectReason := "unknown"
				if len(reason) > 0 {
					if r, ok := reason[0].(string); ok {
						disconnectReason = r
					}
				}
				facades.Log().Info(fmt.Sprintf("Client disconnected: %s, reason: %s", socketID, disconnectReason))
				s.connections.RemoveConnection(socketID)
			})

			socket.On("error", func(err ...interface{}) {
				defer func() {
					if r := recover(); r != nil {
						facades.Log().Error(fmt.Sprintf("Error event panic: %v", r))
					}
				}()
				if len(err) > 0 {
					facades.Log().Error(fmt.Sprintf("Socket error: %v", err[0]))
				}
			})
		}
	})
}

func (s *Socketio) On(event string, handler contracts.EventHandler) {
	s.OnNamespace(DefaultNamespace, event, handler)
}

func (s *Socketio) OnNamespace(namespace, event string, handler contracts.EventHandler) {
	if _, ok := s.namespaces[namespace]; !ok {
		s.namespaces[namespace] = &Namespace{
			name:        namespace,
			handlers:    make(map[string]contracts.EventHandler),
			middlewares: make([]contracts.Middleware, 0),
		}

		nsp := s.server.Of(namespace, nil)
		nsp.On("connection", func(args ...interface{}) {
			defer func() {
				if r := recover(); r != nil {
					facades.Log().Error(fmt.Sprintf("Namespace connection event panic: %v", r))
				}
			}()
			if len(args) > 0 {
				socket := args[0].(*socketio.Socket)
				socketID := string(socket.Id())

				facades.Log().Info(fmt.Sprintf("Client connected to namespace %s: %s", namespace, socketID))
				s.connections.RegisterConnection(socket, namespace)

				socket.On("disconnect", func(reason ...interface{}) {
					defer func() {
						if r := recover(); r != nil {
							facades.Log().Error(fmt.Sprintf("Namespace disconnect event panic: %v", r))
						}
					}()
					disconnectReason := "unknown"
					if len(reason) > 0 {
						if r, ok := reason[0].(string); ok {
							disconnectReason = r
						}
					}
					facades.Log().Info(fmt.Sprintf("Client disconnected from namespace %s: %s, reason: %s", namespace, socketID, disconnectReason))
					s.connections.RemoveConnection(socketID)
				})
			}
		})
	}

	s.namespaces[namespace].handlers[event] = handler

	nsp := s.server.Of(namespace, nil)
	nsp.On(event, func(args ...interface{}) {
		defer func() {
			if r := recover(); r != nil {
				facades.Log().Error(fmt.Sprintf("Event handler panic: %v", r))
			}
		}()
		if len(args) > 0 {
			socket := args[0].(*socketio.Socket)
			var eventArgs []interface{}
			if len(args) > 1 {
				eventArgs = args[1:]
			}

			middlewares := s.namespaces[namespace].middlewares
			if len(middlewares) > 0 {
				index := 0
				var execNext func() error
				execNext = func() error {
					if index >= len(middlewares) {
						return handler(socket, eventArgs...)
					}
					currentMiddleware := middlewares[index]
					index++
					return currentMiddleware.Handle(socket, execNext)
				}
				if err := execNext(); err != nil {
					facades.Log().Error("Event handler error: " + err.Error())
					socket.Emit("error", err.Error())
				}
			} else {
				if err := handler(socket, eventArgs...); err != nil {
					facades.Log().Error("Event handler error: " + err.Error())
					socket.Emit("error", err.Error())
				}
			}
		}
	})
}

func (s *Socketio) Emit(event string, args ...interface{}) {
	s.EmitToNamespace(DefaultNamespace, event, args...)
}

func (s *Socketio) EmitToNamespace(namespace, event string, args ...interface{}) {
	nsp := s.server.Of(namespace, nil)
	nsp.Emit(event, args...)
}

func (s *Socketio) EmitToRoom(namespace, room, event string, args ...interface{}) {
	nsp := s.server.Of(namespace, nil)
	nsp.In(socketio.Room(room)).Emit(event, args...)
}

func (s *Socketio) JoinRoom(socket *socketio.Socket, room string) error {
	socket.Join(socketio.Room(room))
	return nil
}

func (s *Socketio) LeaveRoom(socket *socketio.Socket, room string) error {
	socket.Leave(socketio.Room(room))
	return nil
}

func (s *Socketio) GetClientsInRoom(namespace, room string) []*socketio.Socket {
	return s.connections.GetClientsInRoom(namespace, room)
}

func (s *Socketio) GetAllClients(namespace string) []*socketio.Socket {
	return s.connections.GetClientsInNamespace(namespace)
}

func (s *Socketio) Close() error {
	return s.Shutdown(context.Background())
}

func (s *Socketio) Shutdown(ctx context.Context) error {
	var err error
	s.closeOnce.Do(func() {
		s.closed = true
		s.connections.ClearConnections()

		s.server.Close(func(closeErr error) {
			if closeErr != nil {
				err = closeErr
			}
		})
	})
	return err
}

func (s *Socketio) Server() *socketio.Server {
	return s.server
}

func (s *Socketio) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.server.ServeHandler(nil).ServeHTTP(w, r)
}

func (s *Socketio) Use(middleware contracts.Middleware) {
	s.UseNamespace(DefaultNamespace, middleware)
}

func (s *Socketio) UseNamespace(namespace string, middleware contracts.Middleware) {
	if _, ok := s.namespaces[namespace]; !ok {
		s.namespaces[namespace] = &Namespace{
			name:        namespace,
			handlers:    make(map[string]contracts.EventHandler),
			middlewares: make([]contracts.Middleware, 0),
		}
	}
	s.namespaces[namespace].middlewares = append(s.namespaces[namespace].middlewares, middleware)
}

func (s *Socketio) GetConnectionManager() contracts.ConnectionManager {
	return s.connections
}
