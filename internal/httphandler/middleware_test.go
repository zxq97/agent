package httphandler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zxq97/rental-agent/internal/trace"
)

func TestTraceMiddlewareInjectsTraceIDAndHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := trace.FromCtx(r.Context()); got == "-" || got == "" {
			t.Fatalf("trace id not injected, got %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	traceMiddleware()(next).ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Trace-Id"); got == "" || got == "-" {
		t.Fatalf("X-Trace-Id header missing, got %q", got)
	}
}

func TestHandleChatRequiresSessionID(t *testing.T) {
	h := New(nil, nil, nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"user_id":"u1","message":"你好"}`))

	h.handleChat(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "session_id") {
		t.Fatalf("body should mention session_id, got %s", rr.Body.String())
	}
}
