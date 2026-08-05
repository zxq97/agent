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

// Handler executes a vehicle search from the current session state.
type Handler interface {
	Handle(context.Context, *session.AgentSession, *Input) (*Result, error)
}

type handler struct {
	baseline  baselineProvider
	executor  searchExecutor
	processor resultProcessor
	compiler  *searchplan.ExecutionCompiler
	now       func() time.Time
}

func NewHandler(client guide.Client, compiler *searchplan.Compiler, now func() time.Time, resolver capability.Resolver) (Handler, error) {
	if client == nil {
		return nil, errors.New("search car: guide client is required")
	}
	if compiler == nil {
		return nil, errors.New("search car: filter compiler is required")
	}
	if now == nil {
		return nil, errors.New("search car: clock is required")
	}
	if resolver == nil {
		return nil, errors.New("search car: capability resolver is required")
	}
	executionCompiler, err := searchplan.NewExecutionCompiler(compiler, resolver)
	if err != nil {
		return nil, err
	}
	executor := &guideSearchExecutor{client: client}
	return &handler{
		baseline: newGuideBaselineProvider(executor), executor: executor,
		processor: quoteResultProcessor{},
		compiler:  executionCompiler, now: now,
	}, nil
}

func (h *handler) Handle(ctx context.Context, agentSession *session.AgentSession, input *Input) (result *Result, err error) {
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
		return &Result{Status: ResultWaitingUser, InteractionID: working.Pending.Active.ID, Message: working.Pending.Active.Question}, nil
	}
	missing, err := validateRentalContext(working.Search, now)
	if err != nil {
		return nil, err
	}
	if len(missing) > 0 {
		return &Result{Status: ResultNeedsContext, MissingFields: missing}, nil
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
		return &Result{Status: ResultRejected, Message: "当前没有可返回的上一批结果；如果搜索状态已经过期，请明确刷新后重新搜索。"}, nil
	}

	if operation == OperationNextBatch {
		executionPlan, valid := h.validContinuationPlan(ctx, working, now)
		if valid && working.Search.ActiveSearch.Status == session.SearchSnapshotExhausted {
			return &Result{Status: ResultNoResults, Message: "当前搜索条件下没有更多车辆了。"}, nil
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
		return &Result{Status: ResultRejected, Message: "当前搜索快照已过期或条件已经变化，不能继续原分页；请明确刷新或重新搜索。"}, nil
	}

	progress.Emit(ctx, "vehicle_search", "正在按当前条件搜索可用车辆")
	return h.freshSearch(ctx, working, size, now)
}
