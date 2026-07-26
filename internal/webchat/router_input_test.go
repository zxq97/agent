package webchat

import (
	"testing"
	"time"

	"github.com/zxq97/agent/internal/session"
)

func TestBuildRouterInputProjectsStateWithoutProviderIDs(t *testing.T) {
	pickup := time.Date(2026, 7, 24, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	state := &session.AgentSession{
		Search: session.SearchState{
			Location:   &session.LocationRef{ID: "provider-poi", Name: "虹桥机场"},
			PickupTime: &pickup,
			ActiveSearch: &session.ActiveSearchSnapshot{
				SearchID: "provider-context",
			},
			Requirements: []session.SearchRequirementStateItem{{
				ID: "server-requirement", Facet: "vehicle_type", CanonicalValue: "SUV", Importance: "hard", Status: "active",
			}},
		},
		Pending: session.PendingStore{Active: &session.PendingInteraction{
			ID: "pending-id", Type: session.PendingSelectLocation, Question: "请选择地点",
			Options: []session.PendingOption{{ID: "provider-option", Label: "虹桥机场", Value: "上海"}},
		}},
	}
	input := buildRouterInput(state, []Message{{Role: "user", Content: "上轮"}}, "第一个，再要7座")
	if input.CurrentRental.LocationName != "虹桥机场" || input.CurrentRental.PickupTime == "" || !input.HasPreviousSearch {
		t.Fatalf("unexpected rental input: %#v", input)
	}
	if len(input.CurrentRequirements) != 1 || input.CurrentRequirements[0].Value != "SUV" {
		t.Fatalf("unexpected requirements: %#v", input.CurrentRequirements)
	}
	if input.ActivePending == nil || len(input.ActivePending.Options) != 1 || input.ActivePending.Options[0] != "虹桥机场 上海" {
		t.Fatalf("unexpected pending: %#v", input.ActivePending)
	}
	if len(input.RecentMessages) != 1 || input.RecentMessages[0].Content != "上轮" {
		t.Fatalf("unexpected history: %#v", input.RecentMessages)
	}
}
