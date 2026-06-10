package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

// Manager 内存会话管理器
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	maxTurns int // 最大保留轮次（1 轮 = 1 user + 1 assistant）
}

// NewManager 创建内存会话管理器
func NewManager(maxTurns int) *Manager {
	if maxTurns <= 0 {
		maxTurns = 10
	}
	return &Manager{
		sessions: make(map[string]*Session),
		maxTurns: maxTurns,
	}
}

// Create 创建新会话
func (m *Manager) Create() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := &Session{
		ID:        generateID(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Messages:  make([]*schema.Message, 0),
	}
	m.sessions[s.ID] = s
	return s
}

// Get 获取会话
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// AppendMessage 追加消息到会话
func (m *Manager) AppendMessage(id string, msg *schema.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("会话 %s 不存在", id)
	}

	s.Messages = append(s.Messages, msg)
	s.UpdatedAt = time.Now()

	// 自动裁剪历史
	m.trimHistory(s)

	return nil
}

// GetMessages 获取会话消息（返回副本避免并发问题）
func (m *Manager) GetMessages(id string) ([]*schema.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("会话 %s 不存在", id)
	}

	result := make([]*schema.Message, len(s.Messages))
	copy(result, s.Messages)
	return result, nil
}

// trimHistory 裁剪超出 maxTurns 的历史
// 保留策略：始终保留第一条 system message，然后保留最近的 maxTurns 轮
func (m *Manager) trimHistory(s *Session) {
	if len(s.Messages) <= m.maxTurns*2+1 {
		return
	}

	// 保留第一条（system）和最近 maxTurns 轮
	var kept []*schema.Message
	if len(s.Messages) > 0 && s.Messages[0].Role == schema.System {
		kept = append(kept, s.Messages[0])
	}

	start := len(s.Messages) - m.maxTurns*2
	if start < 0 {
		start = 0
	}
	// 跳过 system message
	if start == 0 && len(s.Messages) > 0 && s.Messages[0].Role == schema.System {
		start = 1
	}

	kept = append(kept, s.Messages[start:]...)
	s.Messages = kept
}

// generateID 生成简单的会话 ID
func generateID() string {
	return fmt.Sprintf("sess_%d", time.Now().UnixNano())
}
