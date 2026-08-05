package webchat

import (
	"strings"

	"github.com/zxq97/agent/internal/domain/generalreply"
	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/domain/rentalrules"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/domain/vehiclecompare"
	"github.com/zxq97/agent/internal/domain/vehiclerequirement"
	"github.com/zxq97/agent/internal/orchestrator"
	"github.com/zxq97/agent/internal/planner"
	"github.com/zxq97/agent/internal/router"
	"github.com/zxq97/agent/internal/turnnormalizer"
)

const maxGeneralReplyHistory = 6

// buildTurnRequest adapts Router evidence to domain inputs in one pass. Router
// decides which domains should run; each domain still parses and validates its
// own evidence.
func buildTurnRequest(sourceText string, history []Message, routes *router.RouteResult) *orchestrator.TurnRequest {
	request := &orchestrator.TurnRequest{
		SourceText: sourceText,
		GeneralReply: &generalreply.Input{
			RecentMessages: generalReplyHistory(history),
		},
	}
	if routes == nil {
		return request
	}
	var candidates []planner.Candidate
	for _, candidate := range routes.Candidates {
		switch candidate.Action {
		case router.ActionModifyRentalContext:
			candidates = append(candidates, planner.Candidate{Type: planner.ActionModifyRentalContext, EvidenceText: candidate.EvidenceText})
		case router.ActionUpdateVehicleRequirements:
			candidates = append(candidates, planner.Candidate{Type: planner.ActionUpdateVehicleRequirements, EvidenceText: candidate.EvidenceText})
		case router.ActionRequestVehicleSearch:
			candidates = append(candidates, planner.Candidate{Type: planner.ActionExecuteVehicleSearch, EvidenceText: candidate.EvidenceText})
		case router.ActionCompareVehicles:
			candidates = append(candidates, planner.Candidate{Type: planner.ActionCompareVehicles, EvidenceText: candidate.EvidenceText})
		case router.ActionQueryRentalRules:
			candidates = append(candidates, planner.Candidate{Type: planner.ActionQueryRentalRules, EvidenceText: candidate.EvidenceText})
		case router.ActionGeneralReply:
			candidates = append(candidates, planner.Candidate{Type: planner.ActionGeneralReply, EvidenceText: candidate.EvidenceText})
		}
	}
	if strings.TrimSpace(routes.UnassignedText) != "" {
		candidates = append(candidates, planner.Candidate{Type: planner.ActionGeneralReply, EvidenceText: routes.UnassignedText})
	}
	request.Plan = planner.New().Build(candidates)
	if action := request.Plan.Action(planner.ActionModifyRentalContext); action != nil {
		request.RentalContext = &rentalcontext.Input{SourceText: action.EvidenceText}
	}
	if action := request.Plan.Action(planner.ActionUpdateVehicleRequirements); action != nil {
		request.VehicleRequirement = &vehiclerequirement.Input{SourceText: action.EvidenceText}
	}
	if action := request.Plan.Action(planner.ActionExecuteVehicleSearch); action != nil {
		signals := turnnormalizer.NormalizeSearch(action.EvidenceText)
		request.SearchRequest = &searchcar.Input{
			Operation:            searchcar.SearchOperation(signals.Operation),
			EvidenceText:         action.EvidenceText,
			NoPreferenceExplicit: signals.NoPreference,
		}
	}
	if action := request.Plan.Action(planner.ActionCompareVehicles); action != nil {
		request.VehicleComparison = &vehiclecompare.Input{EvidenceText: action.EvidenceText}
	}
	if action := request.Plan.Action(planner.ActionQueryRentalRules); action != nil {
		request.RentalRules = &rentalrules.Input{EvidenceText: action.EvidenceText}
	}
	if action := request.Plan.Action(planner.ActionGeneralReply); action != nil {
		request.GeneralReply.SourceText = action.EvidenceText
	}
	return request
}

func generalReplyHistory(history []Message) []generalreply.Message {
	if len(history) > maxGeneralReplyHistory {
		history = history[len(history)-maxGeneralReplyHistory:]
	}
	result := make([]generalreply.Message, 0, len(history))
	for _, message := range history {
		result = append(result, generalreply.Message{Role: message.Role, Content: message.Content})
	}
	return result
}

func appendUniqueReplyText(current, addition string) string {
	addition = strings.TrimSpace(addition)
	if addition == "" {
		return strings.TrimSpace(current)
	}
	for _, existing := range strings.Split(current, "\n") {
		if strings.TrimSpace(existing) == addition {
			return strings.TrimSpace(current)
		}
	}
	if strings.TrimSpace(current) == "" {
		return addition
	}
	return strings.TrimSpace(current) + "\n" + addition
}
