package vehiclecompare

type Status string

const (
	StatusSuccess        Status = "success"
	StatusNeedsSelection Status = "needs_selection"
	StatusNoSearchResult Status = "no_search_result"
)

type Input struct {
	EvidenceText string
}

type Option struct {
	Index                int
	VehicleName          string
	VehicleCode          string
	BrandName            string
	GroupName            string
	Seats                int
	SupplierName         string
	TotalAmount          *float64
	DailyDeductionAmount *float64
	FuelTypeCode         *int
	TransmissionTypeCode *int
}

type Highlights struct {
	LowestTotalPriceIndexes []int
	MostSeatsIndexes        []int
	TotalPriceSpread        *float64
}

type Comparison struct {
	Options     []Option
	Highlights  Highlights
	Scope       string
	Limitations []string
}

type Result struct {
	Status     Status
	Message    string
	Comparison *Comparison
	Candidates []Option
}
