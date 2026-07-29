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
