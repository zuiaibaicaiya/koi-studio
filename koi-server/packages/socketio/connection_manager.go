package socketio

import (
	"sync"
	"time"

	socketio "github.com/zishang520/socket.io/servers/socket/v3"
)

// ConnectionInfo 单个客户端连接的登记信息。
type ConnectionInfo struct {
	Socket    *socketio.Socket
	Namespace string
	JoinTime  time.Time
}

// ConnectionManager 连接登记表。
//
// 只维护「当前在线连接」这一份索引；房间成员、命名空间成员等真实拓扑
// 以 socket.io 服务器内部状态为权威，这里不再重复镜像（此前镜像从未
// 被填充，GetClientsInRoom 永远返回空，属误导性死代码）。
type ConnectionManager struct {
	connections map[string]*ConnectionInfo
	mutex       sync.RWMutex
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*ConnectionInfo),
	}
}

func (cm *ConnectionManager) RegisterConnection(socket *socketio.Socket, namespace string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	cm.connections[string(socket.Id())] = &ConnectionInfo{
		Socket:    socket,
		Namespace: namespace,
		JoinTime:  time.Now(),
	}
}

func (cm *ConnectionManager) RemoveConnection(socketID string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	delete(cm.connections, socketID)
}

func (cm *ConnectionManager) GetConnection(socketID string) *socketio.Socket {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if info, ok := cm.connections[socketID]; ok {
		return info.Socket
	}
	return nil
}

func (cm *ConnectionManager) GetAllConnections() []*socketio.Socket {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	connections := make([]*socketio.Socket, 0, len(cm.connections))
	for _, info := range cm.connections {
		connections = append(connections, info.Socket)
	}
	return connections
}

func (cm *ConnectionManager) GetConnectionCount() int {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	return len(cm.connections)
}

func (cm *ConnectionManager) ClearConnections() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	cm.connections = make(map[string]*ConnectionInfo)
}
