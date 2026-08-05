package vehiclerequirement

import (
	"context"
	"testing"

	"github.com/zxq97/agent/internal/requirement"
	"github.com/zxq97/agent/internal/session"
	"github.com/zxq97/agent/internal/vehiclecatalog"
)

type staticExtractor struct {
	result *ExtractResult
}

func (e staticExtractor) Extract(context.Context, *ExtractionInput) (*ExtractResult, error) {
	return e.result, nil
}

func TestNormalizeVehicleModelUsesCatalogAndBrandHint(t *testing.T) {
	handler := &handler{entities: vehiclecatalog.NewDefaultCatalog()}
	item := handler.normalize(Requirement{
		Facet:      FacetVehicleModel,
		RawText:    "特斯拉 ModelY",
		RawValue:   "ModelY",
		Operation:  OperationReplace,
		Operator:   OperatorEQ,
		Importance: ImportanceHard,
		EntityContext: EntityContext{
			BrandHint: "特斯拉",
		},
	})
	if item.CanonicalValue != "Model Y" || item.EntityID == "" || item.EntityBrandID != "brand:tesla" {
		t.Fatalf("unexpected item: %#v", item)
	}
}

func TestMergePersistsNegativeOperator(t *testing.T) {
	operation := Requirement{Facet: FacetEnergyType, RawText: "不要燃油车", RawValue: "汽油", Operation: OperationAdd, Operator: OperatorNotEQ, Importance: ImportanceHard}
	item := session.SearchRequirementStateItem{ID: "energy", Facet: "energy_type", RawText: operation.RawText, RawValue: operation.RawValue, CanonicalValue: "汽油", Operator: "not_eq", Importance: "hard", Status: "active"}
	result, changed := merge(nil, []Requirement{operation}, []session.SearchRequirementStateItem{item})
	if !changed || len(result) != 1 || result[0].Operator != "not_eq" {
		t.Fatalf("result=%#v changed=%v", result, changed)
	}
}

func TestReplacingWithSameSemanticRequirementDoesNotChangeVersionInput(t *testing.T) {
	current := []session.SearchRequirementStateItem{{ID: "seat", Facet: "seat_num", CanonicalValue: "7", Operator: "eq", Importance: "hard", Status: "active"}}
	operation := Requirement{Facet: FacetSeatNum, RawText: "还是7座", RawValue: "7", Operation: OperationReplace, Operator: OperatorEQ, Importance: ImportanceHard}
	normalized := current[0]
	normalized.RawText = operation.RawText
	result, changed := merge(current, []Requirement{operation}, []session.SearchRequirementStateItem{normalized})
	if changed || len(result) != 1 {
		t.Fatalf("result=%#v changed=%v", result, changed)
	}
}

func TestRemoveEntityAliasMatchesCanonicalValue(t *testing.T) {
	handler := &handler{entities: vehiclecatalog.NewDefaultCatalog()}
	operation := Requirement{Facet: FacetBrand, RawText: "不要Tesla限制了", RawValue: "Tesla", Operation: OperationRemove, Operator: OperatorEQ, Importance: ImportanceHard}
	normalized := handler.normalize(operation)
	current := []session.SearchRequirementStateItem{{ID: "brand", Facet: "brand", CanonicalValue: "特斯拉", Operator: "eq", Importance: "hard", Status: "active"}}
	result, changed := merge(current, []Requirement{operation}, []session.SearchRequirementStateItem{normalized})
	if !changed || len(result) != 0 {
		t.Fatalf("result=%#v changed=%v normalized=%#v", result, changed, normalized)
	}
}

func TestReplacingBrandRemovesOlderSpecificVehicleEntities(t *testing.T) {
	current := []session.SearchRequirementStateItem{
		{ID: "model-y", Facet: "vehicle_model", CanonicalValue: "Model Y", Operator: "eq", Importance: "hard", EntityBrandID: "brand:tesla"},
		{ID: "seat-7", Facet: "seat_num", CanonicalValue: "7", Operator: "eq", Importance: "hard"},
	}
	operation := Requirement{Facet: FacetBrand, RawText: "看一下小米", RawValue: "小米", Operation: OperationReplace, Operator: OperatorEQ, Importance: ImportanceHard}
	normalized := session.SearchRequirementStateItem{
		ID: "xiaomi", Facet: "brand", CanonicalValue: "小米", Operator: "eq", Importance: "hard", EntityID: "brand:xiaomi",
	}
	result, changed := merge(current, []Requirement{operation}, []session.SearchRequirementStateItem{normalized})
	if !changed || len(result) != 2 {
		t.Fatalf("result=%#v changed=%v", result, changed)
	}
	if result[0].Facet != "seat_num" || result[1].Facet != "brand" {
		t.Fatalf("stale model was not removed: %#v", result)
	}
}

func TestReplacingModelRemovesConflictingBrandButKeepsIndependentFacet(t *testing.T) {
	current := []session.SearchRequirementStateItem{
		{ID: "toyota", Facet: "brand", CanonicalValue: "丰田", Operator: "eq", Importance: "hard", EntityID: "brand:toyota"},
		{ID: "seat-7", Facet: "seat_num", CanonicalValue: "7", Operator: "eq", Importance: "hard"},
	}
	operation := Requirement{Facet: FacetVehicleModel, RawText: "改成 Model Y", RawValue: "Model Y", Operation: OperationReplace, Operator: OperatorEQ, Importance: ImportanceHard}
	normalized := session.SearchRequirementStateItem{
		ID: "model-y", Facet: "vehicle_model", CanonicalValue: "Model Y", Operator: "eq", Importance: "hard",
		EntityID: "model:tesla:model-y", EntityBrandID: "brand:tesla",
	}
	result, changed := merge(current, []Requirement{operation}, []session.SearchRequirementStateItem{normalized})
	if !changed || len(result) != 2 {
		t.Fatalf("result=%#v changed=%v", result, changed)
	}
	if result[0].Facet != "seat_num" || result[1].Facet != "vehicle_model" {
		t.Fatalf("conflicting brand was not removed: %#v", result)
	}
}

func TestHandlerReturnsDeltaWithoutMutatingSession(t *testing.T) {
	handler, err := NewHandler(staticExtractor{result: &ExtractResult{
		DomainMatched: true,
		Requirements: []Requirement{{
			Facet: FacetSeatNum, RawText: "7座", RawValue: "7", Operation: OperationAdd,
			Operator: OperatorEQ, Importance: ImportanceHard, Confidence: 1,
		}},
	}}, vehiclecatalog.NewDefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}
	agentSession := &session.AgentSession{}
	result, err := handler.Handle(context.Background(), agentSession, &Input{SourceText: "想要7座"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || len(result.Deltas) != 1 || len(agentSession.Search.Requirements) != 0 {
		t.Fatalf("result=%#v session=%#v", result, agentSession)
	}
	if err := session.NewReducer().Apply(agentSession, result.Deltas...); err != nil {
		t.Fatal(err)
	}
	if len(agentSession.Search.Requirements) != 1 || agentSession.Search.RequirementVersion != 1 {
		t.Fatalf("session=%#v", agentSession)
	}
}

func TestHandlerPreservesOpenSemanticRequirement(t *testing.T) {
	handler, err := NewHandler(staticExtractor{result: &ExtractResult{
		DomainMatched: true,
		Requirements: []Requirement{{
			RawText:       "适合带老人出行",
			SemanticLabel: "elderly_friendly",
			Category:      requirement.CategoryUsageScenario,
			Value:         requirement.Value{Kind: requirement.ValueNone},
			Operation:     OperationAdd,
			Operator:      OperatorEQ,
			Importance:    ImportanceSoft,
			Confidence:    0.9,
		}},
	}}, vehiclecatalog.NewDefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}
	agentSession := &session.AgentSession{}
	result, err := handler.Handle(context.Background(), agentSession, &Input{SourceText: "适合带老人出行"})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.NewReducer().Apply(agentSession, result.Deltas...); err != nil {
		t.Fatal(err)
	}
	if len(agentSession.Search.Requirements) != 1 {
		t.Fatalf("requirements=%#v", agentSession.Search.Requirements)
	}
	stored := agentSession.Search.Requirements[0]
	if stored.Facet != "" || stored.CanonicalType != "" ||
		stored.SemanticLabel != "elderly_friendly" ||
		stored.Category != requirement.CategoryUsageScenario ||
		stored.ID == "" {
		t.Fatalf("stored=%#v", stored)
	}
}

func TestNormalizeTypedTotalBudgetPreservesCompilerContract(t *testing.T) {
	amount := 300.0
	handler := &handler{entities: vehiclecatalog.NewDefaultCatalog()}
	item := handler.normalize(Requirement{
		RawText: "300元以内", Category: requirement.CategoryPrice,
		CanonicalType: FacetPricePreference,
		Value:         requirement.Value{Kind: requirement.ValueNumber, Number: &amount, Unit: "total_CNY"},
		Operation:     OperationReplace,
		Operator:      OperatorLTE,
		Importance:    ImportanceHard,
	})
	if item.CanonicalValue != "total<=300CNY" || item.RawValue != "300" {
		t.Fatalf("unexpected budget normalization: %#v", item)
	}
}

func TestNormalizeVehicleAnyOfKeepsOneRequirement(t *testing.T) {
	handler := &handler{entities: vehiclecatalog.NewDefaultCatalog()}
	item := handler.normalize(Requirement{
		RawText: "宝马或Model Y", SemanticLabel: "vehicle_entity_any_of",
		Category: requirement.CategoryVehicle, Operation: OperationReplace,
		Relation: RelationAnyOf, Operator: OperatorIN, Importance: ImportanceHard,
		Alternatives: []ConstraintAlternative{
			{CanonicalType: FacetBrand, Value: requirement.Value{Kind: requirement.ValueText, Text: "宝马"}},
			{CanonicalType: FacetVehicleModel, Value: requirement.Value{Kind: requirement.ValueText, Text: "Model Y"}, EntityContext: EntityContext{BrandHint: "特斯拉"}},
		},
	})
	if item.Facet != "vehicle_entity_any_of" || len(item.Alternatives) != 2 ||
		item.Alternatives[0].EntityID != "brand:bmw" ||
		item.Alternatives[1].EntityID != "model:tesla:model-y" {
		t.Fatalf("unexpected normalized OR: %#v", item)
	}
}

func TestMergeKeepsDistinctOpenRequirements(t *testing.T) {
	handler := &handler{entities: vehiclecatalog.NewDefaultCatalog()}
	operations := []Requirement{
		{
			RawText: "适合老人出行", SemanticLabel: "elderly_friendly",
			Category: requirement.CategoryUsageScenario, Value: requirement.Value{Kind: requirement.ValueNone},
			Operation: OperationAdd, Operator: OperatorEQ, Importance: ImportanceSoft,
		},
		{
			RawText: "适合冬季驾驶", SemanticLabel: "winter_driving",
			Category: requirement.CategoryUsageScenario, Value: requirement.Value{Kind: requirement.ValueNone},
			Operation: OperationAdd, Operator: OperatorEQ, Importance: ImportanceSoft,
		},
	}
	normalized := []session.SearchRequirementStateItem{
		handler.normalize(operations[0]),
		handler.normalize(operations[1]),
	}
	result, changed := merge(nil, operations, normalized)
	if !changed || len(result) != 2 || result[0].ID == result[1].ID {
		t.Fatalf("distinct open requirements were collapsed: %#v", result)
	}
}

func TestOpenRequirementIdentityDoesNotDependOnSemanticLabel(t *testing.T) {
	first := Requirement{
		RawText: "适合带老人出行", SemanticLabel: "elderly_friendly",
		Category: requirement.CategoryUsageScenario, Operator: OperatorEQ,
	}
	second := first
	second.SemanticLabel = "senior_trip"
	if semanticRequirementID(first) != semanticRequirementID(second) {
		t.Fatalf("semantic label changed server identity: %q != %q", semanticRequirementID(first), semanticRequirementID(second))
	}
}

func TestOpenRequirementDoesNotMatchBySemanticLabelAlone(t *testing.T) {
	left := session.SearchRequirementStateItem{
		ID: "left", RawText: "适合老人出行", SemanticLabel: "trip",
		Category: requirement.CategoryUsageScenario,
	}
	right := session.SearchRequirementStateItem{
		ID: "right", RawText: "适合儿童出行", SemanticLabel: "trip",
		Category: requirement.CategoryUsageScenario,
	}
	if openRequirementMatches(left, right) {
		t.Fatal("different open requirements matched only because their labels were equal")
	}
}

func TestOpenRequirementMatchesStableSemanticLabelAcrossWording(t *testing.T) {
	left := session.SearchRequirementStateItem{
		ID: "left", RawText: "适合老人出行", SemanticLabel: "elderly_friendly",
		Category: requirement.CategoryUsageScenario,
	}
	right := session.SearchRequirementStateItem{
		ID: "right", RawText: "老人上下车方便", SemanticLabel: "elderly_friendly",
		Category: requirement.CategoryUsageScenario,
	}
	if !openRequirementMatches(left, right) {
		t.Fatal("stable semantic label did not match synonymous open requirements")
	}
}
