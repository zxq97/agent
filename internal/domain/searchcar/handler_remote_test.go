package searchcar

import (
	"context"
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
		if next.RequestPage < first.RequestPage && next.Status != ResultNoResults {
			t.Fatalf("continuation moved backwards: first=%#v next=%#v", first, next)
		}
	}
	if agentSession.Search.Baseline.ContextID != baselineContextID || len(agentSession.Search.Baseline.Menu) != baselineMenuCount {
		t.Fatalf("filtered/continuation response overwrote baseline: %#v", agentSession.Search.Baseline)
	}
}

func TestRemoteSearchCompilesSeatFromBaselineMenu(t *testing.T) {
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
	result, err := handler.Handle(log.WithTraceID(context.Background(), "remote-search-seat"), agentSession, &SearchCarInput{Operation: OperationSearchNow, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if agentSession.Search.ActiveSearch == nil || len(agentSession.Search.ActiveSearch.Plan.FilterCodes()) != 1 ||
		agentSession.Search.ActiveSearch.Plan.FilterCodes()[0] != "filter/seat_num/7" {
		t.Fatalf("unexpected compiled plan: result=%#v snapshot=%#v", result, agentSession.Search.ActiveSearch)
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
