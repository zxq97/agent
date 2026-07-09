package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zxq97/rental-agent/internal/agenthub"
	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
)

type fakeRuleRetriever struct {
	content string
	err     error
	query   string
}

func (f *fakeRuleRetriever) Retrieve(_ context.Context, query string) (string, error) {
	f.query = query
	return f.content, f.err
}

type streamModel struct {
	seenReq llm.ChatRequest
	deltas  []string
}

func (m *streamModel) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{}, nil
}

func (m *streamModel) ChatStream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	m.seenReq = req
	ch := make(chan llm.StreamChunk, len(m.deltas))
	for _, d := range m.deltas {
		ch <- llm.StreamChunk{Delta: d}
	}
	close(ch)
	return ch, nil
}

type ruleModelGetter struct {
	model llm.ChatModel
	key   string
}

func (g *ruleModelGetter) Get(bindingKey string) (llm.ChatModel, error) {
	g.key = bindingKey
	return g.model, nil
}

func TestRulesCapabilityFallsBackWhenRetrieverMissing(t *testing.T) {
	res, err := (&RulesCapability{}).Run(context.Background(), CapabilityInput{
		State:    orchestration.New("s1", "u1"),
		Decision: &Decision{Args: map[string]any{"rule_query": "异地还车要加钱吗"}},
		Deps:     &tools.Deps{},
	})

	if err != nil {
		t.Fatal(err)
	}
	if res.Text != RulesFallbackText {
		t.Fatalf("Text = %q, want fallback", res.Text)
	}
}

func TestRulesCapabilityRejectsInternalOperationsContent(t *testing.T) {
	retriever := &fakeRuleRetriever{content: "AI 话术规则: 命中异地还车费时按内部模板回答。prompt 设计如下..."}
	res, err := (&RulesCapability{}).Run(context.Background(), CapabilityInput{
		State:    orchestration.New("s1", "u1"),
		Decision: &Decision{Args: map[string]any{"rule_query": "异地还车要加钱吗"}},
		Deps:     &tools.Deps{AgentHub: retriever},
	})

	if err != nil {
		t.Fatal(err)
	}
	if res.Text != RulesFallbackText {
		t.Fatalf("Text = %q, want fallback for internal content", res.Text)
	}
	if strings.Contains(res.Text, "prompt") || strings.Contains(res.Text, "话术规则") {
		t.Fatalf("internal content leaked in %q", res.Text)
	}
}

func TestRulesCapabilityUsesGroundedStreamWhenContentFound(t *testing.T) {
	retriever := &fakeRuleRetriever{content: "异地还车可能产生费用,具体金额以下单页和商家规则展示为准。"}
	model := &streamModel{deltas: []string{"异地还车", "可能会有费用。"}}
	getter := &ruleModelGetter{model: model}

	res, err := (&RulesCapability{}).Run(context.Background(), CapabilityInput{
		State:   orchestration.New("s1", "u1"),
		Factory: getter,
		Decision: &Decision{Args: map[string]any{
			"rule_query": "异地还车要加钱吗",
		}},
		Deps: &tools.Deps{AgentHub: retriever},
	})

	if err != nil {
		t.Fatal(err)
	}
	if retriever.query != "异地还车要加钱吗" {
		t.Fatalf("query = %q", retriever.query)
	}
	if getter.key != "rules" {
		t.Fatalf("binding = %q, want rules", getter.key)
	}
	if res.Text != "异地还车可能会有费用。" {
		t.Fatalf("Text = %q", res.Text)
	}
	if len(model.seenReq.Messages) != 1 || !strings.Contains(model.seenReq.Messages[0].Content, "【检索到的知识资料】") {
		t.Fatalf("request messages = %#v", model.seenReq.Messages)
	}
	if model.seenReq.Temperature == nil || *model.seenReq.Temperature != 0.2 {
		t.Fatalf("temperature = %#v, want 0.2", model.seenReq.Temperature)
	}
	if model.seenReq.MaxTokens != 800 {
		t.Fatalf("max_tokens = %d, want 800", model.seenReq.MaxTokens)
	}
}

func TestRulesCapabilityFallsBackOnRetrieverError(t *testing.T) {
	retriever := &fakeRuleRetriever{err: errors.New("agenthub down")}
	res, err := (&RulesCapability{}).Run(context.Background(), CapabilityInput{
		State:    orchestration.New("s1", "u1"),
		Decision: &Decision{Args: map[string]any{"rule_query": "怎么取车"}},
		Deps:     &tools.Deps{AgentHub: retriever},
	})

	if err != nil {
		t.Fatal(err)
	}
	if res.Text != RulesFallbackText {
		t.Fatalf("Text = %q, want fallback", res.Text)
	}
}

var _ agenthub.Client = (*fakeRuleRetriever)(nil)
