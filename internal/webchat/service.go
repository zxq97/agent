package webchat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/orchestrator"
	"github.com/zxq97/agent/internal/router"
	"github.com/zxq97/agent/internal/session"
)

var (
	ErrSessionNotFound         = errors.New("web chat: session not found")
	ErrStaleClientSeq          = errors.New("web chat: stale client sequence")
	ErrRequestIDMissing        = errors.New("web chat: request_id is required")
	ErrRequestIdentityConflict = errors.New("web chat: request_id was already used for different content")
	ErrClientSeqInvalid        = errors.New("web chat: client_seq must be positive")
)

type Service struct {
	orchestrator *orchestrator.Orchestrator
	router       router.Router
	store        Store
	now          func() time.Time
}

func NewService(turnOrchestrator *orchestrator.Orchestrator, turnRouter router.Router, store Store, now func() time.Time) (*Service, error) {
	if turnOrchestrator == nil || turnRouter == nil || store == nil {
		return nil, errors.New("web chat: orchestrator, router and store are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{orchestrator: turnOrchestrator, router: turnRouter, store: store, now: now}, nil
}

func (s *Service) CreateSession(ctx context.Context, userID string) (*SessionDetail, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("web chat: user_id is required")
	}
	value, err := s.store.Create(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &SessionDetail{
		SessionID: value.SessionID,
		ClientSeq: value.LatestSeq,
		History:   []Message{},
		State:     stateView(value.State),
	}, nil
}

func (s *Service) ListSessions(ctx context.Context, userID string) ([]SessionSummary, error) {
	return s.store.List(ctx, strings.TrimSpace(userID))
}

func (s *Service) GetSession(ctx context.Context, userID, sessionID string) (*SessionDetail, error) {
	value, err := s.store.Load(ctx, strings.TrimSpace(userID), strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	return &SessionDetail{
		SessionID: value.SessionID,
		ClientSeq: value.LatestSeq,
		History:   append([]Message(nil), value.History...),
		State:     stateView(value.State),
	}, nil
}

func (s *Service) DeleteSession(ctx context.Context, userID, sessionID string) error {
	return s.store.Delete(ctx, strings.TrimSpace(userID), strings.TrimSpace(sessionID))
}

func (s *Service) Chat(ctx context.Context, userID, sessionID, requestID string, clientSeq int64, text string) (*TurnResponse, bool, error) {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
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
	unlock, err := s.store.Lock(ctx, userID, sessionID)
	if err != nil {
		return nil, false, err
	}
	defer unlock()

	requestHash := turnRequestHash(userID, sessionID, requestID, clientSeq, text)
	receivedAt := s.now()
	for attempt := 0; attempt < 2; attempt++ {
		response, replayed, err := s.chatAttempt(ctx, userID, sessionID, requestID, requestHash, clientSeq, text, receivedAt)
		if !errors.Is(err, ErrVersionConflict) || attempt == 1 {
			return response, replayed, err
		}
	}
	return nil, false, ErrVersionConflict
}

func (s *Service) chatAttempt(
	ctx context.Context,
	userID, sessionID, requestID, requestHash string,
	clientSeq int64,
	text string,
	receivedAt time.Time,
) (*TurnResponse, bool, error) {
	value, err := s.store.Load(ctx, userID, sessionID)
	if err != nil {
		return nil, false, err
	}
	if completed, ok := value.Completed[requestID]; ok {
		if completed.RequestHash != requestHash {
			return nil, false, ErrRequestIdentityConflict
		}
		response := cloneTurnResponse(completed.Response)
		return &response, true, nil
	}
	if clientSeq <= value.LatestSeq {
		return nil, false, ErrStaleClientSeq
	}

	routes, err := s.router.Route(ctx, buildRouterInput(value.State, value.History, text))
	if err != nil {
		return nil, false, err
	}
	turnRequest := buildTurnRequest(text, value.History, routes)
	turnRequest.Context = orchestrator.TurnContext{
		RequestID: requestID, ClientSeq: clientSeq, UserID: userID,
		SessionID: sessionID, SourceText: text, ReceivedAt: receivedAt,
		BaseVersion: value.State.Version,
	}
	turnRequest.Plan.BindBaseVersion(value.State.Version)
	draft := session.Clone(value.State)
	turn, err := s.orchestrator.Execute(ctx, draft, turnRequest)
	if err != nil {
		return nil, false, err
	}

	baseVersion := value.State.Version
	draft.Version = baseVersion + 1
	response := formatTurn(draft, turn)
	value.State = draft
	appendMessage(value, Message{Role: "user", Content: text, CreatedAt: receivedAt})
	appendMessage(value, Message{Role: "assistant", Content: response.Message, CreatedAt: receivedAt})
	value.LatestSeq = clientSeq
	cacheCompleted(value, CompletedRequest{
		RequestID:     requestID,
		RequestHash:   requestHash,
		ClientSeq:     clientSeq,
		BaseVersion:   baseVersion,
		ResultVersion: draft.Version,
		Response:      response,
		CompletedAt:   receivedAt,
	})
	if err := s.store.Save(ctx, value, baseVersion); err != nil {
		return nil, false, err
	}
	result := cloneTurnResponse(response)
	return &result, false, nil
}

func turnRequestHash(userID, sessionID, requestID string, clientSeq int64, text string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		userID,
		sessionID,
		requestID,
		strconv.FormatInt(clientSeq, 10),
		text,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}
