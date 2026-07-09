package agent

import (
	"context"
	"testing"
	"time"

	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
)

func TestInsuranceAsksDriverAgeBeforeFetchingDetails(t *testing.T) {
	st := orchestration.New("s1", "u1")
	st.SetQuotes("ctx1", []orchestration.QuoteRef{{
		ReferenceID: "ref1",
		Supplier:    "supplier1",
		CarName:     "大众朗逸",
		BrandName:   "大众",
		Index:       1,
	}})

	res, err := (&InsuranceCapability{}).Run(context.Background(), CapabilityInput{
		State: st,
		Decision: &Decision{Args: map[string]any{
			"vehicle_ref": "第一辆",
		}},
		Deps: &tools.Deps{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Clarification == nil {
		t.Fatal("Clarification is nil, want driver age question")
	}
	if res.Clarification.Slot != "driver_age" {
		t.Fatalf("Slot = %q, want driver_age", res.Clarification.Slot)
	}
	if got := st.SelectedQuoteRef(); got != "ref1" {
		t.Fatalf("SelectedQuoteRef = %q, want ref1", got)
	}
}

func TestResolveQuoteForDetailsUsesSelectedRefWhenVehicleRefEmpty(t *testing.T) {
	st := orchestration.New("s1", "u1")
	st.SetQuotes("ctx1", []orchestration.QuoteRef{{
		ReferenceID: "ref1",
		Supplier:    "supplier1",
		CarName:     "大众朗逸",
		BrandName:   "大众",
		Index:       1,
	}})
	st.SelectQuote("ref1")

	ref, clar, errText := resolveQuoteForDetails(st, "")
	if clar != nil || errText != "" {
		t.Fatalf("clar=%v errText=%q, want direct selected ref", clar, errText)
	}
	if ref != "ref1" {
		t.Fatalf("ref = %q, want ref1", ref)
	}
}

func TestExtractRefsAcceptsStringSlice(t *testing.T) {
	got := extractRefs([]string{"朗逸", "轩逸"})
	if len(got) != 2 || got[0] != "朗逸" || got[1] != "轩逸" {
		t.Fatalf("extractRefs = %#v", got)
	}
}

func TestIsQuoteStaleUsesSelectedRefWithinTTL(t *testing.T) {
	st := orchestration.New("s1", "u1")
	st.SetQuotes("ctx1", []orchestration.QuoteRef{{ReferenceID: "ref1", Index: 1}})
	st.SelectQuote("ref1")
	if st.IsQuoteStale(tools.QuoteTTL) {
		t.Fatal("quote should be fresh")
	}
	if _, _, age := st.SnapshotQuotes(); age > time.Minute {
		t.Fatalf("quote age = %s, want fresh", age)
	}
}
