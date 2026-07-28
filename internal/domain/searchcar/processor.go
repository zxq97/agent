package searchcar

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/searchplan"
	"github.com/zxq97/agent/internal/searchruntime"
	"github.com/zxq97/agent/internal/session"
)

type resultProcessor interface {
	Filter([]guide.VehRate, searchplan.FilterPlan) []guide.VehRate
	Rank([]guide.VehRate, searchplan.FilterPlan) []guide.VehRate
}

type quoteResultProcessor struct{}

func (quoteResultProcessor) Filter(values []guide.VehRate, plan searchplan.FilterPlan) []guide.VehRate {
	return searchplan.ApplyQuoteFilters(values, plan.QuoteFilters)
}

func (quoteResultProcessor) Rank(values []guide.VehRate, plan searchplan.FilterPlan) []guide.VehRate {
	return searchplan.Rerank(values, plan.RankFactors)
}

func (h *SearchCarHandler) finishSearch(
	ctx context.Context,
	agentSession *session.AgentSession,
	plan searchplan.FilterPlan,
	response *guide.SearchResponse,
	requestPage, pageSize int,
	now time.Time,
	fresh bool,
) (*SearchCarResult, error) {
	agentSession.Search.DirtyReason = ""
	rawCount := len(response.VehRates)
	vehicles := h.processor.Filter(response.VehRates, plan)
	lastResponse := response
	lastPage := requestPage

	for len(vehicles) == 0 && len(plan.QuoteFilters) > 0 && rawCount > 0 && lastPage < requestPage+maxQuoteScanPages-1 {
		lastPage++
		next, err := h.executor.Execute(ctx, buildRequest(
			agentSession.Search,
			plan.FilterCodes(),
			plan.ServerSort,
			lastResponse.ContextID,
			lastPage,
			pageSize,
		))
		if err != nil {
			return nil, err
		}
		if next == nil {
			return nil, errors.New("search car: quote scan response is empty")
		}
		lastResponse = next
		rawCount += len(next.VehRates)
		vehicles = h.processor.Filter(next.VehRates, plan)
		if len(next.VehRates) == 0 {
			break
		}
	}

	vehicles = h.processor.Rank(vehicles, plan)
	if fresh || agentSession.Search.ActiveSearch == nil {
		agentSession.Search.ActiveSearch = &session.ActiveSearchSnapshot{
			SearchID:              fmt.Sprintf("search-%d", now.UnixNano()),
			RentalFingerprint:     rentalFingerprint(agentSession.Search),
			RequirementVersion:    agentSession.Search.RequirementVersion,
			FilterPlanHash:        plan.PlanHash,
			CapabilityVersion:     plan.CapabilityVersion,
			RuntimeFingerprint:    plan.RuntimeFingerprint,
			BaselineContextID:     agentSession.Search.Baseline.ContextID,
			ContinuationContextID: lastResponse.ContextID,
			PageSize:              pageSize,
			SeenQuoteIDs:          make(map[string]struct{}),
			SeenVehicleCodes:      make(map[string]struct{}),
			Status:                session.SearchSnapshotActive,
			CreatedAt:             now,
			ExpiresAt:             agentSession.Search.Baseline.SafeExpiresAt,
		}
	}
	snapshot := agentSession.Search.ActiveSearch
	snapshot.ContinuationContextID = lastResponse.ContextID
	snapshot.CurrentPage = lastPage
	snapshot.NextPage = lastPage + 1
	vehicles = unseenVehicles(snapshot, vehicles)

	if len(vehicles) == 0 {
		if rawCount == 0 {
			snapshot.Status = session.SearchSnapshotExhausted
			h.saveResults(agentSession, nil)
			return resultFromPlan(ResultNoResults, plan, nil, lastResponse.ContextID, lastPage), nil
		}
		h.saveResults(agentSession, nil)
		result := resultFromPlan(ResultCapabilityLimit, plan, nil, lastResponse.ContextID, lastPage)
		result.Message = "当前已获取的候选车辆中没有能够验证全部硬条件的结果；系统没有把未验证条件当作已满足。"
		return result, nil
	}

	batch := session.SearchResultBatch{
		BatchNumber: len(snapshot.Batches) + 1,
		RequestPage: lastPage,
		Vehicles:    searchruntime.QuotesFromGuide(vehicles),
		CreatedAt:   now,
	}
	snapshot.Batches = append(snapshot.Batches, batch)
	h.saveResults(agentSession, vehicles)
	status := ResultSuccess
	if hasPartialResolution(plan.Resolutions) {
		status = ResultPartial
	}
	result := resultFromPlan(status, plan, vehicles, lastResponse.ContextID, lastPage)
	if len(plan.RankFactors) > 0 && plan.ServerSort == "" {
		result.RankingScope = "fetched_set"
	}
	return result, nil
}

var _ resultProcessor = quoteResultProcessor{}

func previousBatch(agentSession *session.AgentSession, plan searchplan.FilterPlan) *SearchCarResult {
	snapshot := agentSession.Search.ActiveSearch
	if snapshot == nil || len(snapshot.Batches) < 2 {
		return nil
	}
	currentIndex := -1
	for index := range snapshot.Batches {
		if snapshot.Batches[index].RequestPage == snapshot.CurrentPage {
			currentIndex = index
			break
		}
	}
	if currentIndex <= 0 {
		return nil
	}
	batch := snapshot.Batches[currentIndex-1]
	snapshot.CurrentPage = batch.RequestPage
	return resultFromPlan(cachedBatchStatus(plan), plan, searchruntime.QuotesToGuide(batch.Vehicles), snapshot.ContinuationContextID, batch.RequestPage)
}

func nextCachedBatch(agentSession *session.AgentSession, plan searchplan.FilterPlan) *SearchCarResult {
	snapshot := agentSession.Search.ActiveSearch
	if snapshot == nil {
		return nil
	}
	for _, batch := range snapshot.Batches {
		if batch.RequestPage > snapshot.CurrentPage {
			snapshot.CurrentPage = batch.RequestPage
			return resultFromPlan(cachedBatchStatus(plan), plan, searchruntime.QuotesToGuide(batch.Vehicles), snapshot.ContinuationContextID, batch.RequestPage)
		}
	}
	return nil
}

func cachedBatchStatus(plan searchplan.FilterPlan) SearchResultStatus {
	if hasPartialResolution(plan.Resolutions) {
		return ResultPartial
	}
	return ResultSuccess
}

func unseenVehicles(snapshot *session.ActiveSearchSnapshot, values []guide.VehRate) []guide.VehRate {
	result := make([]guide.VehRate, 0, len(values))
	for _, value := range values {
		quoteID := ""
		vehicleCode := ""
		if value.ReferenceInfo != nil {
			quoteID = value.ReferenceInfo.ReferenceID
		}
		if value.Vehicle != nil {
			vehicleCode = value.Vehicle.VehicleCode
		}
		keySeen := false
		if quoteID != "" {
			if _, exists := snapshot.SeenQuoteIDs[quoteID]; exists {
				keySeen = true
			}
		} else if vehicleCode != "" {
			if _, exists := snapshot.SeenVehicleCodes[vehicleCode]; exists {
				keySeen = true
			}
		}
		if keySeen {
			continue
		}
		if quoteID != "" {
			snapshot.SeenQuoteIDs[quoteID] = struct{}{}
		}
		if vehicleCode != "" {
			snapshot.SeenVehicleCodes[vehicleCode] = struct{}{}
		}
		result = append(result, value)
	}
	return result
}

func applyResolutions(agentSession *session.AgentSession, resolutions []searchplan.Resolution) {
	byID := make(map[string]searchplan.Resolution)
	for _, resolution := range resolutions {
		byID[resolution.RequirementID] = resolution
	}
	for index := range agentSession.Search.Requirements {
		resolution, exists := byID[agentSession.Search.Requirements[index].ID]
		if !exists {
			continue
		}
		agentSession.Search.Requirements[index].ResolutionReason = resolution.Reason
		switch resolution.Capability {
		case searchplan.CapabilityAmbiguous, searchplan.CapabilityUnverifiable, searchplan.CapabilityUnsupported:
			agentSession.Search.Requirements[index].Status = string(resolution.Capability)
		default:
			agentSession.Search.Requirements[index].Status = "active"
		}
	}
}

func resultFromPlan(status SearchResultStatus, plan searchplan.FilterPlan, vehicles []guide.VehRate, contextID string, page int) *SearchCarResult {
	return &SearchCarResult{
		Status:                 status,
		ContextID:              contextID,
		Vehicles:               vehicles,
		AppliedRequirements:    resultsFor(plan.Resolutions, searchplan.CapabilityFilterable),
		VerifiedRequirements:   resultsFor(plan.Resolutions, searchplan.CapabilityVerifiable),
		RankedRequirements:     resultsFor(plan.Resolutions, searchplan.CapabilityRankable),
		AdvisoryRequirements:   resultsFor(plan.Resolutions, searchplan.CapabilityAdvisory),
		UnresolvedRequirements: resultsFor(plan.Resolutions, searchplan.CapabilityAmbiguous, searchplan.CapabilityUnverifiable, searchplan.CapabilityUnsupported),
		RequestPage:            page,
	}
}

func resultsFor(values []searchplan.Resolution, capabilities ...searchplan.Capability) []RequirementResult {
	allowed := make(map[searchplan.Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		allowed[capability] = struct{}{}
	}
	var result []RequirementResult
	for _, value := range values {
		if _, exists := allowed[value.Capability]; !exists {
			continue
		}
		result = append(result, RequirementResult{
			ID:         value.RequirementID,
			RawText:    value.RawText,
			Reason:     value.Reason,
			ReasonCode: value.ReasonCode,
			Importance: value.Importance,
			Capability: value.Capability,
			Status:     value.Status,
		})
	}
	return result
}

func hasPartialResolution(values []searchplan.Resolution) bool {
	for _, value := range values {
		switch value.Capability {
		case searchplan.CapabilityAdvisory, searchplan.CapabilityAmbiguous, searchplan.CapabilityUnverifiable, searchplan.CapabilityUnsupported:
			return true
		}
	}
	return false
}

func (h *SearchCarHandler) saveResults(agentSession *session.AgentSession, vehicles []guide.VehRate) {
	agentSession.Search.LastResults = nil
	for index, value := range vehicles {
		ref := session.VehicleResultRef{Index: index, SupplierCode: value.SupplierCode}
		if value.Vehicle != nil {
			ref.VehicleCode = value.Vehicle.VehicleCode
			ref.VehicleName = value.Vehicle.VehicleName
		}
		if value.ReferenceInfo != nil {
			ref.ReferenceID = value.ReferenceInfo.ReferenceID
		}
		agentSession.Search.LastResults = append(agentSession.Search.LastResults, ref)
	}
}
