package searchplan

import "testing"

func TestRelaxRequirementOnlyRelaxesExplicitRequirement(t *testing.T) {
	plan := FilterPlan{
		MenuFilters: []MenuFilter{
			{RequirementID: "brand", Code: "filter/brand/丰田"},
			{RequirementID: "budget", Code: "filter/total_fee/le_500"},
		},
		LocalVerifiers: []LocalVerifier{
			{RequirementID: "brand", Facet: "brand"},
			{RequirementID: "budget", Facet: "price_total"},
		},
		Resolutions: []Resolution{
			{RequirementID: "brand", RawText: "必须丰田", Importance: "hard", Capability: CapabilityFilterable},
			{RequirementID: "budget", RawText: "总价500以内", Importance: "hard", Capability: CapabilityFilterable},
		},
	}
	alternative, ok := RelaxRequirement(plan, "budget")
	if !ok || len(alternative.MenuFilters) != 1 ||
		alternative.MenuFilters[0].RequirementID != "brand" ||
		len(alternative.LocalVerifiers) != 1 ||
		len(alternative.Disclosures) != 1 ||
		alternative.Disclosures[0].Kind != DisclosureHardRelaxed {
		t.Fatalf("unexpected alternative: %#v", alternative)
	}
}
