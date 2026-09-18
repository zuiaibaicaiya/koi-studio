package services

import (
	"sync"
	"time"
)

// MeetingContext 会议上下文，保存客户端会话对应的会议信息。
//
// 说话人列表在实时转写过程中可能被动态追加（用户在转写页框选文字注册新说话人），
// 而音频工作协程会在每次断句时读取该列表，因此运行时的读写统一走
// SpeakerIDList / AddSpeakerID（内部加锁），不要在运行期间直接访问字段。
type MeetingContext struct {
	// mu 保护运行期间可变更的字段。
	mu sync.RWMutex
	// MeetingID 关联的会议ID
	MeetingID uint
	// SpeakerIDs 会议选择的说话人ID列表。
	// 仅用于构造上下文（Bind 之前赋值）；运行时请使用 SpeakerIDList / AddSpeakerID。
	SpeakerIDs []uint
	// HotWordLibraryIDs 会议选择的热词库ID列表
	HotWordLibraryIDs []uint
	// HotwordsStr 已加载的热词格式化字符串
	HotwordsStr string
	// AudioStartTime 音频开始时间（用于计算绝对时间）
	AudioStartTime time.Time
}

// SpeakerIDList 返回会议当前的说话人ID列表副本。
func (c *MeetingContext) SpeakerIDList() []uint {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.SpeakerIDs) == 0 {
		return nil
	}
	ids := make([]uint, len(c.SpeakerIDs))
	copy(ids, c.SpeakerIDs)

	return ids
}

// AddSpeakerID 追加一个说话人ID，已存在时不做变更。
// 返回 true 表示本次确实新增了说话人。
func (c *MeetingContext) AddSpeakerID(id uint) bool {
	if id == 0 {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, existing := range c.SpeakerIDs {
		if existing == id {
			return false
		}
	}
	c.SpeakerIDs = append(c.SpeakerIDs, id)

	return true
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

// AddSpeakerID 把说话人加入某会议下全部活跃客户端上下文，返回被更新的上下文数量。
//
// 用于会议进行中动态注册新说话人：无需重连即可让后续转写参与声纹比对。
func (m *MeetingSessionManager) AddSpeakerID(meetingID uint, speakerID uint) int {
	if meetingID == 0 || speakerID == 0 {
		return 0
	}

	m.mu.RLock()
	contexts := make([]*MeetingContext, 0, len(m.sessions))
	for _, ctx := range m.sessions {
		if ctx.MeetingID == meetingID {
			contexts = append(contexts, ctx)
		}
	}
	m.mu.RUnlock()

	updated := 0
	for _, ctx := range contexts {
		if ctx.AddSpeakerID(speakerID) {
			updated++
		}
	}

	return updated
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
