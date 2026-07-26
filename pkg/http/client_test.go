package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zxq97/agent/pkg/log"
)

func TestClientPostJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("authorization = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != `{"keyword":"南京路"}` {
			t.Fatalf("body = %s", body)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()

	var logs bytes.Buffer
	log.Init(log.NewJSONLogger(&logs))
	t.Cleanup(func() { log.Init(nil) })
	ctx := log.WithTraceID(context.Background(), "trace-http-1")
	body, err := NewClient(&Config{TimeoutSec: 1}).PostJSON(ctx, "maps search", server.URL, "token-1", []byte(`{"keyword":"南京路"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"code":0}` {
		t.Fatalf("body = %s", body)
	}
	var entry log.Entry
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.TraceID != "trace-http-1" || entry.Operation != "maps search" || entry.DurationMS < 0 || entry.Request == nil || entry.Response == nil {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestClientPostJSONRejectsNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	body, err := NewClient(&Config{TimeoutSec: 1}).PostJSON(context.Background(), "test non_ok", server.URL, "", nil)
	if err == nil {
		t.Fatal("PostJSON should reject a non-200 response")
	}
	if body != nil {
		t.Fatalf("body = %q, want nil", body)
	}
}

func TestClientPostJSONOmitsAuthorizationWithoutBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("authorization = %q, want empty", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if _, err := NewClient(&Config{TimeoutSec: 1}).PostJSON(context.Background(), "maps search", server.URL, "", nil); err != nil {
		t.Fatal(err)
	}
}
