package vehiclerequirement

import (
	"testing"

	"github.com/zxq97/agent/internal/session"
	"github.com/zxq97/agent/internal/vehiclecatalog"
)

func TestNormalizeVehicleModelUsesCatalogAndBrandHint(t *testing.T) {
	handler := &Handler{entities: vehiclecatalog.NewDefaultCatalog()}
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
	handler := &Handler{entities: vehiclecatalog.NewDefaultCatalog()}
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
