// Package searchruntime owns provider-neutral persisted search runtime state.
// Session stores these values without depending on Guide DTOs or compiler
// implementation types.
package searchruntime

type MenuGroup struct {
	Name       string
	GroupCode  string
	GroupType  string
	GroupItems []GroupItem
}

type GroupItem struct {
	Items []MenuItem
}

type MenuItem struct {
	Name string
	Code string
}

type Quote struct {
	SupplierCode        string
	SupplierDisplayName string
	Vehicle             *Vehicle
	DailyDeduction      float64
	TotalCharge         *Charge
	Reference           *Reference
	FreeDepositType     int
}

type Vehicle struct {
	Name             string
	Code             string
	BrandName        string
	GroupName        string
	Seats            int
	FuelType         int
	TransmissionType int
}

type Charge struct {
	TotalAmount        float64
	DeductionAmount    float64
	DeductionAmountInt int64
}

type Reference struct {
	ID string
}
