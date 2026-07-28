package session

import (
	"testing"
	"time"
)

func TestReducerAppliesRequirementDeltaAndInvalidatesDerivedSearch(t *testing.T) {
	agentSession := &AgentSession{Search: SearchState{
		Goal:         SearchGoalState{NoPreference: true},
		ActiveSearch: &ActiveSearchSnapshot{SearchID: "old-search"},
	}}
	delta := &RequirementDelta{
		Requirements: []SearchRequirementStateItem{{
			ID: "seat", Facet: "seat_num", CanonicalValue: "7", Status: "active",
		}},
		IncrementVersion:  true,
		ActivateGoal:      true,
		ClearNoPreference: true,
		MemoryText:        "想要7座",
	}
	if err := NewReducer().Apply(agentSession, delta); err != nil {
		t.Fatal(err)
	}
	if len(agentSession.Search.Requirements) != 1 ||
		agentSession.Search.RequirementVersion != 1 ||
		agentSession.Search.ActiveSearch != nil ||
		agentSession.Search.Goal.Status != SearchGoalActive ||
		agentSession.Search.Goal.NoPreference ||
		len(agentSession.Memory.RecentSearchCarTexts) != 1 {
		t.Fatalf("unexpected session: %#v", agentSession)
	}
}

func TestReducerKeepsRentalDeltaInsideRentalOwnership(t *testing.T) {
	pickup := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	agentSession := &AgentSession{Search: SearchState{
		Requirements: []SearchRequirementStateItem{{ID: "seat", Facet: "seat_num"}},
	}}
	delta := &RentalContextDelta{
		Location:   &LocationRef{ID: "poi", Name: "虹桥"},
		PickupTime: &pickup,
		Pending:    PendingStore{},
	}
	if err := NewReducer().Apply(agentSession, delta); err != nil {
		t.Fatal(err)
	}
	if agentSession.Search.Location == nil || agentSession.Search.Location.ID != "poi" ||
		agentSession.Search.PickupTime == nil || len(agentSession.Search.Requirements) != 1 {
		t.Fatalf("unexpected session: %#v", agentSession)
	}
}

func TestReducerAppliesSearchPolicyDelta(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	noPreference := true
	askCount := 1
	agentSession := &AgentSession{}
	if err := NewReducer().Apply(agentSession, &SearchPolicyDelta{
		NoPreference:       &noPreference,
		PreferenceAskCount: &askCount,
		LastAskedAt:        &now,
	}); err != nil {
		t.Fatal(err)
	}
	if !agentSession.Search.Goal.NoPreference ||
		agentSession.Search.Goal.PreferenceAskCount != 1 ||
		!agentSession.Search.Goal.LastAskedAt.Equal(now) {
		t.Fatalf("unexpected goal: %#v", agentSession.Search.Goal)
	}
}

func TestReducerRequirementDeltaReplayIsIdempotent(t *testing.T) {
	agentSession := &AgentSession{}
	delta := &RequirementDelta{
		Requirements: []SearchRequirementStateItem{{
			ID: "seat", Facet: "seat_num", CanonicalValue: "7", Status: "active",
		}},
		IncrementVersion: true,
		MemoryText:       "想要7座",
	}
	reducer := NewReducer()
	if err := reducer.Apply(agentSession, delta, delta); err != nil {
		t.Fatal(err)
	}
	if agentSession.Search.RequirementVersion != 1 || len(agentSession.Memory.RecentSearchCarTexts) != 1 {
		t.Fatalf("delta replay changed state twice: %#v", agentSession)
	}
}

func TestReducerOwnsRentalStateChangesAndInvalidation(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	pickup := now.Add(24 * time.Hour)
	agentSession := &AgentSession{Search: SearchState{
		Location:     &LocationRef{ID: "old"},
		ActiveSearch: &ActiveSearchSnapshot{SearchID: "old-search"},
	}}
	delta := &RentalContextDelta{
		Location: &LocationRef{ID: "new"}, PickupTime: &pickup,
		ReceivedAt: now,
	}
	reducer := NewReducer()
	if err := reducer.Apply(agentSession, delta, delta); err != nil {
		t.Fatal(err)
	}
	if agentSession.Search.ActiveSearch != nil ||
		agentSession.Search.DirtyReason != "rental_context_changed" ||
		len(agentSession.StateChanges) != 2 {
		t.Fatalf("unexpected reduced state: %#v", agentSession)
	}
	for _, change := range agentSession.StateChanges {
		if !change.CreatedAt.Equal(now) {
			t.Fatalf("state change uses a different turn time: %#v", change)
		}
	}
}

func TestSearchRuntimeDeltaCannotOverwriteSemanticInputs(t *testing.T) {
	pickup := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	agentSession := &AgentSession{Search: SearchState{
		Location:     &LocationRef{ID: "poi"},
		PickupTime:   &pickup,
		Requirements: []SearchRequirementStateItem{{ID: "seat", CanonicalValue: "7", Status: "active"}},
	}}
	delta := &SearchRuntimeDelta{
		DirtyReason: "guide_error",
		RequirementResolution: []RequirementResolutionUpdate{{
			RequirementID: "seat", Status: "unverifiable", Reason: "missing field",
		}},
	}
	if err := NewReducer().Apply(agentSession, delta); err != nil {
		t.Fatal(err)
	}
	if agentSession.Search.Location == nil || agentSession.Search.Location.ID != "poi" ||
		agentSession.Search.PickupTime == nil || !agentSession.Search.PickupTime.Equal(pickup) ||
		agentSession.Search.Requirements[0].CanonicalValue != "7" ||
		agentSession.Search.Requirements[0].Status != "unverifiable" {
		t.Fatalf("runtime delta crossed ownership boundary: %#v", agentSession.Search)
	}
}
