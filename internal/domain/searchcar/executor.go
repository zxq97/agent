package searchcar

import (
	"context"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/guide"
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
	applyResolutions(agentSession, plan.Resolutions)
	if blocking := executionPlan.FirstBlockingResolution(); blocking != nil {
		agentSession.Search.DirtyReason = "capability_limit"
		return &SearchCarResult{
			Status:                 ResultCapabilityLimit,
			UnresolvedRequirements: resultsFor(plan.Resolutions, searchplan.CapabilityAmbiguous, searchplan.CapabilityUnverifiable, searchplan.CapabilityUnsupported),
			Message:                "当前无法可靠执行硬条件：" + blocking.RawText + "。" + blocking.Reason,
			CapabilityResolutions:  executionPlan.Resolutions,
		}, nil
	}

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
	if result != nil {
		result.CapabilityResolutions = executionPlan.Resolutions
	}
	return result, err
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
