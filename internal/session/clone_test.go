package session

import (
	"testing"
	"time"

	"github.com/zxq97/agent/internal/requirement"
	"github.com/zxq97/agent/internal/searchruntime"
)

func TestCloneDoesNotShareMutableSessionState(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	number := 7.0
	minimum := 100.0
	maximum := 300.0
	original := &AgentSession{
		Search: SearchState{
			Location: &LocationRef{ID: "location-1", Name: "虹桥"},
			Requirements: []SearchRequirementStateItem{
				{
					ID: "seat", Facet: "seat_num", CanonicalValue: "7",
					Value: requirement.Value{Kind: requirement.ValueNumber, Number: &number},
				},
				{
					ID: "budget",
					Value: requirement.Value{
						Kind:  requirement.ValueRange,
						Range: &requirement.NumberRange{Min: &minimum, Max: &maximum},
					},
				},
				{
					ID: "entity",
					Value: requirement.Value{
						Kind:   requirement.ValueEntity,
						Entity: &requirement.EntityRef{ID: "vehicle-entity"},
					},
				},
			},
			Baseline: &GuideBaselineCache{
				Menu: []searchruntime.MenuGroup{{GroupItems: []searchruntime.GroupItem{{
					Items: []searchruntime.MenuItem{{Name: "SUV", Code: "filter/type/suv"}},
				}}}},
				BaseQuotes: []searchruntime.Quote{{Vehicle: &searchruntime.Vehicle{Code: "vehicle-1"}}},
			},
			ActiveSearch: &ActiveSearchSnapshot{
				SeenQuoteIDs:     map[string]struct{}{"quote-1": {}},
				SeenVehicleCodes: map[string]struct{}{"vehicle-1": {}},
				Batches:          []SearchResultBatch{{Vehicles: []searchruntime.Quote{{Vehicle: &searchruntime.Vehicle{Code: "vehicle-1"}}}}},
			},
		},
		Pending: PendingStore{Active: &PendingInteraction{
			ID:              "pending-1",
			Options:         []PendingOption{{ID: "option-1", Location: &LocationRef{ID: "location-1"}}},
			BlockingActions: []PendingAction{ActionExecuteVehicleSearch},
			ResolvedAt:      &now,
		}},
		StateChanges: []StateChange{{
			Field: "location", OldValue: &LocationRef{ID: "old-location"},
			NewValue: &LocationRef{ID: "location-1"},
		}},
		Memory: ConversationMemory{RecentSearchCarTexts: []string{"7座"}},
	}

	cloned := Clone(original)
	cloned.Search.Location.Name = "浦东"
	cloned.Search.Requirements[0].CanonicalValue = "5"
	*cloned.Search.Requirements[0].Value.Number = 5
	*cloned.Search.Requirements[1].Value.Range.Min = 200
	cloned.Search.Requirements[2].Value.Entity.ID = "other-entity"
	cloned.Search.Baseline.Menu[0].GroupItems[0].Items[0].Name = "MPV"
	cloned.Search.Baseline.BaseQuotes[0].Vehicle.Code = "vehicle-2"
	delete(cloned.Search.ActiveSearch.SeenQuoteIDs, "quote-1")
	cloned.Search.ActiveSearch.Batches[0].Vehicles[0].Vehicle.Code = "vehicle-3"
	cloned.Pending.Active.Options[0].Location.ID = "location-2"
	cloned.Pending.Active.BlockingActions[0] = ActionModifyRentalContext
	cloned.StateChanges[0].OldValue.(*LocationRef).ID = "mutated"
	cloned.Memory.RecentSearchCarTexts[0] = "5座"

	if original.Search.Location.Name != "虹桥" ||
		original.Search.Requirements[0].CanonicalValue != "7" ||
		*original.Search.Requirements[0].Value.Number != 7 ||
		*original.Search.Requirements[1].Value.Range.Min != 100 ||
		original.Search.Requirements[2].Value.Entity.ID != "vehicle-entity" ||
		original.Search.Baseline.Menu[0].GroupItems[0].Items[0].Name != "SUV" ||
		original.Search.Baseline.BaseQuotes[0].Vehicle.Code != "vehicle-1" ||
		original.Search.ActiveSearch.Batches[0].Vehicles[0].Vehicle.Code != "vehicle-1" ||
		original.Pending.Active.Options[0].Location.ID != "location-1" ||
		original.Pending.Active.BlockingActions[0] != ActionExecuteVehicleSearch ||
		original.StateChanges[0].OldValue.(*LocationRef).ID != "old-location" ||
		original.Memory.RecentSearchCarTexts[0] != "7座" {
		t.Fatalf("clone shares mutable state with original: %#v", original)
	}
	if _, exists := original.Search.ActiveSearch.SeenQuoteIDs["quote-1"]; !exists {
		t.Fatal("clone shares seen quote map with original")
	}
}
