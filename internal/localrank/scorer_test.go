package localrank

import (
	"testing"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/searchplan"
)

func TestRankUsesAvailableFactsAndReturnsEvidence(t *testing.T) {
	values := []guide.VehRate{
		{Vehicle: &guide.Vehicle{VehicleCode: "sedan", VehicleName: "紧凑型轿车", GroupName: "紧凑型", Seats: 4}},
		{Vehicle: &guide.Vehicle{VehicleCode: "mpv", VehicleName: "家庭MPV", GroupName: "MPV", Seats: 7}},
	}
	ranked, report := Rank(values, []searchplan.ExploratoryRank{{
		RequirementID: "family",
		ScenarioID:    "family_trip_v1",
		ModelVersion:  "family_trip_v1",
		Weight:        1,
	}})
	if !report.Applied || ranked[0].Vehicle.VehicleCode != "mpv" ||
		len(report.EvidenceByRequirement["family"]) == 0 {
		t.Fatalf("unexpected rank result: %#v %#v", ranked, report)
	}
}

func TestRankDoesNotScoreWithoutMinimumFacts(t *testing.T) {
	values := []guide.VehRate{{Vehicle: &guide.Vehicle{VehicleCode: "unknown"}}}
	ranked, report := Rank(values, []searchplan.ExploratoryRank{{
		RequirementID: "family",
		ScenarioID:    "family_trip_v1",
		ModelVersion:  "family_trip_v1",
		Weight:        1,
	}})
	if report.Applied || ranked[0].Vehicle.VehicleCode != "unknown" ||
		len(report.UnscoredRequirementIDs) != 1 {
		t.Fatalf("unexpected rank result: %#v %#v", ranked, report)
	}
}
