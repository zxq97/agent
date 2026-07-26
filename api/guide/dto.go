package guide

// RentalInfo describes pickup or drop-off data required by rental-guide.
type RentalInfo struct {
	CityID       int       `json:"city_id"`
	LocationName string    `json:"location_name,omitempty"`
	DateTime     string    `json:"date_time,omitempty"`
	POI          *Location `json:"poi,omitempty"`
}

// Location contains geographic coordinates sent to rental-guide.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// SearchRequest is the rental-guide store-list request.
type SearchRequest struct {
	PickupRentalInfo  *RentalInfo `json:"pickup_rental_info"`
	DropoffRentalInfo *RentalInfo `json:"dropoff_rental_info"`
	Filter            FilterInfo  `json:"filter_info"`
	Page              int         `json:"page"`
	PageSize          int         `json:"page_size"`
	ContextID         string      `json:"context_id,omitempty"`
	Xenv              string      `json:"xenv,omitempty"`
}

type FilterInfo struct {
	FilterCodes []string `json:"filter_codes"`
	SortCode    string   `json:"sort_code,omitempty"`
	GroupCode   string   `json:"group_code,omitempty"`
}

// SearchResponse contains the continuation context, menu, and quotes.
type SearchResponse struct {
	ContextID string      `json:"context_id"`
	MenuGroup []MenuGroup `json:"menu_group"`
	VehRates  []VehRate   `json:"veh_rates"`
}

type MenuGroup struct {
	Name       string      `json:"name"`
	GroupCode  string      `json:"group_code"`
	GroupType  string      `json:"group_type"`
	GroupItems []GroupItem `json:"group_items"`
}

type GroupItem struct {
	Items []Item `json:"items"`
}

type Item struct {
	Name     string `json:"name"`
	ItemCode string `json:"item_code"`
}

type VehRate struct {
	SupplierCode         string       `json:"supplier_code"`
	SupplierDisplayName  string       `json:"supplier_display_name"`
	Vehicle              *Vehicle     `json:"vehicle"`
	DailyDeductionAmount float64      `json:"daily_deduction_amount"`
	TotalCharge          *TotalCharge `json:"total_charge"`
	ReferenceInfo        *RefInfo     `json:"reference_info"`
	FreeDepositType      int          `json:"free_deposit_type"`
}

type Vehicle struct {
	VehicleName      string `json:"vehicle_name"`
	VehicleCode      string `json:"vehicle_code"`
	BrandName        string `json:"brand_name"`
	GroupName        string `json:"group_name"`
	Seats            int    `json:"seats"`
	FuelType         int    `json:"fuel_type"`
	TransmissionType int    `json:"transmission_type"`
}

type TotalCharge struct {
	TotalAmount        float64 `json:"total_amount"`
	DeductionAmount    float64 `json:"deduction_amount"`
	DeductionAmountInt int64   `json:"deduction_amount_int"`
}

type RefInfo struct {
	ReferenceID string `json:"reference_id"`
}
