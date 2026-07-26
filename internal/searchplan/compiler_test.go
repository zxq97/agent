package searchplan

import (
	"testing"

	"github.com/zxq97/agent/api/guide"
)

func TestCompilerUsesFacetCodePrefixes(t *testing.T) {
	compiler := NewCompiler()
	plan := compiler.Compile([]Requirement{
		{ID: "seat", Facet: "seat_num", RawText: "7座", CanonicalValue: "7", Operator: "eq", Importance: "hard", Status: "active"},
		{ID: "energy", Facet: "energy_type", RawText: "纯电", CanonicalValue: "纯电", Operator: "eq", Importance: "hard", Status: "active"},
	}, baselineMenu())
	codes := plan.FilterCodes()
	if len(codes) != 2 || codes[0] != "filter/seat_num/7" || codes[1] != "filter/fuel/electric" {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestCompilerNeverFabricatesVehicleEntityCode(t *testing.T) {
	compiler := NewCompiler()
	plan := compiler.Compile([]Requirement{{
		ID: "model", Facet: "vehicle_model", RawText: "Model Y", CanonicalValue: "Model Y", Operator: "eq", Importance: "hard", Status: "active",
	}}, baselineMenu())
	if len(plan.FilterCodes()) != 0 || len(plan.QuoteFilters) != 1 || plan.Resolutions[0].Capability != CapabilityVerifiable {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestCompilerRemovesParentBrandForModel(t *testing.T) {
	compiler := NewCompiler()
	plan := compiler.Compile([]Requirement{
		{ID: "brand", Facet: "brand", RawText: "特斯拉", CanonicalValue: "特斯拉", Operator: "eq", Importance: "hard", Status: "active", EntityID: "brand:tesla"},
		{ID: "model", Facet: "vehicle_model", RawText: "Model Y", CanonicalValue: "Model Y", Operator: "eq", Importance: "hard", Status: "active", EntityID: "model:y", EntityBrandID: "brand:tesla"},
	}, baselineMenu())
	if len(plan.QuoteFilters) != 1 || plan.QuoteFilters[0].RequirementID != "model" {
		t.Fatalf("unexpected quote filters: %#v", plan.QuoteFilters)
	}
}

func TestComfortDoesNotBecomeRankFactor(t *testing.T) {
	plan := NewCompiler().Compile([]Requirement{{
		ID: "comfort", Facet: "comfort_preference", RawText: "坐着舒服", CanonicalValue: "乘坐舒适", Operator: "eq", Importance: "soft", Status: "active",
	}}, baselineMenu())
	if len(plan.RankFactors) != 0 || len(plan.Resolutions) != 1 || plan.Resolutions[0].Capability != CapabilityUnverifiable {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestCompilerBlocksDifferentHardValuesInSameFacet(t *testing.T) {
	plan := NewCompiler().Compile([]Requirement{
		{ID: "brand-toyota", Facet: "brand", CanonicalValue: "丰田", Operator: "eq", Importance: "hard", EntityID: "brand:toyota"},
		{ID: "brand-tesla", Facet: "brand", CanonicalValue: "特斯拉", Operator: "eq", Importance: "hard", EntityID: "brand:tesla"},
	}, nil)
	if blocking := plan.FirstBlockingResolution(); blocking == nil || blocking.ReasonCode != "conflicting_requirements" {
		t.Fatalf("expected a blocking conflict, got %#v", plan.Resolutions)
	}
	if len(plan.QuoteFilters) != 0 {
		t.Fatalf("conflicting values became accidental AND filters: %#v", plan.QuoteFilters)
	}
}

func TestCompilerBlocksConflictingBrandAndModel(t *testing.T) {
	plan := NewCompiler().Compile([]Requirement{
		{ID: "brand-toyota", Facet: "brand", CanonicalValue: "丰田", Operator: "eq", Importance: "hard", EntityID: "brand:toyota"},
		{ID: "model-y", Facet: "vehicle_model", CanonicalValue: "Model Y", Operator: "eq", Importance: "hard", EntityID: "model:tesla:model-y", EntityBrandID: "brand:tesla"},
	}, nil)
	if blocking := plan.FirstBlockingResolution(); blocking == nil || blocking.ReasonCode != "conflicting_requirements" {
		t.Fatalf("expected hierarchy conflict, got %#v", plan.Resolutions)
	}
}

func baselineMenu() []guide.MenuGroup {
	return []guide.MenuGroup{
		{Name: "推荐排序", GroupItems: []guide.GroupItem{{Items: []guide.Item{{Name: "总价最低", ItemCode: "sort_total_price_asc"}}}}},
		{Name: "快速选车", GroupItems: []guide.GroupItem{
			{Items: []guide.Item{{Name: "一年新车", ItemCode: "filter/car_age/2"}}},
			{Items: []guide.Item{{Name: "7座", ItemCode: "filter/seat_num/7"}}},
			{Items: []guide.Item{{Name: "自动", ItemCode: "filter/transmission/auto"}}},
			{Items: []guide.Item{{Name: "纯电动", ItemCode: "filter/fuel/electric"}}},
			{Items: []guide.Item{{Name: "SUV", ItemCode: "filter/vehcle_choice/suv"}, {Name: "舒适型", ItemCode: "filter/vehcle_choice/shushi"}}},
		}},
	}
}
