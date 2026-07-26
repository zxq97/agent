package searchcar

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/progress"
	"github.com/zxq97/agent/internal/searchplan"
	"github.com/zxq97/agent/internal/session"
)

const (
	baselineServiceTTL = 15 * time.Minute
	baselineSafeTTL    = 14 * time.Minute
	maxQuoteScanPages  = 3
)

type SearchCarHandler struct {
	client   guide.Client
	compiler *searchplan.Compiler
	now      func() time.Time
}

func NewSearchCarHandler(client guide.Client, compiler *searchplan.Compiler, now func() time.Time) (*SearchCarHandler, error) {
	if client == nil {
		return nil, errors.New("search car: guide client is required")
	}
	if compiler == nil {
		compiler = searchplan.NewCompiler()
	}
	if now == nil {
		now = time.Now
	}
	return &SearchCarHandler{client: client, compiler: compiler, now: now}, nil
}

func (h *SearchCarHandler) Handle(ctx context.Context, agentSession *session.AgentSession, input *SearchCarInput) (*SearchCarResult, error) {
	if agentSession == nil {
		return nil, errors.New("search car: session is required")
	}
	if input == nil {
		return nil, errors.New("search car: input is required")
	}
	now := h.now()
	agentSession.Pending.Expire(now)
	if agentSession.Pending.Blocks(session.ActionExecuteVehicleSearch) {
		return &SearchCarResult{Status: ResultWaitingUser, InteractionID: agentSession.Pending.Active.ID, Message: agentSession.Pending.Active.Question}, nil
	}
	missing, err := validateRentalContext(agentSession.Search, now)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		return &SearchCarResult{Status: ResultNeedsContext, MissingFields: missing}, nil
	}

	size := input.PageSize
	if size <= 0 {
		size = 20
	}
	operation := input.Operation
	if operation == "" {
		operation = ParseOperation(input.EvidenceText)
	}

	if operation == OperationPreviousBatch {
		if result := previousBatch(agentSession); result != nil {
			progress.Emit(ctx, "result_navigation", "正在切换到上一批结果")
			h.saveResults(agentSession, result.Vehicles)
			return result, nil
		}
		operation = OperationSearchNow
	}

	if operation == OperationNextBatch && h.snapshotValid(agentSession, now) {
		if result := nextCachedBatch(agentSession); result != nil {
			progress.Emit(ctx, "result_navigation", "正在切换到下一批结果")
			h.saveResults(agentSession, result.Vehicles)
			return result, nil
		}
		progress.Emit(ctx, "vehicle_search", "正在继续搜索更多可用车辆")
		return h.continueSearch(ctx, agentSession, size, now)
	}
	if operation == OperationNextBatch && h.snapshotMatchesCurrentState(agentSession) &&
		agentSession.Search.ActiveSearch.Status == session.SearchSnapshotExhausted {
		return &SearchCarResult{Status: ResultNoResults, Message: "当前搜索条件下没有更多车辆了。"}, nil
	}

	progress.Emit(ctx, "vehicle_search", "正在按当前条件搜索可用车辆")
	return h.freshSearch(ctx, agentSession, size, now)
}

func (h *SearchCarHandler) freshSearch(ctx context.Context, agentSession *session.AgentSession, pageSize int, now time.Time) (*SearchCarResult, error) {
	baseline, err := h.ensureBaseline(ctx, agentSession, pageSize, now)
	if err != nil {
		return nil, err
	}
	plan := h.compiler.Compile(planRequirements(agentSession.Search.Requirements), baseline.Menu)
	applyResolutions(agentSession, plan.Resolutions)
	if blocking := plan.FirstBlockingResolution(); blocking != nil {
		return &SearchCarResult{
			Status:                 ResultCapabilityLimit,
			UnresolvedRequirements: resultsFor(plan.Resolutions, searchplan.CapabilityAmbiguous, searchplan.CapabilityUnverifiable, searchplan.CapabilityUnsupported),
			Message:                "当前无法可靠执行硬条件：" + blocking.RawText + "。" + blocking.Reason,
		}, nil
	}

	response := &guide.SearchResponse{ContextID: baseline.ContextID, VehRates: append([]guide.VehRate(nil), baseline.BaseQuotes...)}
	if len(plan.FilterCodes()) > 0 || plan.ServerSort != "" {
		response, err = h.client.SearchQuotes(ctx, buildRequest(agentSession.Search, plan.FilterCodes(), plan.ServerSort, baseline.ContextID, 1, pageSize))
		if err != nil {
			return nil, err
		}
		if response == nil {
			return nil, errors.New("search car: filtered response is empty")
		}
	}
	return h.finishSearch(ctx, agentSession, plan, response, 1, pageSize, now, true)
}

func (h *SearchCarHandler) continueSearch(ctx context.Context, agentSession *session.AgentSession, pageSize int, now time.Time) (*SearchCarResult, error) {
	snapshot := agentSession.Search.ActiveSearch
	page := snapshot.NextPage
	response, err := h.client.SearchQuotes(ctx, buildRequest(
		agentSession.Search,
		snapshot.Plan.FilterCodes(),
		snapshot.Plan.ServerSort,
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
	return h.finishSearch(ctx, agentSession, snapshot.Plan, response, page, pageSize, now, false)
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
	rawCount := len(response.VehRates)
	vehicles := searchplan.ApplyQuoteFilters(response.VehRates, plan.QuoteFilters)
	lastResponse := response
	lastPage := requestPage

	for len(vehicles) == 0 && len(plan.QuoteFilters) > 0 && rawCount > 0 && lastPage < requestPage+maxQuoteScanPages-1 {
		lastPage++
		next, err := h.client.SearchQuotes(ctx, buildRequest(
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
		vehicles = searchplan.ApplyQuoteFilters(next.VehRates, plan.QuoteFilters)
		if len(next.VehRates) == 0 {
			break
		}
	}

	vehicles = searchplan.Rerank(vehicles, plan.RankFactors)
	if fresh || agentSession.Search.ActiveSearch == nil {
		agentSession.Search.ActiveSearch = &session.ActiveSearchSnapshot{
			SearchID:              fmt.Sprintf("search-%d", now.UnixNano()),
			RentalFingerprint:     rentalFingerprint(agentSession.Search),
			RequirementVersion:    agentSession.Search.RequirementVersion,
			FilterPlanHash:        plan.PlanHash,
			BaselineContextID:     agentSession.Search.Baseline.ContextID,
			ContinuationContextID: lastResponse.ContextID,
			Plan:                  plan,
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
		Vehicles:    append([]guide.VehRate(nil), vehicles...),
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

func (h *SearchCarHandler) ensureBaseline(ctx context.Context, agentSession *session.AgentSession, pageSize int, now time.Time) (*session.GuideBaselineCache, error) {
	fingerprint := rentalFingerprint(agentSession.Search)
	if baseline := agentSession.Search.Baseline; baseline != nil &&
		baseline.Complete &&
		baseline.RentalFingerprint == fingerprint &&
		baseline.ContextID != "" &&
		len(baseline.Menu) > 0 &&
		now.Before(baseline.SafeExpiresAt) {
		return baseline, nil
	}
	response, err := h.client.SearchQuotes(ctx, buildRequest(agentSession.Search, nil, "", "", 1, pageSize))
	if err != nil {
		return nil, err
	}
	if response == nil || response.ContextID == "" || len(response.MenuGroup) == 0 {
		return nil, errors.New("search car: baseline response is incomplete")
	}
	baseline := &session.GuideBaselineCache{
		RentalFingerprint: fingerprint,
		ContextID:         response.ContextID,
		Menu:              append([]guide.MenuGroup(nil), response.MenuGroup...),
		BaseQuotes:        append([]guide.VehRate(nil), response.VehRates...),
		FirstReceivedAt:   now,
		ServiceExpiresAt:  now.Add(baselineServiceTTL),
		SafeExpiresAt:     now.Add(baselineSafeTTL),
		Complete:          true,
	}
	agentSession.Search.Baseline = baseline
	return baseline, nil
}

func (h *SearchCarHandler) snapshotValid(agentSession *session.AgentSession, now time.Time) bool {
	snapshot := agentSession.Search.ActiveSearch
	if snapshot == nil || snapshot.Status != session.SearchSnapshotActive {
		return false
	}
	return h.snapshotMatchesCurrentState(agentSession) &&
		snapshot.FilterPlanHash == snapshot.Plan.PlanHash &&
		snapshot.ContinuationContextID != "" &&
		now.Before(snapshot.ExpiresAt)
}

func (h *SearchCarHandler) snapshotMatchesCurrentState(agentSession *session.AgentSession) bool {
	snapshot := agentSession.Search.ActiveSearch
	if snapshot == nil {
		return false
	}
	return snapshot.RentalFingerprint == rentalFingerprint(agentSession.Search) &&
		snapshot.RequirementVersion == agentSession.Search.RequirementVersion
}

func previousBatch(agentSession *session.AgentSession) *SearchCarResult {
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
	return resultFromPlan(cachedBatchStatus(snapshot.Plan), snapshot.Plan, append([]guide.VehRate(nil), batch.Vehicles...), snapshot.ContinuationContextID, batch.RequestPage)
}

func nextCachedBatch(agentSession *session.AgentSession) *SearchCarResult {
	snapshot := agentSession.Search.ActiveSearch
	if snapshot == nil {
		return nil
	}
	for _, batch := range snapshot.Batches {
		if batch.RequestPage > snapshot.CurrentPage {
			snapshot.CurrentPage = batch.RequestPage
			return resultFromPlan(cachedBatchStatus(snapshot.Plan), snapshot.Plan, append([]guide.VehRate(nil), batch.Vehicles...), snapshot.ContinuationContextID, batch.RequestPage)
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

func rentalFingerprint(state session.SearchState) string {
	if state.Location == nil || state.PickupTime == nil || state.ReturnTime == nil {
		return ""
	}
	return state.Location.ID + "|" + state.PickupTime.Format(time.RFC3339Nano) + "|" + state.ReturnTime.Format(time.RFC3339Nano)
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
		})
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
