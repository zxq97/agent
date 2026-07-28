package webchat

import (
	"testing"

	"github.com/zxq97/agent/internal/capability"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/orchestrator"
	"github.com/zxq97/agent/internal/searchplan"
	"github.com/zxq97/agent/internal/session"
)

func TestFormatTurnExposesExecutedAndUnresolvedRequirements(t *testing.T) {
	response := formatTurn(&session.AgentSession{}, &orchestrator.TurnResult{
		SearchCar: &searchcar.SearchCarResult{
			Status: searchcar.ResultPartial,
			AppliedRequirements: []searchcar.RequirementResult{{
				ID: "seat", RawText: "7座", Status: "filterable",
				Capability: searchplan.CapabilityFilterable,
			}},
			CapabilityResolutions: []capability.Resolution{{
				RequirementID: "elderly", RawText: "适合老人出行",
				Status:     capability.ResolutionInsufficientData,
				ReasonCode: "scenario_model_unavailable",
			}},
		},
	})
	if len(response.RequirementResolutions) != 2 ||
		len(response.RequirementResolutions[0].Executions) != 1 ||
		response.RequirementResolutions[0].Executions[0] != "remote_filter" ||
		response.RequirementResolutions[1].Status != string(capability.ResolutionInsufficientData) ||
		len(response.RequirementResolutions[1].Executions) != 0 {
		t.Fatalf("unexpected resolution views: %#v", response.RequirementResolutions)
	}
}
