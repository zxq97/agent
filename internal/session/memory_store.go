package session

import (
	"context"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

// MemoryStore 是 in-memory 实现,带 TTL 过期。
// 用途:CLI 调试 / 单元测试 / 本地无 Redis 时的兜底。
// 不适合生产:重启即清空、跨 pod 不共享。
type MemoryStore struct {
	mu  sync.RWMutex
	ttl time.Duration
	bag map[string]*memoryEntry
}

type memoryEntry struct {
	history []*schema.Message
	expire  time.Time
}

// NewMemoryStore 创建一个内存 store。ttlHours <=0 表示永不过期。
func NewMemoryStore(ttlHours int) *MemoryStore {
	var ttl time.Duration
	if ttlHours > 0 {
		ttl = time.Duration(ttlHours) * time.Hour
	}
	return &MemoryStore{ttl: ttl, bag: map[string]*memoryEntry{}}
}

func (m *MemoryStore) key(userID, sessionID string) string {
	return userID + "|" + sessionID
}

func (m *MemoryStore) Get(_ context.Context, userID, sessionID string) ([]*schema.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.bag[m.key(userID, sessionID)]
	if !ok {
		return nil, nil
	}
	if !e.expire.IsZero() && time.Now().After(e.expire) {
		return nil, nil
	}
	// 返回拷贝,避免外部修改影响内部
	out := make([]*schema.Message, len(e.history))
	copy(out, e.history)
	return out, nil
}

func (m *MemoryStore) Save(_ context.Context, userID, sessionID string, history []*schema.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	saved := make([]*schema.Message, len(history))
	copy(saved, history)
	expire := time.Time{}
	if m.ttl > 0 {
		expire = time.Now().Add(m.ttl)
	}
	m.bag[m.key(userID, sessionID)] = &memoryEntry{history: saved, expire: expire}
	return nil
}

func (m *MemoryStore) Touch(_ context.Context, userID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.bag[m.key(userID, sessionID)]
	if !ok {
		return nil
	}
	if m.ttl > 0 {
		e.expire = time.Now().Add(m.ttl)
	}
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, userID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.bag, m.key(userID, sessionID))
	return nil
}
