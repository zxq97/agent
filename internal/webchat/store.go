package webchat

import (
	"crypto/rand"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/zxq97/agent/internal/session"
)

const (
	maxHistoryMessages = 100
	maxCompletedTurns  = 50
)

type sessionRecord struct {
	mu sync.Mutex

	userID    string
	sessionID string
	state     *session.AgentSession
	history   []Message
	updatedAt time.Time
	latestSeq int64
	completed map[string]TurnResponse
	order     []string
}

type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]map[string]*sessionRecord
	now      func() time.Time
}

func NewMemoryStore(now func() time.Time) *MemoryStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryStore{sessions: make(map[string]map[string]*sessionRecord), now: now}
}

func (s *MemoryStore) Create(userID string) *sessionRecord {
	now := s.now()
	record := &sessionRecord{userID: userID, sessionID: newID(), state: &session.AgentSession{}, updatedAt: now, completed: make(map[string]TurnResponse)}
	record.state.SessionID = record.sessionID
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[userID] == nil {
		s.sessions[userID] = make(map[string]*sessionRecord)
	}
	s.sessions[userID][record.sessionID] = record
	return record
}

func (s *MemoryStore) Get(userID, sessionID string) *sessionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[userID][sessionID]
}

func (s *MemoryStore) Delete(userID, sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := s.sessions[userID]
	if values == nil || values[sessionID] == nil {
		return false
	}
	delete(values, sessionID)
	if len(values) == 0 {
		delete(s.sessions, userID)
	}
	return true
}

func (s *MemoryStore) List(userID string) []SessionSummary {
	s.mu.RLock()
	records := make([]*sessionRecord, 0, len(s.sessions[userID]))
	for _, record := range s.sessions[userID] {
		records = append(records, record)
	}
	s.mu.RUnlock()

	result := make([]SessionSummary, 0, len(records))
	for _, record := range records {
		record.mu.Lock()
		preview := "新对话"
		if len(record.history) > 0 {
			preview = record.history[len(record.history)-1].Content
		}
		result = append(result, SessionSummary{SessionID: record.sessionID, Preview: preview, UpdatedAt: record.updatedAt})
		record.mu.Unlock()
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result
}

func (r *sessionRecord) appendMessage(message Message) {
	r.history = append(r.history, message)
	if len(r.history) > maxHistoryMessages {
		r.history = append([]Message(nil), r.history[len(r.history)-maxHistoryMessages:]...)
	}
	r.updatedAt = message.CreatedAt
}

func (r *sessionRecord) cache(requestID string, response TurnResponse) {
	if requestID == "" {
		return
	}
	if _, exists := r.completed[requestID]; !exists {
		r.order = append(r.order, requestID)
	}
	r.completed[requestID] = response
	for len(r.order) > maxCompletedTurns {
		delete(r.completed, r.order[0])
		r.order = r.order[1:]
	}
}

func newID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:])
}
