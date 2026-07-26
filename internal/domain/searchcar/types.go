package searchcar

import (
	"strings"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/searchplan"
)

type SearchOperation string

const (
	OperationSearchNow     SearchOperation = "search_now"
	OperationNextBatch     SearchOperation = "next_batch"
	OperationPreviousBatch SearchOperation = "previous_batch"
	OperationRefresh       SearchOperation = "refresh"
)

func ParseOperation(evidenceText string) SearchOperation {
	text := strings.ToLower(strings.TrimSpace(evidenceText))
	for _, phrase := range []string{"上一批", "上一页", "返回上一"} {
		if strings.Contains(text, phrase) {
			return OperationPreviousBatch
		}
	}
	for _, phrase := range []string{"刷新", "重新搜", "重新查", "更新一下"} {
		if strings.Contains(text, phrase) {
			return OperationRefresh
		}
	}
	for _, phrase := range []string{"换一批", "还有别的", "还有其他", "下一批", "下一页", "继续看", "更多"} {
		if strings.Contains(text, phrase) {
			return OperationNextBatch
		}
	}
	return OperationSearchNow
}

type SearchCarInput struct {
	Operation    SearchOperation
	EvidenceText string
	PageSize     int
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
}

func NeedsRequirementResult(message string) *SearchCarResult {
	return &SearchCarResult{Status: ResultNeedsRequirement, Message: message}
}
