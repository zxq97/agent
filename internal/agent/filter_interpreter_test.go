package agent

import (
	"context"
	"testing"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/types"
)

type fakeModelGetter struct {
	model llm.ChatModel
}

func (f fakeModelGetter) Get(bindingKey string) (llm.ChatModel, error) {
	return f.model, nil
}

type fakeChatModel struct {
	content string
}

func (f fakeChatModel) Chat(context.Context, llm.ChatRequest) (*llm.ChatResponse, error) {
	return &llm.ChatResponse{Content: f.content}, nil
}

func (f fakeChatModel) ChatStream(context.Context, llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	ch := make(chan llm.StreamChunk)
	close(ch)
	return ch, nil
}

func TestInterpretFilterAdoptsHighConfidenceJSON(t *testing.T) {
	dec := &Decision{
		SearchMode: SearchModeRefine,
		NeedDelta:  []types.NeedDelta{{Op: DeltaAdd, Type: "vehicle_type", Value: "SUV", Hardness: "soft", Confidence: 0.6}},
	}
	updated := InterpretFilterIfNeeded(context.Background(), FilterInterpretInput{
		Factory: fakeModelGetter{model: fakeChatModel{content: `{
			"search_mode":"budget_down",
			"feedback_ref":"第一辆",
			"confidence":0.82,
			"need_delta":[
				{"op":"NEGATE","type":"energy_type","value":"纯电","hardness":"hard","confidence":0.9},
				{"op":"UPDATE","type":"price_preference","value":"更低预算","hardness":"hard","confidence":0.8}
			]
		}`}},
		Decision: dec,
		UserText: "第一辆不喜欢,不要电车,预算再低点",
		State:    orchestration.New("s1", "u1"),
		Reason:   "test",
	})

	if updated.SearchMode != SearchModeBudgetDown {
		t.Fatalf("SearchMode = %q, want %q", updated.SearchMode, SearchModeBudgetDown)
	}
	if updated.FeedbackRef != "第一辆" {
		t.Fatalf("FeedbackRef = %q, want 第一辆", updated.FeedbackRef)
	}
	if len(updated.NeedDelta) != 2 {
		t.Fatalf("NeedDelta len = %d, want 2", len(updated.NeedDelta))
	}
}

func TestInterpretFilterKeepsOriginalOnLowConfidence(t *testing.T) {
	dec := &Decision{
		SearchMode: SearchModeRefine,
		NeedDelta:  []types.NeedDelta{{Op: DeltaAdd, Type: "vehicle_type", Value: "SUV", Hardness: "soft", Confidence: 0.6}},
	}
	updated := InterpretFilterIfNeeded(context.Background(), FilterInterpretInput{
		Factory: fakeModelGetter{model: fakeChatModel{content: `{
			"search_mode":"budget_down",
			"confidence":0.3,
			"need_delta":[{"op":"UPDATE","type":"price_preference","value":"更低预算","hardness":"hard","confidence":0.8}]
		}`}},
		Decision: dec,
		UserText: "预算低点",
		State:    orchestration.New("s1", "u1"),
		Reason:   "test",
	})

	if updated.SearchMode != SearchModeRefine {
		t.Fatalf("SearchMode = %q, want original %q", updated.SearchMode, SearchModeRefine)
	}
	if len(updated.NeedDelta) != 1 || updated.NeedDelta[0].Type != "vehicle_type" {
		t.Fatalf("NeedDelta = %#v, want original vehicle_type delta", updated.NeedDelta)
	}
}
