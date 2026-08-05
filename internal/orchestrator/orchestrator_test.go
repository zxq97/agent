package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/api/maps"
	"github.com/zxq97/agent/internal/domain"
	"github.com/zxq97/agent/internal/domain/generalreply"
	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/domain/rentalrules"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/domain/vehiclecompare"
	"github.com/zxq97/agent/internal/domain/vehiclerequirement"
	"github.com/zxq97/agent/internal/searchpolicy"
	"github.com/zxq97/agent/internal/session"
)

type staticID string

func (id staticID) NewID() string { return string(id) }

func newCommandRentalHandler(t *testing.T, now time.Time) rentalcontext.Handler {
	t.Helper()
	llmClient, err := llm.NewHTTPClient(&llm.HTTPConfig{
		Endpoint: "http://unused.invalid",
		APIKey:   "test-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	extractor, err := rentalcontext.NewExtractor(llmClient)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := rentalcontext.NewHandler(
		extractor,
		maps.NewHTTPClient(&maps.HTTPConfig{Endpoint: "http://unused.invalid"}),
		staticID("next"),
		func() time.Time { return now },
		time.UTC,
		rentalcontext.DefaultAmbiguityConfig(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

type successfulSearch struct {
	calls int
}

type requirementDeltaHandler struct{}

type silentGeneralReply struct{}

func (silentGeneralReply) Handle(context.Context, *session.AgentSession, *generalreply.Input) (*generalreply.Result, error) {
	return &generalreply.Result{}, nil
}

func newTestRentalRulesHandler() rentalrules.Handler {
	handler, err := rentalrules.NewHandler(rentalrules.NewDefaultCatalog())
	if err != nil {
		panic(err)
	}
	return handler
}

func newTestOrchestrator(
	rental rentalcontext.Handler,
	requirement vehiclerequirement.Handler,
	search searchcar.Handler,
	policy SearchPolicy,
	now func() time.Time,
	general ...generalreply.Handler,
) *Orchestrator {
	if rental == nil {
		rental = domainMismatchRental{}
	}
	if requirement == nil {
		requirement = domainMismatchRequirement{}
	}
	if search == nil {
		search = &successfulSearch{}
	}
	var generalHandler generalreply.Handler = silentGeneralReply{}
	if len(general) > 0 && general[0] != nil {
		generalHandler = general[0]
	}
	value, err := NewWithExtensions(
		rental,
		requirement,
		search,
		policy,
		now,
		generalHandler,
		vehiclecompare.NewHandler(),
		newTestRentalRulesHandler(),
	)
	if err != nil {
		panic(err)
	}
	return value
}

func TestNewWithExtensionsRequiresCompleteDependencies(t *testing.T) {
	if _, err := NewWithExtensions(
		nil,
		domainMismatchRequirement{},
		&successfulSearch{},
		searchpolicy.New(1, time.Now),
		time.Now,
		silentGeneralReply{},
		vehiclecompare.NewHandler(),
		newTestRentalRulesHandler(),
	); err == nil {
		t.Fatal("expected missing dependency error")
	}
}

func (requirementDeltaHandler) Handle(context.Context, *session.AgentSession, *vehiclerequirement.Input) (*vehiclerequirement.Result, error) {
	requirements := []session.SearchRequirementStateItem{{
		ID: "seat", Facet: "seat_num", CanonicalValue: "7",
		Operator: "eq", Importance: "hard", Status: "active",
	}}
	return &vehiclerequirement.Result{
		Changed: true, Requirements: requirements,
		Deltas: []session.StateDelta{&session.RequirementDelta{
			Requirements: requirements, IncrementVersion: true, ActivateGoal: true,
		}},
	}, nil
}

func (h *successfulSearch) Handle(context.Context, *session.AgentSession, *searchcar.Input) (*searchcar.Result, error) {
	h.calls++
	return &searchcar.Result{Status: searchcar.ResultNeedsContext}, nil
}

func TestExecuteResolvesPendingAndAppliesOtherCondition(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	pickup := now.Add(48 * time.Hour)
	handler := newCommandRentalHandler(t, now)
	agentSession := &session.AgentSession{Pending: session.PendingStore{Active: &session.PendingInteraction{
		ID: "choose-location", Type: session.PendingSelectLocation, Status: session.PendingActive,
		Options:         []session.PendingOption{{ID: "airport", Label: "虹桥机场", Location: &session.LocationRef{ID: "airport", Name: "虹桥机场", CityID: "310000", Latitude: 31.2, Longitude: 121.3}}},
		BlockingActions: []session.PendingAction{session.ActionExecuteVehicleSearch}, CreatedAt: now, ExpireAt: now.Add(10 * time.Minute), MaxMissedUserTurns: 2,
	}, DeferredActions: []session.DeferredAction{{ID: "budget", Action: session.ActionExecuteVehicleSearch, EvidenceText: "预算300", BlockedByPendingID: "choose-location"}}}}
	search := &successfulSearch{}
	orchestrator := newTestOrchestrator(handler, nil, search, searchpolicy.New(1, func() time.Time { return now }), func() time.Time { return now })
	result, err := orchestrator.Execute(context.Background(), agentSession, &TurnRequest{
		SourceText: "虹桥机场，改成后天下午",
		RentalContext: &rentalcontext.Input{Command: &rentalcontext.Command{
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
	handler := newCommandRentalHandler(t, now)
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
	result, err := newTestOrchestrator(
		handler, requirementDeltaHandler{}, search,
		searchpolicy.New(1, func() time.Time { return now }), func() time.Time { return now },
	).Execute(context.Background(), agentSession, &TurnRequest{
		SourceText:         "虹桥机场，还要7座",
		VehicleRequirement: &vehiclerequirement.Input{SourceText: "还要7座"},
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
	orchestrator := newTestOrchestrator(nil, nil, nil, searchpolicy.New(1, func() time.Time { return now }), func() time.Time { return now })
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
	result, err := newTestOrchestrator(nil, nil, search, searchpolicy.New(1, func() time.Time { return now }), func() time.Time { return now }).Execute(context.Background(), agentSession, &TurnRequest{SourceText: "继续"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExpiredPending == nil || len(result.RevalidateActions) != 0 ||
		search.calls != 1 || len(agentSession.Pending.DeferredActions) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

type domainMismatchRental struct{}

func (domainMismatchRental) Handle(context.Context, *session.AgentSession, *rentalcontext.Input) (*rentalcontext.Result, error) {
	return nil, domain.ErrDomainMismatch
}

type domainMismatchRequirement struct{}

func (domainMismatchRequirement) Handle(context.Context, *session.AgentSession, *vehiclerequirement.Input) (*vehiclerequirement.Result, error) {
	return nil, domain.ErrDomainMismatch
}

type recordingGeneralReply struct {
	source string
}

func (h *recordingGeneralReply) Handle(_ context.Context, _ *session.AgentSession, input *generalreply.Input) (*generalreply.Result, error) {
	h.source = input.SourceText
	return &generalreply.Result{Message: "fallback:" + input.SourceText}, nil
}

func TestExecuteTreatsDomainMismatchAsNonFatal(t *testing.T) {
	result, err := newTestOrchestrator(domainMismatchRental{}, domainMismatchRequirement{}, nil, searchpolicy.New(1, time.Now), time.Now).Execute(context.Background(), &session.AgentSession{}, &TurnRequest{
		SourceText:         "闲聊",
		RentalContext:      &rentalcontext.Input{SourceText: "闲聊"},
		VehicleRequirement: &vehiclerequirement.Input{SourceText: "闲聊"},
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
	result, err := newTestOrchestrator(
		domainMismatchRental{},
		domainMismatchRequirement{},
		nil,
		searchpolicy.New(1, time.Now),
		time.Now,
		general,
	).Execute(context.Background(), &session.AgentSession{}, &TurnRequest{
		SourceText:         "SUV和MPV有什么区别",
		RentalContext:      &rentalcontext.Input{SourceText: "SUV和MPV有什么区别"},
		VehicleRequirement: &vehiclerequirement.Input{SourceText: "SUV和MPV有什么区别"},
		GeneralReply:       &generalreply.Input{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if general.source != "SUV和MPV有什么区别" || result.GeneralReply == nil || result.GeneralReply.Message == "" {
		t.Fatalf("source=%q result=%#v", general.source, result)
	}
}

type recordingComparison struct {
	evidence string
}

func (h *recordingComparison) Handle(_ context.Context, _ *session.AgentSession, input *vehiclecompare.Input) (*vehiclecompare.Result, error) {
	h.evidence = input.EvidenceText
	return &vehiclecompare.Result{Status: vehiclecompare.StatusSuccess, Message: "compared"}, nil
}

type recordingRules struct {
	evidence string
}

func (h *recordingRules) Handle(_ context.Context, input *rentalrules.Input) (*rentalrules.Result, error) {
	h.evidence = input.EvidenceText
	return &rentalrules.Result{Status: rentalrules.StatusSuccess, Message: "rules"}, nil
}

func TestExecuteRunsComparisonAndRulesHandlers(t *testing.T) {
	comparison := &recordingComparison{}
	rules := &recordingRules{}
	value, err := NewWithExtensions(
		domainMismatchRental{},
		domainMismatchRequirement{},
		&successfulSearch{},
		searchpolicy.New(1, time.Now),
		time.Now,
		&recordingGeneralReply{},
		comparison,
		rules,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := value.Execute(context.Background(), &session.AgentSession{}, &TurnRequest{
		SourceText:        "对比1和2，再看押金",
		VehicleComparison: &vehiclecompare.Input{EvidenceText: "对比1和2"},
		RentalRules:       &rentalrules.Input{EvidenceText: "押金"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if comparison.evidence != "对比1和2" || rules.evidence != "押金" ||
		result.VehicleComparison == nil || result.RentalRules == nil {
		t.Fatalf("unexpected result: %#v", result)
	}
}
