package session

import (
	"strings"
	"time"

	"github.com/zxq97/agent/internal/requirement"
	"github.com/zxq97/agent/internal/searchruntime"
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
	DirtyReason        string
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
	SemanticLabel  string
	Category       requirement.Category
	CanonicalType  string
	Value          requirement.Value

	Operator   string
	Relation   string
	Importance string
	Status     string

	EntityID         string
	EntityType       string
	EntityBrandID    string
	EntityParentID   string
	EntityResolution string
	ResolutionReason string
	CatalogVersion   string
	Alternatives     []SearchRequirementAlternative
}

type SearchRequirementAlternative struct {
	Facet            string `json:"facet"`
	RawValue         string `json:"raw_value"`
	CanonicalValue   string `json:"canonical_value"`
	EntityID         string `json:"-"`
	EntityType       string `json:"-"`
	EntityBrandID    string `json:"-"`
	EntityParentID   string `json:"-"`
	EntityResolution string `json:"-"`
}

func (r SearchRequirementStateItem) DisplayType() string {
	if r.CanonicalType != "" {
		return r.CanonicalType
	}
	if r.Facet != "" {
		return r.Facet
	}
	if r.SemanticLabel != "" {
		return r.SemanticLabel
	}
	return string(r.Category)
}

func (r SearchRequirementStateItem) DisplayValue() string {
	if len(r.Alternatives) > 0 {
		values := make([]string, 0, len(r.Alternatives))
		for _, alternative := range r.Alternatives {
			if alternative.CanonicalValue != "" {
				values = append(values, alternative.CanonicalValue)
			}
		}
		if len(values) > 0 {
			return strings.Join(values, " 或 ")
		}
	}
	if r.CanonicalValue != "" {
		return r.CanonicalValue
	}
	if r.RawValue != "" {
		return r.RawValue
	}
	return r.RawText
}

type GuideBaselineCache struct {
	RentalFingerprint string
	ContextID         string
	Menu              []searchruntime.MenuGroup
	BaseQuotes        []searchruntime.Quote

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

	RentalFingerprint     string
	RequirementVersion    int64
	FilterPlanHash        string
	CapabilityVersion     string
	RuntimeFingerprint    string
	RelaxedRequirementIDs []string

	BaselineContextID     string
	ContinuationContextID string

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
	Vehicles    []searchruntime.Quote
	CreatedAt   time.Time
}

type VehicleResultRef struct {
	Index        int
	VehicleCode  string
	VehicleName  string
	SupplierCode string
	ReferenceID  string
}
