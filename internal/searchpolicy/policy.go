package searchpolicy

import (
	"time"

	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/session"
)

type Decision string

const (
	DecisionSkip          Decision = "skip"
	DecisionSearch        Decision = "search"
	DecisionAskPreference Decision = "ask_preference"
	DecisionWaitPending   Decision = "wait_pending"
)

type Input struct {
	ExplicitSearchRequested bool
	NoPreferenceExplicit    bool
	RequestedOperation      searchcar.SearchOperation

	RentalContextChanged bool
	RequirementsChanged  bool
	HadPreviousSearch    bool
	ReceivedAt           time.Time
}

type Result struct {
	Decision  Decision
	Operation searchcar.SearchOperation
	Message   string
	Deltas    []session.StateDelta
}

type Policy struct {
	maxPreferenceAsks int
	now               func() time.Time
}

func New(maxPreferenceAsks int, now func() time.Time) *Policy {
	if maxPreferenceAsks <= 0 {
		maxPreferenceAsks = 1
	}
	if now == nil {
		now = time.Now
	}
	return &Policy{maxPreferenceAsks: maxPreferenceAsks, now: now}
}

func (p *Policy) Evaluate(agentSession *session.AgentSession, input Input) Result {
	if agentSession == nil {
		return Result{Decision: DecisionSkip}
	}
	operation := input.RequestedOperation
	if operation == "" {
		operation = searchcar.OperationSearchNow
	}
	if input.RentalContextChanged || input.RequirementsChanged {
		operation = searchcar.OperationSearchNow
	}
	if agentSession.Pending.Blocks(session.ActionExecuteVehicleSearch) {
		return Result{Decision: DecisionWaitPending}
	}
	if input.ExplicitSearchRequested {
		if input.NoPreferenceExplicit && len(agentSession.Search.Requirements) == 0 {
			noPreference := true
			return Result{
				Decision:  DecisionSearch,
				Operation: operation,
				Deltas:    []session.StateDelta{&session.SearchPolicyDelta{NoPreference: &noPreference}},
			}
		}
		return Result{Decision: DecisionSearch, Operation: operation}
	}
	if input.RequirementsChanged {
		return Result{Decision: DecisionSearch, Operation: searchcar.OperationSearchNow}
	}
	if input.RentalContextChanged && input.HadPreviousSearch {
		return Result{Decision: DecisionSearch, Operation: searchcar.OperationSearchNow}
	}
	if input.RentalContextChanged && rentalContextComplete(agentSession.Search) {
		if len(agentSession.Search.Requirements) > 0 || agentSession.Search.Goal.NoPreference {
			return Result{Decision: DecisionSearch, Operation: searchcar.OperationSearchNow}
		}
		if agentSession.Search.Goal.PreferenceAskCount < p.maxPreferenceAsks {
			now := input.ReceivedAt
			if now.IsZero() {
				now = p.now()
			}
			nextAskCount := agentSession.Search.Goal.PreferenceAskCount + 1
			return Result{
				Decision: DecisionAskPreference,
				Message:  "对品牌、车型、座位数、能源类型或预算有要求吗？如果都可以，也可以直接告诉我开始搜索。",
				Deltas: []session.StateDelta{&session.SearchPolicyDelta{
					PreferenceAskCount: &nextAskCount,
					LastAskedAt:        &now,
				}},
			}
		}
		noPreference := true
		return Result{
			Decision:  DecisionSearch,
			Operation: searchcar.OperationSearchNow,
			Deltas:    []session.StateDelta{&session.SearchPolicyDelta{NoPreference: &noPreference}},
		}
	}
	return Result{Decision: DecisionSkip}
}

func rentalContextComplete(state session.SearchState) bool {
	return state.Location != nil && state.PickupTime != nil && state.ReturnTime != nil
}
