// Package vehiclefacts assembles provenance-aware facts used by local ranking.
package vehiclefacts

import (
	"strings"

	"github.com/zxq97/agent/api/guide"
)

type NumberFact struct {
	Value      float64
	Available  bool
	Source     string
	Confidence float64
}

type TextFact struct {
	Value      string
	Available  bool
	Source     string
	Confidence float64
}

type Facts struct {
	Seats       NumberFact
	VehicleName TextFact
	GroupName   TextFact
}

// FromGuide uses only fields present in the current quote. Missing values stay
// unavailable instead of becoming fabricated zero-value facts.
func FromGuide(value guide.VehRate) Facts {
	if value.Vehicle == nil {
		return Facts{}
	}
	vehicle := value.Vehicle
	result := Facts{}
	if vehicle.Seats > 0 {
		result.Seats = NumberFact{Value: float64(vehicle.Seats), Available: true, Source: "guide_quote", Confidence: 1}
	}
	if strings.TrimSpace(vehicle.VehicleName) != "" {
		result.VehicleName = TextFact{Value: vehicle.VehicleName, Available: true, Source: "guide_quote", Confidence: 1}
	}
	if strings.TrimSpace(vehicle.GroupName) != "" {
		result.GroupName = TextFact{Value: vehicle.GroupName, Available: true, Source: "guide_quote", Confidence: 1}
	}
	return result
}
