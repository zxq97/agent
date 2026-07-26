package webchat

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/orchestrator"
	"github.com/zxq97/agent/internal/router"
)

var (
	ErrSessionNotFound  = errors.New("web chat: session not found")
	ErrStaleClientSeq   = errors.New("web chat: stale client sequence")
	ErrRequestIDMissing = errors.New("web chat: request_id is required")
	ErrClientSeqInvalid = errors.New("web chat: client_seq must be positive")
)

type Service struct {
	orchestrator *orchestrator.Orchestrator
	router       router.Router
	store        *MemoryStore
	now          func() time.Time
}

func NewService(turnOrchestrator *orchestrator.Orchestrator, turnRouter router.Router, store *MemoryStore, now func() time.Time) (*Service, error) {
	if turnOrchestrator == nil || turnRouter == nil || store == nil {
		return nil, errors.New("web chat: orchestrator, router and store are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{orchestrator: turnOrchestrator, router: turnRouter, store: store, now: now}, nil
}

func (s *Service) CreateSession(userID string) (*SessionDetail, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("web chat: user_id is required")
	}
	record := s.store.Create(userID)
	return &SessionDetail{SessionID: record.sessionID, ClientSeq: record.latestSeq, History: []Message{}, State: stateView(record.state)}, nil
}

func (s *Service) ListSessions(userID string) []SessionSummary {
	return s.store.List(strings.TrimSpace(userID))
}

func (s *Service) GetSession(userID, sessionID string) (*SessionDetail, error) {
	record := s.store.Get(strings.TrimSpace(userID), strings.TrimSpace(sessionID))
	if record == nil {
		return nil, ErrSessionNotFound
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	history := append([]Message(nil), record.history...)
	return &SessionDetail{SessionID: record.sessionID, ClientSeq: record.latestSeq, History: history, State: stateView(record.state)}, nil
}

func (s *Service) DeleteSession(userID, sessionID string) bool {
	return s.store.Delete(strings.TrimSpace(userID), strings.TrimSpace(sessionID))
}

func (s *Service) Chat(ctx context.Context, userID, sessionID, requestID string, clientSeq int64, text string) (*TurnResponse, bool, error) {
	record := s.store.Get(strings.TrimSpace(userID), strings.TrimSpace(sessionID))
	if record == nil {
		return nil, false, ErrSessionNotFound
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false, errors.New("web chat: message is required")
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, false, ErrRequestIDMissing
	}
	if clientSeq <= 0 {
		return nil, false, ErrClientSeqInvalid
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if cached, ok := record.completed[requestID]; ok {
		copy := cached
		return &copy, true, nil
	}
	if clientSeq <= record.latestSeq {
		return nil, false, ErrStaleClientSeq
	}
	now := s.now()
	routes, err := s.router.Route(ctx, buildRouterInput(record.state, record.history, text))
	if err != nil {
		return nil, false, err
	}
	turnRequest := buildTurnRequest(text, record.history, routes)
	turn, err := s.orchestrator.Execute(ctx, record.state, turnRequest)
	if err != nil {
		return nil, false, err
	}
	// The per-session lock serializes this in-memory store. Increment the
	// domain version once after a successful turn so Pending/BaseVersion and a
	// future CAS-backed store share the same optimistic-concurrency contract.
	record.state.Version++
	response := formatTurn(record.state, turn)
	record.appendMessage(Message{Role: "user", Content: text, CreatedAt: now})
	record.appendMessage(Message{Role: "assistant", Content: response.Message, CreatedAt: s.now()})
	if clientSeq > record.latestSeq {
		record.latestSeq = clientSeq
	}
	record.cache(requestID, response)
	return &response, false, nil
}
