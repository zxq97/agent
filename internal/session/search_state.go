package session

import (
	"time"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/searchplan"
)

type LocationRef struct {
	ID        string
	Name      string
	Address   string
	CityID    string
	Latitude  float64
	Longitude float64
}

type SearchGoalStatus string

const (
	SearchGoalInactive SearchGoalStatus = "inactive"
	SearchGoalActive   SearchGoalStatus = "active"
)

type SearchGoalState struct {
	Status             SearchGoalStatus
	PreferenceAskCount int
	NoPreference       bool
	LastAskedAt        time.Time
}

// SearchState stores semantic search inputs separately from provider-owned
// baseline and continuation state.
type SearchState struct {
	Location   *LocationRef
	PickupTime *time.Time
	ReturnTime *time.Time

	Goal               SearchGoalState
	RequirementVersion int64
	Requirements       []SearchRequirementStateItem

	Baseline     *GuideBaselineCache
	ActiveSearch *ActiveSearchSnapshot
	LastResults  []VehicleResultRef
}

type SearchRequirementStateItem struct {
	ID string

	Facet          string
	RawText        string
	RawValue       string
	CanonicalValue string

	Operator   string
	Importance string
	Status     string

	EntityID         string
	EntityType       string
	EntityBrandID    string
	EntityParentID   string
	EntityResolution string
	ResolutionReason string
	CatalogVersion   string
}

type GuideBaselineCache struct {
	RentalFingerprint string
	ContextID         string
	Menu              []guide.MenuGroup
	BaseQuotes        []guide.VehRate

	FirstReceivedAt  time.Time
	ServiceExpiresAt time.Time
	SafeExpiresAt    time.Time
	Complete         bool
}

type SearchSnapshotStatus string

const (
	SearchSnapshotActive     SearchSnapshotStatus = "active"
	SearchSnapshotExhausted  SearchSnapshotStatus = "exhausted"
	SearchSnapshotExpired    SearchSnapshotStatus = "expired"
	SearchSnapshotSuperseded SearchSnapshotStatus = "superseded"
)

type ActiveSearchSnapshot struct {
	SearchID string

	RentalFingerprint  string
	RequirementVersion int64
	FilterPlanHash     string

	BaselineContextID     string
	ContinuationContextID string
	Plan                  searchplan.FilterPlan

	CurrentPage int
	PageSize    int
	NextPage    int

	SeenQuoteIDs     map[string]struct{}
	SeenVehicleCodes map[string]struct{}
	Batches          []SearchResultBatch

	Status    SearchSnapshotStatus
	CreatedAt time.Time
	ExpiresAt time.Time
}

type SearchResultBatch struct {
	BatchNumber int
	RequestPage int
	Vehicles    []guide.VehRate
	CreatedAt   time.Time
}

type VehicleResultRef struct {
	Index        int
	VehicleCode  string
	VehicleName  string
	SupplierCode string
	ReferenceID  string
}
