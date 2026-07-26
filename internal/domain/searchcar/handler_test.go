package searchcar

import (
	"testing"
	"time"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/searchplan"
	"github.com/zxq97/agent/internal/session"
)

func TestMissingContext(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name  string
		state session.SearchState
		want  []SearchMissingField
	}{
		{name: "all", want: []SearchMissingField{MissingLocation, MissingPickupTime, MissingReturnTime}},
		{name: "pickup", state: session.SearchState{Location: &session.LocationRef{}, ReturnTime: &now}, want: []SearchMissingField{MissingPickupTime}},
		{name: "complete", state: session.SearchState{Location: &session.LocationRef{}, PickupTime: timePtr(now.Add(time.Hour)), ReturnTime: timePtr(now.Add(2 * time.Hour))}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateRentalContext(test.state, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("got %#v want %#v", got, test.want)
			}
		})
	}
}

func TestParseOperation(t *testing.T) {
	tests := map[string]SearchOperation{
		"换一批":   OperationNextBatch,
		"还有别的吗": OperationNextBatch,
		"上一批":   OperationPreviousBatch,
		"刷新一下":  OperationRefresh,
		"直接搜":   OperationSearchNow,
	}
	for text, want := range tests {
		if got := ParseOperation(text); got != want {
			t.Fatalf("ParseOperation(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestBuildSearchRequest(t *testing.T) {
	zone := time.FixedZone("CST", 8*60*60)
	pickup := time.Date(2026, 7, 20, 10, 0, 0, 0, zone)
	ret := pickup.Add(48 * time.Hour)
	state := session.SearchState{Location: &session.LocationRef{ID: "poi", Name: "首都机场", CityID: "110000", Latitude: 40.08, Longitude: 116.58}, PickupTime: &pickup, ReturnTime: &ret}
	request := buildRequest(state, []string{"filter/seat_num/7"}, "", "ctx", 1, 20)
	if request.PickupRentalInfo.CityID != 110000 || request.DropoffRentalInfo.DateTime == request.PickupRentalInfo.DateTime || request.PickupRentalInfo.POI.Latitude == 0 {
		t.Fatalf("unexpected request: %#v", request)
	}
}

func TestSaveResults(t *testing.T) {
	handler := &SearchCarHandler{}
	agentSession := &session.AgentSession{}
	handler.saveResults(agentSession, []guide.VehRate{{SupplierCode: "supplier", Vehicle: &guide.Vehicle{VehicleCode: "car-1", VehicleName: "SUV"}, ReferenceInfo: &guide.RefInfo{ReferenceID: "ref"}}})
	if len(agentSession.Search.LastResults) != 1 || agentSession.Search.LastResults[0].VehicleCode != "car-1" || agentSession.Search.LastResults[0].ReferenceID != "ref" {
		t.Fatalf("unexpected refs: %#v", agentSession.Search.LastResults)
	}
}

func TestPreviousAndNextUseCachedBatches(t *testing.T) {
	plan := searchplan.FilterPlan{PlanHash: "plan"}
	firstVehicle := guide.VehRate{Vehicle: &guide.Vehicle{VehicleCode: "first"}}
	secondVehicle := guide.VehRate{Vehicle: &guide.Vehicle{VehicleCode: "second"}}
	agentSession := &session.AgentSession{Search: session.SearchState{ActiveSearch: &session.ActiveSearchSnapshot{
		Plan: plan, CurrentPage: 2, ContinuationContextID: "latest",
		Batches: []session.SearchResultBatch{
			{RequestPage: 1, Vehicles: []guide.VehRate{firstVehicle}},
			{RequestPage: 2, Vehicles: []guide.VehRate{secondVehicle}},
		},
	}}}
	previous := previousBatch(agentSession)
	if previous == nil || previous.RequestPage != 1 || previous.Vehicles[0].Vehicle.VehicleCode != "first" {
		t.Fatalf("previous=%#v", previous)
	}
	next := nextCachedBatch(agentSession)
	if next == nil || next.RequestPage != 2 || next.Vehicles[0].Vehicle.VehicleCode != "second" {
		t.Fatalf("next=%#v", next)
	}
}

func TestSnapshotValidationRejectsChangedRequirementsAndExpiredContext(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	pickup := now.Add(time.Hour)
	returnTime := pickup.Add(time.Hour)
	state := session.SearchState{
		Location: &session.LocationRef{ID: "poi"}, PickupTime: &pickup, ReturnTime: &returnTime,
		RequirementVersion: 2,
	}
	state.ActiveSearch = &session.ActiveSearchSnapshot{
		RentalFingerprint: rentalFingerprint(state), RequirementVersion: 2,
		FilterPlanHash: "plan", Plan: searchplan.FilterPlan{PlanHash: "plan"},
		ContinuationContextID: "context", Status: session.SearchSnapshotActive, ExpiresAt: now.Add(time.Minute),
	}
	handler := &SearchCarHandler{}
	agentSession := &session.AgentSession{Search: state}
	if !handler.snapshotValid(agentSession, now) {
		t.Fatal("valid snapshot was rejected")
	}
	agentSession.Search.RequirementVersion++
	if handler.snapshotValid(agentSession, now) {
		t.Fatal("snapshot survived a requirement version change")
	}
	agentSession.Search.RequirementVersion--
	if handler.snapshotValid(agentSession, now.Add(2*time.Minute)) {
		t.Fatal("expired snapshot was accepted")
	}
}

func TestUnseenVehiclesDeduplicatesByQuoteThenVehicle(t *testing.T) {
	snapshot := &session.ActiveSearchSnapshot{
		SeenQuoteIDs:     map[string]struct{}{"quote-seen": {}},
		SeenVehicleCodes: map[string]struct{}{"car-seen": {}},
	}
	values := []guide.VehRate{
		{ReferenceInfo: &guide.RefInfo{ReferenceID: "quote-seen"}, Vehicle: &guide.Vehicle{VehicleCode: "car-new-1"}},
		{Vehicle: &guide.Vehicle{VehicleCode: "car-seen"}},
		{ReferenceInfo: &guide.RefInfo{ReferenceID: "quote-new"}, Vehicle: &guide.Vehicle{VehicleCode: "car-new-2"}},
	}
	result := unseenVehicles(snapshot, values)
	if len(result) != 1 || result[0].ReferenceInfo.ReferenceID != "quote-new" {
		t.Fatalf("result=%#v", result)
	}
}

func timePtr(value time.Time) *time.Time { return &value }
