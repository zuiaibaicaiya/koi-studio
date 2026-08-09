package socketio

import (
	"sync"
	"time"

	socketio "github.com/zishang520/socket.io/servers/socket/v3"
)

type ConnectionInfo struct {
	Socket    *socketio.Socket
	Namespace string
	Rooms     []string
	JoinTime  time.Time
}

type ConnectionManager struct {
	connections map[string]*ConnectionInfo
	namespaces  map[string]map[string]*socketio.Socket
	rooms       map[string]map[string]bool
	mutex       sync.RWMutex
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*ConnectionInfo),
		namespaces:  make(map[string]map[string]*socketio.Socket),
		rooms:      make(map[string]map[string]bool),
	}
}

func (cm *ConnectionManager) RegisterConnection(socket *socketio.Socket, namespace string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	socketID := string(socket.Id())
	cm.connections[socketID] = &ConnectionInfo{
		Socket:    socket,
		Namespace: namespace,
		Rooms:     []string{},
		JoinTime:  time.Now(),
	}

	if _, ok := cm.namespaces[namespace]; !ok {
		cm.namespaces[namespace] = make(map[string]*socketio.Socket)
	}
	cm.namespaces[namespace][socketID] = socket
}

func (cm *ConnectionManager) RemoveConnection(socketID string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	if info, ok := cm.connections[socketID]; ok {
		if nsp, ok := cm.namespaces[info.Namespace]; ok {
			delete(nsp, socketID)
		}
		for _, room := range info.Rooms {
			if sockets, ok := cm.rooms[room]; ok {
				delete(sockets, socketID)
			}
		}
		delete(cm.connections, socketID)
	}
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

func (cm *ConnectionManager) GetClientsInRoom(namespace, room string) []*socketio.Socket {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	roomKey := namespace + ":" + room
	sockets, ok := cm.rooms[roomKey]
	if !ok {
		return []*socketio.Socket{}
	}

	clients := make([]*socketio.Socket, 0, len(sockets))
	for socketID := range sockets {
		if info, ok := cm.connections[socketID]; ok {
			clients = append(clients, info.Socket)
		}
	}
	return clients
}

func (cm *ConnectionManager) GetClientsInNamespace(namespace string) []*socketio.Socket {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	sockets, ok := cm.namespaces[namespace]
	if !ok {
		return []*socketio.Socket{}
	}

	clients := make([]*socketio.Socket, 0, len(sockets))
	for _, socket := range sockets {
		clients = append(clients, socket)
	}
	return clients
}

func (cm *ConnectionManager) BroadcastToAll(event string, args ...interface{}) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	for _, info := range cm.connections {
		info.Socket.Emit(event, args...)
	}
}

func (cm *ConnectionManager) BroadcastToNamespace(namespace string, event string, args ...interface{}) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if sockets, ok := cm.namespaces[namespace]; ok {
		for _, socket := range sockets {
			socket.Emit(event, args...)
		}
	}
}

func (cm *ConnectionManager) ClearConnections() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	cm.connections = make(map[string]*ConnectionInfo)
	cm.namespaces = make(map[string]map[string]*socketio.Socket)
	cm.rooms = make(map[string]map[string]bool)
}

func (cm *ConnectionManager) AddToRoom(socketID, namespace, room string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	roomKey := namespace + ":" + room
	if _, ok := cm.rooms[roomKey]; !ok {
		cm.rooms[roomKey] = make(map[string]bool)
	}
	cm.rooms[roomKey][socketID] = true

	if info, ok := cm.connections[socketID]; ok {
		info.Rooms = append(info.Rooms, room)
	}
}

func (cm *ConnectionManager) RemoveFromRoom(socketID, namespace, room string) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	roomKey := namespace + ":" + room
	if sockets, ok := cm.rooms[roomKey]; ok {
		delete(sockets, socketID)
	}

	if info, ok := cm.connections[socketID]; ok {
		newRooms := make([]string, 0, len(info.Rooms))
		for _, r := range info.Rooms {
			if r != room {
				newRooms = append(newRooms, r)
			}
		}
		info.Rooms = newRooms
	}
}
