package prompt

import (
	"strings"
	"testing"
)

func TestRenderDecideSystemReplacesKnowledgePlaceholders(t *testing.T) {
	sys, err := RenderDecideSystem(DecideSystemVars{
		Now:           "2026-07-03 10:00 周五",
		AssistantName: "小租",
		RequiredSlots: "required slots asset",
		SceneKB:       "scene kb asset",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sys, "{{") {
		t.Fatalf("system prompt still has template placeholder:\n%s", sys)
	}
	for _, want := range []string{"required slots asset", "scene kb asset", "库存事实铁律", "跳过即作罢铁律"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("system prompt missing %q", want)
		}
	}
}
