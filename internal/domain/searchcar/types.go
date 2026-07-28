package searchcar

import (
	"time"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/capability"
	"github.com/zxq97/agent/internal/searchplan"
	"github.com/zxq97/agent/internal/session"
	"github.com/zxq97/agent/internal/turnnormalizer"
)

type SearchOperation string

const (
	OperationSearchNow     SearchOperation = "search_now"
	OperationNextBatch     SearchOperation = "next_batch"
	OperationPreviousBatch SearchOperation = "previous_batch"
	OperationRefresh       SearchOperation = "refresh"
)

func ParseOperation(evidenceText string) SearchOperation {
	return SearchOperation(turnnormalizer.NormalizeSearch(evidenceText).Operation)
}

type SearchCarInput struct {
	Operation            SearchOperation
	EvidenceText         string
	NoPreferenceExplicit bool
	PageSize             int
	ReceivedAt           time.Time
}

type SearchMissingField string

const (
	MissingLocation   SearchMissingField = "location"
	MissingPickupTime SearchMissingField = "pickup_time"
	MissingReturnTime SearchMissingField = "return_time"
)

type SearchResultStatus string

const (
	ResultSuccess          SearchResultStatus = "success"
	ResultNeedsContext     SearchResultStatus = "needs_context"
	ResultNeedsRequirement SearchResultStatus = "needs_requirement"
	ResultNoResults        SearchResultStatus = "no_results"
	ResultPartial          SearchResultStatus = "partial"
	ResultRejected         SearchResultStatus = "rejected"
	ResultWaitingUser      SearchResultStatus = "waiting_user"
	ResultCapabilityLimit  SearchResultStatus = "capability_limit"
)

type RequirementResult struct {
	ID, RawText, Reason, ReasonCode string
	Importance                      string
	Capability                      searchplan.Capability
	Status                          string
}

type SearchCarResult struct {
	Status        SearchResultStatus
	InteractionID string
	ContextID     string
	Vehicles      []guide.VehRate

	AppliedRequirements    []RequirementResult
	VerifiedRequirements   []RequirementResult
	RankedRequirements     []RequirementResult
	AdvisoryRequirements   []RequirementResult
	UnresolvedRequirements []RequirementResult
	MissingFields          []SearchMissingField
	Message                string
	RankingScope           string
	RequestPage            int
	Deltas                 []session.StateDelta
	CapabilityResolutions  []capability.Resolution
}

func NeedsRequirementResult(message string) *SearchCarResult {
	return &SearchCarResult{Status: ResultNeedsRequirement, Message: message}
}
