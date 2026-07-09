package orchestration

import (
	"strings"
	"testing"
	"time"

	"github.com/zxq97/rental-agent/internal/types"
)

func TestSummarizeForLog_EmptyState(t *testing.T) {
	s := New("s1", "u1")
	out := SummarizeForLog(s, "pipeline_start")

	mustContain(t, out, "[state] tag=pipeline_start session=s1")
	mustContain(t, out, "rental: pickup=\"\" city_id=0")
	mustContain(t, out, "pickup_ready=false")
	mustContain(t, out, "profile: (empty)")
	mustContain(t, out, "needs: (empty)")
	mustContain(t, out, "last_search: (none)")
	mustContain(t, out, "last_quotes: (none)")
	mustContain(t, out, "cached_menu: (none)")
	mustContain(t, out, "history: 0 entries")
	mustContain(t, out, "summary=<no>")
}

func TestSummarizeForLog_RichState_HidesIDs(t *testing.T) {
	s := New("s2", "u2")
	s.TurnCount = 3
	s.Rental = RentalCtx{
		PickupCityID:  1,
		PickupName:    "首都机场T3",
		PickupPOI:     RentalPOI{CityID: 1, Name: "首都机场T3", Latitude: 40.052, Longitude: 116.615},
		PickupTime:    time.Date(2026, 7, 8, 14, 0, 0, 0, time.UTC),
		DropoffCityID: 1,
		DropoffName:   "首都机场T3",
		DropoffPOI:    RentalPOI{CityID: 1, Name: "首都机场T3", Latitude: 40.052, Longitude: 116.615},
		DropoffTime:   time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		ContextID:     "ctx-super-secret-abcdef",
	}
	s.Profile = Profile{
		TripScene: "家庭出游", Companions: "老人小孩",
		PriceSensitivity: "medium", StylePreference: "空间舒适",
	}
	s.Constraints = types.SearchConstraints{
		Hard: []types.UserNeed{
			{Type: "vehicle_type", Value: "SUV", Hardness: "hard", Confidence: 0.9, BornTurn: 1, LastReinforced: 3},
			{Type: "energy_type", Value: "纯电", Hardness: "hard", Confidence: 0.85, BornTurn: 2},
		},
		Soft: []types.UserNeed{
			{Type: "comfort_preference", Value: "空间大", Hardness: "soft", Confidence: 0.6},
		},
		Negative: []types.UserNeed{
			{Type: "brand", Value: "大众", Hardness: "hard", Confidence: 0.9, Negative: true},
		},
	}
	s.LastSearch = &types.LastSearchState{
		SearchMode: "refine", FilterCodes: []string{"filter/vehcle_choice/suv", "filter/energy/ev"},
		Page: 1, PageSize: 6, HasMore: true,
		ShownRefs:    []string{"r1", "r2", "r3"},
		ExcludedRefs: []string{"r5"},
		MinPrice:     180, MaxPrice: 420, RelaxLevel: 0,
	}
	s.SetQuotes("ctx-super-secret-abcdef", []QuoteRef{
		{ReferenceID: "r1-secret", Supplier: "sup-a", CarName: "比亚迪宋PLUS", DailyPrice: 180, TotalPrice: 540, Index: 1},
		{ReferenceID: "r2-secret", Supplier: "sup-b", CarName: "特斯拉Model Y", DailyPrice: 380, TotalPrice: 1140, Index: 2},
	})
	s.SelectedRef = "r1-secret"
	s.CachedMenu = []types.MenuGroupView{
		{GroupCode: "filter/vehcle_choice", GroupName: "车型", Items: []types.MenuItemView{
			{ItemCode: "suv", Name: "SUV"}, {ItemCode: "sedan", Name: "轿车"},
		}},
		{GroupCode: "filter/energy", GroupName: "能源", Items: []types.MenuItemView{{ItemCode: "ev", Name: "纯电"}}},
	}
	s.Summary = "第 1-2 轮:用户问 SUV / 电车, 助手 推荐 3 辆"

	out := SummarizeForLog(s, "search_written")

	// 主字段落地
	mustContain(t, out, "tag=search_written session=s2 user=u2 turn=3")
	mustContain(t, out, "pickup=\"首都机场T3\" city_id=1")
	mustContain(t, out, "pickup_poi=<yes>")
	mustContain(t, out, "pickup_ready=true")
	mustContain(t, out, "trip_scene=\"家庭出游\"")
	mustContain(t, out, "needs.hard: [vehicle_type=SUV/hard/0.90(born=1,reinf=3), energy_type=纯电/hard/0.85")
	mustContain(t, out, "needs.soft: [comfort_preference=空间大/soft/0.60")
	mustContain(t, out, "needs.negative: [brand=大众/hard/0.90")
	mustContain(t, out, "last_search: mode=refine filter_codes=[filter/vehcle_choice/suv filter/energy/ev] page=1 page_size=6 has_more=true shown=3 excluded=1 relax=0 price_range=[180,420]")
	mustContain(t, out, "last_quotes[2/2 age=")
	mustContain(t, out, "[1]比亚迪宋PLUS ¥180/日")
	mustContain(t, out, "cached_menu: 2 groups")
	mustContain(t, out, "summary=<yes>")

	// ID 铁律:任何 secret / supplier / context_id 原值都不能出现
	forbidden := []string{
		"ctx-super-secret", "r1-secret", "r2-secret", "sup-a", "sup-b",
	}
	for _, f := range forbidden {
		if strings.Contains(out, f) {
			t.Fatalf("summary must NOT contain raw ID %q, got:\n%s", f, out)
		}
	}
	// 应该看到 <yes>/<no> 占位
	if !strings.Contains(out, "context_id=<yes>") {
		t.Fatalf("context_id should be marked <yes>, got:\n%s", out)
	}
	if !strings.Contains(out, "ref=<yes>") || !strings.Contains(out, "supplier=<yes>") {
		t.Fatalf("quote ref/supplier should be marked <yes>, got:\n%s", out)
	}
	if !strings.Contains(out, "selected_ref: <yes>") {
		t.Fatalf("selected_ref should be marked <yes>, got:\n%s", out)
	}
}

func TestSummarizeForLog_NilStateSafe(t *testing.T) {
	out := SummarizeForLog(nil, "pipeline_start")
	if !strings.Contains(out, "(nil state)") {
		t.Fatalf("expect nil-state marker, got %q", out)
	}
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("summary missing %q. full:\n%s", sub, s)
	}
}
