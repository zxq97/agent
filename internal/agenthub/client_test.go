package agenthub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRetrieveReturnsEmptyWhenKeyMissing(t *testing.T) {
	c := New(Config{Host: "http://example.test", Timeout: 1})

	content, err := c.Retrieve(context.Background(), "异地还车费")

	if err != nil {
		t.Fatalf("Retrieve error = %v, want nil", err)
	}
	if content != "" {
		t.Fatalf("content = %q, want empty", content)
	}
}

func TestRetrievePostsWorkflowAndExtractsContent(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"succeeded","outputs":{"content":"异地还车费以下单页展示为准。"}}}`))
	}))
	defer srv.Close()
	c := New(Config{Host: srv.URL, RetrievalAPIKey: "secret", Timeout: 1})

	content, err := c.Retrieve(context.Background(), "异地还车费")

	if err != nil {
		t.Fatalf("Retrieve error = %v, want nil", err)
	}
	if content != "异地还车费以下单页展示为准。" {
		t.Fatalf("content = %q", content)
	}
	if gotPath != "/v1/workflows/run" {
		t.Fatalf("path = %q, want /v1/workflows/run", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotBody["response_mode"] != "blocking" || gotBody["user"] != "rental_agent" {
		t.Fatalf("body = %#v", gotBody)
	}
	inputs, ok := gotBody["inputs"].(map[string]any)
	if !ok || inputs["input"] != "异地还车费" {
		t.Fatalf("inputs = %#v", gotBody["inputs"])
	}
}

func TestRetrieveFailsWhenWorkflowStatusNotSucceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"failed","outputs":{"content":"bad"}}}`))
	}))
	defer srv.Close()
	c := New(Config{Host: srv.URL, RetrievalAPIKey: "secret", Timeout: 1})

	content, err := c.Retrieve(context.Background(), "异地还车费")

	if err == nil {
		t.Fatal("Retrieve error is nil, want status error")
	}
	if content != "" {
		t.Fatalf("content = %q, want empty on error", content)
	}
}
