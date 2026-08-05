package vehiclecompare

import (
	"context"
	"testing"

	"github.com/zxq97/agent/internal/searchruntime"
	"github.com/zxq97/agent/internal/session"
)

func TestHandlerComparesSelectedCurrentQuotes(t *testing.T) {
	agentSession := comparisonSession()
	result, err := NewHandler().Handle(context.Background(), agentSession, &Input{EvidenceText: "对比1和2"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSuccess || result.Comparison == nil ||
		len(result.Comparison.Options) != 2 ||
		len(result.Comparison.Highlights.LowestTotalPriceIndexes) != 1 ||
		result.Comparison.Highlights.LowestTotalPriceIndexes[0] != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestHandlerRequiresSelectionWhenManyQuotesExist(t *testing.T) {
	agentSession := comparisonSession()
	third := session.VehicleResultRef{Index: 2, VehicleCode: "car-3", ReferenceID: "ref-3"}
	agentSession.Search.LastResults = append(agentSession.Search.LastResults, third)
	agentSession.Search.ActiveSearch.Batches[0].Vehicles = append(
		agentSession.Search.ActiveSearch.Batches[0].Vehicles,
		searchruntime.Quote{Vehicle: &searchruntime.Vehicle{Name: "车辆三", Code: "car-3"}, Reference: &searchruntime.Reference{ID: "ref-3"}},
	)
	result, err := NewHandler().Handle(context.Background(), agentSession, &Input{EvidenceText: "对比一下"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusNeedsSelection || len(result.Candidates) != 3 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestSelectOptionsDoesNotTreatModelNameNumberAsOptionIndex(t *testing.T) {
	values := []Option{
		{Index: 1, VehicleName: "Model 3"},
		{Index: 2, VehicleName: "Model Y"},
		{Index: 3, VehicleName: "其他车辆"},
	}
	selected := selectOptions("对比Model 3和Model Y", values)
	if len(selected) != 2 || selected[0].Index != 1 || selected[1].Index != 2 {
		t.Fatalf("unexpected selected options: %#v", selected)
	}
}

func comparisonSession() *session.AgentSession {
	quotes := []searchruntime.Quote{
		{SupplierDisplayName: "供应商A", Vehicle: &searchruntime.Vehicle{Name: "车辆一", Code: "car-1", Seats: 5}, TotalCharge: &searchruntime.Charge{TotalAmount: 500}, Reference: &searchruntime.Reference{ID: "ref-1"}},
		{SupplierDisplayName: "供应商B", Vehicle: &searchruntime.Vehicle{Name: "车辆二", Code: "car-2", Seats: 7}, TotalCharge: &searchruntime.Charge{TotalAmount: 450}, Reference: &searchruntime.Reference{ID: "ref-2"}},
	}
	return &session.AgentSession{Search: session.SearchState{
		LastResults: []session.VehicleResultRef{
			{Index: 0, VehicleCode: "car-1", ReferenceID: "ref-1"},
			{Index: 1, VehicleCode: "car-2", ReferenceID: "ref-2"},
		},
		ActiveSearch: &session.ActiveSearchSnapshot{Batches: []session.SearchResultBatch{{Vehicles: quotes}}},
	}}
}
