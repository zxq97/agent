package httphandler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/orchestrator"
	"github.com/zxq97/agent/internal/router"
	"github.com/zxq97/agent/internal/searchpolicy"
	"github.com/zxq97/agent/internal/session"
	"github.com/zxq97/agent/internal/webchat"
	"github.com/zxq97/agent/pkg/log"
)

type noMatchRental struct{}

func (noMatchRental) Handle(context.Context, *session.AgentSession, *rentalcontext.ModifyRentalContextInput) (*rentalcontext.ModifyRentalContextResult, error) {
	return nil, rentalcontext.ErrDomainMismatch
}

type generalOnlyRouter struct{}

func (generalOnlyRouter) Route(_ context.Context, input *router.Input) (*router.RouteResult, error) {
	return &router.RouteResult{Candidates: []router.RouteCandidate{{Action: router.ActionGeneralReply, EvidenceText: input.SourceText, Confidence: 1}}}, nil
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	service, err := webchat.NewService(
		orchestrator.New(noMatchRental{}, nil, nil, searchpolicy.New(1, func() time.Time { return now }), func() time.Time { return now }),
		generalOnlyRouter{},
		webchat.NewMemoryStore(func() time.Time { return now }),
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(service, log.NewJSONLogger(&bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	return handler.Mux(filepath.Join("..", "..", "web"))
}

func TestSessionAndChatEndpoints(t *testing.T) {
	handler := newTestHandler(t)
	create := httptest.NewRecorder()
	handler.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(`{"user_id":"user"}`)))
	if create.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var detail webchat.SessionDetail
	if err := json.Unmarshal(create.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.SessionID == "" || create.Header().Get("X-Trace-Id") == "" {
		t.Fatalf("detail=%#v trace=%q", detail, create.Header().Get("X-Trace-Id"))
	}

	body, _ := json.Marshal(map[string]any{"user_id": "user", "session_id": detail.SessionID, "request_id": "request-1", "client_seq": 1, "message": "你好"})
	chat := httptest.NewRecorder()
	handler.ServeHTTP(chat, httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body)))
	if chat.Code != http.StatusOK || !strings.Contains(chat.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("chat status=%d content_type=%q body=%s", chat.Code, chat.Header().Get("Content-Type"), chat.Body.String())
	}
	for _, event := range []string{"event: accepted", "event: progress", "event: result", "event: done"} {
		if !strings.Contains(chat.Body.String(), event) {
			t.Fatalf("missing %q in %s", event, chat.Body.String())
		}
	}

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/sessions/"+detail.SessionID+"?user_id=user", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"client_seq":1`) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
}

func TestHealthAndStaticPage(t *testing.T) {
	handler := newTestHandler(t)
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"status":"ok"`) {
		t.Fatalf("health status=%d body=%s", health.Code, health.Body.String())
	}
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "小租") {
		t.Fatalf("page status=%d body=%s", page.Code, page.Body.String())
	}
}
