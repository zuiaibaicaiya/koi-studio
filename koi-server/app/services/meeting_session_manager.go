package services

import (
	"sync"
	"time"
)

// MeetingContext 会议上下文，保存客户端会话对应的会议信息
type MeetingContext struct {
	// MeetingID 关联的会议ID
	MeetingID uint
	// SpeakerIDs 会议选择的说话人ID列表
	SpeakerIDs []uint
	// HotWordLibraryIDs 会议选择的热词库ID列表
	HotWordLibraryIDs []uint
	// HotwordsStr 已加载的热词格式化字符串
	HotwordsStr string
	// AudioStartTime 音频开始时间（用于计算绝对时间）
	AudioStartTime time.Time
}

// MeetingSessionManager 管理 Socket.IO 客户端连接与会话之间的映射
//
// 当客户端通过 join-meeting 事件绑定到某个会议后进行记录，
// 断连或离开会议时清理，从而让转写服务在产出结果时知晓所属的会议上下文。
type MeetingSessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*MeetingContext // clientID → context
}

// NewMeetingSessionManager 创建会话管理器实例
func NewMeetingSessionManager() *MeetingSessionManager {
	return &MeetingSessionManager{
		sessions: make(map[string]*MeetingContext),
	}
}

// Bind 将客户端绑定到指定会议上下文
func (m *MeetingSessionManager) Bind(clientID string, ctx *MeetingContext) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions[clientID] = ctx
}

// Unbind 解除客户端绑定
func (m *MeetingSessionManager) Unbind(clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, clientID)
}

// Context 获取客户端关联的会议上下文，不存在时返回 nil
func (m *MeetingSessionManager) Context(clientID string) *MeetingContext {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ctx, ok := m.sessions[clientID]
	if !ok {
		return nil
	}

	return ctx
}

// ClientsByMeetingID 查找某会议下的所有活跃客户端ID
func (m *MeetingSessionManager) ClientsByMeetingID(meetingID uint) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var clients []string
	for clientID, ctx := range m.sessions {
		if ctx.MeetingID == meetingID {
			clients = append(clients, clientID)
		}
	}

	return clients
}
