package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/zxq97/agent/internal/domain/generalreply"
	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/domain/vehiclerequirement"
	"github.com/zxq97/agent/internal/searchpolicy"
	"github.com/zxq97/agent/internal/session"
)

type staticID string

func (id staticID) NewID() string { return string(id) }

type successfulSearch struct {
	calls int
}

type requirementDeltaHandler struct{}

func (requirementDeltaHandler) Handle(context.Context, *session.AgentSession, *vehiclerequirement.UpdateInput) (*vehiclerequirement.UpdateResult, error) {
	requirements := []session.SearchRequirementStateItem{{
		ID: "seat", Facet: "seat_num", CanonicalValue: "7",
		Operator: "eq", Importance: "hard", Status: "active",
	}}
	return &vehiclerequirement.UpdateResult{
		Changed: true, Requirements: requirements,
		Deltas: []session.StateDelta{&session.RequirementDelta{
			Requirements: requirements, IncrementVersion: true, ActivateGoal: true,
		}},
	}, nil
}

func (h *successfulSearch) Handle(context.Context, *session.AgentSession, *searchcar.SearchCarInput) (*searchcar.SearchCarResult, error) {
	h.calls++
	return &searchcar.SearchCarResult{Status: searchcar.ResultNeedsContext}, nil
}

func TestExecuteResolvesPendingAndAppliesOtherCondition(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	pickup := now.Add(48 * time.Hour)
	handler, err := rentalcontext.NewModifyRentalContextHandler(nil, nil, staticID("next"), func() time.Time { return now }, time.UTC, rentalcontext.DefaultAmbiguityConfig())
	if err != nil {
		t.Fatal(err)
	}
	agentSession := &session.AgentSession{Pending: session.PendingStore{Active: &session.PendingInteraction{
		ID: "choose-location", Type: session.PendingSelectLocation, Status: session.PendingActive,
		Options:         []session.PendingOption{{ID: "airport", Label: "虹桥机场", Location: &session.LocationRef{ID: "airport", Name: "虹桥机场", CityID: "310000", Latitude: 31.2, Longitude: 121.3}}},
		BlockingActions: []session.PendingAction{session.ActionExecuteVehicleSearch}, CreatedAt: now, ExpireAt: now.Add(10 * time.Minute), MaxMissedUserTurns: 2,
	}, DeferredActions: []session.DeferredAction{{ID: "budget", Action: session.ActionExecuteVehicleSearch, EvidenceText: "预算300", BlockedByPendingID: "choose-location"}}}}
	search := &successfulSearch{}
	orchestrator := New(handler, nil, search, searchpolicy.New(1, func() time.Time { return now }), func() time.Time { return now })
	result, err := orchestrator.Execute(context.Background(), agentSession, &TurnRequest{
		SourceText: "虹桥机场，改成后天下午",
		RentalContext: &rentalcontext.ModifyRentalContextInput{Command: &rentalcontext.ModifyRentalContextCommand{
			PickupTime: &pickup,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if agentSession.Pending.Active != nil || agentSession.Search.Location == nil || agentSession.Search.Location.ID != "airport" || agentSession.Search.PickupTime == nil || !agentSession.Search.PickupTime.Equal(pickup) {
		t.Fatalf("session = %#v", agentSession)
	}
	if len(result.RentalContext) != 2 || len(result.RevalidateActions) != 0 ||
		search.calls != 1 || len(agentSession.Pending.DeferredActions) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecuteKeepsRequirementAlongsidePendingAnswer(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	handler, err := rentalcontext.NewModifyRentalContextHandler(nil, nil, staticID("next"), func() time.Time { return now }, time.UTC, rentalcontext.DefaultAmbiguityConfig())
	if err != nil {
		t.Fatal(err)
	}
	agentSession := &session.AgentSession{Pending: session.PendingStore{Active: &session.PendingInteraction{
		ID: "choose-location", Type: session.PendingSelectLocation, Status: session.PendingActive,
		Options: []session.PendingOption{{
			ID: "airport", Label: "虹桥机场",
			Location: &session.LocationRef{ID: "airport", Name: "虹桥机场", CityID: "310000"},
		}},
		BlockingActions: []session.PendingAction{session.ActionExecuteVehicleSearch},
		CreatedAt:       now, ExpireAt: now.Add(10 * time.Minute),
	}}}
	search := &successfulSearch{}
	result, err := New(
		handler, requirementDeltaHandler{}, search,
		searchpolicy.New(1, func() time.Time { return now }), func() time.Time { return now },
	).Execute(context.Background(), agentSession, &TurnRequest{
		SourceText:         "虹桥机场，还要7座",
		VehicleRequirement: &vehiclerequirement.UpdateInput{SourceText: "还要7座"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if agentSession.Pending.Active != nil ||
		agentSession.Search.Location == nil || agentSession.Search.Location.ID != "airport" ||
		len(agentSession.Search.Requirements) != 1 ||
		agentSession.Search.Requirements[0].CanonicalValue != "7" ||
		result.VehicleRequirement == nil || search.calls != 1 {
		t.Fatalf("pending answer lost mixed intent: session=%#v result=%#v", agentSession, result)
	}
}

func TestExecuteSuspendsIgnoredPendingAfterTwoTurns(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	agentSession := &session.AgentSession{Pending: session.PendingStore{Active: &session.PendingInteraction{ID: "location", Type: session.PendingSelectLocation, Status: session.PendingActive, CreatedAt: now, ExpireAt: now.Add(time.Hour), MaxMissedUserTurns: 2}}}
	orchestrator := New(nil, nil, nil, searchpolicy.New(1, func() time.Time { return now }), func() time.Time { return now })
	if _, err := orchestrator.Execute(context.Background(), agentSession, &TurnRequest{SourceText: "我想要SUV"}); err != nil {
		t.Fatal(err)
	}
	result, err := orchestrator.Execute(context.Background(), agentSession, &TurnRequest{SourceText: "最好是新能源"})
	if err != nil {
		t.Fatal(err)
	}
	if result.SuspendedPending == nil || result.SuspendedPending.Status != session.PendingSuspended || agentSession.Pending.Active != nil {
		t.Fatalf("result=%#v active=%#v", result, agentSession.Pending.Active)
	}
}

func TestExecuteReturnsDeferredActionsWhenPendingExpires(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	agentSession := &session.AgentSession{Pending: session.PendingStore{
		Active:          &session.PendingInteraction{ID: "location", Type: session.PendingSelectLocation, Status: session.PendingActive, CreatedAt: now.Add(-time.Hour), ExpireAt: now.Add(-time.Minute)},
		DeferredActions: []session.DeferredAction{{ID: "budget", Action: session.ActionExecuteVehicleSearch, BlockedByPendingID: "location"}},
	}}
	search := &successfulSearch{}
	result, err := New(nil, nil, search, searchpolicy.New(1, func() time.Time { return now }), func() time.Time { return now }).Execute(context.Background(), agentSession, &TurnRequest{SourceText: "继续"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExpiredPending == nil || len(result.RevalidateActions) != 0 ||
		search.calls != 1 || len(agentSession.Pending.DeferredActions) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

type domainMismatchRental struct{}

func (domainMismatchRental) Handle(context.Context, *session.AgentSession, *rentalcontext.ModifyRentalContextInput) (*rentalcontext.ModifyRentalContextResult, error) {
	return nil, rentalcontext.ErrDomainMismatch
}

type domainMismatchRequirement struct{}

func (domainMismatchRequirement) Handle(context.Context, *session.AgentSession, *vehiclerequirement.UpdateInput) (*vehiclerequirement.UpdateResult, error) {
	return nil, vehiclerequirement.ErrDomainMismatch
}

type recordingGeneralReply struct {
	source string
}

func (h *recordingGeneralReply) Handle(_ context.Context, _ *session.AgentSession, input *generalreply.Input) (*generalreply.Result, error) {
	h.source = input.SourceText
	return &generalreply.Result{Message: "fallback:" + input.SourceText}, nil
}

func TestExecuteTreatsDomainMismatchAsNonFatal(t *testing.T) {
	result, err := New(domainMismatchRental{}, domainMismatchRequirement{}, nil, searchpolicy.New(1, time.Now), time.Now).Execute(context.Background(), &session.AgentSession{}, &TurnRequest{
		SourceText:         "闲聊",
		RentalContext:      &rentalcontext.ModifyRentalContextInput{SourceText: "闲聊"},
		VehicleRequirement: &vehiclerequirement.UpdateInput{SourceText: "闲聊"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RentalContext) != 0 || result.VehicleRequirement != nil || result.SearchCar != nil {
		t.Fatalf("result=%#v", result)
	}
}

func TestExecuteSendsDomainMismatchTextToGeneralReply(t *testing.T) {
	general := &recordingGeneralReply{}
	result, err := New(
		domainMismatchRental{},
		domainMismatchRequirement{},
		nil,
		searchpolicy.New(1, time.Now),
		time.Now,
		general,
	).Execute(context.Background(), &session.AgentSession{}, &TurnRequest{
		SourceText:         "SUV和MPV有什么区别",
		RentalContext:      &rentalcontext.ModifyRentalContextInput{SourceText: "SUV和MPV有什么区别"},
		VehicleRequirement: &vehiclerequirement.UpdateInput{SourceText: "SUV和MPV有什么区别"},
		GeneralReply:       &generalreply.Input{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if general.source != "SUV和MPV有什么区别" || result.GeneralReply == nil || result.GeneralReply.Message == "" {
		t.Fatalf("source=%q result=%#v", general.source, result)
	}
}
