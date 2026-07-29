package searchplan

import "github.com/zxq97/agent/internal/requirement"

type Capability string

const (
	CapabilityFilterable   Capability = "filterable"
	CapabilityVerifiable   Capability = "verifiable"
	CapabilityRankable     Capability = "rankable"
	CapabilityAdvisory     Capability = "advisory"
	CapabilityAmbiguous    Capability = "ambiguous"
	CapabilityUnverifiable Capability = "unverifiable"
	CapabilityUnsupported  Capability = "unsupported"
)

type Requirement struct {
	ID string

	Facet          string
	RawText        string
	RawValue       string
	CanonicalValue string
	Operator       string
	Importance     string
	Status         string

	EntityID         string
	EntityType       string
	EntityBrandID    string
	EntityParentID   string
	EntityResolution string

	SemanticLabel string
	Category      requirement.Category
	Value         requirement.Value
}

type MenuFilter struct {
	RequirementID string
	Code          string
	Name          string
	Prefilter     bool
}

type QuoteFilter struct {
	RequirementID string
	Facet         string
	Operator      string
	Value         string
}

// LocalVerifier validates that a quote returned for a remote filter still
// satisfies the deterministic requirement.
type LocalVerifier struct {
	RequirementID string
	Facet         string
	EntityID      string
	ExpectedBrand string
	ExpectedNames []string
	Operator      string
	Value         string
	MinValue      string
	MaxValue      string
}

// VehicleVerifier is kept as a source-compatible alias while callers migrate
// to the generic LocalVerifier name.
type VehicleVerifier = LocalVerifier

type DisclosureKind string

const (
	DisclosureHardUnmapped      DisclosureKind = "hard_unmapped"
	DisclosureHardRelaxed       DisclosureKind = "hard_relaxed"
	DisclosureExploratoryRanked DisclosureKind = "exploratory_ranked"
	DisclosureVerifierMismatch  DisclosureKind = "verifier_mismatch"
)

// Disclosure is a deterministic user-facing fact that a reply must preserve.
type Disclosure struct {
	RequirementID string
	RawText       string
	Kind          DisclosureKind
	Message       string
	Evidence      []string
	MustMention   bool
}

type RankFactorType string

const (
	RankPriceLow       RankFactorType = "price_low"
	RankSeatsTarget    RankFactorType = "seats_target"
	RankPreferredBrand RankFactorType = "preferred_brand"
	RankPreferredModel RankFactorType = "preferred_model"
)

type RankFactor struct {
	RequirementID string
	Type          RankFactorType
	Value         string
	Weight        float64
	DataField     string
}

// ExploratoryRank describes a versioned scenario scorer. It may change order,
// but it can never prove that a vehicle satisfies a hard requirement.
type ExploratoryRank struct {
	RequirementID string
	RawText       string
	ScenarioID    string
	ModelVersion  string
	Importance    string
	Weight        float64
}

type Resolution struct {
	RequirementID string
	RawText       string
	Importance    string
	Capability    Capability
	Status        string
	ReasonCode    string
	Reason        string
}

type FilterPlan struct {
	MenuFilters []MenuFilter
	ServerSort  string

	QuoteFilters     []QuoteFilter
	LocalVerifiers   []LocalVerifier
	RankFactors      []RankFactor
	ExploratoryRanks []ExploratoryRank
	Resolutions      []Resolution
	Disclosures      []Disclosure

	RelaxedRequirementIDs []string

	CapabilityVersion  string
	RuntimeFingerprint string
	PlanHash           string
}

func (p FilterPlan) HasLocalVerification() bool {
	return len(p.LocalVerifiers) > 0
}

func (p FilterPlan) FilterCodes() []string {
	result := make([]string, 0, len(p.MenuFilters))
	seen := make(map[string]struct{})
	for _, filter := range p.MenuFilters {
		if filter.Code == "" {
			continue
		}
		if _, exists := seen[filter.Code]; exists {
			continue
		}
		seen[filter.Code] = struct{}{}
		result = append(result, filter.Code)
	}
	return result
}

func (p FilterPlan) FirstBlockingResolution() *Resolution {
	for index := range p.Resolutions {
		resolution := &p.Resolutions[index]
		if resolution.Importance != "hard" {
			continue
		}
		switch resolution.Capability {
		case CapabilityAmbiguous, CapabilityUnverifiable, CapabilityUnsupported:
			return resolution
		}
	}
	return nil
}
