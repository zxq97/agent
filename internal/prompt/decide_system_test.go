package prompt

import (
	"strings"
	"testing"
)

func TestRenderDecideSystemMentionsSearchMode(t *testing.T) {
	sys, err := RenderDecideSystem(DecideSystemVars{Now: "2026-07-03 10:00 周五", AssistantName: "小租"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"search_mode", "negative_feedback", "budget_down", "budget_up", "feedback_ref"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("system prompt missing %q", want)
		}
	}
}
