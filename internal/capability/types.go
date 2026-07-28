package capability

import (
	"context"

	"github.com/zxq97/agent/internal/requirement"
)

type ResolutionStatus string

const (
	ResolutionResolved          ResolutionStatus = "resolved"
	ResolutionPartiallyResolved ResolutionStatus = "partially_resolved"
	ResolutionAmbiguous         ResolutionStatus = "ambiguous"
	ResolutionUnsupported       ResolutionStatus = "unsupported"
	ResolutionInsufficientData  ResolutionStatus = "insufficient_data"
)

type MatchMethod string

const (
	MatchCanonical MatchMethod = "canonical"
	MatchRule      MatchMethod = "rule"
	MatchAlias     MatchMethod = "alias"
	MatchLLM       MatchMethod = "llm_candidate"
)

type ExecutionMode string

const (
	ExecutionRemoteFilter ExecutionMode = "remote_filter"
	ExecutionRemoteSort   ExecutionMode = "remote_sort"
	ExecutionLocalFilter  ExecutionMode = "local_filter"
	ExecutionLocalRank    ExecutionMode = "local_rank"
)

type Requirement struct {
	ID            string               `json:"id"`
	RawText       string               `json:"raw_text"`
	SemanticLabel string               `json:"semantic_label"`
	Category      requirement.Category `json:"category"`
	CanonicalType string               `json:"canonical_type"`
	Value         requirement.Value    `json:"value"`
	Operator      string               `json:"operator"`
	Importance    string               `json:"importance"`
}

type Execution struct {
	RequirementID  string
	CapabilityID   string
	Mode           ExecutionMode
	RequiredFields []string
	RequiredMenus  []string
	Operation      string
	Value          string
	Confidence     float64
	Reason         string
}

type Resolution struct {
	RequirementID string
	RawText       string
	Importance    string
	Status        ResolutionStatus
	MatchMethod   MatchMethod
	CapabilityIDs []string
	Executions    []Execution

	ResolvedPart   string
	UnresolvedPart string
	ReasonCode     string
	Reason         string
	Confidence     float64
}

type RuntimeContext struct {
	MenuFingerprint   string
	MenuCodes         map[string]struct{}
	ResultFields      map[string]struct{}
	CatalogVersion    string
	RentalFingerprint string
}

type Resolver interface {
	Resolve(context.Context, Requirement, RuntimeContext) Resolution
	CatalogVersion() string
}

type MatchCandidate struct {
	ID             string          `json:"capability_id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	SupportedModes []ExecutionMode `json:"supported_modes"`
}

type MatchRequest struct {
	Requirement Requirement      `json:"requirement"`
	Candidates  []MatchCandidate `json:"candidates"`
}

type Match struct {
	CapabilityID string  `json:"capability_id"`
	Relation     string  `json:"relation"`
	Confidence   float64 `json:"confidence"`
}

type Matcher interface {
	Match(context.Context, *MatchRequest) ([]Match, error)
}
