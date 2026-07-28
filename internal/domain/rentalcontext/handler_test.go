package rentalcontext

import (
	"testing"
	"time"

	"github.com/zxq97/agent/api/maps"
	"github.com/zxq97/agent/internal/session"
)

func TestMapExtractResult(t *testing.T) {
	pickup := "2026-07-20T10:00:00+08:00"
	returnTime := "2026-07-22T18:00:00+08:00"
	tests := []struct {
		name      string
		result    *RentalContextExtractResult
		wantErr   error
		ambiguous int
	}{
		{name: "location only", result: &RentalContextExtractResult{LocationQuery: "首都机场", DomainMatched: true, PickupTime: ExtractedTime{Status: ResolutionAbsent}, ReturnTime: ExtractedTime{Status: ResolutionAbsent}}},
		{name: "pickup and return", result: &RentalContextExtractResult{DomainMatched: true, PickupTime: ExtractedTime{Status: ResolutionResolved, Value: &pickup}, ReturnTime: ExtractedTime{Status: ResolutionResolved, Value: &returnTime}}},
		{name: "ambiguous pickup", result: &RentalContextExtractResult{DomainMatched: true, PickupTime: ExtractedTime{Status: ResolutionAmbiguous, Raw: "晚上"}, ReturnTime: ExtractedTime{Status: ResolutionAbsent}}, ambiguous: 1},
		{name: "domain mismatch", result: &RentalContextExtractResult{DomainMatched: false}, wantErr: ErrDomainMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, ambiguous, err := mapExtractResult(test.result)
			if err != test.wantErr {
				t.Fatalf("mapExtractResult() error = %v, want %v", err, test.wantErr)
			}
			if err == nil && (command == nil || len(ambiguous) != test.ambiguous) {
				t.Fatalf("command=%#v ambiguous=%#v", command, ambiguous)
			}
		})
	}
}

func TestValidateTimes(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	past, pickup, ret := now.Add(-time.Hour), now.Add(time.Hour), now.Add(2*time.Hour)
	tests := []struct {
		name  string
		value preview
		want  error
	}{
		{name: "valid", value: preview{pickup: &pickup, ret: &ret}},
		{name: "pickup past", value: preview{pickup: &past}, want: ErrPickupTimeInPast},
		{name: "return past", value: preview{ret: &past}, want: ErrReturnTimeInPast},
		{name: "equal", value: preview{pickup: &pickup, ret: &pickup}, want: ErrInvalidRentalTimeRange},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validateTimes(test.value, now); got != test.want {
				t.Fatalf("validateTimes()=%v want=%v", got, test.want)
			}
		})
	}
}

func TestApplyRentalContext(t *testing.T) {
	oldPickup := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	oldReturn := oldPickup.Add(48 * time.Hour)
	newPickup := oldPickup.Add(24 * time.Hour)
	s := &session.AgentSession{Search: session.SearchState{
		Location: &session.LocationRef{ID: "old", Name: "旧地点", CityID: "1"}, PickupTime: &oldPickup, ReturnTime: &oldReturn,
		ActiveSearch: &session.ActiveSearchSnapshot{SearchID: "old-search"},
	}}
	modified := apply(s, &ModifyRentalContextCommand{PickupTime: &newPickup}, nil)
	if len(modified) != 1 || modified[0] != ModifiedPickupTime || !s.Search.PickupTime.Equal(newPickup) ||
		!s.Search.ReturnTime.Equal(oldReturn) || s.Search.ActiveSearch == nil {
		t.Fatalf("unexpected session: %#v modified=%#v", s.Search, modified)
	}
	locationValue := &maps.Candidate{ID: "new", Name: "首都机场", Address: "北京", CityID: 1}
	modified = apply(s, &ModifyRentalContextCommand{}, locationValue)
	if len(modified) != 1 || modified[0] != ModifiedLocation || s.Search.Location.ID != "new" {
		t.Fatalf("location was not applied: %#v", s.Search.Location)
	}
}

func TestBuildExtractionInputUsesOnlyRecentRentalHistory(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	handler, err := NewModifyRentalContextHandler(nil, nil, funcID("test"), func() time.Time { return now }, time.UTC, DefaultAmbiguityConfig())
	if err != nil {
		t.Fatal(err)
	}
	s := &session.AgentSession{Search: session.SearchState{Location: &session.LocationRef{Name: "首都机场"}}, Memory: session.ConversationMemory{RecentRentalContextTexts: []string{"第一条", "第二条", "第三条"}}}
	input := handler.buildExtractionInput(s, "提前一天取车", now)
	if input.CurrentState.LocationName != "首都机场" || len(input.RecentDomainHistory) != 2 || input.RecentDomainHistory[0].UserText != "第二条" || input.RecentDomainHistory[1].UserText != "第三条" {
		t.Fatalf("unexpected extraction input: %#v", input)
	}
}

type funcID string

func (f funcID) NewID() string { return string(f) }
