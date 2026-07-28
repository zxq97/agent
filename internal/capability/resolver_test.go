package capability

import (
	"context"
	"testing"

	"github.com/zxq97/agent/internal/requirement"
)

type staticMatcher struct {
	matches []Match
}

func (m staticMatcher) Match(context.Context, *MatchRequest) ([]Match, error) {
	return m.matches, nil
}

func TestResolverKeepsScenarioRelevantButUnexecutable(t *testing.T) {
	resolver := NewResolver(NewDefaultCatalog(), nil)
	result := resolver.Resolve(context.Background(), Requirement{
		ID: "elderly", RawText: "适合带老人出行", SemanticLabel: "elderly_friendly",
		Category: requirement.CategoryUsageScenario, Importance: "soft",
	}, RuntimeContext{ResultFields: guideFields()})
	if result.Status != ResolutionInsufficientData || len(result.Executions) != 0 ||
		result.ReasonCode != "scenario_model_unavailable" {
		t.Fatalf("unexpected resolution: %#v", result)
	}
}

func TestResolverExecutesSoftRankOnlyWithRequiredField(t *testing.T) {
	resolver := NewResolver(NewDefaultCatalog(), nil)
	value := Requirement{
		ID: "budget", RawText: "优先便宜的", SemanticLabel: "budget_friendly",
		Category: requirement.CategoryPreference, Importance: "soft",
	}
	resolved := resolver.Resolve(context.Background(), value, RuntimeContext{ResultFields: guideFields()})
	if resolved.Status != ResolutionResolved || len(resolved.Executions) != 1 ||
		resolved.Executions[0].Mode != ExecutionLocalRank {
		t.Fatalf("unexpected resolution: %#v", resolved)
	}
	unresolved := resolver.Resolve(context.Background(), value, RuntimeContext{ResultFields: map[string]struct{}{}})
	if unresolved.Status != ResolutionInsufficientData || len(unresolved.Executions) != 0 {
		t.Fatalf("unexpected resolution without fields: %#v", unresolved)
	}
}

func TestResolverDoesNotDowngradeHardRequirementToRank(t *testing.T) {
	resolver := NewResolver(NewDefaultCatalog(), nil)
	result := resolver.Resolve(context.Background(), Requirement{
		ID: "budget", RawText: "必须便宜", SemanticLabel: "budget_friendly",
		Category: requirement.CategoryPreference, Importance: "hard",
	}, RuntimeContext{ResultFields: guideFields()})
	if result.Status != ResolutionUnsupported || len(result.Executions) != 0 ||
		result.ReasonCode != "hard_requirement_not_filterable" {
		t.Fatalf("unexpected resolution: %#v", result)
	}
}

func TestResolverRejectsMatcherCapabilityOutsideCandidates(t *testing.T) {
	resolver := NewResolver(NewDefaultCatalog(), staticMatcher{matches: []Match{{
		CapabilityID: "luxury_level", Relation: "exact", Confidence: 1,
	}}})
	result := resolver.Resolve(context.Background(), Requirement{
		ID: "children", RawText: "必须放三个儿童座椅", SemanticLabel: "three_child_seats",
		Category: requirement.CategoryUsageScenario, Importance: "hard",
	}, RuntimeContext{ResultFields: guideFields()})
	if result.Status != ResolutionAmbiguous || len(result.Executions) != 0 {
		t.Fatalf("unexpected resolution: %#v", result)
	}
}

func TestRelevantMatcherRelationNeverCreatesExecution(t *testing.T) {
	resolver := NewResolver(NewDefaultCatalog(), staticMatcher{matches: []Match{{
		CapabilityID: "large_space", Relation: "relevant", Confidence: 0.9,
	}}})
	result := resolver.Resolve(context.Background(), Requirement{
		ID: "children", RawText: "必须放三个儿童座椅", SemanticLabel: "three_child_seats",
		Category: requirement.CategoryUsageScenario, Importance: "hard",
	}, RuntimeContext{ResultFields: map[string]struct{}{"vehicle.wheelbase": {}, "vehicle.size": {}}})
	if result.Status != ResolutionInsufficientData || len(result.Executions) != 0 ||
		result.ReasonCode != "semantic_relation_not_executable" {
		t.Fatalf("unexpected resolution: %#v", result)
	}
}

func TestResolverRejectsRuntimeCatalogVersionDrift(t *testing.T) {
	resolver := NewResolver(NewDefaultCatalog(), nil)
	result := resolver.Resolve(context.Background(), Requirement{
		ID: "budget", RawText: "优先便宜的", SemanticLabel: "budget_friendly",
		Category: requirement.CategoryPreference, Importance: "soft",
	}, RuntimeContext{
		CatalogVersion: "stale-version",
		ResultFields:   guideFields(),
	})
	if result.Status != ResolutionInsufficientData ||
		result.ReasonCode != "catalog_version_mismatch" ||
		len(result.Executions) != 0 {
		t.Fatalf("unexpected resolution: %#v", result)
	}
}

func TestResolverRejectsExecutionWithUnapprovedFieldContract(t *testing.T) {
	catalog := NewCatalog("test", []Definition{{
		ID: "budget_friendly", Name: "价格优先",
		Categories: []requirement.Category{requirement.CategoryPreference},
		Aliases:    []string{"budget_friendly"},
		LocalRank: &ExecutionDefinition{
			RequiredFields: []string{"vehicle.untrusted_score"},
			Operation:      "price_low",
		},
	}})
	resolver := NewResolver(catalog, nil)
	result := resolver.Resolve(context.Background(), Requirement{
		ID: "budget", RawText: "优先便宜", SemanticLabel: "budget_friendly",
		Category: requirement.CategoryPreference, Importance: "soft",
	}, RuntimeContext{
		CatalogVersion: "test",
		ResultFields:   map[string]struct{}{"vehicle.untrusted_score": {}},
	})
	if result.Status == ResolutionResolved || len(result.Executions) != 0 {
		t.Fatalf("unapproved field entered execution plan: %#v", result)
	}
}

func guideFields() map[string]struct{} {
	return map[string]struct{}{"total_charge.total_amount": {}, "vehicle.seats": {}}
}
