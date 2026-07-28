package searchcar

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/capability"
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
	baseline  baselineProvider
	executor  searchExecutor
	processor resultProcessor
	compiler  *searchplan.ExecutionCompiler
	now       func() time.Time
}

func NewSearchCarHandler(client guide.Client, compiler *searchplan.Compiler, now func() time.Time, resolvers ...capability.Resolver) (*SearchCarHandler, error) {
	if client == nil {
		return nil, errors.New("search car: guide client is required")
	}
	if compiler == nil {
		compiler = searchplan.NewCompiler()
	}
	if now == nil {
		now = time.Now
	}
	var resolver capability.Resolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	}
	executor := &guideSearchExecutor{client: client}
	return &SearchCarHandler{
		baseline: newGuideBaselineProvider(executor), executor: executor,
		processor: quoteResultProcessor{},
		compiler:  searchplan.NewExecutionCompiler(compiler, resolver), now: now,
	}, nil
}

func (h *SearchCarHandler) Handle(ctx context.Context, agentSession *session.AgentSession, input *SearchCarInput) (result *SearchCarResult, err error) {
	if agentSession == nil {
		return nil, errors.New("search car: session is required")
	}
	if input == nil {
		return nil, errors.New("search car: input is required")
	}
	working := session.Clone(agentSession)
	defer func() {
		if err == nil && result != nil {
			result.Deltas = []session.StateDelta{session.SearchRuntimeDeltaFrom(working)}
		}
	}()
	now := input.ReceivedAt
	if now.IsZero() {
		now = h.now()
	}
	if working.Pending.Blocks(session.ActionExecuteVehicleSearch) {
		return &SearchCarResult{Status: ResultWaitingUser, InteractionID: working.Pending.Active.ID, Message: working.Pending.Active.Question}, nil
	}
	missing, err := validateRentalContext(working.Search, now)
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
		executionPlan, valid := h.validContinuationPlan(ctx, working, now)
		if valid {
			if cached := previousBatch(working, executionPlan.FilterPlan); cached != nil {
				progress.Emit(ctx, "result_navigation", "正在切换到上一批结果")
				h.saveResults(working, cached.Vehicles)
				return cached, nil
			}
		}
		operation = OperationSearchNow
	}

	if operation == OperationNextBatch {
		executionPlan, valid := h.validContinuationPlan(ctx, working, now)
		if valid && working.Search.ActiveSearch.Status == session.SearchSnapshotExhausted {
			return &SearchCarResult{Status: ResultNoResults, Message: "当前搜索条件下没有更多车辆了。"}, nil
		}
		if valid {
			if cached := nextCachedBatch(working, executionPlan.FilterPlan); cached != nil {
				progress.Emit(ctx, "result_navigation", "正在切换到下一批结果")
				h.saveResults(working, cached.Vehicles)
				return cached, nil
			}
			progress.Emit(ctx, "vehicle_search", "正在继续搜索更多可用车辆")
			return h.continueSearch(ctx, working, executionPlan.FilterPlan, size, now)
		}
	}

	progress.Emit(ctx, "vehicle_search", "正在按当前条件搜索可用车辆")
	return h.freshSearch(ctx, working, size, now)
}
