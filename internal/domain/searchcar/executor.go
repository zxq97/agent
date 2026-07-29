package searchcar

import (
	"context"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/capability"
	"github.com/zxq97/agent/internal/searchplan"
	"github.com/zxq97/agent/internal/searchruntime"
	"github.com/zxq97/agent/internal/session"
)

type searchExecutor interface {
	Execute(context.Context, *guide.SearchRequest) (*guide.SearchResponse, error)
}

type guideSearchExecutor struct {
	client guide.Client
}

func (e *guideSearchExecutor) Execute(ctx context.Context, request *guide.SearchRequest) (*guide.SearchResponse, error) {
	return e.client.SearchQuotes(ctx, request)
}

func (h *SearchCarHandler) freshSearch(ctx context.Context, agentSession *session.AgentSession, pageSize int, now time.Time) (*SearchCarResult, error) {
	baseline, err := h.ensureBaseline(ctx, agentSession, pageSize, now)
	if err != nil {
		return nil, err
	}
	executionPlan := h.compileExecutionPlan(ctx, agentSession, baseline)
	plan := executionPlan.FilterPlan
	effectivePlan := plan
	applyResolutions(agentSession, plan.Resolutions)

	response := &guide.SearchResponse{ContextID: baseline.ContextID, VehRates: searchruntime.QuotesToGuide(baseline.BaseQuotes)}
	if len(plan.FilterCodes()) > 0 || plan.ServerSort != "" {
		response, err = h.executor.Execute(ctx, buildRequest(agentSession.Search, plan.FilterCodes(), plan.ServerSort, baseline.ContextID, 1, pageSize))
		if err != nil {
			return nil, err
		}
		if response == nil {
			return nil, errors.New("search car: filtered response is empty")
		}
	}
	result, err := h.finishSearch(ctx, agentSession, plan, response, 1, pageSize, now, true)
	if err != nil {
		return result, err
	}
	if result != nil && (result.Status == ResultNoResults || result.Status == ResultCapabilityLimit) {
		if alternative, ok := searchplan.FirstRelaxedAlternative(plan); ok {
			effectivePlan = alternative
			alternativeResponse := &guide.SearchResponse{
				ContextID: baseline.ContextID,
				VehRates:  searchruntime.QuotesToGuide(baseline.BaseQuotes),
			}
			if len(alternative.FilterCodes()) > 0 || alternative.ServerSort != "" {
				alternativeResponse, err = h.executor.Execute(ctx, buildRequest(
					agentSession.Search,
					alternative.FilterCodes(),
					alternative.ServerSort,
					baseline.ContextID,
					1,
					pageSize,
				))
				if err != nil {
					return nil, err
				}
				if alternativeResponse == nil {
					return nil, errors.New("search car: relaxed response is empty")
				}
			}
			applyResolutions(agentSession, alternative.Resolutions)
			result, err = h.finishSearch(ctx, agentSession, alternative, alternativeResponse, 1, pageSize, now, true)
			if err != nil {
				return result, err
			}
		}
	}
	if result != nil {
		result.CapabilityResolutions = capabilityResolutionsForPlan(effectivePlan, executionPlan.Resolutions)
	}
	return result, nil
}

func capabilityResolutionsForPlan(plan searchplan.FilterPlan, values []capability.Resolution) []capability.Resolution {
	result := append([]capability.Resolution(nil), values...)
	relaxed := make(map[string]struct{}, len(plan.RelaxedRequirementIDs))
	for _, requirementID := range plan.RelaxedRequirementIDs {
		relaxed[requirementID] = struct{}{}
	}
	for index := range result {
		if _, exists := relaxed[result[index].RequirementID]; !exists {
			continue
		}
		result[index].Status = capability.ResolutionPartiallyResolved
		result[index].ReasonCode = "hard_requirement_relaxed_after_no_results"
		result[index].Reason = "严格搜索无结果后，仅为提供替代候选而移除此条件"
		result[index].Executions = nil
		result[index].ResolvedPart = ""
		result[index].UnresolvedPart = result[index].RawText
	}
	return result
}

func (h *SearchCarHandler) continueSearch(ctx context.Context, agentSession *session.AgentSession, plan searchplan.FilterPlan, pageSize int, now time.Time) (*SearchCarResult, error) {
	snapshot := agentSession.Search.ActiveSearch
	page := snapshot.NextPage
	response, err := h.executor.Execute(ctx, buildRequest(
		agentSession.Search,
		plan.FilterCodes(),
		plan.ServerSort,
		snapshot.ContinuationContextID,
		page,
		pageSize,
	))
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("search car: continuation response is empty")
	}
	return h.finishSearch(ctx, agentSession, plan, response, page, pageSize, now, false)
}

var _ searchExecutor = (*guideSearchExecutor)(nil)

func (h *SearchCarHandler) compileExecutionPlan(ctx context.Context, agentSession *session.AgentSession, baseline *session.GuideBaselineCache) searchplan.SearchExecutionPlan {
	return h.compiler.Compile(
		ctx,
		planRequirements(agentSession.Search.Requirements),
		searchruntime.MenusToGuide(baseline.Menu),
		h.runtimeCapabilityContext(agentSession.Search, baseline),
		agentSession.Search.RequirementVersion,
	)
}

func validateRentalContext(state session.SearchState, now time.Time) ([]SearchMissingField, error) {
	var missing []SearchMissingField
	if state.Location == nil {
		missing = append(missing, MissingLocation)
	}
	if state.PickupTime == nil {
		missing = append(missing, MissingPickupTime)
	}
	if state.ReturnTime == nil {
		missing = append(missing, MissingReturnTime)
	}
	if len(missing) > 0 {
		return missing, nil
	}
	if !state.ReturnTime.After(*state.PickupTime) {
		return nil, errors.New("search car: return time must be after pickup time")
	}
	if !state.PickupTime.After(now) {
		return nil, errors.New("search car: pickup time must be in the future")
	}
	return nil, nil
}

func buildRequest(state session.SearchState, codes []string, sortCode, contextID string, page, pageSize int) *guide.SearchRequest {
	city, _ := strconv.Atoi(state.Location.CityID)
	return &guide.SearchRequest{
		PickupRentalInfo:  rentalInfo(state.Location, *state.PickupTime, city),
		DropoffRentalInfo: rentalInfo(state.Location, *state.ReturnTime, city),
		Filter:            guide.FilterInfo{FilterCodes: codes, SortCode: sortCode},
		Page:              page,
		PageSize:          pageSize,
		ContextID:         contextID,
	}
}

func rentalInfo(location *session.LocationRef, value time.Time, city int) *guide.RentalInfo {
	return &guide.RentalInfo{
		CityID:       city,
		LocationName: location.Name,
		DateTime:     value.Format("2006-01-02 15:04:05"),
		POI:          &guide.Location{Latitude: location.Latitude, Longitude: location.Longitude},
	}
}

func planRequirements(values []session.SearchRequirementStateItem) []searchplan.Requirement {
	result := make([]searchplan.Requirement, 0, len(values))
	for _, value := range values {
		result = append(result, searchplan.Requirement{
			ID:               value.ID,
			Facet:            value.Facet,
			RawText:          value.RawText,
			RawValue:         value.RawValue,
			CanonicalValue:   value.CanonicalValue,
			Operator:         value.Operator,
			Importance:       value.Importance,
			Status:           value.Status,
			EntityID:         value.EntityID,
			EntityType:       value.EntityType,
			EntityBrandID:    value.EntityBrandID,
			EntityParentID:   value.EntityParentID,
			EntityResolution: value.EntityResolution,
			SemanticLabel:    value.SemanticLabel,
			Category:         value.Category,
			Value:            value.Value,
		})
	}
	return result
}
