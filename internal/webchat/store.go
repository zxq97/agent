package webchat

import (
	"context"
	"crypto/rand"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/session"
)

const (
	maxHistoryMessages = 100
	maxCompletedTurns  = 50
)

var ErrVersionConflict = errors.New("web chat store: version conflict")

type CompletedRequest struct {
	RequestID     string
	RequestHash   string
	ClientSeq     int64
	BaseVersion   int64
	ResultVersion int64
	Response      TurnResponse
	CompletedAt   time.Time
}

type SessionEnvelope struct {
	UserID    string
	SessionID string
	State     *session.AgentSession
	History   []Message
	UpdatedAt time.Time
	LatestSeq int64
	Completed map[string]CompletedRequest
	Order     []string
}

// Store owns atomic persistence of Session state, chat history, sequence, and
// completed idempotent responses.
type Store interface {
	Create(context.Context, string) (*SessionEnvelope, error)
	Lock(context.Context, string, string) (func(), error)
	Load(context.Context, string, string) (*SessionEnvelope, error)
	Save(context.Context, *SessionEnvelope, int64) error
	Delete(context.Context, string, string) error
	List(context.Context, string) ([]SessionSummary, error)
}

type memoryRecord struct {
	turnMu sync.Mutex
	mu     sync.RWMutex
	value  *SessionEnvelope
}

type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]map[string]*memoryRecord
	now      func() time.Time
}

func NewMemoryStore(now func() time.Time) *MemoryStore {
	if now == nil {
		now = time.Now
	}
	return &MemoryStore{sessions: make(map[string]map[string]*memoryRecord), now: now}
}

func (s *MemoryStore) Create(ctx context.Context, userID string) (*SessionEnvelope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := s.now()
	sessionID := newID()
	value := &SessionEnvelope{
		UserID:    userID,
		SessionID: sessionID,
		State:     &session.AgentSession{SessionID: sessionID},
		UpdatedAt: now,
		Completed: make(map[string]CompletedRequest),
	}
	record := &memoryRecord{value: value}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[userID] == nil {
		s.sessions[userID] = make(map[string]*memoryRecord)
	}
	s.sessions[userID][sessionID] = record
	return cloneEnvelope(value), nil
}

func (s *MemoryStore) Lock(ctx context.Context, userID, sessionID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record := s.record(userID, sessionID)
	if record == nil {
		return nil, ErrSessionNotFound
	}
	record.turnMu.Lock()
	s.mu.RLock()
	current := s.sessions[userID][sessionID]
	s.mu.RUnlock()
	if current != record {
		record.turnMu.Unlock()
		return nil, ErrSessionNotFound
	}
	return record.turnMu.Unlock, nil
}

func (s *MemoryStore) Load(ctx context.Context, userID, sessionID string) (*SessionEnvelope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record := s.record(userID, sessionID)
	if record == nil {
		return nil, ErrSessionNotFound
	}
	record.mu.RLock()
	defer record.mu.RUnlock()
	return cloneEnvelope(record.value), nil
}

func (s *MemoryStore) Save(ctx context.Context, value *SessionEnvelope, expectedVersion int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if value == nil || value.State == nil {
		return errors.New("web chat store save: session envelope is required")
	}
	record := s.record(value.UserID, value.SessionID)
	if record == nil {
		return ErrSessionNotFound
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if record.value == nil || record.value.State == nil ||
		record.value.State.Version != expectedVersion ||
		value.State.Version != expectedVersion+1 {
		return ErrVersionConflict
	}
	record.value = cloneEnvelope(value)
	return nil
}

func (s *MemoryStore) Delete(ctx context.Context, userID, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	record := s.record(userID, sessionID)
	if record == nil {
		return ErrSessionNotFound
	}
	record.turnMu.Lock()
	defer record.turnMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	values := s.sessions[userID]
	if values == nil || values[sessionID] != record {
		return ErrSessionNotFound
	}
	delete(values, sessionID)
	if len(values) == 0 {
		delete(s.sessions, userID)
	}
	return nil
}

func (s *MemoryStore) List(ctx context.Context, userID string) ([]SessionSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	records := make([]*memoryRecord, 0, len(s.sessions[userID]))
	for _, record := range s.sessions[userID] {
		records = append(records, record)
	}
	s.mu.RUnlock()

	result := make([]SessionSummary, 0, len(records))
	for _, record := range records {
		record.mu.RLock()
		preview := "新对话"
		if len(record.value.History) > 0 {
			preview = record.value.History[len(record.value.History)-1].Content
		}
		result = append(result, SessionSummary{
			SessionID: record.value.SessionID,
			Preview:   preview,
			UpdatedAt: record.value.UpdatedAt,
		})
		record.mu.RUnlock()
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result, nil
}

func (s *MemoryStore) record(userID, sessionID string) *memoryRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[userID][sessionID]
}

func cloneEnvelope(value *SessionEnvelope) *SessionEnvelope {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.State = session.Clone(value.State)
	cloned.History = append([]Message(nil), value.History...)
	cloned.Order = append([]string(nil), value.Order...)
	cloned.Completed = make(map[string]CompletedRequest, len(value.Completed))
	for key, completed := range value.Completed {
		completed.Response = cloneTurnResponse(completed.Response)
		cloned.Completed[key] = completed
	}
	return &cloned
}

func appendMessage(value *SessionEnvelope, message Message) {
	value.History = append(value.History, message)
	if len(value.History) > maxHistoryMessages {
		value.History = append([]Message(nil), value.History[len(value.History)-maxHistoryMessages:]...)
	}
	value.UpdatedAt = message.CreatedAt
}

func cacheCompleted(value *SessionEnvelope, completed CompletedRequest) {
	if completed.RequestID == "" {
		return
	}
	if _, exists := value.Completed[completed.RequestID]; !exists {
		value.Order = append(value.Order, completed.RequestID)
	}
	completed.Response = cloneTurnResponse(completed.Response)
	value.Completed[completed.RequestID] = completed
	for len(value.Order) > maxCompletedTurns {
		delete(value.Completed, value.Order[0])
		value.Order = value.Order[1:]
	}
}

func cloneTurnResponse(value TurnResponse) TurnResponse {
	cloned := value
	cloned.Vehicles = append([]VehicleView(nil), value.Vehicles...)
	cloned.RequirementResolutions = make([]RequirementResolutionView, len(value.RequirementResolutions))
	for index := range value.RequirementResolutions {
		cloned.RequirementResolutions[index] = value.RequirementResolutions[index]
		cloned.RequirementResolutions[index].Executions = append([]string(nil), value.RequirementResolutions[index].Executions...)
	}
	cloned.State.Requirements = append([]RequirementView(nil), value.State.Requirements...)
	if value.State.Location != nil {
		location := *value.State.Location
		cloned.State.Location = &location
	}
	if value.State.PickupTime != nil {
		pickup := *value.State.PickupTime
		cloned.State.PickupTime = &pickup
	}
	if value.State.ReturnTime != nil {
		returnTime := *value.State.ReturnTime
		cloned.State.ReturnTime = &returnTime
	}
	cloned.Pending = clonePendingView(value.Pending)
	cloned.State.Pending = clonePendingView(value.State.Pending)
	return cloned
}

func clonePendingView(value *PendingView) *PendingView {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Options = append([]PendingOptionView(nil), value.Options...)
	return &cloned
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
