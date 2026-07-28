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
}

type QuoteFilter struct {
	RequirementID string
	Facet         string
	Operator      string
	Value         string
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

	QuoteFilters []QuoteFilter
	RankFactors  []RankFactor
	Resolutions  []Resolution

	CapabilityVersion  string
	RuntimeFingerprint string
	PlanHash           string
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
