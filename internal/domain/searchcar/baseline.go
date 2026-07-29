package searchcar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/capability"
	"github.com/zxq97/agent/internal/searchplan"
	"github.com/zxq97/agent/internal/searchruntime"
	"github.com/zxq97/agent/internal/session"
)

type baselineProvider interface {
	GetOrFetch(context.Context, *session.AgentSession, int, time.Time) (*session.GuideBaselineCache, error)
}

type guideBaselineProvider struct {
	executor searchExecutor
}

func newGuideBaselineProvider(executor searchExecutor) *guideBaselineProvider {
	return &guideBaselineProvider{executor: executor}
}

func (h *SearchCarHandler) ensureBaseline(ctx context.Context, agentSession *session.AgentSession, pageSize int, now time.Time) (*session.GuideBaselineCache, error) {
	return h.baseline.GetOrFetch(ctx, agentSession, pageSize, now)
}

func (p *guideBaselineProvider) GetOrFetch(ctx context.Context, agentSession *session.AgentSession, pageSize int, now time.Time) (*session.GuideBaselineCache, error) {
	fingerprint := rentalFingerprint(agentSession.Search)
	if baseline := agentSession.Search.Baseline; baseline != nil &&
		baseline.Complete &&
		baseline.RentalFingerprint == fingerprint &&
		baseline.ContextID != "" &&
		len(baseline.Menu) > 0 &&
		now.Before(baseline.SafeExpiresAt) {
		return baseline, nil
	}
	response, err := p.executor.Execute(ctx, buildRequest(agentSession.Search, nil, "", "", 1, pageSize))
	if err != nil {
		return nil, err
	}
	if response == nil || response.ContextID == "" || len(response.MenuGroup) == 0 {
		return nil, errors.New("search car: baseline response is incomplete")
	}
	baseline := &session.GuideBaselineCache{
		RentalFingerprint: fingerprint,
		ContextID:         response.ContextID,
		Menu:              searchruntime.MenusFromGuide(response.MenuGroup),
		BaseQuotes:        searchruntime.QuotesFromGuide(response.VehRates),
		FirstReceivedAt:   now,
		ServiceExpiresAt:  now.Add(baselineServiceTTL),
		SafeExpiresAt:     now.Add(baselineSafeTTL),
		Complete:          true,
	}
	agentSession.Search.Baseline = baseline
	return baseline, nil
}

var _ baselineProvider = (*guideBaselineProvider)(nil)

func (h *SearchCarHandler) validContinuationPlan(ctx context.Context, agentSession *session.AgentSession, now time.Time) (searchplan.SearchExecutionPlan, bool) {
	snapshot := agentSession.Search.ActiveSearch
	if snapshot == nil || (snapshot.Status != session.SearchSnapshotActive && snapshot.Status != session.SearchSnapshotExhausted) {
		return searchplan.SearchExecutionPlan{}, false
	}
	if !h.snapshotMatchesCurrentState(agentSession) ||
		snapshot.ContinuationContextID == "" ||
		!now.Before(snapshot.ExpiresAt) {
		return searchplan.SearchExecutionPlan{}, false
	}
	executionPlan := h.compileExecutionPlan(ctx, agentSession, agentSession.Search.Baseline)
	for _, requirementID := range snapshot.RelaxedRequirementIDs {
		var ok bool
		executionPlan.FilterPlan, ok = searchplan.RelaxRequirement(executionPlan.FilterPlan, requirementID)
		if !ok {
			return searchplan.SearchExecutionPlan{}, false
		}
	}
	executionPlan.PlanHash = executionPlan.FilterPlan.PlanHash
	if executionPlan.FilterPlan.PlanHash != snapshot.FilterPlanHash {
		return searchplan.SearchExecutionPlan{}, false
	}
	return executionPlan, true
}

func (h *SearchCarHandler) snapshotMatchesCurrentState(agentSession *session.AgentSession) bool {
	snapshot := agentSession.Search.ActiveSearch
	if snapshot == nil {
		return false
	}
	return snapshot.RentalFingerprint == rentalFingerprint(agentSession.Search) &&
		snapshot.RequirementVersion == agentSession.Search.RequirementVersion &&
		snapshot.CapabilityVersion == h.compiler.CatalogVersion() &&
		agentSession.Search.Baseline != nil &&
		snapshot.RuntimeFingerprint == searchplan.RuntimeFingerprint(
			h.runtimeCapabilityContext(agentSession.Search, agentSession.Search.Baseline),
		)
}

func rentalFingerprint(state session.SearchState) string {
	if state.Location == nil || state.PickupTime == nil || state.ReturnTime == nil {
		return ""
	}
	return state.Location.ID + "|" + state.PickupTime.Format(time.RFC3339Nano) + "|" + state.ReturnTime.Format(time.RFC3339Nano)
}

func (h *SearchCarHandler) runtimeCapabilityContext(state session.SearchState, baseline *session.GuideBaselineCache) capability.RuntimeContext {
	return capability.RuntimeContext{
		MenuFingerprint:   menuFingerprint(baseline.Menu),
		MenuCodes:         menuCodes(baseline.Menu),
		ResultFields:      guideResultFields(),
		CatalogVersion:    h.compiler.CatalogVersion(),
		RentalFingerprint: rentalFingerprint(state),
	}
}

func menuCodes(menu []searchruntime.MenuGroup) map[string]struct{} {
	result := make(map[string]struct{})
	for _, group := range menu {
		for _, itemGroup := range group.GroupItems {
			for _, item := range itemGroup.Items {
				if item.Code != "" {
					result[item.Code] = struct{}{}
				}
			}
		}
	}
	return result
}

func menuFingerprint(menu []searchruntime.MenuGroup) string {
	data, _ := json.Marshal(menu)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func guideResultFields() map[string]struct{} {
	return map[string]struct{}{
		"vehicle.vehicle_name":          {},
		"vehicle.vehicle_code":          {},
		"vehicle.brand_name":            {},
		"vehicle.group_name":            {},
		"vehicle.seats":                 {},
		"vehicle.fuel_type":             {},
		"vehicle.transmission_type":     {},
		"total_charge.total_amount":     {},
		"total_charge.deduction_amount": {},
		"daily_deduction_amount":        {},
		"reference_info.reference_id":   {},
	}
}
