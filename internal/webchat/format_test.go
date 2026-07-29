package webchat

import (
	"strings"
	"testing"

	"github.com/zxq97/agent/internal/capability"
	"github.com/zxq97/agent/internal/domain/rentalrules"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/domain/vehiclecompare"
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

func TestFormatTurnExposesComparisonAndRentalRules(t *testing.T) {
	total := 500.0
	response := formatTurn(&session.AgentSession{}, &orchestrator.TurnResult{
		VehicleComparison: &vehiclecompare.Result{
			Status:  vehiclecompare.StatusSuccess,
			Message: "已完成对比。",
			Comparison: &vehiclecompare.Comparison{Options: []vehiclecompare.Option{{
				Index: 1, VehicleName: "车辆一", TotalAmount: &total,
			}}},
		},
		RentalRules: &rentalrules.Result{
			Status:  rentalrules.StatusSuccess,
			Message: "以下为通用指引。",
			Rules: []rentalrules.Rule{{
				ID: "deposit", Title: "押金", Guidance: "以订单为准。",
			}},
		},
	})
	if response.VehicleComparison == nil ||
		len(response.VehicleComparison.Options) != 1 ||
		len(response.RentalRules) != 1 ||
		!strings.Contains(response.Message, "已完成对比") ||
		!strings.Contains(response.Message, "以下为通用指引") {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestCloneTurnResponseDeepCopiesComparison(t *testing.T) {
	total := 500.0
	spread := 50.0
	value := TurnResponse{VehicleComparison: &VehicleComparisonView{
		Options: []ComparisonOptionView{{Index: 1, TotalAmount: &total}},
		Highlights: &ComparisonHighlights{
			LowestTotalPriceIndexes: []int{1},
			TotalPriceSpread:        &spread,
		},
	}}
	cloned := cloneTurnResponse(value)
	*value.VehicleComparison.Options[0].TotalAmount = 600
	value.VehicleComparison.Highlights.LowestTotalPriceIndexes[0] = 2
	*value.VehicleComparison.Highlights.TotalPriceSpread = 100
	if *cloned.VehicleComparison.Options[0].TotalAmount != 500 ||
		cloned.VehicleComparison.Highlights.LowestTotalPriceIndexes[0] != 1 ||
		*cloned.VehicleComparison.Highlights.TotalPriceSpread != 50 {
		t.Fatalf("comparison was not deeply cloned: %#v", cloned.VehicleComparison)
	}
}

func TestFormatTurnAlwaysIncludesRequiredDisclosure(t *testing.T) {
	response := formatTurn(&session.AgentSession{}, &orchestrator.TurnResult{
		SearchCar: &searchcar.SearchCarResult{
			Status: searchcar.ResultPartial,
			Disclosures: []searchplan.Disclosure{{
				RequirementID: "elderly",
				Kind:          searchplan.DisclosureExploratoryRanked,
				Message:       "适合老人未被严格验证，仅按座位数排序。",
				MustMention:   true,
			}},
		},
	})
	if !strings.Contains(response.Message, "适合老人未被严格验证") {
		t.Fatalf("required disclosure missing: %q", response.Message)
	}
}

func TestFormatTurnExposesVehicleLocalVerifier(t *testing.T) {
	requirement := searchcar.RequirementResult{
		ID: "model-y", RawText: "Model Y", Status: "filterable",
		Capability: searchplan.CapabilityFilterable,
	}
	response := formatTurn(&session.AgentSession{}, &orchestrator.TurnResult{
		SearchCar: &searchcar.SearchCarResult{
			Status:                      searchcar.ResultSuccess,
			AppliedRequirements:         []searchcar.RequirementResult{requirement},
			LocallyVerifiedRequirements: []searchcar.RequirementResult{requirement},
		},
	})
	if len(response.RequirementResolutions) != 1 ||
		len(response.RequirementResolutions[0].Executions) != 2 ||
		response.RequirementResolutions[0].Executions[0] != "remote_filter" ||
		response.RequirementResolutions[0].Executions[1] != "local_verifier" {
		t.Fatalf("unexpected resolution views: %#v", response.RequirementResolutions)
	}
}
