package rentalcontext

type ResolutionStatus string

const (
	ResolutionAbsent    ResolutionStatus = "absent"
	ResolutionResolved  ResolutionStatus = "resolved"
	ResolutionAmbiguous ResolutionStatus = "ambiguous"
)

type ExtractedTime struct {
	Status ResolutionStatus `json:"status"`
	Raw    string           `json:"raw"`
	Value  *string          `json:"value"`
}

type RentalContextExtractResult struct {
	LocationQuery string        `json:"location_query"`
	PickupTime    ExtractedTime `json:"pickup_time"`
	ReturnTime    ExtractedTime `json:"return_time"`
	DomainMatched bool          `json:"domain_matched"`
}

type AmbiguousField struct {
	Field string
	Raw   string
}
