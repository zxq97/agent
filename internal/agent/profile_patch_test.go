package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
)

func TestBuildDecisionParsesProfilePatch(t *testing.T) {
	dec := buildDecision([]llm.ToolCall{{
		Function: llm.FunctionCall{
			Name: ToolSearchVehicles,
			Arguments: `{
				"search_mode":"initial",
				"profile_patch":{"trip_scene":"家庭出游","companions":"老人小孩","price_sensitivity":"high","style_preference":"空间舒适"}
			}`,
		},
	}}, "")

	if dec.ProfilePatch == nil {
		t.Fatal("ProfilePatch is nil")
	}
	if dec.ProfilePatch.TripScene != "家庭出游" || dec.ProfilePatch.Companions != "老人小孩" {
		t.Fatalf("ProfilePatch = %#v", dec.ProfilePatch)
	}
}

func TestApplyProfilePatchAndStatePrefix(t *testing.T) {
	st := orchestration.New("s1", "u1")
	ApplyProfilePatch(st, &ProfilePatch{TripScene: "商务接送", PriceSensitivity: "low"})

	prefix := BuildStatePrefix(st, time.Date(2026, 7, 3, 10, 0, 0, 0, time.Local))

	if !strings.Contains(prefix, "profile") || !strings.Contains(prefix, "商务接送") || !strings.Contains(prefix, "low") {
		t.Fatalf("prefix missing profile:\n%s", prefix)
	}
}
