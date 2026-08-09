# Socket.IO 后端包说明文档

## 1. 包概述

Socket.IO 后端包是 Koi-Electron 项目中用于实现实时通信的核心组件，基于 Go 语言和 zishang520/socket.io 库开发。该包提供了简洁易用的接口，用于处理客户端与服务器之间的实时双向通信。

## 2. 目录结构

```
socketio/
├── contracts/         # 接口定义
│   └── socketio.go    # Socket.IO 接口定义
├── facades/           # 门面模式
│   └── socketio.go    # Socket.IO 门面
├── setup/             # 安装配置
│   ├── stubs/         # 配置模板
│   ├── setup.go       # 安装逻辑
│   └── stubs.go       # 模板生成
├── tests/             # 测试文件
│   └── socketio_test.go # Socket.IO 测试
├── README.md          # 说明文档
├── go.mod             # Go 模块文件
├── go.sum             # 依赖校验文件
├── service_provider.go # 服务提供者
└── socketio.go        # 核心实现
```

## 3. 核心功能

### 3.1 事件处理

- **命名空间管理**: 支持多个命名空间，隔离不同功能的通信
- **事件注册**: 支持注册自定义事件处理器
- **事件分发**: 支持向特定命名空间、房间或所有客户端发送事件

### 3.2 连接管理

- **连接监听**: 监听客户端连接和断开事件
- **错误处理**: 处理连接过程中的错误
- **房间管理**: 支持客户端加入和离开房间

### 3.3 数据传输

- **事件发送**: 支持向客户端发送事件和数据
- **广播功能**: 支持向多个客户端广播消息
- **房间消息**: 支持向特定房间的客户端发送消息

## 4. 接口设计

### 4.1 Socketio 接口

```go
// Socketio 接口定义
type Socketio interface {
    // On 注册默认命名空间的事件处理器
    On(event string, handler EventHandler)
    // OnNamespace 注册特定命名空间的事件处理器
    OnNamespace(namespace, event string, handler EventHandler)
    // Emit 向默认命名空间的所有客户端发送事件
    Emit(event string, args ...interface{})
    // EmitToNamespace 向特定命名空间的所有客户端发送事件
    EmitToNamespace(namespace, event string, args ...interface{})
    // EmitToRoom 向特定房间的所有客户端发送事件
    EmitToRoom(namespace, room, event string, args ...interface{})
    // JoinRoom 将客户端添加到房间
    JoinRoom(socket *socketio.Socket, room string) error
    // LeaveRoom 将客户端从房间移除
    LeaveRoom(socket *socketio.Socket, room string) error
    // GetClientsInRoom 返回房间中的所有客户端
    GetClientsInRoom(namespace, room string) []*socketio.Socket
    // GetAllClients 返回命名空间中的所有客户端
    GetAllClients(namespace string) []*socketio.Socket
    // Close 关闭 Socket.IO 服务器
    Close() error
    // Server 返回底层的 socketio.Server
    Server() *socketio.Server
    // ServeHTTP 实现 http.Handler 接口
    ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// EventHandler 事件处理器类型
type EventHandler func(*socketio.Socket, ...interface{}) error
```

### 4.2 实现类

- **Socketio**: 核心实现类，封装了底层的 socketio.Server
- **Namespace**: 命名空间类，管理特定命名空间的事件处理器

## 5. 核心实现

### 5.1 Socketio 结构体

```go
type Socketio struct {
    server     *socketio.Server     // 底层 Socket.IO 服务器
    namespaces map[string]*Namespace // 命名空间映射
}
```

### 5.2 初始化方法

```go
func NewSocketio() *Socketio {
    server := socketio.NewServer(nil, nil)

    sio := &Socketio{
        server:     server,
        namespaces: make(map[string]*Namespace),
    }

    sio.setupServer()

    return sio
}
```

### 5.3 事件注册

```go
// 注册默认命名空间的事件处理器
func (s *Socketio) On(event string, handler contracts.EventHandler) {
    s.OnNamespace("/", event, handler)
}

// 注册特定命名空间的事件处理器
func (s *Socketio) OnNamespace(namespace, event string, handler contracts.EventHandler) {
    if _, ok := s.namespaces[namespace]; !ok {
        s.namespaces[namespace] = &Namespace{
            name:     namespace,
            handlers: make(map[string]contracts.EventHandler),
        }

        nsp := s.server.Of(namespace, nil)
        nsp.On("connection", func(args ...interface{}) {
            if len(args) > 0 {
                socket := args[0].(*socketio.Socket)
                log.Println("Client connected to namespace", namespace, ":", socket.Id())
            }
        })
    }

    nsp := s.server.Of(namespace, nil)
    nsp.On(event, func(args ...interface{}) {
        if len(args) > 0 {
            socket := args[0].(*socketio.Socket)
            var eventArgs []interface{}
            if len(args) > 1 {
                eventArgs = args[1:]
            }
            handler(socket, eventArgs...)
        }
    })
}
```

### 5.4 事件发送

```go
// 向默认命名空间的所有客户端发送事件
func (s *Socketio) Emit(event string, args ...interface{}) {
    s.EmitToNamespace("/", event, args...)
}

// 向特定命名空间的所有客户端发送事件
func (s *Socketio) EmitToNamespace(namespace, event string, args ...interface{}) {
    nsp := s.server.Of(namespace, nil)
    nsp.Emit(event, args...)
}

// 向特定房间的所有客户端发送事件
func (s *Socketio) EmitToRoom(namespace, room, event string, args ...interface{}) {
    nsp := s.server.Of(namespace, nil)
    nsp.In(socketio.Room(room)).Emit(event, args...)
}
```

## 6. 集成到 Goravel 框架

### 6.1 服务提供者

```go
package socketio

import (
    "github.com/goravel/framework/contracts/binding"
    "github.com/goravel/framework/contracts/foundation"
)

const Binding = "socketio"

var App foundation.Application

type ServiceProvider struct {
}

// Relationship returns the relationship of the service provider.
func (r *ServiceProvider) Relationship() binding.Relationship {
    return binding.Relationship{
        Bindings: []string{},
        Dependencies: []string{},
        ProvideFor: []string{},
    }
}

// Register registers the service provider.
func (r *ServiceProvider) Register(app foundation.Application) {
    App = app

    app.Bind(Binding, func(app foundation.Application) (any, error) {
        return NewSocketio(), nil
    })
}

// Boot boots the service provider, will be called after all service providers are registered.
func (r *ServiceProvider) Boot(app foundation.Application) {

}
```

### 6.2 门面模式

通过门面模式，可以方便地在应用的任何地方访问 Socket.IO 服务：

```go
// socketio.go
package facades

import (
    "koi-server/packages/socketio"
    "koi-server/packages/socketio/contracts"
)

var Socketio contracts.Socketio

func init() {
    Socketio = socketio.App.Make(socketio.Binding).(contracts.Socketio)
}
```

## 7. 使用示例

### 7.1 基本使用

```go
import (
    "koi-server/packages/socketio"
    "koi-server/packages/socketio/contracts"
    socketio_lib "github.com/zishang520/socket.io/servers/socket/v3"
)

// 初始化 Socket.IO 服务
sio := socketio.NewSocketio()

// 注册连接事件
sio.On("connection", func(socket *socketio_lib.Socket, args ...interface{}) error {
    fmt.Println("Client connected:", socket.Id())
    
    // 发送欢迎消息
    socket.Emit("welcome", "Welcome to socket.io server!")
    
    // 注册消息事件
    socket.On("message", func(args ...interface{}) {
        if len(args) > 0 {
            message := args[0]
            fmt.Println("Received message:", message)
            
            // 回显消息
            socket.Emit("message", "Server received:", message)
        }
    })
    
    // 注册断开连接事件
    socket.On("disconnect", func(args ...interface{}) {
        fmt.Println("Client disconnected:", socket.Id())
    })
    
    return nil
})

// 注册自定义命名空间
sio.OnNamespace("/chat", "connection", func(socket *socketio_lib.Socket, args ...interface{}) error {
    fmt.Println("Client connected to chat namespace:", socket.Id())
    socket.Emit("welcome", "Welcome to chat room!")
    return nil
})

// 处理 HTTP 请求
http.HandleFunc("/socket.io/", func(w http.ResponseWriter, r *http.Request) {
    sio.ServeHTTP(w, r)
})

// 启动 HTTP 服务器
http.ListenAndServe(":8000", nil)
```

### 7.2 与 Goravel 框架集成

```go
// 在控制器中使用
package api

import (
    "koi-server/packages/socketio"
    "koi-server/packages/socketio/contracts"
    socketio_lib "github.com/zishang520/socket.io/servers/socket/v3"
    "github.com/goravel/framework/contracts/http"
)

type SocketioController struct {
    socketio contracts.Socketio
}

func NewSocketioController() *SocketioController {
    return &SocketioController{
        socketio: socketio.NewSocketio(),
    }
}

// SetupSocketio 初始化 socket.io 并设置事件处理
func (c *SocketioController) SetupSocketio() {
    // 处理默认命名空间的连接事件
    c.socketio.On("connection", func(socket *socketio_lib.Socket, args ...interface{}) error {
        // 事件处理...
        return nil
    })
}

// ServeSocketio 处理 socket.io HTTP 请求
func (c *SocketioController) ServeSocketio(ctx http.Context) http.Response {
    // 使用Goravel的Context接口获取请求和响应
    req := ctx.Request()
    res := ctx.Response()

    // 调用socket.io的ServeHTTP方法
    c.socketio.ServeHTTP(res.Writer(), req.Origin())
    return nil
}
```

## 8. 配置与部署

### 8.1 配置选项

Socket.IO 服务器支持以下配置选项：

- **传输方式**: WebSocket、轮询等
- **路径**: Socket.IO 请求的路径
- **命名空间**: 隔离不同功能的通信
- **房间**: 分组客户端，实现定向消息

### 8.2 部署建议

- **负载均衡**: 如果使用负载均衡，需要确保 Socket.IO 连接能够正确路由到同一服务器实例，或使用 Redis 等存储来共享连接状态
- **性能优化**: 根据预期的并发连接数，调整服务器配置和 Socket.IO 相关参数
- **安全配置**: 启用 HTTPS，设置适当的 CORS 策略，防止未授权访问

## 9. 测试与调试

### 9.1 测试文件

测试文件位于 `tests/socketio_test.go`，包含了对 Socket.IO 核心功能的测试。

### 9.2 调试技巧

- **日志记录**: 使用 `log.Println` 记录连接、断开和事件信息
- **网络监控**: 使用浏览器的网络面板监控 WebSocket 连接和数据传输
- **错误处理**: 确保所有错误都被正确捕获和处理

## 10. 常见问题

| 问题 | 可能原因 | 解决方案 |
|------|---------|--------|
| 连接失败 | 服务器未运行或端口错误 | 检查后端服务是否启动，端口是否正确 |
| 事件不响应 | 事件名称不匹配或命名空间错误 | 检查事件名称和命名空间是否正确 |
| 数据传输失败 | 数据格式错误或过大 | 检查数据格式，避免传输过大的数据 |
| 重连失败 | 网络问题或服务器异常 | 检查网络连接，确保服务器正常运行 |
| 性能问题 | 并发连接数过高 | 优化服务器配置，考虑使用负载均衡 |

## 11. 版本兼容性

- **后端**: zishang520/socket.io v3
- **前端**: socket.io-client v4.8.3

确保前后端 Socket.IO 版本兼容，避免因版本差异导致的通信问题。

## 12. 总结

Socket.IO 后端包为 Koi-Electron 项目提供了强大的实时通信能力，支持多种场景下的实时数据传输。通过合理的设计和使用，可以构建出响应迅速、用户体验良好的实时应用。

该包具有以下特点：

- **简洁易用**: 提供了清晰的接口，方便集成和使用
- **功能丰富**: 支持命名空间、房间、广播等高级功能
- **稳定可靠**: 基于成熟的 zishang520/socket.io 库
- **易于扩展**: 模块化设计，便于添加新功能

在使用过程中，应注意连接管理、事件命名规范、数据传输优化等方面，确保系统的稳定性和性能。同时，定期测试和监控 Socket.IO 连接状态，及时发现和解决潜在问题。