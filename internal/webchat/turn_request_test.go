package webchat

import (
	"testing"

	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/planner"
	"github.com/zxq97/agent/internal/router"
)

func TestBuildTurnRequestMapsAllActionsInOnePass(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "旧消息1"},
		{Role: "assistant", Content: "旧回复1"},
	}
	routes := &router.RouteResult{
		Candidates: []router.RouteCandidate{
			{Action: router.ActionModifyRentalContext, EvidenceText: "明天虹桥取"},
			{Action: router.ActionUpdateVehicleRequirements, EvidenceText: "要七座SUV"},
			{Action: router.ActionRequestVehicleSearch, EvidenceText: "换一批"},
			{Action: router.ActionGeneralReply, EvidenceText: "顺便说下规则"},
		},
		UnassignedText: "还有这部分",
	}

	request := buildTurnRequest("明天虹桥取，要七座SUV，换一批，顺便说下规则，还有这部分", history, routes)

	if request.RentalContext == nil || request.RentalContext.SourceText != "明天虹桥取" {
		t.Fatalf("rental input=%#v", request.RentalContext)
	}
	if request.VehicleRequirement == nil || request.VehicleRequirement.SourceText != "要七座SUV" {
		t.Fatalf("requirement input=%#v", request.VehicleRequirement)
	}
	if request.SearchRequest == nil || request.SearchRequest.Operation != searchcar.OperationNextBatch ||
		request.SearchRequest.EvidenceText != "换一批" {
		t.Fatalf("search input=%#v", request.SearchRequest)
	}
	if request.GeneralReply == nil || request.GeneralReply.SourceText != "顺便说下规则\n还有这部分" ||
		len(request.GeneralReply.RecentMessages) != len(history) {
		t.Fatalf("general input=%#v", request.GeneralReply)
	}
}

func TestBuildTurnRequestMapsComparisonAndRentalRules(t *testing.T) {
	request := buildTurnRequest("对比1和2，再说下押金规则", nil, &router.RouteResult{
		Candidates: []router.RouteCandidate{
			{Action: router.ActionCompareVehicles, EvidenceText: "对比1和2"},
			{Action: router.ActionQueryRentalRules, EvidenceText: "押金规则"},
		},
	})
	if request.VehicleComparison == nil ||
		request.VehicleComparison.EvidenceText != "对比1和2" ||
		request.RentalRules == nil ||
		request.RentalRules.EvidenceText != "押金规则" ||
		request.Plan.Action(planner.ActionCompareVehicles) == nil ||
		request.Plan.Action(planner.ActionQueryRentalRules) == nil {
		t.Fatalf("unexpected request: %#v", request)
	}
}

func TestBuildTurnRequestDeduplicatesGeneralTextExactly(t *testing.T) {
	routes := &router.RouteResult{
		Candidates:     []router.RouteCandidate{{Action: router.ActionGeneralReply, EvidenceText: "解释规则"}},
		UnassignedText: "解释规则",
	}
	request := buildTurnRequest("解释规则", nil, routes)
	if request.GeneralReply.SourceText != "解释规则" {
		t.Fatalf("general text=%q", request.GeneralReply.SourceText)
	}
}

func TestBuildTurnRequestNormalizesNoPreferenceOutsidePolicy(t *testing.T) {
	request := buildTurnRequest("车型都可以，直接搜", nil, &router.RouteResult{
		Candidates: []router.RouteCandidate{{
			Action: router.ActionRequestVehicleSearch, EvidenceText: "车型都可以，直接搜",
		}},
	})
	if request.SearchRequest == nil ||
		request.SearchRequest.Operation != searchcar.OperationSearchNow ||
		!request.SearchRequest.NoPreferenceExplicit {
		t.Fatalf("search control was not normalized: %#v", request.SearchRequest)
	}
}
