// Package session 提供会话持久化接口与内存实现。
// P4 正式上线后切 Redis 实现,本期内存兜底。
package session

import (
	"sync"
	"time"

	"github.com/zxq97/rental-agent/internal/orchestration"
)

// Summary 会话摘要,供列表展示。
type Summary struct {
	SessionID string    `json:"session_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Preview   string    `json:"preview"` // 首条用户消息截断
}

// Store 会话存储接口。
type Store interface {
	// Get 按 userID+sessionID 取会话,不存在或过期返回 nil。
	Get(userID, sessionID string) *orchestration.ConversationState
	// Put 写入/更新会话。
	Put(userID, sessionID string, state *orchestration.ConversationState)
	// Delete 删除会话。
	Delete(userID, sessionID string)
	// List 返回用户所有会话摘要,按 UpdatedAt 降序。
	List(userID string) []Summary
}

// MemoryStore 内存实现,开发调试用。
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*orchestration.ConversationState // key: "userID:sessionID"
	userIdx  map[string][]string                         // userID -> []sessionID
	ttl      time.Duration
}

// NewMemoryStore 构造内存 store。
func NewMemoryStore(ttlHours int) *MemoryStore {
	if ttlHours <= 0 {
		ttlHours = 24
	}
	return &MemoryStore{
		sessions: make(map[string]*orchestration.ConversationState),
		userIdx:  make(map[string][]string),
		ttl:      time.Duration(ttlHours) * time.Hour,
	}
}

func storeKey(userID, sessionID string) string {
	return userID + ":" + sessionID
}

func (m *MemoryStore) Get(userID, sessionID string) *orchestration.ConversationState {
	m.mu.RLock()
	s, ok := m.sessions[storeKey(userID, sessionID)]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	// 惰性 TTL 检查
	if time.Since(s.UpdatedAt) > m.ttl {
		m.Delete(userID, sessionID)
		return nil
	}
	return s
}

func (m *MemoryStore) Put(userID, sessionID string, state *orchestration.ConversationState) {
	key := storeKey(userID, sessionID)
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.sessions[key]
	m.sessions[key] = state
	if !exists {
		m.userIdx[userID] = append(m.userIdx[userID], sessionID)
	}
}

func (m *MemoryStore) Delete(userID, sessionID string) {
	key := storeKey(userID, sessionID)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, key)
	ids := m.userIdx[userID]
	for i, id := range ids {
		if id == sessionID {
			m.userIdx[userID] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	if len(m.userIdx[userID]) == 0 {
		delete(m.userIdx, userID)
	}
}

func (m *MemoryStore) List(userID string) []Summary {
	m.mu.RLock()
	ids := make([]string, len(m.userIdx[userID]))
	copy(ids, m.userIdx[userID])
	m.mu.RUnlock()

	var result []Summary
	for _, sid := range ids {
		s := m.Get(userID, sid)
		if s == nil {
			continue // 已过期
		}
		preview := extractPreview(s)
		result = append(result, Summary{
			SessionID: sid,
			CreatedAt: s.CreatedAt,
			UpdatedAt: s.UpdatedAt,
			Preview:   preview,
		})
	}
	// 按 UpdatedAt 降序
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].UpdatedAt.After(result[i].UpdatedAt) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	return result
}

// extractPreview 取首条用户消息前 50 字作为预览。
func extractPreview(s *orchestration.ConversationState) string {
	history := s.SnapshotHistory()
	for _, h := range history {
		if h.Msg != nil && h.Msg.Role == "user" && h.Msg.Content != "" {
			r := []rune(h.Msg.Content)
			if len(r) > 50 {
				return string(r[:50]) + "..."
			}
			return h.Msg.Content
		}
	}
	return "新对话"
}
