package generalreply

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/session"
)

const generalReplyTestConfigPath = "../../../conf/dev.yaml"

func TestHandlerWithRemoteService(t *testing.T) {
	if os.Getenv("RUN_REMOTE_INTEGRATION") != "1" {
		t.Skip("set RUN_REMOTE_INTEGRATION=1 to run real LLM integration tests")
	}
	cfg, err := config.Load(generalReplyTestConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(cfg.LLM.APIKey) == "" {
		t.Skip("LLM API key is required for remote general reply tests")
	}
	client, err := llm.NewHTTPClient(&llm.HTTPConfig{
		Endpoint: cfg.LLM.Endpoint, APIKey: cfg.LLM.APIKey, TimeoutSec: cfg.LLM.TimeoutSec,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(client)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := handler.Handle(ctx, &session.AgentSession{}, &Input{
		SourceText: "你好，你能帮我做什么？",
		RecentMessages: []Message{
			{Role: "user", Content: "你好"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || strings.TrimSpace(result.Message) == "" {
		t.Fatalf("result=%#v", result)
	}
}
