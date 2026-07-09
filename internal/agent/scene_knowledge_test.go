package agent

import "testing"

func TestSceneKnowledgeInfersSUVForFamilyWithElderlyAndKids(t *testing.T) {
	patch := MatchSceneKnowledge("带老人小孩周末出去玩")

	if len(patch.Needs) == 0 {
		t.Fatal("Needs is empty, want SUV soft inference")
	}
	got := patch.Needs[0]
	if got.Type != "vehicle_type" || got.Value != "SUV" || got.Hardness != "soft" || got.Confidence < 0.5 {
		t.Fatalf("need = %#v", got)
	}
	if patch.Tip == "" {
		t.Fatal("Tip is empty")
	}
}

func TestRequiredSlotsRenderMentionsCoreDimensions(t *testing.T) {
	out := RenderRequiredSlots()
	for _, want := range []string{"seat_num", "vehicle_type", "price_preference", "sufficiency"} {
		if !contains(out, want) {
			t.Fatalf("required slots missing %q:\n%s", want, out)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexString(s, sub) >= 0)
}

func indexString(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
