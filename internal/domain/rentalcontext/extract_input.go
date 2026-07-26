package rentalcontext

import "time"

type ExtractionInput struct {
	SourceText          string               `json:"source_text"`
	CurrentState        CurrentRentalContext `json:"current_state"`
	RecentDomainHistory []DomainHistoryItem  `json:"recent_domain_history"`
	Now                 time.Time            `json:"now"`
	Timezone            string               `json:"timezone"`
}

type CurrentRentalContext struct {
	LocationName string     `json:"location_name"`
	PickupTime   *time.Time `json:"pickup_time"`
	ReturnTime   *time.Time `json:"return_time"`
}

type DomainHistoryItem struct {
	UserText string `json:"user_text"`
}
