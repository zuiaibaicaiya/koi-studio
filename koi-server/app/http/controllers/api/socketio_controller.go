package api

import (
	"bytes"
	"fmt"
	"strconv"

	"koi-server/app/facades"
	"koi-server/app/services/audio"
	"koi-server/packages/socketio/contracts"

	"github.com/goravel/framework/contracts/http"
	socketio_lib "github.com/zishang520/socket.io/servers/socket/v3"
	"github.com/zishang520/socket.io/v3/pkg/types"
)

type SocketioController struct {
	socketio     contracts.Socketio
	audioService *audio.AudioService
}

func NewSocketioController() *SocketioController {
	return &SocketioController{
		socketio:     facades.Socketio(),
		audioService: audio.GetAudioService(),
	}
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

// GetSocketio 获取 socket.io 实例
func (c *SocketioController) GetSocketio() contracts.Socketio {
	return c.socketio
}

// RegisterHandlers 注册事件处理器
func (c *SocketioController) RegisterHandlers() {
	facades.Log().Info("开始注册 Socket.IO 事件处理器")

	// 处理默认命名空间的连接事件
	c.socketio.On("connection", func(socket *socketio_lib.Socket, args ...interface{}) error {
		clientID := string(socket.Id())
		facades.Log().Info("客户端连接: " + clientID)
		// 注册socket到AudioService
		c.audioService.RegisterClientSocket(clientID, socket)
		// 发送欢迎消息
		socket.Emit("welcome", "欢迎连接到Socket.IO服务器")

		// 处理断开连接事件
		socket.On("disconnect", func(args ...interface{}) {
			facades.Log().Info("客户端断开连接: " + clientID)
			// 从AudioService注销socket
			c.audioService.UnregisterClientSocket(clientID)
		})

		// 注册 transcript 事件处理器（用于接收转写结果）
		socket.On("transcript", func(args ...interface{}) {
			facades.Log().Info("接收到 transcript 事件: " + fmt.Sprintf("%v", args))
		})

		// 注册 error 事件处理器
		socket.On("error", func(args ...interface{}) {
			facades.Log().Error("Socket 错误: " + fmt.Sprintf("%v", args))
		})

		// 注册 hello 事件
		socket.On("hello", func(args ...interface{}) {
			facades.Log().Info("接收到 hello 事件: " + fmt.Sprintf("%v", args))
			// 发送响应
			socket.Emit("hello-response", "Hello from server")
		})

		// 注册 with-binary 事件
		socket.On("with-binary", func(args ...interface{}) {
			facades.Log().Info("接收到 with-binary 事件")
			facades.Log().Info("参数数量: " + fmt.Sprintf("%d", len(args)))

			// 处理音频数据
			if len(args) >= 2 {
				// 提取数据和标志
				facades.Log().Info("第一个参数类型: " + fmt.Sprintf("%T", args[0]))
				facades.Log().Info("第二个参数类型: " + fmt.Sprintf("%T", args[1]))

				// 尝试不同的类型转换
				var data []byte
				var flag int

				// 处理数据
				switch v := args[0].(type) {
				case []byte:
					data = v
					facades.Log().Info("数据类型: []byte, 长度: " + fmt.Sprintf("%d", len(data)))
				case string:
					data = []byte(v)
					facades.Log().Info("数据类型: string, 长度: " + fmt.Sprintf("%d", len(data)))
				case []int8:
					data = make([]byte, len(v))
					for i, b := range v {
						data[i] = byte(b)
					}
					facades.Log().Info("数据类型: []int8, 长度: " + fmt.Sprintf("%d", len(data)))
				case []uint16:
					data = make([]byte, len(v)*2)
					for i, b := range v {
						data[i*2] = byte(b & 0xFF)
						data[i*2+1] = byte(b >> 8)
					}
					facades.Log().Info("数据类型: []uint16, 长度: " + fmt.Sprintf("%d", len(data)))
				case []int16:
					data = make([]byte, len(v)*2)
					for i, b := range v {
						data[i*2] = byte(b & 0xFF)
						data[i*2+1] = byte(b >> 8)
					}
					facades.Log().Info("数据类型: []int16, 长度: " + fmt.Sprintf("%d", len(data)))
				case *types.BytesBuffer:
					// 处理 BytesBuffer 类型
					data = v.Bytes()
					facades.Log().Info("数据类型: *types.BytesBuffer, 长度: " + fmt.Sprintf("%d", len(data)))
				case *bytes.Buffer:
					// 处理标准 bytes.Buffer 类型
					data = v.Bytes()
					facades.Log().Info("数据类型: *bytes.Buffer, 长度: " + fmt.Sprintf("%d", len(data)))
				default:
					// 尝试检查是否为二进制数据的其他表示形式
					facades.Log().Error("不支持的数据类型: " + fmt.Sprintf("%T", v))
					// 尝试将任何类型转换为字符串，然后再转换为 []byte
					str := fmt.Sprintf("%v", v)
					data = []byte(str)
					facades.Log().Info("数据类型: unknown, 转换为字符串后长度: " + fmt.Sprintf("%d", len(data)))
				}

				// 处理标志
				switch v := args[1].(type) {
				case int:
					flag = v
					facades.Log().Info("标志类型: int, 值: " + fmt.Sprintf("%d", flag))
				case float64:
					flag = int(v)
					facades.Log().Info("标志类型: float64, 值: " + fmt.Sprintf("%d", flag))
				case string:
					// 尝试从字符串转换
					f, err := strconv.Atoi(v)
					if err != nil {
						facades.Log().Error("Failed to convert flag to int: " + err.Error())
						socket.Emit("error", "Failed to convert flag to int")
						return
					}
					flag = f
					facades.Log().Info("标志类型: string, 值: " + fmt.Sprintf("%d", flag))
				default:
					facades.Log().Error("不支持的标志类型: " + fmt.Sprintf("%T", v))
					socket.Emit("error", "Unsupported flag type")
					return
				}

				// 模型尚未加载完成时丢弃音频，避免在工作协程排队堆积
				if !c.audioService.IsModelReady() {
					return
				}

				// 处理音频数据
				err := c.audioService.ProcessAudioData(string(socket.Id()), data, flag)
				if err != nil {
					facades.Log().Error("Failed to process audio data: " + err.Error())
					socket.Emit("error", "Failed to process audio data")
					return
				}

				// 发送响应
				socket.Emit("with-binary-response", "Audio data processed successfully")
			} else {
				facades.Log().Error("Insufficient arguments for with-binary event")
				socket.Emit("error", "Insufficient arguments for with-binary event")
			}
		})

		// 注册 message 事件
		socket.On("message", func(args ...interface{}) {
			if len(args) > 0 {
				message := args[0]
				facades.Log().Info("接收到消息: " + fmt.Sprintf("%v", message))
				// 发送响应
				socket.Emit("message", message)
			}
		})

		// 注册设置热词事件
		socket.On("set-hotwords", func(args ...interface{}) {
			facades.Log().Info("接收到 set-hotwords 事件: " + fmt.Sprintf("%v", args))

			if len(args) >= 1 {
				var hotwords string
				var score float32 = 2.0

				// 处理热词
				if h, ok := args[0].(string); ok {
					hotwords = h
				} else {
					facades.Log().Error("热词类型错误，期望 string 类型")
					socket.Emit("hotwords-error", "Hotwords must be a string")
					return
				}

				// 处理热词分数（可选）
				if len(args) >= 2 {
					if s, ok := args[1].(float64); ok {
						score = float32(s)
					} else if s, ok := args[1].(int); ok {
						score = float32(s)
					}
				}

				if err := c.audioService.SetHotwords(hotwords, score); err != nil {
					facades.Log().Error("Failed to set hotwords: " + err.Error())
					socket.Emit("hotwords-error", "Failed to set hotwords: "+err.Error())
					return
				}
				facades.Log().Info("热词已设置: " + hotwords + ", 分数: " + fmt.Sprintf("%.1f", score))

				// 发送响应
				socket.Emit("hotwords-set", map[string]interface{}{
					"hotwords": hotwords,
					"score":    score,
				})
			} else {
				facades.Log().Error("set-hotwords 事件缺少参数")
				socket.Emit("hotwords-error", "Missing hotwords argument")
			}
		})

		// 注册获取热词事件
		socket.On("get-hotwords", func(args ...interface{}) {
			facades.Log().Info("接收到 get-hotwords 事件")

			hotwords, score := c.audioService.GetHotwords()

			// 发送响应
			socket.Emit("hotwords-data", map[string]interface{}{
				"hotwords": hotwords,
				"score":    score,
			})
		})

		return nil
	})

	facades.Log().Info("Socket.IO 事件处理器注册完成")
}

// SendMessage 发送消息到指定客户端
func (c *SocketioController) SendMessage(socket *socketio_lib.Socket, event string, args ...interface{}) error {
	return socket.Emit(event, args...)
}

// BroadcastMessage 广播消息到所有客户端
func (c *SocketioController) BroadcastMessage(event string, args ...interface{}) {
	c.socketio.Emit(event, args...)
}

// BroadcastToNamespace 广播消息到指定命名空间
func (c *SocketioController) BroadcastToNamespace(namespace, event string, args ...interface{}) {
	c.socketio.EmitToNamespace(namespace, event, args...)
}

// BroadcastToRoom 广播消息到指定房间
func (c *SocketioController) BroadcastToRoom(namespace, room, event string, args ...interface{}) {
	c.socketio.EmitToRoom(namespace, room, event, args...)
}

// JoinRoom 将客户端加入房间
func (c *SocketioController) JoinRoom(socket *socketio_lib.Socket, room string) error {
	return c.socketio.JoinRoom(socket, room)
}

// LeaveRoom 将客户端从房间移除
func (c *SocketioController) LeaveRoom(socket *socketio_lib.Socket, room string) error {
	return c.socketio.LeaveRoom(socket, room)
}

// GetConnectionCount 获取连接数量
func (c *SocketioController) GetConnectionCount() int {
	return c.socketio.GetConnectionManager().GetConnectionCount()
}

// GetAllConnections 获取所有连接
func (c *SocketioController) GetAllConnections() []*socketio_lib.Socket {
	return c.socketio.GetConnectionManager().GetAllConnections()
}

// GetConnection 根据ID获取连接
func (c *SocketioController) GetConnection(socketID string) *socketio_lib.Socket {
	return c.socketio.GetConnectionManager().GetConnection(socketID)
}
