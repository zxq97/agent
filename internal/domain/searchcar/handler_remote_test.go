package searchcar

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/searchplan"
	"github.com/zxq97/agent/internal/session"
	"github.com/zxq97/agent/pkg/log"
)

const searchCarConfigPath = "../../../conf/dev.yaml"

func TestRemoteSearchUsesIsolatedBaselineAndContinuation(t *testing.T) {
	requireRemoteGuide(t)
	cfg, err := config.Load(searchCarConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	client := guide.NewHTTPClient(&guide.HTTPConfig{Endpoint: cfg.Guide.Endpoint, Phone: cfg.Guide.Phone, TimeoutSec: cfg.Guide.Timeout})
	handler, err := NewSearchCarHandler(client, searchplan.NewCompiler(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	agentSession := remoteSearchSession()
	ctx := log.WithTraceID(context.Background(), "remote-search-pipeline")

	first, err := handler.Handle(ctx, agentSession, &SearchCarInput{Operation: OperationSearchNow, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.NewReducer().Apply(agentSession, first.Deltas...); err != nil {
		t.Fatal(err)
	}
	if agentSession.Search.Baseline == nil || agentSession.Search.Baseline.ContextID == "" || len(agentSession.Search.Baseline.Menu) == 0 {
		t.Fatalf("missing baseline: %#v", agentSession.Search.Baseline)
	}
	baselineContextID := agentSession.Search.Baseline.ContextID
	baselineMenuCount := len(agentSession.Search.Baseline.Menu)

	if first.Status == ResultSuccess || first.Status == ResultPartial {
		next, err := handler.Handle(ctx, agentSession, &SearchCarInput{Operation: OperationNextBatch, PageSize: 10})
		if err != nil {
			t.Fatal(err)
		}
		if err := session.NewReducer().Apply(agentSession, next.Deltas...); err != nil {
			t.Fatal(err)
		}
		if next.RequestPage < first.RequestPage && next.Status != ResultNoResults {
			t.Fatalf("continuation moved backwards: first=%#v next=%#v", first, next)
		}
	}
	if agentSession.Search.Baseline.ContextID != baselineContextID || len(agentSession.Search.Baseline.Menu) != baselineMenuCount {
		t.Fatalf("filtered/continuation response overwrote baseline: %#v", agentSession.Search.Baseline)
	}
}

func TestRemoteSearchCompilesSeatFromBaselineMenu(t *testing.T) {
	requireRemoteGuide(t)
	cfg, err := config.Load(searchCarConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	client := guide.NewHTTPClient(&guide.HTTPConfig{Endpoint: cfg.Guide.Endpoint, Phone: cfg.Guide.Phone, TimeoutSec: cfg.Guide.Timeout})
	handler, err := NewSearchCarHandler(client, searchplan.NewCompiler(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	agentSession := remoteSearchSession()
	agentSession.Search.RequirementVersion = 1
	agentSession.Search.Requirements = []session.SearchRequirementStateItem{{
		ID: "seat:7", Facet: "seat_num", RawText: "7座", RawValue: "7", CanonicalValue: "7",
		Operator: "eq", Importance: "hard", Status: "active",
	}}
	ctx := log.WithTraceID(context.Background(), "remote-search-seat")
	result, err := handler.Handle(ctx, agentSession, &SearchCarInput{Operation: OperationSearchNow, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.NewReducer().Apply(agentSession, result.Deltas...); err != nil {
		t.Fatal(err)
	}
	if agentSession.Search.ActiveSearch == nil {
		t.Fatalf("missing active search: result=%#v", result)
	}
	executionPlan := handler.compileExecutionPlan(ctx, agentSession, agentSession.Search.Baseline)
	if len(executionPlan.FilterPlan.FilterCodes()) != 1 || executionPlan.FilterPlan.FilterCodes()[0] != "filter/seat_num/7" {
		t.Fatalf("unexpected compiled plan: result=%#v snapshot=%#v", result, agentSession.Search.ActiveSearch)
	}
}

func requireRemoteGuide(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_REMOTE_INTEGRATION") != "1" {
		t.Skip("set RUN_REMOTE_INTEGRATION=1 to run real Guide integration tests")
	}
}

func remoteSearchSession() *session.AgentSession {
	pickup := time.Now().Add(24 * time.Hour)
	returnTime := pickup.Add(24 * time.Hour)
	return &session.AgentSession{Search: session.SearchState{
		Location: &session.LocationRef{
			ID: "capital-airport", Name: "北京首都国际机场", CityID: "10",
			Latitude: 40.0801, Longitude: 116.5846,
		},
		PickupTime: &pickup,
		ReturnTime: &returnTime,
		Goal:       session.SearchGoalState{Status: session.SearchGoalActive, NoPreference: true},
	}}
}
