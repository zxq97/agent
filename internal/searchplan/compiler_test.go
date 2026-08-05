package searchplan

import (
	"testing"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/requirement"
	"github.com/zxq97/agent/internal/vehiclecatalog"
)

func TestNewCompilerWithVehicleCatalogRequiresCatalog(t *testing.T) {
	if _, err := NewCompilerWithVehicleCatalog(nil); err == nil {
		t.Fatal("expected missing vehicle catalog error")
	}
}

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

func TestCompilerBuildsHardNegativeEnumFilterFromConfirmedMapping(t *testing.T) {
	compiler, err := NewCompilerWithProviderEnums(vehiclecatalog.NewDefaultCatalog(), ProviderEnumCatalog{
		Version:   "guide-enums-test-v1",
		FuelTypes: map[string][]int{"汽油": {1, 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := compiler.Compile([]Requirement{{
		ID: "no-fuel", Facet: "energy_type", RawText: "不要燃油车", CanonicalValue: "汽油",
		Operator: "not_eq", Importance: "hard", Status: "active",
	}}, baselineMenu())
	if len(plan.FilterCodes()) != 0 || len(plan.LocalVerifiers) != 1 ||
		plan.LocalVerifiers[0].Operator != "not_in" ||
		len(plan.LocalVerifiers[0].ProviderValues) != 2 ||
		plan.Resolutions[0].Capability != CapabilityVerifiable {
		t.Fatalf("unexpected negative plan: %#v", plan)
	}
}

func TestCompilerDoesNotGuessMissingProviderEnumMapping(t *testing.T) {
	plan := NewCompiler().Compile([]Requirement{{
		ID: "no-manual", Facet: "transmission", RawText: "不要手动挡", CanonicalValue: "手动挡",
		Operator: "not_eq", Importance: "hard", Status: "active",
	}}, baselineMenu())
	if len(plan.LocalVerifiers) != 0 || len(plan.Resolutions) != 1 ||
		plan.Resolutions[0].ReasonCode != "provider_enum_mapping_missing" {
		t.Fatalf("unexpected unresolved enum plan: %#v", plan)
	}
}

func TestCompilerRanksSoftEnergyWithConfirmedMapping(t *testing.T) {
	compiler, err := NewCompilerWithProviderEnums(vehiclecatalog.NewDefaultCatalog(), ProviderEnumCatalog{
		Version:   "guide-enums-test-v1",
		FuelTypes: map[string][]int{"纯电动": {2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := compiler.Compile([]Requirement{{
		ID: "prefer-electric", Facet: "energy_type", RawText: "最好纯电", CanonicalValue: "纯电",
		Operator: "eq", Importance: "soft", Status: "active",
	}}, baselineMenu())
	if len(plan.FilterCodes()) != 0 || len(plan.RankFactors) != 1 ||
		plan.RankFactors[0].Type != RankPreferredEnergy ||
		plan.Resolutions[0].Capability != CapabilityRankable {
		t.Fatalf("unexpected soft energy plan: %#v", plan)
	}
}

func TestProviderEnumVersionParticipatesInPlanHash(t *testing.T) {
	compile := func(version string) FilterPlan {
		compiler, err := NewCompilerWithProviderEnums(vehiclecatalog.NewDefaultCatalog(), ProviderEnumCatalog{
			Version: version, FuelTypes: map[string][]int{"汽油": {1}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return compiler.Compile([]Requirement{{
			ID: "no-fuel", Facet: "energy_type", RawText: "不要燃油车", CanonicalValue: "汽油",
			Operator: "not_eq", Importance: "hard", Status: "active",
		}}, baselineMenu())
	}
	first := compile("guide-enums-v1")
	second := compile("guide-enums-v2")
	if first.PlanHash == second.PlanHash || first.ProviderEnumVersion != "guide-enums-v1" {
		t.Fatalf("provider enum version missing from plan identity: first=%#v second=%#v", first, second)
	}
}

func TestCompilerBuildsHardNegativeBrandFilter(t *testing.T) {
	plan := NewCompiler().Compile([]Requirement{{
		ID: "no-tesla", Facet: "brand", RawText: "不要特斯拉", CanonicalValue: "特斯拉",
		Operator: "not_eq", Importance: "hard", Status: "active", EntityID: "brand:tesla",
	}}, baselineMenu())
	if len(plan.FilterCodes()) != 0 || len(plan.LocalVerifiers) != 1 ||
		plan.LocalVerifiers[0].Operator != "not_in" ||
		plan.Resolutions[0].Capability != CapabilityVerifiable {
		t.Fatalf("unexpected negative brand plan: %#v", plan)
	}
}

func TestCompilerBuildsCrossLevelVehicleAnyOfVerifier(t *testing.T) {
	plan := NewCompiler().Compile([]Requirement{{
		ID: "vehicle-or", Facet: "vehicle_entity_any_of", RawText: "宝马或Model Y",
		Operator: "in", Relation: "any_of", Importance: "hard", Status: "active",
		Alternatives: []RequirementAlternative{
			{Facet: "brand", CanonicalValue: "宝马", EntityID: "brand:bmw", EntityResolution: "resolved"},
			{Facet: "vehicle_model", CanonicalValue: "Model Y", EntityID: "model:tesla:model-y", EntityBrandID: "brand:tesla", EntityResolution: "resolved"},
		},
	}}, baselineMenu())
	if len(plan.FilterCodes()) != 0 || len(plan.LocalVerifiers) != 1 ||
		len(plan.LocalVerifiers[0].Alternatives) != 2 ||
		plan.Resolutions[0].Capability != CapabilityVerifiable {
		t.Fatalf("unexpected any_of plan: %#v", plan)
	}
	filtered, _ := ApplyLocalVerifiers([]guide.VehRate{
		{Vehicle: &guide.Vehicle{BrandName: "宝马", VehicleName: "宝马325Li"}},
		{Vehicle: &guide.Vehicle{BrandName: "特斯拉", VehicleName: "特斯拉Model Y"}},
		{Vehicle: &guide.Vehicle{BrandName: "丰田", VehicleName: "丰田凯美瑞"}},
	}, plan.LocalVerifiers)
	if len(filtered) != 2 {
		t.Fatalf("OR verifier did not preserve both alternatives: %#v", filtered)
	}
}

func TestCompilerMapsTotalPriceToGuideFilter(t *testing.T) {
	amount := 450.0
	plan := NewCompiler().Compile([]Requirement{{
		ID: "budget", Facet: "price_preference", RawText: "总价450元以内",
		CanonicalValue: "total<=450CNY", Operator: "lte", Importance: "hard", Status: "active",
		Value: requirement.Value{Kind: requirement.ValueNumber, Number: &amount, Unit: "total_CNY"},
	}}, baselineMenu())
	if len(plan.FilterCodes()) != 1 ||
		plan.FilterCodes()[0] != "filter/total_fee/le_450" ||
		len(plan.QuoteFilters) != 0 ||
		plan.Resolutions[0].Capability != CapabilityFilterable {
		t.Fatalf("unexpected price plan: %#v", plan)
	}
}

func TestCompilerDoesNotUseDailyPriceCodeForTotalBudget(t *testing.T) {
	amount := 450.0
	menu := []guide.MenuGroup{{GroupItems: []guide.GroupItem{{Items: []guide.Item{
		{Name: "450元以下", ItemCode: "filter/price/le_450"},
	}}}}}
	plan := NewCompiler().Compile([]Requirement{{
		ID: "budget", Facet: "price_preference", RawText: "总价450元以内",
		CanonicalValue: "total<=450CNY", Operator: "lte", Importance: "hard", Status: "active",
		Value: requirement.Value{Kind: requirement.ValueNumber, Number: &amount, Unit: "total_CNY"},
	}}, menu)
	if len(plan.FilterCodes()) != 0 ||
		len(plan.QuoteFilters) != 0 ||
		plan.Resolutions[0].Capability != CapabilityUnverifiable {
		t.Fatalf("unexpected price plan: %#v", plan)
	}
}

func TestCompilerDoesNotBroadenSeatThreshold(t *testing.T) {
	menu := []guide.MenuGroup{{GroupItems: []guide.GroupItem{{Items: []guide.Item{
		{Name: "8座及以上", ItemCode: "filter/seat_num/ge_8"},
	}}}}}
	plan := NewCompiler().Compile([]Requirement{{
		ID: "seat", Facet: "seat_num", RawText: "至少9座", CanonicalValue: "9",
		Operator: "gte", Importance: "hard", Status: "active",
	}}, menu)
	if len(plan.MenuFilters) != 1 || !plan.MenuFilters[0].Prefilter ||
		plan.MenuFilters[0].Code != "filter/seat_num/ge_8" ||
		len(plan.LocalVerifiers) != 1 ||
		plan.LocalVerifiers[0].Operator != "gte" ||
		plan.LocalVerifiers[0].Value != "9" {
		t.Fatalf("unexpected seat plan: %#v", plan)
	}
}

func TestCompilerDoesNotTreatSeatBucketAsExactSeatCount(t *testing.T) {
	menu := []guide.MenuGroup{{GroupItems: []guide.GroupItem{{Items: []guide.Item{
		{Name: "7座", ItemCode: "filter/seat_num/6_7"},
	}}}}}
	plan := NewCompiler().Compile([]Requirement{{
		ID: "seat", Facet: "seat_num", RawText: "7座", CanonicalValue: "7",
		Operator: "eq", Importance: "hard", Status: "active",
	}}, menu)
	if len(plan.MenuFilters) != 1 || !plan.MenuFilters[0].Prefilter ||
		plan.MenuFilters[0].Code != "filter/seat_num/6_7" ||
		len(plan.LocalVerifiers) != 1 ||
		plan.LocalVerifiers[0].Operator != "eq" ||
		plan.LocalVerifiers[0].Value != "7" {
		t.Fatalf("unexpected seat plan: %#v", plan)
	}
}

func TestCompilerMapsCatalogedVehicleEntityAndAddsVerifier(t *testing.T) {
	compiler := NewCompiler()
	plan := compiler.Compile([]Requirement{{
		ID: "model", Facet: "vehicle_model", RawText: "Model Y", CanonicalValue: "Model Y", Operator: "eq", Importance: "hard", Status: "active",
		EntityID: "model:tesla:model-y", EntityBrandID: "brand:tesla",
	}}, baselineMenu())
	if len(plan.FilterCodes()) != 1 ||
		plan.FilterCodes()[0] != "filter/vehicle_name/特斯拉Model Y" ||
		len(plan.LocalVerifiers) != 1 ||
		plan.Resolutions[0].Capability != CapabilityFilterable {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestCompilerDoesNotMapUncatalogedVehicleEntity(t *testing.T) {
	plan := NewCompiler().Compile([]Requirement{{
		ID: "model", Facet: "vehicle_model", RawText: "不存在车型", CanonicalValue: "不存在车型",
		Operator: "eq", Importance: "hard", Status: "active",
	}}, baselineMenu())
	if len(plan.FilterCodes()) != 0 ||
		len(plan.LocalVerifiers) != 0 ||
		plan.Resolutions[0].Capability != CapabilityUnverifiable {
		t.Fatalf("unexpected plan: %#v", plan)
	}
}

func TestCompilerRemovesParentBrandForModel(t *testing.T) {
	compiler := NewCompiler()
	plan := compiler.Compile([]Requirement{
		{ID: "brand", Facet: "brand", RawText: "特斯拉", CanonicalValue: "特斯拉", Operator: "eq", Importance: "hard", Status: "active", EntityID: "brand:tesla"},
		{ID: "model", Facet: "vehicle_model", RawText: "Model Y", CanonicalValue: "Model Y", Operator: "eq", Importance: "hard", Status: "active", EntityID: "model:tesla:model-y", EntityBrandID: "brand:tesla"},
	}, baselineMenu())
	if len(plan.FilterCodes()) != 1 ||
		plan.FilterCodes()[0] != "filter/vehicle_name/特斯拉Model Y" ||
		len(plan.LocalVerifiers) != 1 ||
		plan.LocalVerifiers[0].RequirementID != "model" {
		t.Fatalf("unexpected vehicle plan: %#v", plan)
	}
}

func TestCompilerMapsBrandAndSeriesToGuideFilters(t *testing.T) {
	brandPlan := NewCompiler().Compile([]Requirement{{
		ID: "brand", Facet: "brand", RawText: "特斯拉", CanonicalValue: "特斯拉",
		Operator: "eq", Importance: "hard", Status: "active", EntityID: "brand:tesla",
	}}, baselineMenu())
	if len(brandPlan.FilterCodes()) != 1 ||
		brandPlan.FilterCodes()[0] != "filter/brand/特斯拉" ||
		len(brandPlan.LocalVerifiers) != 1 {
		t.Fatalf("unexpected brand plan: %#v", brandPlan)
	}

	seriesPlan := NewCompiler().Compile([]Requirement{{
		ID: "series", Facet: "vehicle_series", RawText: "宝马3系", CanonicalValue: "宝马3系",
		Operator: "eq", Importance: "hard", Status: "active",
		EntityID: "series:bmw:3", EntityBrandID: "brand:bmw",
	}}, baselineMenu())
	if len(seriesPlan.FilterCodes()) != 1 ||
		seriesPlan.FilterCodes()[0] != "filter/vehicle_name/宝马325Li" ||
		len(seriesPlan.LocalVerifiers) != 1 {
		t.Fatalf("unexpected series plan: %#v", seriesPlan)
	}
}

func TestCompilerExpandsSeriesToSameGroupModelFilters(t *testing.T) {
	catalog := vehiclecatalog.NewStaticCatalog([]vehiclecatalog.Entity{
		{ID: "brand:test", Type: vehiclecatalog.EntityBrand, CanonicalName: "测试品牌"},
		{ID: "series:test:s1", Type: vehiclecatalog.EntitySeries, CanonicalName: "S1", BrandID: "brand:test"},
		{ID: "model:test:s1:a", Type: vehiclecatalog.EntityModel, CanonicalName: "A款", ParentID: "series:test:s1", BrandID: "brand:test", ProviderBindings: []vehiclecatalog.ProviderBinding{{Provider: "guide", ProviderName: "测试品牌A款"}}},
		{ID: "model:test:s1:b", Type: vehiclecatalog.EntityModel, CanonicalName: "B款", ParentID: "series:test:s1", BrandID: "brand:test", ProviderBindings: []vehiclecatalog.ProviderBinding{{Provider: "guide", ProviderName: "测试品牌B款"}}},
	})
	compiler, err := NewCompilerWithVehicleCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	plan := compiler.Compile([]Requirement{{
		ID: "series", Facet: "vehicle_series", RawText: "S1", CanonicalValue: "S1",
		Operator: "eq", Importance: "hard", Status: "active",
		EntityID: "series:test:s1", EntityBrandID: "brand:test",
	}}, baselineMenu())
	codes := plan.FilterCodes()
	if len(codes) != 2 ||
		codes[0] != "filter/vehicle_name/测试品牌A款" ||
		codes[1] != "filter/vehicle_name/测试品牌B款" ||
		len(plan.LocalVerifiers) != 1 {
		t.Fatalf("unexpected series expansion: %#v", plan)
	}
}

func TestVehicleVerifierChecksBrandModelAndSeries(t *testing.T) {
	brandVerifier := VehicleVerifier{
		Facet:         "brand",
		ExpectedNames: []string{"特斯拉"},
	}
	if status := VerifyVehicle(guide.VehRate{
		Vehicle: &guide.Vehicle{BrandName: "比亚迪", VehicleName: "比亚迪汉"},
	}, brandVerifier); status != VehicleVerificationMismatch {
		t.Fatalf("brand status=%q", status)
	}

	modelVerifier := VehicleVerifier{
		Facet:         "vehicle_model",
		ExpectedBrand: "特斯拉",
		ExpectedNames: []string{"Model Y", "特斯拉Model Y"},
	}
	if status := VerifyVehicle(guide.VehRate{
		Vehicle: &guide.Vehicle{BrandName: "特斯拉", VehicleName: "特斯拉 Model Y 标准续航版"},
	}, modelVerifier); status != VehicleVerificationMatch {
		t.Fatalf("model status=%q", status)
	}

	seriesPlan := NewCompiler().Compile([]Requirement{{
		ID: "series", Facet: "vehicle_series", RawText: "宝马3系", CanonicalValue: "宝马3系",
		Operator: "eq", Importance: "hard", Status: "active",
		EntityID: "series:bmw:3", EntityBrandID: "brand:bmw",
	}}, baselineMenu())
	if status := VerifyVehicle(guide.VehRate{
		Vehicle: &guide.Vehicle{BrandName: "宝马", VehicleName: "宝马325Li"},
	}, seriesPlan.LocalVerifiers[0]); status != VehicleVerificationMatch {
		t.Fatalf("series status=%q verifier=%#v", status, seriesPlan.LocalVerifiers[0])
	}

	filtered := ApplyVehicleVerifiers([]guide.VehRate{
		{Vehicle: &guide.Vehicle{BrandName: "宝马", VehicleName: "宝马325Li"}},
		{Vehicle: &guide.Vehicle{BrandName: "宝马", VehicleName: "宝马X5"}},
		{},
	}, seriesPlan.LocalVerifiers)
	if len(filtered) != 1 || filtered[0].Vehicle.VehicleName != "宝马325Li" {
		t.Fatalf("filtered=%#v", filtered)
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
	if len(plan.FilterCodes()) != 0 || len(plan.LocalVerifiers) != 0 {
		t.Fatalf("conflicting values became accidental filters: %#v", plan)
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
			{Items: []guide.Item{{Name: "450元以下", ItemCode: "filter/total_fee/le_450"}}},
		}},
	}
}
