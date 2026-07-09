package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
	"github.com/zxq97/rental-agent/internal/tyche"
	"github.com/zxq97/rental-agent/internal/types"
)

// --- unit: parseRentalTime ---

func TestParseRentalTimeVariants(t *testing.T) {
	cases := []struct {
		input string
		want  string // expected "2006-01-02 15:04:05" formatted
		ok    bool
	}{
		{"2026-07-10 14:00:00", "2026-07-10 14:00:00", true},
		{"2026-07-10 14:00", "2026-07-10 14:00:00", true},
		{"2026-7-1 9:30", "2026-07-01 09:30:00", true},
		{"随便写", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, err := parseRentalTime(c.input)
		if c.ok {
			if err != nil {
				t.Errorf("input=%q err=%v", c.input, err)
				continue
			}
			if got.Format("2006-01-02 15:04:05") != c.want {
				t.Errorf("input=%q got=%s want=%s", c.input, got.Format("2006-01-02 15:04:05"), c.want)
			}
		} else {
			if err == nil {
				t.Errorf("input=%q should fail, got=%s", c.input, got)
			}
		}
	}
}

// --- scenario: update_rental 地点未传 → 提示 ---

func TestScenario_UpdateRental_NothingChanged(t *testing.T) {
	st := orchestration.New("s", "u")
	st.Rental.PickupCityID = 1
	st.Rental.PickupName = "首都机场T3"
	emit := &captureEmitter{}
	in := CapabilityInput{
		State:    st,
		Decision: &Decision{Tool: ToolUpdateRental, Args: map[string]any{}},
		Deps:     &tools.Deps{},
		Emit:     emit,
	}
	res, err := (&UpdateRentalCapability{}).Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !strings.Contains(res.Text, "想改什么") {
		t.Fatalf("res=%#v", res)
	}
}

// --- scenario: update_rental 改时间 ---

func TestScenario_UpdateRental_ChangeTime(t *testing.T) {
	st := orchestration.New("s", "u")
	st.Rental.PickupCityID = 1
	st.Rental.PickupName = "首都机场T3"
	st.Rental.DropoffCityID = 1
	st.Rental.DropoffName = "首都机场T3"
	// 给一些老数据,验证 reset
	st.SetQuotes("old-ctx", []orchestration.QuoteRef{
		{ReferenceID: "r1", CarName: "朗逸", Index: 1},
	})

	emit := &captureEmitter{}
	in := CapabilityInput{
		State: st,
		Decision: &Decision{
			Tool: ToolUpdateRental,
			Args: map[string]any{
				"pickup_time":  "2026-07-12 10:00",
				"dropoff_time": "2026-07-13 18:00",
			},
		},
		Deps: &tools.Deps{},
		Emit: emit,
	}
	res, err := (&UpdateRentalCapability{}).Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !strings.Contains(res.Text, "取车时间改为") {
		t.Fatalf("res=%#v", res)
	}
	// 时间应写入
	if st.Rental.PickupTime.IsZero() {
		t.Fatalf("PickupTime should be set")
	}
	want := time.Date(2026, 7, 12, 10, 0, 0, 0, time.Local)
	if !st.Rental.PickupTime.Equal(want) {
		t.Fatalf("PickupTime=%v want=%v", st.Rental.PickupTime, want)
	}
	// Reset 应清掉旧报价
	_, quotes, _ := st.SnapshotQuotes()
	if len(quotes) != 0 {
		t.Fatalf("quotes should be cleared, got %d", len(quotes))
	}
	if st.Rental.ContextID != "" {
		t.Fatalf("ContextID should be cleared, got %q", st.Rental.ContextID)
	}
}

func TestScenario_UpdateRental_ChangeTimeContinuesSearch(t *testing.T) {
	var reqBody guideRentalRequestForTest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeGuideQuoteResponse(t, w)
	}))
	defer srv.Close()

	st := orchestration.New("s", "u")
	st.Rental.PickupCityID = 1
	st.Rental.PickupName = "首都机场T3"
	st.Rental.PickupPOI = orchestration.RentalPOI{CityID: 1, Name: "首都机场T3", Latitude: 40.052, Longitude: 116.615}
	st.Rental.DropoffCityID = 1
	st.Rental.DropoffName = "首都机场T3"
	st.Rental.DropoffPOI = st.Rental.PickupPOI
	st.Rental.PickupTime = time.Date(2026, 7, 10, 10, 0, 0, 0, time.Local)
	st.Rental.DropoffTime = time.Date(2026, 7, 11, 18, 0, 0, 0, time.Local)
	st.Constraints.Hard = []types.UserNeed{{Type: "vehicle_type", Value: "SUV", Hardness: "hard", Confidence: 0.9}}
	st.LastSearch = &types.LastSearchState{SearchMode: SearchModeInitial, FilterCodes: []string{"filter/vehcle_choice/suv"}, Page: 1, PageSize: 6}

	res, err := (&UpdateRentalCapability{}).Run(context.Background(), CapabilityInput{
		State:     st,
		UserInput: "改成周日取车周一还车",
		Decision: &Decision{Tool: ToolUpdateRental, Args: map[string]any{
			"pickup_time":  "2026-07-12 10:00",
			"dropoff_time": "2026-07-13 18:00",
		}},
		Deps:    &tools.Deps{Guide: tyche.NewGuideClient(srv.URL, "", 1, nil)},
		Factory: fakeModelGetter{model: fakeChatModel{}},
		Emit:    &captureEmitter{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !strings.Contains(res.Text, "继续按刚才的条件帮你搜") {
		t.Fatalf("res=%#v", res)
	}
	if len(st.LastQuotes) != 1 {
		t.Fatalf("LastQuotes=%+v, want 1 quote after auto search", st.LastQuotes)
	}
	if reqBody.PickupRentalInfo.DateTime != "2026-07-12 10:00:00" || reqBody.DropoffRentalInfo.DateTime != "2026-07-13 18:00:00" {
		t.Fatalf("guide times = pickup %q dropoff %q", reqBody.PickupRentalInfo.DateTime, reqBody.DropoffRentalInfo.DateTime)
	}
}

// --- scenario: update_rental 改取车地点(tyche 不可用)→ Clarification ---

func TestScenario_UpdateRental_PickupResolveFail(t *testing.T) {
	st := orchestration.New("s", "u")
	st.Rental.PickupCityID = 1
	st.Rental.PickupName = "首都机场T3"
	in := CapabilityInput{
		State: st,
		Decision: &Decision{
			Tool: ToolUpdateRental,
			Args: map[string]any{"pickup_text": "随便说个不存在的地方"},
		},
		Deps: &tools.Deps{}, // Tyche nil → Call 会返回 is_error
		Emit: &captureEmitter{},
	}
	res, err := (&UpdateRentalCapability{}).Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Clarification == nil {
		t.Fatalf("expect clarification on resolve fail, got %#v", res)
	}
	if res.Clarification.Slot != "pickup_location" {
		t.Fatalf("slot=%s want pickup_location", res.Clarification.Slot)
	}
	// 地点不应被改
	if st.Rental.PickupName != "首都机场T3" {
		t.Fatalf("PickupName should stay, got %q", st.Rental.PickupName)
	}
}

// --- scenario: PreRoute 认领 update_rental ---

func TestScenario_PreRoute_UpdateRentalAction(t *testing.T) {
	ac := &AgentContext{
		State:     orchestration.New("s", "u"),
		EventType: "action_click",
		Action: &ClientAction{
			Type:    "update_rental",
			Label:   "改取车时间",
			Payload: map[string]any{"pickup_time": "2026-07-12 10:00"},
		},
	}
	sig, err := (&PreRouteStage{}).Handle(context.Background(), ac)
	if err != nil || sig != SignalContinue {
		t.Fatalf("sig=%s err=%v", sig, err)
	}
	if ac.Decision == nil || ac.Decision.Tool != ToolUpdateRental {
		t.Fatalf("Decision=%#v", ac.Decision)
	}
	if ac.Decision.Args["pickup_time"] != "2026-07-12 10:00" {
		t.Fatalf("args=%v", ac.Decision.Args)
	}
}

// --- scenario: ResetForRentalChange 清空关联数据 ---

func TestResetForRentalChange(t *testing.T) {
	st := orchestration.New("s", "u")
	st.SetQuotes("ctx-old", []orchestration.QuoteRef{{ReferenceID: "r1", Index: 1}})
	st.SelectedRef = "r1"
	st.LastSearch = &types.LastSearchState{SearchMode: "refine"}
	st.CachedMenu = []types.MenuGroupView{{GroupCode: "filter/test"}}

	st.ResetForRentalChange()

	if st.Rental.ContextID != "" {
		t.Fatalf("ContextID not cleared")
	}
	_, quotes, _ := st.SnapshotQuotes()
	if len(quotes) != 0 {
		t.Fatalf("quotes not cleared")
	}
	if st.SelectedRef != "" {
		t.Fatalf("SelectedRef not cleared")
	}
	if st.LastSearch != nil {
		t.Fatalf("LastSearch not cleared")
	}
	if st.CachedMenu != nil {
		t.Fatalf("CachedMenu not cleared")
	}
}
