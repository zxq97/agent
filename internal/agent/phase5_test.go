package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
)

type captureEmitter struct {
	texts  []string
	events []capturedEvent
}

type capturedEvent struct {
	name   string
	detail string
}

func (e *captureEmitter) Text(delta string) {
	e.texts = append(e.texts, delta)
}

func (e *captureEmitter) Event(name, detail string) {
	e.events = append(e.events, capturedEvent{name: name, detail: detail})
}

func TestPreRouteCompareActionBuildsCompareDecision(t *testing.T) {
	ac := &AgentContext{
		EventType: "action_click",
		Action: &ClientAction{
			Type: "compare",
			Payload: map[string]any{
				"vehicle_refs": []any{"朗逸", "轩逸"},
			},
		},
	}

	sig, err := (&PreRouteStage{}).Handle(context.Background(), ac)

	if err != nil {
		t.Fatal(err)
	}
	if sig != SignalContinue {
		t.Fatalf("signal = %s, want continue", sig)
	}
	if ac.Decision == nil || ac.Decision.Tool != ToolCompare {
		t.Fatalf("decision = %#v, want compare", ac.Decision)
	}
	refs := extractRefs(ac.Decision.Args["vehicle_refs"])
	if len(refs) != 2 || refs[0] != "朗逸" || refs[1] != "轩逸" {
		t.Fatalf("vehicle_refs = %#v", ac.Decision.Args["vehicle_refs"])
	}
}

func TestGuideActionStageEmitsCompareAndFeedbackActions(t *testing.T) {
	st := orchestration.New("s1", "u1")
	st.SetQuotes("ctx1", []orchestration.QuoteRef{
		{CarName: "大众朗逸", BrandName: "大众", Index: 1},
		{CarName: "日产轩逸", BrandName: "日产", Index: 2},
	})
	emitter := &captureEmitter{}
	ac := &AgentContext{
		State:    st,
		Emit:     emitter,
		Decision: &Decision{Tool: ToolSearchVehicles},
		Result:   &CapabilityResult{ToolName: tools.ToolSearchQuotes},
	}

	sig, err := (&GuideActionStage{}).Handle(context.Background(), ac)

	if err != nil {
		t.Fatal(err)
	}
	if sig != SignalContinue {
		t.Fatalf("signal = %s, want continue", sig)
	}
	var quick string
	for _, ev := range emitter.events {
		if ev.name == "quick_action" {
			quick = ev.detail
			break
		}
	}
	if quick == "" {
		t.Fatalf("quick_action not emitted: %#v", emitter.events)
	}
	for _, want := range []string{"compare", "大众朗逸", "日产轩逸", "feedback_positive", "feedback_negative"} {
		if !strings.Contains(quick, want) {
			t.Fatalf("quick_action missing %q: %s", want, quick)
		}
	}
}

func TestFileFeedbackStoreWritesJSONL(t *testing.T) {
	path := t.TempDir() + "/feedback.jsonl"
	store := NewFileFeedbackStore(path)

	err := store.Save(context.Background(), FeedbackSnapshot{
		UserID:    "u1",
		SessionID: "s1",
		Rating:    "negative",
		Message:   "不满意",
	})

	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"rating":"negative"`) || !strings.Contains(string(body), `"session_id":"s1"`) {
		t.Fatalf("feedback file = %s", body)
	}
}
