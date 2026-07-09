package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/types"
)

func TestBuildStatePrefixIncludesSafeRentalNeedsAndQuotes(t *testing.T) {
	st := orchestration.New("s1", "u1")
	st.Rental.PickupName = "北京首都机场"
	st.Rental.DropoffName = "北京南站"
	st.Rental.ContextID = "ctx-secret"
	st.Constraints.Hard = []types.UserNeed{{
		Type:       "vehicle_type",
		Value:      "SUV",
		Hardness:   "hard",
		Confidence: 0.92,
	}}
	st.SetQuotes("ctx-secret", []orchestration.QuoteRef{{
		ReferenceID: "ref-secret",
		Supplier:    "supplier-secret",
		CarName:     "大众朗逸",
		BrandName:   "大众",
		DailyPrice:  188,
		TotalPrice:  752,
		Index:       1,
	}})

	prefix := BuildStatePrefix(st, time.Date(2026, 7, 3, 15, 0, 0, 0, time.Local))

	for _, want := range []string{"## 当前会话状态", "北京首都机场", "北京南站", "vehicle_type=SUV", "大众朗逸", "188"} {
		if !strings.Contains(prefix, want) {
			t.Fatalf("prefix missing %q:\n%s", want, prefix)
		}
	}
	for _, forbidden := range []string{"ctx-secret", "ref-secret", "supplier-secret"} {
		if strings.Contains(prefix, forbidden) {
			t.Fatalf("prefix leaked %q:\n%s", forbidden, prefix)
		}
	}
}

func TestBuildStatePrefixHidesStaleQuotes(t *testing.T) {
	st := orchestration.New("s1", "u1")

	prefix := BuildStatePrefix(st, time.Date(2026, 7, 3, 15, 0, 0, 0, time.Local))

	if strings.Contains(prefix, "last_quotes") {
		t.Fatalf("empty state should not include last_quotes:\n%s", prefix)
	}
}
