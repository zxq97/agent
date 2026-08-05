package orchestrator

import (
	"context"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/domain"
	"github.com/zxq97/agent/internal/domain/generalreply"
	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/domain/rentalrules"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/domain/vehiclecompare"
	"github.com/zxq97/agent/internal/domain/vehiclerequirement"
	"github.com/zxq97/agent/internal/pendingresolver"
	"github.com/zxq97/agent/internal/planner"
	"github.com/zxq97/agent/internal/searchpolicy"
	"github.com/zxq97/agent/internal/session"
	"github.com/zxq97/agent/internal/turnnormalizer"
)

type SearchPolicy interface {
	Evaluate(*session.AgentSession, searchpolicy.Input) searchpolicy.Result
}

type TurnContext struct {
	RequestID   string
	ClientSeq   int64
	UserID      string
	SessionID   string
	SourceText  string
	ReceivedAt  time.Time
	BaseVersion int64
}

// TurnRequest contains Router-approved domain evidence. Requirement updates and
// explicit search requests are separate so one can run without the other.
type TurnRequest struct {
	Context    TurnContext
	SourceText string
	ReceivedAt time.Time
	Plan       planner.ActionPlan

	RentalContext      *rentalcontext.Input
	VehicleRequirement *vehiclerequirement.Input
	SearchRequest      *searchcar.Input
	VehicleComparison  *vehiclecompare.Input
	RentalRules        *rentalrules.Input
	GeneralReply       *generalreply.Input
}

type TurnResult struct {
	RentalContext      []*rentalcontext.Result
	VehicleRequirement *vehiclerequirement.Result
	SearchCar          *searchcar.Result
	VehicleComparison  *vehiclecompare.Result
	RentalRules        *rentalrules.Result
	GeneralReply       *generalreply.Result

	ActivePending     *session.PendingInteraction
	RevalidateActions []session.DeferredAction
	ExpiredPending    *session.PendingInteraction
	SuspendedPending  *session.PendingInteraction
	FailedActions     []FailedAction
}

type FailedAction struct {
	Action     string
	ReasonCode string
	Cause      error
}

// Orchestrator executes the Router-selected domains serially, then asks the
// deterministic SearchPolicy whether Guide search should run.
type Orchestrator struct {
	rental      rentalcontext.Handler
	requirement vehiclerequirement.Handler
	search      searchcar.Handler
	policy      SearchPolicy
	reducer     *session.Reducer
	pending     *pendingresolver.Resolver
	planner     *planner.Planner
	now         func() time.Time
	general     generalreply.Handler
	comparison  vehiclecompare.Handler
	rules       rentalrules.Handler
}

func NewWithExtensions(
	rental rentalcontext.Handler,
	requirement vehiclerequirement.Handler,
	search searchcar.Handler,
	policy SearchPolicy,
	now func() time.Time,
	general generalreply.Handler,
	comparison vehiclecompare.Handler,
	rules rentalrules.Handler,
) (*Orchestrator, error) {
	if rental == nil || requirement == nil || search == nil || policy == nil ||
		now == nil || general == nil || comparison == nil || rules == nil {
		return nil, errors.New("orchestrator: all handlers, policy and clock are required")
	}
	return &Orchestrator{
		rental:      rental,
		requirement: requirement,
		search:      search,
		policy:      policy,
		reducer:     session.NewReducer(),
		pending:     pendingresolver.New(),
		planner:     planner.New(),
		now:         now,
		general:     general,
		comparison:  comparison,
		rules:       rules,
	}, nil
}

func (o *Orchestrator) Execute(ctx context.Context, agentSession *session.AgentSession, request *TurnRequest) (*TurnResult, error) {
	if agentSession == nil {
		return nil, errors.New("orchestrator execute: session is required")
	}
	if request == nil {
		return nil, errors.New("orchestrator execute: request is required")
	}
	requestCopy := *request
	request = &requestCopy
	if request.SourceText == "" {
		request.SourceText = request.Context.SourceText
	}
	request.Plan = o.ensurePlan(request)
	request.Plan.BindBaseVersion(request.Context.BaseVersion)
	applyActionPlan(request)
	now := request.Context.ReceivedAt
	if now.IsZero() {
		now = request.ReceivedAt
	}
	if now.IsZero() {
		now = o.now()
	}
	startingPendingID := ""
	if agentSession.Pending.Active != nil {
		startingPendingID = agentSession.Pending.Active.ID
	}
	hadPreviousSearch := agentSession.Search.ActiveSearch != nil || len(agentSession.Search.LastResults) > 0
	pendingState := session.ClonePendingStore(agentSession.Pending)
	expiredPending := pendingState.Expire(now)
	if err := o.reducer.Apply(agentSession, session.PendingDeltaFrom(pendingState)); err != nil {
		return nil, err
	}
	result := &TurnResult{ExpiredPending: expiredPending}
	addressed := result.ExpiredPending != nil

	residual := request.SourceText
	rentalRan := false
	requirementRan := false
	searchRan := false
	rentalChanged := false
	requirementsChanged := false
	searchBlockedByFailure := false
	var fallbackTexts []string

	if active := agentSession.Pending.Active; active != nil {
		resolution := o.pending.Resolve(active, residual)
		residual = resolution.ResidualText
		if resolution.Event == pendingresolver.EventCancelled {
			pendingState := session.ClonePendingStore(agentSession.Pending)
			pendingState.Finish(session.PendingCancelled, now)
			pendingState.RemoveDeferredByAction(session.ActionExecuteVehicleSearch)
			if err := o.reducer.Apply(agentSession, session.PendingDeltaFrom(pendingState)); err != nil {
				return nil, err
			}
			addressed = true
		} else if active.Type == session.PendingSelectLocation {
			if option := resolution.SelectedOption; option != nil {
				rentalResult, err := o.rental.Handle(ctx, agentSession, &rentalcontext.Input{
					Command:    &rentalcontext.Command{LocationID: option.ID, InteractionID: active.ID},
					ReceivedAt: now,
				})
				if err != nil {
					return nil, err
				}
				if err := o.reducer.Apply(agentSession, rentalResult.Deltas...); err != nil {
					return nil, err
				}
				result.RentalContext = append(result.RentalContext, rentalResult)
				rentalRan = true
				rentalChanged = rentalResult.Status == rentalcontext.ResultSuccess && len(rentalResult.ModifiedFields) > 0
				addressed = agentSession.Pending.Active == nil || agentSession.Pending.Active.ID != startingPendingID
			}
		}
	}

	if addressed && startingPendingID != "" {
		deferred := deferredBlockedBy(agentSession.Pending.DeferredActions, startingPendingID)
		var candidates []planner.Candidate
		for _, action := range deferred {
			actionType, ok := plannerAction(action.Action)
			if !ok {
				continue
			}
			candidates = append(candidates, planner.Candidate{
				Type:         actionType,
				EvidenceText: action.EvidenceText,
				SourceID:     action.ID,
				BaseVersion:  action.BaseVersion,
				BlockedBy:    action.BlockedByPendingID,
			})
		}
		request.Plan = o.planner.Merge(request.Plan, candidates)
		applyActionPlan(request)
	}

	if request.RentalContext != nil {
		input := *request.RentalContext
		if input.SourceText == "" || addressed {
			input.SourceText = residual
		}
		input.ReceivedAt = now
		if input.Command != nil || hasMeaningfulText(input.SourceText) {
			rentalResult, err := o.rental.Handle(ctx, agentSession, &input)
			if err != nil && !errors.Is(err, domain.ErrDomainMismatch) {
				return nil, err
			}
			if err == nil {
				if err := o.reducer.Apply(agentSession, rentalResult.Deltas...); err != nil {
					return nil, err
				}
				result.RentalContext = append(result.RentalContext, rentalResult)
				rentalRan = true
				rentalChanged = rentalChanged || (rentalResult.Status == rentalcontext.ResultSuccess && len(rentalResult.ModifiedFields) > 0)
			} else {
				fallbackTexts = appendUniqueText(fallbackTexts, input.SourceText)
			}
		}
	}

	if startingPendingID != "" &&
		(agentSession.Pending.Active == nil || agentSession.Pending.Active.ID != startingPendingID) {
		addressed = true
		deferred := deferredBlockedBy(agentSession.Pending.DeferredActions, startingPendingID)
		var candidates []planner.Candidate
		for _, action := range deferred {
			actionType, ok := plannerAction(action.Action)
			if !ok {
				continue
			}
			candidates = append(candidates, planner.Candidate{
				Type:         actionType,
				EvidenceText: action.EvidenceText,
				SourceID:     action.ID,
				BaseVersion:  action.BaseVersion,
				BlockedBy:    action.BlockedByPendingID,
			})
		}
		request.Plan = o.planner.Merge(request.Plan, candidates)
		applyActionPlan(request)
	}

	// Requirement extraction is deliberately allowed to run even when a
	// location/time Pending blocks Guide search, so mixed input is not lost.
	if request.VehicleRequirement != nil {
		input := *request.VehicleRequirement
		if input.SourceText == "" {
			input.SourceText = request.SourceText
		}
		requirementResult, err := o.requirement.Handle(ctx, agentSession, &input)
		if err != nil && !errors.Is(err, domain.ErrDomainMismatch) {
			if !rentalChanged && !addressed {
				return nil, err
			}
			result.FailedActions = append(result.FailedActions, FailedAction{
				Action:     string(session.ActionUpdateVehicleRequirements),
				ReasonCode: "requirement_extraction_failed",
				Cause:      err,
			})
			searchBlockedByFailure = true
		}
		if err == nil {
			if err := o.reducer.Apply(agentSession, requirementResult.Deltas...); err != nil {
				return nil, err
			}
			result.VehicleRequirement = requirementResult
			requirementRan = true
			requirementsChanged = requirementResult.Changed
		} else {
			fallbackTexts = appendUniqueText(fallbackTexts, input.SourceText)
		}
	}

	explicitSearch := request.SearchRequest != nil
	policyInput := searchpolicy.Input{
		ExplicitSearchRequested: explicitSearch,
		RentalContextChanged:    rentalChanged,
		RequirementsChanged:     requirementsChanged,
		HadPreviousSearch:       hadPreviousSearch,
		ReceivedAt:              now,
	}
	if request.SearchRequest != nil {
		policyInput.NoPreferenceExplicit = request.SearchRequest.NoPreferenceExplicit
		policyInput.RequestedOperation = request.SearchRequest.Operation
	}
	decision := searchpolicy.Result{Decision: searchpolicy.DecisionSkip}
	if !searchBlockedByFailure {
		decision = o.policy.Evaluate(agentSession, policyInput)
		if err := o.reducer.Apply(agentSession, decision.Deltas...); err != nil {
			return nil, err
		}
	}
	switch decision.Decision {
	case searchpolicy.DecisionAskPreference:
		result.SearchCar = searchcar.NeedsRequirementResult(decision.Message)
	case searchpolicy.DecisionSearch:
		searchInput := &searchcar.Input{Operation: decision.Operation, ReceivedAt: now}
		if request.SearchRequest != nil {
			searchInput.EvidenceText = request.SearchRequest.EvidenceText
			searchInput.NoPreferenceExplicit = request.SearchRequest.NoPreferenceExplicit
			searchInput.PageSize = request.SearchRequest.PageSize
		}
		searchResult, err := o.search.Handle(ctx, agentSession, searchInput)
		if err != nil {
			reasonCode := "search_external_failure"
			if isSearchContextError(err) {
				reasonCode = "search_invalid_context"
			} else if err := o.reducer.Apply(agentSession, &session.SearchDirtyDelta{Reason: "guide_error"}); err != nil {
				return nil, err
			}
			result.FailedActions = append(result.FailedActions, FailedAction{
				Action:     string(session.ActionExecuteVehicleSearch),
				ReasonCode: reasonCode,
				Cause:      err,
			})
			break
		}
		if err := o.reducer.Apply(agentSession, searchResult.Deltas...); err != nil {
			return nil, err
		}
		result.SearchCar = searchResult
		searchRan = true
	case searchpolicy.DecisionWaitPending, searchpolicy.DecisionSkip:
	}

	if request.VehicleComparison != nil {
		comparisonResult, err := o.comparison.Handle(ctx, agentSession, request.VehicleComparison)
		if err != nil {
			return nil, err
		}
		result.VehicleComparison = comparisonResult
	}

	if request.RentalRules != nil {
		ruleResult, err := o.rules.Handle(ctx, request.RentalRules)
		if err != nil {
			return nil, err
		}
		result.RentalRules = ruleResult
	}

	generalInput := request.GeneralReply
	if generalInput != nil {
		fallbackTexts = appendUniqueText(fallbackTexts, generalInput.SourceText)
	}
	if len(fallbackTexts) > 0 {
		input := &generalreply.Input{SourceText: strings.Join(fallbackTexts, "\n")}
		if generalInput != nil {
			input.RecentMessages = append([]generalreply.Message(nil), generalInput.RecentMessages...)
		}
		generalResult, err := o.general.Handle(ctx, agentSession, input)
		if err != nil {
			result.FailedActions = append(result.FailedActions, FailedAction{
				Action:     string(planner.ActionGeneralReply),
				ReasonCode: "general_reply_failure",
				Cause:      err,
			})
			result.GeneralReply = &generalreply.Result{Message: "这部分内容暂时无法回答，但已确认的租车条件会保留。"}
		} else {
			result.GeneralReply = generalResult
		}
	}

	if startingPendingID != "" {
		if agentSession.Pending.Active == nil || agentSession.Pending.Active.ID != startingPendingID {
			addressed = true
		}
		if addressed {
			pendingState := session.ClonePendingStore(agentSession.Pending)
			for _, deferred := range deferredBlockedBy(agentSession.Pending.DeferredActions, startingPendingID) {
				if actionWasRevalidated(deferred.Action, rentalRan, requirementRan, searchRan) {
					pendingState.RemoveDeferred(deferred.ID)
					continue
				}
				result.RevalidateActions = append(result.RevalidateActions, deferred)
			}
			if err := o.reducer.Apply(agentSession, session.PendingDeltaFrom(pendingState)); err != nil {
				return nil, err
			}
		} else {
			pendingState := session.ClonePendingStore(agentSession.Pending)
			result.SuspendedPending = pendingState.MarkNotAddressed(now)
			if err := o.reducer.Apply(agentSession, session.PendingDeltaFrom(pendingState)); err != nil {
				return nil, err
			}
		}
	}
	result.ActivePending = agentSession.Pending.Active
	return result, nil
}

func isSearchContextError(err error) bool {
	return errors.Is(err, searchcar.ErrReturnNotAfterPickup) ||
		errors.Is(err, searchcar.ErrPickupNotFuture) ||
		errors.Is(err, searchcar.ErrInvalidCityID)
}

func appendUniqueText(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (o *Orchestrator) ensurePlan(request *TurnRequest) planner.ActionPlan {
	if len(request.Plan.Actions) > 0 {
		return request.Plan
	}
	var candidates []planner.Candidate
	if request.RentalContext != nil {
		candidates = append(candidates, planner.Candidate{Type: planner.ActionModifyRentalContext, EvidenceText: request.RentalContext.SourceText})
	}
	if request.VehicleRequirement != nil {
		candidates = append(candidates, planner.Candidate{Type: planner.ActionUpdateVehicleRequirements, EvidenceText: request.VehicleRequirement.SourceText})
	}
	if request.SearchRequest != nil {
		candidates = append(candidates, planner.Candidate{Type: planner.ActionExecuteVehicleSearch, EvidenceText: request.SearchRequest.EvidenceText})
	}
	if request.VehicleComparison != nil {
		candidates = append(candidates, planner.Candidate{Type: planner.ActionCompareVehicles, EvidenceText: request.VehicleComparison.EvidenceText})
	}
	if request.RentalRules != nil {
		candidates = append(candidates, planner.Candidate{Type: planner.ActionQueryRentalRules, EvidenceText: request.RentalRules.EvidenceText})
	}
	if request.GeneralReply != nil && request.GeneralReply.SourceText != "" {
		candidates = append(candidates, planner.Candidate{Type: planner.ActionGeneralReply, EvidenceText: request.GeneralReply.SourceText})
	}
	return o.planner.Build(candidates)
}

func applyActionPlan(request *TurnRequest) {
	if request == nil {
		return
	}
	if action := request.Plan.Action(planner.ActionModifyRentalContext); action != nil {
		if request.RentalContext == nil {
			request.RentalContext = &rentalcontext.Input{}
		}
		input := *request.RentalContext
		input.SourceText = appendEvidence(input.SourceText, action.EvidenceText)
		request.RentalContext = &input
	}
	if action := request.Plan.Action(planner.ActionUpdateVehicleRequirements); action != nil {
		if request.VehicleRequirement == nil {
			request.VehicleRequirement = &vehiclerequirement.Input{}
		}
		input := *request.VehicleRequirement
		input.SourceText = appendEvidence(input.SourceText, action.EvidenceText)
		request.VehicleRequirement = &input
	}
	if action := request.Plan.Action(planner.ActionExecuteVehicleSearch); action != nil {
		if request.SearchRequest == nil {
			request.SearchRequest = &searchcar.Input{}
		}
		input := *request.SearchRequest
		input.EvidenceText = appendEvidence(input.EvidenceText, action.EvidenceText)
		if input.Operation == "" {
			signals := turnnormalizer.NormalizeSearch(input.EvidenceText)
			input.Operation = searchcar.SearchOperation(signals.Operation)
			input.NoPreferenceExplicit = signals.NoPreference
		}
		request.SearchRequest = &input
	}
	if action := request.Plan.Action(planner.ActionCompareVehicles); action != nil {
		if request.VehicleComparison == nil {
			request.VehicleComparison = &vehiclecompare.Input{}
		}
		input := *request.VehicleComparison
		input.EvidenceText = appendEvidence(input.EvidenceText, action.EvidenceText)
		request.VehicleComparison = &input
	}
	if action := request.Plan.Action(planner.ActionQueryRentalRules); action != nil {
		if request.RentalRules == nil {
			request.RentalRules = &rentalrules.Input{}
		}
		input := *request.RentalRules
		input.EvidenceText = appendEvidence(input.EvidenceText, action.EvidenceText)
		request.RentalRules = &input
	}
	if action := request.Plan.Action(planner.ActionGeneralReply); action != nil {
		if request.GeneralReply == nil {
			request.GeneralReply = &generalreply.Input{}
		}
		input := *request.GeneralReply
		input.SourceText = appendEvidence(input.SourceText, action.EvidenceText)
		request.GeneralReply = &input
	}
}

func appendEvidence(current, addition string) string {
	current = strings.TrimSpace(current)
	addition = strings.TrimSpace(addition)
	if current == "" {
		return addition
	}
	if addition == "" || current == addition || strings.Contains(current, addition) {
		return current
	}
	return current + "\n" + addition
}

func plannerAction(action session.PendingAction) (planner.ActionType, bool) {
	switch action {
	case session.ActionModifyRentalContext:
		return planner.ActionModifyRentalContext, true
	case session.ActionUpdateVehicleRequirements:
		return planner.ActionUpdateVehicleRequirements, true
	case session.ActionExecuteVehicleSearch:
		return planner.ActionExecuteVehicleSearch, true
	default:
		return "", false
	}
}

func actionWasRevalidated(action session.PendingAction, rentalRan, requirementRan, searchRan bool) bool {
	switch action {
	case session.ActionModifyRentalContext:
		return rentalRan
	case session.ActionUpdateVehicleRequirements:
		return requirementRan
	case session.ActionExecuteVehicleSearch:
		return searchRan
	default:
		return false
	}
}

func hasMeaningfulText(text string) bool {
	return strings.Trim(text, " \t\r\n，,。.!！?？") != ""
}

func deferredBlockedBy(actions []session.DeferredAction, pendingID string) []session.DeferredAction {
	var result []session.DeferredAction
	for _, action := range actions {
		if action.BlockedByPendingID == pendingID {
			result = append(result, action)
		}
	}
	return result
}
