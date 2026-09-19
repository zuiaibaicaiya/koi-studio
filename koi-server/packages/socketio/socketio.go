// Package socketio 基于 zishang520/socket.io 封装的服务端实例，
// 提供命名空间注册、中间件链、连接登记与广播能力。
package socketio

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	sioconfig "koi-server/packages/socketio/config"
	"koi-server/packages/socketio/contracts"

	goravelconfig "github.com/goravel/framework/contracts/config"
	"github.com/goravel/framework/facades"
	socketio "github.com/zishang520/socket.io/servers/socket/v3"
	"github.com/zishang520/socket.io/v3/pkg/types"
)

// shutdownGracePeriod Close 回调异常不返回时的兜底超时。
const shutdownGracePeriod = 5 * time.Second

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
	// maxConnections 由配置 socketio.connection.max_connections 决定，
	// <= 0 表示不限制。
	maxConnections int
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
		sio.maxConnections = socketioConfig.Connection.MaxConnections

		opts := buildServerOptions(socketioConfig)
		server := socketio.NewServer(nil, opts)

		if socketioConfig.Server.Path != "" {
			server.SetPath(socketioConfig.Server.Path)
		}

		sio.server = server
	})

	return sio
}

// buildServerOptions 把配置翻译为引擎选项。
// 此前配置文件中的心跳、CORS、消息大小均未生效（使用库默认值），这里统一应用。
func buildServerOptions(cfg sioconfig.Config) *socketio.ServerOptions {
	opts := socketio.DefaultServerOptions()

	if cfg.Connection.PingInterval > 0 {
		opts.SetPingInterval(time.Duration(cfg.Connection.PingInterval) * time.Millisecond)
	}
	if cfg.Connection.PingTimeout > 0 {
		opts.SetPingTimeout(time.Duration(cfg.Connection.PingTimeout) * time.Millisecond)
	}
	if cfg.Connection.ConnectTimeout > 0 {
		opts.SetConnectTimeout(time.Duration(cfg.Connection.ConnectTimeout) * time.Millisecond)
	}
	if cfg.Message.MaxMessageSize > 0 {
		opts.SetMaxHttpBufferSize(int64(cfg.Message.MaxMessageSize))
	}

	cors := &types.Cors{
		Origin:         "*",
		Methods:        []string{"GET", "POST"},
		AllowedHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization"},
	}
	if len(cfg.Server.CORS.Origins) > 0 {
		cors.Origin = cfg.Server.CORS.Origins
	}
	opts.SetCors(cors)

	return opts
}

func (s *Socketio) loadConfig() sioconfig.Config {
	return sioconfig.FromConfig(s.config)
}

// OnNamespace 在指定命名空间上注册事件处理器，并同时负责该命名空间的
// 连接登记（注册/注销 ConnectionManager、连接数上限）。
//
// 注意：Server.On 只是默认命名空间 "/" 的别名，若再单独为 "/" 注册
// connection 监听会造成同一连接被登记两次，因此连接管理统一收敛在这里。
func (s *Socketio) OnNamespace(namespace, event string, handler contracts.EventHandler) {
	nsp := s.server.Of(namespace, nil)

	s.mu.Lock()
	ns, exists := s.namespaces[namespace]
	if !exists {
		ns = &Namespace{
			name:        namespace,
			handlers:    make(map[string]contracts.EventHandler),
			middlewares: make([]contracts.Middleware, 0),
		}
		s.namespaces[namespace] = ns
	}
	ns.handlers[event] = handler
	s.mu.Unlock()

	if !exists {
		nsp.On("connection", s.handleNamespaceConnection(namespace))
	}

	nsp.On(event, s.handleEvent(namespace, handler))
}

// handleNamespaceConnection 命名空间级别的连接生命周期管理。
func (s *Socketio) handleNamespaceConnection(namespace string) func(args ...any) {
	return func(args ...any) {
		defer recoverPanic("namespace connection event")

		if len(args) == 0 {
			return
		}
		socket, ok := args[0].(*socketio.Socket)
		if !ok {
			return
		}
		socketID := string(socket.Id())

		if s.maxConnections > 0 && s.connections.GetConnectionCount() >= s.maxConnections {
			facades.Log().Warning(fmt.Sprintf(
				"socketio: connection limit %d reached, rejecting client %s", s.maxConnections, socketID))
			socket.Disconnect(true)
			return
		}

		facades.Log().Info(fmt.Sprintf("socketio: client %s connected to namespace %s", socketID, namespace))
		s.connections.RegisterConnection(socket, namespace)

		socket.On("disconnect", func(reason ...any) {
			defer recoverPanic("namespace disconnect event")

			disconnectReason := "unknown"
			if len(reason) > 0 {
				if r, ok := reason[0].(string); ok {
					disconnectReason = r
				}
			}
			facades.Log().Info(fmt.Sprintf("socketio: client %s disconnected from namespace %s, reason: %s",
				socketID, namespace, disconnectReason))
			s.connections.RemoveConnection(socketID)
		})
	}
}

// handleEvent 事件分发：中间件链 + panic 恢复 + 错误回执。
func (s *Socketio) handleEvent(namespace string, handler contracts.EventHandler) func(args ...any) {
	return func(args ...any) {
		defer recoverPanic("event handler")

		if len(args) == 0 {
			return
		}
		socket, ok := args[0].(*socketio.Socket)
		if !ok {
			return
		}
		var eventArgs []any
		if len(args) > 1 {
			eventArgs = args[1:]
		}

		if err := runWithMiddlewares(s.middlewaresOf(namespace), socket, eventArgs, handler); err != nil {
			facades.Log().Error("Event handler error: " + err.Error())
			_ = socket.Emit("error", err.Error())
		}
	}
}

// runWithMiddlewares 依次执行中间件链，链尾调用业务处理器。
func runWithMiddlewares(middlewares []contracts.Middleware, socket *socketio.Socket, eventArgs []any, handler contracts.EventHandler) error {
	index := 0
	var execNext func() error
	execNext = func() error {
		if index >= len(middlewares) {
			return handler(socket, eventArgs...)
		}
		current := middlewares[index]
		index++
		return current.Handle(socket, execNext)
	}
	return execNext()
}

// recoverPanic 统一的 panic 恢复，防止单个事件异常拖垮进程。
func recoverPanic(scene string) {
	if r := recover(); r != nil {
		facades.Log().Error(fmt.Sprintf("socketio %s panic: %v", scene, r))
	}
}

func (s *Socketio) On(event string, handler contracts.EventHandler) {
	s.OnNamespace(DefaultNamespace, event, handler)
}

func (s *Socketio) Emit(event string, args ...any) {
	s.EmitToNamespace(DefaultNamespace, event, args...)
}

func (s *Socketio) EmitToNamespace(namespace, event string, args ...any) {
	s.server.Of(namespace, nil).Emit(event, args...)
}

func (s *Socketio) EmitToRoom(namespace, room, event string, args ...any) {
	s.server.Of(namespace, nil).In(socketio.Room(room)).Emit(event, args...)
}

func (s *Socketio) JoinRoom(socket *socketio.Socket, room string) error {
	socket.Join(socketio.Room(room))
	return nil
}

func (s *Socketio) LeaveRoom(socket *socketio.Socket, room string) error {
	socket.Leave(socketio.Room(room))
	return nil
}

func (s *Socketio) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()
	return s.Shutdown(ctx)
}

func (s *Socketio) Shutdown(ctx context.Context) error {
	var err error
	s.closeOnce.Do(func() {
		s.closed = true
		s.connections.ClearConnections()

		// server.Close 的回调在无内建 httpServer 时是同步调用的，
		// 这里仍用 channel 兜底，避免实现变化导致永久阻塞。
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.server.Close(func(closeErr error) {
				if closeErr != nil {
					facades.Log().Error("socketio: server close error: " + closeErr.Error())
				}
			})
		}()

		select {
		case <-done:
		case <-ctx.Done():
			err = fmt.Errorf("socketio: shutdown timed out: %w", ctx.Err())
		}
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
