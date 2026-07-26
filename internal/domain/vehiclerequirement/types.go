package vehiclerequirement

import (
	"context"

	"github.com/zxq97/agent/internal/session"
)

type Facet string

const (
	FacetSeatNum           Facet = "seat_num"
	FacetVehicleType       Facet = "vehicle_type"
	FacetPricePreference   Facet = "price_preference"
	FacetCarAge            Facet = "car_age"
	FacetComfortPreference Facet = "comfort_preference"
	FacetEnergyType        Facet = "energy_type"
	FacetTransmission      Facet = "transmission"
	FacetBrand             Facet = "brand"
	FacetVehicleSeries     Facet = "vehicle_series"
	FacetVehicleModel      Facet = "vehicle_model"
	FacetCustom            Facet = "custom"
)

type Operation string

const (
	OperationAdd     Operation = "add"
	OperationReplace Operation = "replace"
	OperationRemove  Operation = "remove"
)

type Operator string

const (
	OperatorEQ       Operator = "eq"
	OperatorNotEQ    Operator = "not_eq"
	OperatorGT       Operator = "gt"
	OperatorGTE      Operator = "gte"
	OperatorLT       Operator = "lt"
	OperatorLTE      Operator = "lte"
	OperatorIN       Operator = "in"
	OperatorNotIN    Operator = "not_in"
	OperatorContains Operator = "contains"
)

type Importance string

const (
	ImportanceHard Importance = "hard"
	ImportanceSoft Importance = "soft"
)

type EntityContext struct {
	BrandHint  string `json:"brand_hint"`
	SeriesHint string `json:"series_hint"`
}

type Requirement struct {
	Facet         Facet         `json:"facet"`
	RawText       string        `json:"raw_text"`
	RawValue      string        `json:"raw_value"`
	Operation     Operation     `json:"operation"`
	Operator      Operator      `json:"operator"`
	Importance    Importance    `json:"importance"`
	Confidence    float64       `json:"confidence"`
	EntityContext EntityContext `json:"entity_context"`
}

type ExtractResult struct {
	Requirements  []Requirement `json:"requirements"`
	DomainMatched bool          `json:"domain_matched"`
}

type RequirementView struct {
	Facet          string `json:"facet"`
	RawValue       string `json:"raw_value"`
	CanonicalValue string `json:"canonical_value"`
	Operator       string `json:"operator"`
	Importance     string `json:"importance"`
	Status         string `json:"status"`
}

type ExtractionInput struct {
	SourceText          string            `json:"source_text"`
	CurrentRequirements []RequirementView `json:"current_requirements"`
	RecentDomainHistory []string          `json:"recent_domain_history"`
}

type Extractor interface {
	Extract(context.Context, *ExtractionInput) (*ExtractResult, error)
}

type UpdateInput struct {
	SourceText string
}

type UpdateResult struct {
	Changed      bool
	Requirements []session.SearchRequirementStateItem
}
