package searchplan

import (
	"testing"

	"github.com/zxq97/agent/api/guide"
)

func TestApplyLocalVerifiersRejectsSeatAndTotalPriceMismatch(t *testing.T) {
	values := []guide.VehRate{
		{Vehicle: &guide.Vehicle{Seats: 7}, TotalCharge: &guide.TotalCharge{TotalAmount: 450}},
		{Vehicle: &guide.Vehicle{Seats: 6}, TotalCharge: &guide.TotalCharge{TotalAmount: 450}},
		{Vehicle: &guide.Vehicle{Seats: 7}, TotalCharge: &guide.TotalCharge{TotalAmount: 550}},
	}
	filtered, report := ApplyLocalVerifiers(values, []LocalVerifier{
		{RequirementID: "seat", Facet: "seat_num", Operator: "eq", Value: "7"},
		{RequirementID: "budget", Facet: "price_total", Operator: "lte", Value: "500"},
	})
	if len(filtered) != 1 || report.RejectedQuotes != 2 ||
		report.ByRequirement["seat"].Mismatch != 1 ||
		report.ByRequirement["budget"].Mismatch != 1 {
		t.Fatalf("unexpected verification: %#v %#v", filtered, report)
	}
}

func TestApplyLocalVerifiersRejectsExcludedAndUnknownEnumValues(t *testing.T) {
	values := []guide.VehRate{
		{Vehicle: &guide.Vehicle{VehicleCode: "fuel", FuelType: 1}},
		{Vehicle: &guide.Vehicle{VehicleCode: "electric", FuelType: 2}},
		{Vehicle: &guide.Vehicle{VehicleCode: "unknown"}},
	}
	filtered, report := ApplyLocalVerifiers(values, []LocalVerifier{{
		RequirementID: "no-fuel", Facet: "energy_type", Operator: "not_in", ProviderValues: []int{1},
	}})
	if len(filtered) != 1 || filtered[0].Vehicle.VehicleCode != "electric" ||
		report.ByRequirement["no-fuel"].Mismatch != 1 ||
		report.ByRequirement["no-fuel"].Unknown != 1 {
		t.Fatalf("unexpected negative verification: %#v %#v", filtered, report)
	}
}

func TestApplyLocalVerifiersRejectsExcludedBrand(t *testing.T) {
	filtered, report := ApplyLocalVerifiers([]guide.VehRate{
		{Vehicle: &guide.Vehicle{BrandName: "特斯拉"}},
		{Vehicle: &guide.Vehicle{BrandName: "比亚迪"}},
		{},
	}, []LocalVerifier{{
		RequirementID: "no-tesla", Facet: "brand", Operator: "not_in", ExpectedNames: []string{"特斯拉"},
	}})
	if len(filtered) != 1 || filtered[0].Vehicle.BrandName != "比亚迪" ||
		report.ByRequirement["no-tesla"].Mismatch != 1 ||
		report.ByRequirement["no-tesla"].Unknown != 1 {
		t.Fatalf("unexpected brand exclusion: %#v %#v", filtered, report)
	}
}

func TestRerankNormalizesDifferentFactorScales(t *testing.T) {
	values := []guide.VehRate{
		{Vehicle: &guide.Vehicle{VehicleCode: "cheap", BrandName: "其他"}, TotalCharge: &guide.TotalCharge{TotalAmount: 100}},
		{Vehicle: &guide.Vehicle{VehicleCode: "preferred", BrandName: "特斯拉"}, TotalCharge: &guide.TotalCharge{TotalAmount: 200}},
	}
	ranked := Rerank(values, []RankFactor{
		{Type: RankPriceLow, Weight: 1},
		{Type: RankPreferredBrand, Value: "特斯拉", Weight: 2},
	})
	if ranked[0].Vehicle.VehicleCode != "preferred" {
		t.Fatalf("factor scales were not normalized: %#v", ranked)
	}
}
