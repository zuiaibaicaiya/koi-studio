package offlinetranscribe

import (
	"errors"
	"sync"
	"time"
)

// 任务状态
const (
	StatusPending   = "pending"   // 等待中
	StatusRunning   = "running"   // 转写中
	StatusCompleted = "completed" // 已完成
	StatusFailed    = "failed"    // 失败
)

// Progress 转写进度信息
type Progress struct {
	MeetingID    uint      `json:"meeting_id"`
	Status       string    `json:"status"`
	Progress     int       `json:"progress"`      // 0-100
	CurrentStep  string    `json:"current_step"`  // 当前步骤描述
	TotalSeconds float64   `json:"total_seconds"` // 音频总时长（秒）
	ErrorMessage string    `json:"error_message,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
}

// ProgressManager 管理转写任务的进度信息，线程安全。
type ProgressManager struct {
	mu    sync.RWMutex
	items map[uint]*Progress
}

func NewProgressManager() *ProgressManager {
	return &ProgressManager{
		items: make(map[uint]*Progress),
	}
}

// Init 初始化一个会议的转写进度
func (m *ProgressManager) Init(meetingID uint, totalSeconds float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[meetingID] = &Progress{
		MeetingID:    meetingID,
		Status:       StatusPending,
		Progress:     0,
		CurrentStep:  "等待开始",
		TotalSeconds: totalSeconds,
	}
}

// Start 标记任务开始运行
func (m *ProgressManager) Start(meetingID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.items[meetingID]; ok {
		p.Status = StatusRunning
		p.StartedAt = time.Now()
		p.CurrentStep = "正在加载模型"
		p.Progress = 5
	}
}

// Update 更新进度百分比和当前步骤
func (m *ProgressManager) Update(meetingID uint, progress int, step string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.items[meetingID]; ok {
		if progress > p.Progress {
			p.Progress = progress
		}
		if step != "" {
			p.CurrentStep = step
		}
	}
}

// Complete 标记任务完成
func (m *ProgressManager) Complete(meetingID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.items[meetingID]; ok {
		p.Status = StatusCompleted
		p.Progress = 100
		p.CurrentStep = "转写完成"
		p.FinishedAt = time.Now()
	}
}

// Fail 标记任务失败
func (m *ProgressManager) Fail(meetingID uint, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.items[meetingID]; ok {
		p.Status = StatusFailed
		if err != nil {
			p.ErrorMessage = err.Error()
		} else {
			p.ErrorMessage = "未知错误"
		}
		p.CurrentStep = "转写失败"
		p.FinishedAt = time.Now()
	}
}

// Get 获取指定会议的进度信息
func (m *ProgressManager) Get(meetingID uint) (*Progress, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.items[meetingID]
	if !ok {
		return nil, errors.New("任务不存在")
	}
	// 返回副本避免外部修改
	cp := *p
	return &cp, nil
}

// Remove 清理已完成或失败的进度记录
func (m *ProgressManager) Remove(meetingID uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, meetingID)
}
