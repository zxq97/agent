package searchpolicy

import (
	"testing"
	"time"

	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/session"
)

func TestConditionChangeOverridesNextBatch(t *testing.T) {
	policy := New(1, time.Now)
	agentSession := &session.AgentSession{}
	result := policy.Evaluate(agentSession, Input{
		ExplicitSearchRequested: true,
		RequestedOperation:      searchcar.OperationNextBatch,
		RequirementsChanged:     true,
	})
	if result.Decision != DecisionSearch || result.Operation != searchcar.OperationSearchNow {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestFirstCompleteContextAsksPreferenceWithoutPending(t *testing.T) {
	now := time.Now()
	pickup := now.Add(time.Hour)
	returnTime := now.Add(2 * time.Hour)
	agentSession := &session.AgentSession{Search: session.SearchState{
		Location:   &session.LocationRef{ID: "poi"},
		PickupTime: &pickup,
		ReturnTime: &returnTime,
	}}
	result := New(1, func() time.Time { return now }).Evaluate(agentSession, Input{RentalContextChanged: true})
	if result.Decision != DecisionAskPreference || agentSession.Search.Goal.PreferenceAskCount != 1 || agentSession.Pending.Active != nil {
		t.Fatalf("result=%#v session=%#v", result, agentSession)
	}
}

func TestExplicitNoPreferenceSearchesImmediately(t *testing.T) {
	agentSession := &session.AgentSession{}
	result := New(1, time.Now).Evaluate(agentSession, Input{
		ExplicitSearchRequested: true,
		SearchEvidence:          "都行，直接搜",
	})
	if result.Decision != DecisionSearch || !agentSession.Search.Goal.NoPreference {
		t.Fatalf("result=%#v goal=%#v", result, agentSession.Search.Goal)
	}
}

func TestBlockingPendingOnlyBlocksSearch(t *testing.T) {
	agentSession := &session.AgentSession{
		Search: session.SearchState{ActiveSearch: &session.ActiveSearchSnapshot{SearchID: "old"}},
		Pending: session.PendingStore{Active: &session.PendingInteraction{
			Status:          session.PendingActive,
			BlockingActions: []session.PendingAction{session.ActionExecuteVehicleSearch},
		}},
	}
	result := New(1, time.Now).Evaluate(agentSession, Input{RequirementsChanged: true})
	if result.Decision != DecisionWaitPending || agentSession.Search.ActiveSearch != nil {
		t.Fatalf("result=%#v search=%#v", result, agentSession.Search.ActiveSearch)
	}
}
