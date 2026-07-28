package planner

import "testing"

func TestBuildOrdersAndDeduplicatesActions(t *testing.T) {
	plan := New().Build([]Candidate{
		{Type: ActionExecuteVehicleSearch, EvidenceText: "直接搜"},
		{Type: ActionUpdateVehicleRequirements, EvidenceText: "7座"},
		{Type: ActionModifyRentalContext, EvidenceText: "虹桥取车"},
		{Type: ActionUpdateVehicleRequirements, EvidenceText: "SUV"},
		{Type: ActionExecuteVehicleSearch, EvidenceText: "直接搜"},
	})
	if len(plan.Actions) != 3 ||
		plan.Actions[0].Type != ActionModifyRentalContext ||
		plan.Actions[1].Type != ActionUpdateVehicleRequirements ||
		plan.Actions[2].Type != ActionExecuteVehicleSearch ||
		plan.Actions[1].EvidenceText != "7座\nSUV" {
		t.Fatalf("plan=%#v", plan)
	}
	if len(plan.Actions[2].DependsOn) != 1 || plan.Actions[2].DependsOn[0] != plan.Actions[1].ID {
		t.Fatalf("dependencies=%#v", plan.Actions[2].DependsOn)
	}
}

func TestMergeAddsDeferredActionOnlyOnce(t *testing.T) {
	planner := New()
	base := planner.Build([]Candidate{{Type: ActionModifyRentalContext, EvidenceText: "虹桥"}})
	plan := planner.Merge(base, []Candidate{
		{Type: ActionExecuteVehicleSearch, EvidenceText: "直接搜", SourceID: "deferred-search"},
		{Type: ActionExecuteVehicleSearch, EvidenceText: "直接搜", SourceID: "deferred-search"},
	})
	if len(plan.Actions) != 2 || plan.Action(ActionExecuteVehicleSearch) == nil ||
		plan.Action(ActionExecuteVehicleSearch).DeferredID != "deferred-search" {
		t.Fatalf("plan=%#v", plan)
	}
}

func TestActionPlanBindsBaseVersion(t *testing.T) {
	plan := New().Build([]Candidate{
		{Type: ActionModifyRentalContext},
		{Type: ActionExecuteVehicleSearch},
	})
	plan.BindBaseVersion(7)
	for _, action := range plan.Actions {
		if action.BaseVersion != 7 {
			t.Fatalf("action did not inherit turn base version: %#v", action)
		}
	}
}
