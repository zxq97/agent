package agent

import (
	"encoding/json"

	"github.com/zxq97/rental-agent/internal/orchestration"
)

func parseProfilePatch(args map[string]any) *ProfilePatch {
	raw, ok := args["profile_patch"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var p ProfilePatch
	if err := json.Unmarshal(b, &p); err != nil {
		return nil
	}
	if p.TripScene == "" && p.Companions == "" && p.PriceSensitivity == "" && p.StylePreference == "" {
		return nil
	}
	return &p
}

func ApplyProfilePatch(state *orchestration.ConversationState, patch *ProfilePatch) {
	if state == nil || patch == nil {
		return
	}
	if patch.TripScene != "" {
		state.Profile.TripScene = patch.TripScene
	}
	if patch.Companions != "" {
		state.Profile.Companions = patch.Companions
	}
	if patch.PriceSensitivity != "" {
		state.Profile.PriceSensitivity = patch.PriceSensitivity
	}
	if patch.StylePreference != "" {
		state.Profile.StylePreference = patch.StylePreference
	}
}
