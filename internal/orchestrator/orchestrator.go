package orchestrator

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/domain/generalreply"
	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/domain/vehiclerequirement"
	"github.com/zxq97/agent/internal/searchpolicy"
	"github.com/zxq97/agent/internal/session"
)

var ordinalOptionPattern = regexp.MustCompile(`^\s*(?:第|选)?([1-9])(?:个|项)?(?:[，,。\s]|$)`)

type RentalContextHandler interface {
	Handle(context.Context, *session.AgentSession, *rentalcontext.ModifyRentalContextInput) (*rentalcontext.ModifyRentalContextResult, error)
}

type VehicleRequirementHandler interface {
	Handle(context.Context, *session.AgentSession, *vehiclerequirement.UpdateInput) (*vehiclerequirement.UpdateResult, error)
}

type SearchCarHandler interface {
	Handle(context.Context, *session.AgentSession, *searchcar.SearchCarInput) (*searchcar.SearchCarResult, error)
}

type GeneralReplyHandler interface {
	Handle(context.Context, *session.AgentSession, *generalreply.Input) (*generalreply.Result, error)
}

type SearchPolicy interface {
	Evaluate(*session.AgentSession, searchpolicy.Input) searchpolicy.Result
}

// TurnRequest contains Router-approved domain evidence. Requirement updates and
// explicit search requests are separate so one can run without the other.
type TurnRequest struct {
	SourceText string

	RentalContext      *rentalcontext.ModifyRentalContextInput
	VehicleRequirement *vehiclerequirement.UpdateInput
	SearchRequest      *searchcar.SearchCarInput
	GeneralReply       *generalreply.Input
}

type TurnResult struct {
	RentalContext      []*rentalcontext.ModifyRentalContextResult
	VehicleRequirement *vehiclerequirement.UpdateResult
	SearchCar          *searchcar.SearchCarResult
	GeneralReply       *generalreply.Result

	ActivePending     *session.PendingInteraction
	RevalidateActions []session.DeferredAction
	ExpiredPending    *session.PendingInteraction
	SuspendedPending  *session.PendingInteraction
}

// Orchestrator executes the Router-selected domains serially, then asks the
// deterministic SearchPolicy whether Guide search should run.
type Orchestrator struct {
	rental      RentalContextHandler
	requirement VehicleRequirementHandler
	search      SearchCarHandler
	policy      SearchPolicy
	now         func() time.Time
	general     GeneralReplyHandler
}

func New(
	rental RentalContextHandler,
	requirement VehicleRequirementHandler,
	search SearchCarHandler,
	policy SearchPolicy,
	now func() time.Time,
	general ...GeneralReplyHandler,
) *Orchestrator {
	if now == nil {
		now = time.Now
	}
	if policy == nil {
		policy = searchpolicy.New(1, now)
	}
	var generalHandler GeneralReplyHandler
	if len(general) > 0 {
		generalHandler = general[0]
	}
	return &Orchestrator{rental: rental, requirement: requirement, search: search, policy: policy, now: now, general: generalHandler}
}

func (o *Orchestrator) Execute(ctx context.Context, agentSession *session.AgentSession, request *TurnRequest) (*TurnResult, error) {
	if agentSession == nil {
		return nil, errors.New("orchestrator execute: session is required")
	}
	if request == nil {
		return nil, errors.New("orchestrator execute: request is required")
	}
	now := o.now()
	startingPendingID := ""
	if agentSession.Pending.Active != nil {
		startingPendingID = agentSession.Pending.Active.ID
	}
	hadPreviousSearch := agentSession.Search.ActiveSearch != nil || len(agentSession.Search.LastResults) > 0
	result := &TurnResult{ExpiredPending: agentSession.Pending.Expire(now)}
	addressed := result.ExpiredPending != nil

	residual := request.SourceText
	rentalRan := false
	requirementRan := false
	searchRan := false
	rentalChanged := false
	requirementsChanged := false
	var fallbackTexts []string

	if active := agentSession.Pending.Active; active != nil {
		var cancelled bool
		residual, cancelled = removeCancellation(residual)
		if cancelled {
			agentSession.Pending.Finish(session.PendingCancelled, now)
			agentSession.Pending.RemoveDeferredByAction(session.ActionExecuteVehicleSearch)
			addressed = true
		} else if active.Type == session.PendingSelectLocation {
			option, rest := selectPendingOption(active.Options, residual)
			if option != nil {
				if o.rental == nil {
					return nil, errors.New("orchestrator execute: rental context handler is required to resolve location pending")
				}
				rentalResult, err := o.rental.Handle(ctx, agentSession, &rentalcontext.ModifyRentalContextInput{
					Command: &rentalcontext.ModifyRentalContextCommand{LocationID: option.ID, InteractionID: active.ID},
				})
				if err != nil {
					return nil, err
				}
				result.RentalContext = append(result.RentalContext, rentalResult)
				residual = rest
				rentalRan = true
				rentalChanged = rentalResult.Status == rentalcontext.ResultSuccess && len(rentalResult.ModifiedFields) > 0
				addressed = agentSession.Pending.Active == nil || agentSession.Pending.Active.ID != startingPendingID
			}
		}
	}

	if request.RentalContext != nil {
		if o.rental == nil {
			return nil, errors.New("orchestrator execute: rental context handler is required")
		}
		input := *request.RentalContext
		if input.SourceText == "" || addressed {
			input.SourceText = residual
		}
		if input.Command != nil || hasMeaningfulText(input.SourceText) {
			rentalResult, err := o.rental.Handle(ctx, agentSession, &input)
			if err != nil && !errors.Is(err, rentalcontext.ErrDomainMismatch) {
				return nil, err
			}
			if err == nil {
				result.RentalContext = append(result.RentalContext, rentalResult)
				rentalRan = true
				rentalChanged = rentalChanged || (rentalResult.Status == rentalcontext.ResultSuccess && len(rentalResult.ModifiedFields) > 0)
			} else {
				fallbackTexts = appendUniqueText(fallbackTexts, input.SourceText)
			}
		}
	}

	// Requirement extraction is deliberately allowed to run even when a
	// location/time Pending blocks Guide search, so mixed input is not lost.
	if request.VehicleRequirement != nil {
		if o.requirement == nil {
			return nil, errors.New("orchestrator execute: vehicle requirement handler is required")
		}
		input := *request.VehicleRequirement
		if input.SourceText == "" {
			input.SourceText = request.SourceText
		}
		requirementResult, err := o.requirement.Handle(ctx, agentSession, &input)
		if err != nil && !errors.Is(err, vehiclerequirement.ErrDomainMismatch) {
			return nil, err
		}
		if err == nil {
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
	}
	if request.SearchRequest != nil {
		policyInput.SearchEvidence = request.SearchRequest.EvidenceText
		policyInput.RequestedOperation = request.SearchRequest.Operation
	}
	decision := o.policy.Evaluate(agentSession, policyInput)
	switch decision.Decision {
	case searchpolicy.DecisionAskPreference:
		result.SearchCar = searchcar.NeedsRequirementResult(decision.Message)
	case searchpolicy.DecisionSearch:
		if o.search == nil {
			return nil, errors.New("orchestrator execute: search car handler is required")
		}
		searchInput := &searchcar.SearchCarInput{Operation: decision.Operation}
		if request.SearchRequest != nil {
			searchInput.EvidenceText = request.SearchRequest.EvidenceText
			searchInput.PageSize = request.SearchRequest.PageSize
		}
		searchResult, err := o.search.Handle(ctx, agentSession, searchInput)
		if err != nil {
			return nil, err
		}
		result.SearchCar = searchResult
		searchRan = true
	case searchpolicy.DecisionWaitPending, searchpolicy.DecisionSkip:
	}

	generalInput := request.GeneralReply
	if generalInput != nil {
		fallbackTexts = appendUniqueText(fallbackTexts, generalInput.SourceText)
	}
	if len(fallbackTexts) > 0 && o.general != nil {
		input := &generalreply.Input{SourceText: strings.Join(fallbackTexts, "\n")}
		if generalInput != nil {
			input.RecentMessages = append([]generalreply.Message(nil), generalInput.RecentMessages...)
		}
		generalResult, err := o.general.Handle(ctx, agentSession, input)
		if err != nil {
			return nil, err
		}
		result.GeneralReply = generalResult
	}

	if startingPendingID != "" {
		if agentSession.Pending.Active == nil || agentSession.Pending.Active.ID != startingPendingID {
			addressed = true
		}
		if addressed {
			for _, deferred := range deferredBlockedBy(agentSession.Pending.DeferredActions, startingPendingID) {
				if actionWasRevalidated(deferred.Action, rentalRan, requirementRan, searchRan) {
					agentSession.Pending.RemoveDeferred(deferred.ID)
					continue
				}
				result.RevalidateActions = append(result.RevalidateActions, deferred)
			}
		} else {
			result.SuspendedPending = agentSession.Pending.MarkNotAddressed(now)
		}
	}
	result.ActivePending = agentSession.Pending.Active
	return result, nil
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

func removeCancellation(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	for _, phrase := range []string{"先不搜了", "不用了", "算了", "取消"} {
		if strings.HasPrefix(trimmed, phrase) {
			return strings.TrimSpace(trimmed[len(phrase):]), true
		}
	}
	return text, false
}

func selectPendingOption(options []session.PendingOption, text string) (*session.PendingOption, string) {
	if match := ordinalOptionPattern.FindStringSubmatchIndex(text); len(match) >= 4 {
		value, err := strconv.Atoi(text[match[2]:match[3]])
		if err == nil && value > 0 && value <= len(options) {
			return &options[value-1], strings.TrimSpace(text[:match[0]] + text[match[1]:])
		}
	}
	var selected *session.PendingOption
	start, end := -1, -1
	for index := range options {
		for _, candidate := range []string{options[index].Label, options[index].Value} {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			matchIndex := strings.Index(strings.ToLower(text), strings.ToLower(candidate))
			if matchIndex < 0 {
				continue
			}
			if selected != nil && selected.ID != options[index].ID {
				return nil, text
			}
			selected = &options[index]
			start, end = matchIndex, matchIndex+len(candidate)
		}
	}
	if selected == nil {
		return nil, text
	}
	return selected, strings.TrimSpace(text[:start] + text[end:])
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
