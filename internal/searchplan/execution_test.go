package searchplan

import (
	"context"
	"testing"

	"github.com/zxq97/agent/internal/capability"
	"github.com/zxq97/agent/internal/requirement"
)

type noMatchCapabilityMatcher struct{}

func (noMatchCapabilityMatcher) Match(context.Context, *capability.MatchRequest) ([]capability.Match, error) {
	return nil, nil
}

func newTestExecutionCompiler(t *testing.T) *ExecutionCompiler {
	t.Helper()
	resolver, err := capability.NewResolver(capability.NewDefaultCatalog(), noMatchCapabilityMatcher{})
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := NewExecutionCompiler(NewCompiler(), resolver)
	if err != nil {
		t.Fatal(err)
	}
	return compiler
}

func TestNewExecutionCompilerRequiresDependencies(t *testing.T) {
	resolver, err := capability.NewResolver(capability.NewDefaultCatalog(), noMatchCapabilityMatcher{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewExecutionCompiler(nil, resolver); err == nil {
		t.Fatal("expected missing filter compiler error")
	}
	if _, err := NewExecutionCompiler(NewCompiler(), nil); err == nil {
		t.Fatal("expected missing capability resolver error")
	}
}

func TestExecutionCompilerMaterializesSoftExploratoryRequirement(t *testing.T) {
	compiler := newTestExecutionCompiler(t)
	plan := compiler.Compile(context.Background(), []Requirement{{
		ID: "elderly", RawText: "适合带老人出行", SemanticLabel: "elderly_friendly",
		Category: requirement.CategoryUsageScenario, Importance: "soft", Operator: "eq",
	}}, nil, capability.RuntimeContext{
		ResultFields:      map[string]struct{}{"vehicle.seats": {}},
		RentalFingerprint: "rental",
	}, 3)
	if plan.RequirementVersion != 3 || plan.PlanHash == "" ||
		len(plan.Unresolved) != 0 || plan.FirstBlockingResolution() != nil ||
		len(plan.LocalRanks) != 1 || len(plan.FilterPlan.ExploratoryRanks) != 1 ||
		len(plan.FilterPlan.Disclosures) != 1 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestExecutionCompilerRanksHardOpenRequirementWithoutBlocking(t *testing.T) {
	compiler := newTestExecutionCompiler(t)
	plan := compiler.Compile(context.Background(), []Requirement{{
		ID: "elderly", RawText: "必须适合老人", SemanticLabel: "elderly_friendly",
		Category: requirement.CategoryUsageScenario, Importance: "hard", Operator: "eq",
	}}, nil, capability.RuntimeContext{ResultFields: map[string]struct{}{"vehicle.seats": {}}}, 1)
	if plan.FirstBlockingResolution() != nil || len(plan.RemoteFilters) != 0 ||
		len(plan.LocalFilters) != 0 || len(plan.LocalRanks) != 1 ||
		len(plan.FilterPlan.Disclosures) != 1 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestExecutionCompilerMaterializesResolvedOpenRank(t *testing.T) {
	compiler := newTestExecutionCompiler(t)
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
