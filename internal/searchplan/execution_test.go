package searchplan

import (
	"context"
	"testing"

	"github.com/zxq97/agent/internal/capability"
	"github.com/zxq97/agent/internal/requirement"
)

func TestExecutionCompilerPreservesSoftOpenRequirementAsUnresolved(t *testing.T) {
	compiler := NewExecutionCompiler(NewCompiler(), capability.NewResolver(capability.NewDefaultCatalog(), nil))
	plan := compiler.Compile(context.Background(), []Requirement{{
		ID: "elderly", RawText: "适合带老人出行", SemanticLabel: "elderly_friendly",
		Category: requirement.CategoryUsageScenario, Importance: "soft", Operator: "eq",
	}}, nil, capability.RuntimeContext{
		ResultFields:      map[string]struct{}{"vehicle.seats": {}},
		RentalFingerprint: "rental",
	}, 3)
	if plan.RequirementVersion != 3 || plan.PlanHash == "" ||
		len(plan.Unresolved) != 1 || plan.FirstBlockingResolution() != nil ||
		len(plan.FilterPlan.Resolutions) != 1 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestExecutionCompilerBlocksHardOpenRequirement(t *testing.T) {
	compiler := NewExecutionCompiler(NewCompiler(), capability.NewResolver(capability.NewDefaultCatalog(), nil))
	plan := compiler.Compile(context.Background(), []Requirement{{
		ID: "elderly", RawText: "必须适合老人", SemanticLabel: "elderly_friendly",
		Category: requirement.CategoryUsageScenario, Importance: "hard", Operator: "eq",
	}}, nil, capability.RuntimeContext{ResultFields: map[string]struct{}{}}, 1)
	if plan.FirstBlockingResolution() == nil || len(plan.RemoteFilters) != 0 ||
		len(plan.LocalFilters) != 0 || len(plan.LocalRanks) != 0 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestExecutionCompilerMaterializesResolvedOpenRank(t *testing.T) {
	compiler := NewExecutionCompiler(NewCompiler(), capability.NewResolver(capability.NewDefaultCatalog(), nil))
	plan := compiler.Compile(context.Background(), []Requirement{{
		ID: "budget", RawText: "优先便宜的", SemanticLabel: "budget_friendly",
		Category: requirement.CategoryPreference, Importance: "soft", Operator: "eq",
	}}, nil, capability.RuntimeContext{
		ResultFields: map[string]struct{}{"total_charge.total_amount": {}},
	}, 1)
	if len(plan.LocalRanks) != 1 || len(plan.FilterPlan.RankFactors) != 1 ||
		plan.FilterPlan.RankFactors[0].Type != RankPriceLow {
		t.Fatalf("resolved execution was not materialized: %#v", plan)
	}
}
