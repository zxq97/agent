package webchat

import (
	"strings"

	"github.com/zxq97/agent/internal/domain/generalreply"
	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/domain/vehiclerequirement"
	"github.com/zxq97/agent/internal/orchestrator"
	"github.com/zxq97/agent/internal/router"
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
	for _, candidate := range routes.Candidates {
		switch candidate.Action {
		case router.ActionModifyRentalContext:
			request.RentalContext = &rentalcontext.ModifyRentalContextInput{SourceText: candidate.EvidenceText}
		case router.ActionUpdateVehicleRequirements:
			request.VehicleRequirement = &vehiclerequirement.UpdateInput{SourceText: candidate.EvidenceText}
		case router.ActionRequestVehicleSearch:
			request.SearchRequest = &searchcar.SearchCarInput{
				Operation:    searchcar.ParseOperation(candidate.EvidenceText),
				EvidenceText: candidate.EvidenceText,
			}
		case router.ActionGeneralReply:
			request.GeneralReply.SourceText = appendUniqueReplyText(request.GeneralReply.SourceText, candidate.EvidenceText)
		}
	}
	request.GeneralReply.SourceText = appendUniqueReplyText(request.GeneralReply.SourceText, routes.UnassignedText)
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
