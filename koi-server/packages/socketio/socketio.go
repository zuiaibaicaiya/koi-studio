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
	server *socketio.Server
	config goravelconfig.Config
	// mu 保护 namespaces：事件回调在库的内部协程中读取中间件链，
	// 而命名空间可能在运行期继续注册，必须避免并发读写 map。
	mu          sync.RWMutex
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
	s.server.On("connection", func(args ...any) {
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

			socket.On("disconnect", func(reason ...any) {
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

			socket.On("error", func(err ...any) {
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
	s.mu.Lock()
	_, exists := s.namespaces[namespace]
	if !exists {
		s.namespaces[namespace] = &Namespace{
			name:        namespace,
			handlers:    make(map[string]contracts.EventHandler),
			middlewares: make([]contracts.Middleware, 0),
		}
	}
	s.namespaces[namespace].handlers[event] = handler
	s.mu.Unlock()

	if !exists {
		nsp := s.server.Of(namespace, nil)
		nsp.On("connection", func(args ...any) {
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

				socket.On("disconnect", func(reason ...any) {
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

	nsp := s.server.Of(namespace, nil)
	nsp.On(event, func(args ...any) {
		defer func() {
			if r := recover(); r != nil {
				facades.Log().Error(fmt.Sprintf("Event handler panic: %v", r))
			}
		}()
		if len(args) > 0 {
			socket := args[0].(*socketio.Socket)
			var eventArgs []any
			if len(args) > 1 {
				eventArgs = args[1:]
			}

			middlewares := s.middlewaresOf(namespace)
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

func (s *Socketio) Emit(event string, args ...any) {
	s.EmitToNamespace(DefaultNamespace, event, args...)
}

func (s *Socketio) EmitToNamespace(namespace, event string, args ...any) {
	nsp := s.server.Of(namespace, nil)
	nsp.Emit(event, args...)
}

func (s *Socketio) EmitToRoom(namespace, room, event string, args ...any) {
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
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.namespaces[namespace]; !ok {
		s.namespaces[namespace] = &Namespace{
			name:        namespace,
			handlers:    make(map[string]contracts.EventHandler),
			middlewares: make([]contracts.Middleware, 0),
		}
	}
	s.namespaces[namespace].middlewares = append(s.namespaces[namespace].middlewares, middleware)
}

// middlewaresOf 并发安全地读取命名空间的中间件链快照。
func (s *Socketio) middlewaresOf(namespace string) []contracts.Middleware {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nsp, ok := s.namespaces[namespace]
	if !ok || len(nsp.middlewares) == 0 {
		return nil
	}
	return append([]contracts.Middleware(nil), nsp.middlewares...)
}

func (s *Socketio) GetConnectionManager() contracts.ConnectionManager {
	return s.connections
}
