package session

import (
	"reflect"
	"time"

	"github.com/pkg/errors"
)

type DeltaType string

const (
	DeltaRequirements  DeltaType = "requirements"
	DeltaSearchPolicy  DeltaType = "search_policy"
	DeltaRentalContext DeltaType = "rental_context"
	DeltaSearchRuntime DeltaType = "search_runtime"
	DeltaPending       DeltaType = "pending"
	DeltaSearchDirty   DeltaType = "search_dirty"
)

// StateDelta is a typed request to change a SessionDraft. Domain components
// return deltas; only Reducer applies them to the working session.
type StateDelta interface {
	DeltaType() DeltaType
}

type RequirementDelta struct {
	Requirements      []SearchRequirementStateItem
	IncrementVersion  bool
	ActivateGoal      bool
	ClearNoPreference bool
	MemoryText        string
}

func (*RequirementDelta) DeltaType() DeltaType { return DeltaRequirements }

type SearchPolicyDelta struct {
	NoPreference       *bool
	PreferenceAskCount *int
	LastAskedAt        *time.Time
}

func (*SearchPolicyDelta) DeltaType() DeltaType { return DeltaSearchPolicy }

// RentalContextDelta contains the complete state owned by the rental context
// domain after one handler execution. It is intentionally narrower than a full
// AgentSession replacement so it cannot overwrite requirements or search cache.
type RentalContextDelta struct {
	Location    *LocationRef
	PickupTime  *time.Time
	ReturnTime  *time.Time
	Pending     PendingStore
	MemoryTexts []string
	ReceivedAt  time.Time
}

func (*RentalContextDelta) DeltaType() DeltaType { return DeltaRentalContext }

type SearchRuntimeDelta struct {
	DirtyReason           string
	Baseline              *GuideBaselineCache
	ActiveSearch          *ActiveSearchSnapshot
	LastResults           []VehicleResultRef
	RequirementResolution []RequirementResolutionUpdate
}

func (*SearchRuntimeDelta) DeltaType() DeltaType { return DeltaSearchRuntime }

type RequirementResolutionUpdate struct {
	RequirementID string
	Status        string
	Reason        string
}

type PendingDelta struct {
	Pending PendingStore
}

func (*PendingDelta) DeltaType() DeltaType { return DeltaPending }

type SearchDirtyDelta struct {
	Reason string
}

func (*SearchDirtyDelta) DeltaType() DeltaType { return DeltaSearchDirty }

type Reducer struct{}

func NewReducer() *Reducer {
	return &Reducer{}
}

func (r *Reducer) Apply(agentSession *AgentSession, deltas ...StateDelta) error {
	if agentSession == nil {
		return errors.New("session reducer: session is required")
	}
	for _, delta := range deltas {
		if delta == nil {
			continue
		}
		switch value := delta.(type) {
		case *RequirementDelta:
			applyRequirementDelta(agentSession, value)
		case *SearchPolicyDelta:
			applySearchPolicyDelta(agentSession, value)
		case *RentalContextDelta:
			applyRentalContextDelta(agentSession, value)
		case *SearchRuntimeDelta:
			applySearchRuntimeDelta(agentSession, value)
		case *PendingDelta:
			agentSession.Pending = clonePendingStore(value.Pending)
		case *SearchDirtyDelta:
			agentSession.Search.DirtyReason = value.Reason
			agentSession.Search.ActiveSearch = nil
		default:
			return errors.Errorf("session reducer: unsupported delta type %q", delta.DeltaType())
		}
	}
	return nil
}

func applyRequirementDelta(agentSession *AgentSession, delta *RequirementDelta) {
	changed := !reflect.DeepEqual(agentSession.Search.Requirements, delta.Requirements)
	agentSession.Search.Requirements = cloneRequirements(delta.Requirements)
	if delta.IncrementVersion && changed {
		agentSession.Search.RequirementVersion++
		agentSession.Search.ActiveSearch = nil
		agentSession.Search.DirtyReason = "requirements_changed"
	}
	if delta.ActivateGoal {
		agentSession.Search.Goal.Status = SearchGoalActive
	}
	if delta.ClearNoPreference {
		agentSession.Search.Goal.NoPreference = false
	}
	if delta.MemoryText != "" && changed {
		agentSession.Memory.RecentSearchCarTexts = append(agentSession.Memory.RecentSearchCarTexts, delta.MemoryText)
		if len(agentSession.Memory.RecentSearchCarTexts) > 10 {
			agentSession.Memory.RecentSearchCarTexts = append([]string(nil), agentSession.Memory.RecentSearchCarTexts[len(agentSession.Memory.RecentSearchCarTexts)-10:]...)
		}
	}
}

func applySearchPolicyDelta(agentSession *AgentSession, delta *SearchPolicyDelta) {
	if delta.NoPreference != nil {
		agentSession.Search.Goal.NoPreference = *delta.NoPreference
	}
	if delta.PreferenceAskCount != nil {
		agentSession.Search.Goal.PreferenceAskCount = *delta.PreferenceAskCount
	}
	if delta.LastAskedAt != nil {
		agentSession.Search.Goal.LastAskedAt = *delta.LastAskedAt
	}
}

func applyRentalContextDelta(agentSession *AgentSession, delta *RentalContextDelta) {
	changed := rentalContextChanged(agentSession, delta)
	appendRentalStateChanges(agentSession, delta)
	agentSession.Search.Location = cloneLocation(delta.Location)
	agentSession.Search.PickupTime = cloneTime(delta.PickupTime)
	agentSession.Search.ReturnTime = cloneTime(delta.ReturnTime)
	if changed {
		agentSession.Search.ActiveSearch = nil
		agentSession.Search.DirtyReason = "rental_context_changed"
	}
	agentSession.Pending = clonePendingStore(delta.Pending)
	agentSession.Memory.RecentRentalContextTexts = append([]string(nil), delta.MemoryTexts...)
}

func appendRentalStateChanges(agentSession *AgentSession, delta *RentalContextDelta) {
	if locationID(agentSession.Search.Location) != locationID(delta.Location) {
		agentSession.StateChanges = append(agentSession.StateChanges, StateChange{
			Field: "location", OldValue: cloneLocation(agentSession.Search.Location),
			NewValue: cloneLocation(delta.Location), Operation: "replace", CreatedAt: delta.ReceivedAt,
		})
	}
	if !timesEqual(agentSession.Search.PickupTime, delta.PickupTime) {
		agentSession.StateChanges = append(agentSession.StateChanges, StateChange{
			Field: "pickup_time", OldValue: cloneTime(agentSession.Search.PickupTime),
			NewValue: cloneTime(delta.PickupTime), Operation: "replace", CreatedAt: delta.ReceivedAt,
		})
	}
	if !timesEqual(agentSession.Search.ReturnTime, delta.ReturnTime) {
		agentSession.StateChanges = append(agentSession.StateChanges, StateChange{
			Field: "return_time", OldValue: cloneTime(agentSession.Search.ReturnTime),
			NewValue: cloneTime(delta.ReturnTime), Operation: "replace", CreatedAt: delta.ReceivedAt,
		})
	}
}

func rentalContextChanged(agentSession *AgentSession, delta *RentalContextDelta) bool {
	if locationID(agentSession.Search.Location) != locationID(delta.Location) {
		return true
	}
	if !timesEqual(agentSession.Search.PickupTime, delta.PickupTime) ||
		!timesEqual(agentSession.Search.ReturnTime, delta.ReturnTime) {
		return true
	}
	return false
}

func locationID(value *LocationRef) string {
	if value == nil {
		return ""
	}
	return value.ID
}

func timesEqual(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func applySearchRuntimeDelta(agentSession *AgentSession, delta *SearchRuntimeDelta) {
	agentSession.Search.DirtyReason = delta.DirtyReason
	agentSession.Search.Baseline = cloneBaseline(delta.Baseline)
	agentSession.Search.ActiveSearch = cloneSearchSnapshot(delta.ActiveSearch)
	agentSession.Search.LastResults = append([]VehicleResultRef(nil), delta.LastResults...)
	byID := make(map[string]RequirementResolutionUpdate, len(delta.RequirementResolution))
	for _, update := range delta.RequirementResolution {
		byID[update.RequirementID] = update
	}
	for index := range agentSession.Search.Requirements {
		update, exists := byID[agentSession.Search.Requirements[index].ID]
		if !exists {
			continue
		}
		agentSession.Search.Requirements[index].Status = update.Status
		agentSession.Search.Requirements[index].ResolutionReason = update.Reason
	}
}

func RentalDeltaFrom(agentSession *AgentSession, receivedAt time.Time) *RentalContextDelta {
	if agentSession == nil {
		return nil
	}
	return &RentalContextDelta{
		Location:    cloneLocation(agentSession.Search.Location),
		PickupTime:  cloneTime(agentSession.Search.PickupTime),
		ReturnTime:  cloneTime(agentSession.Search.ReturnTime),
		Pending:     clonePendingStore(agentSession.Pending),
		MemoryTexts: append([]string(nil), agentSession.Memory.RecentRentalContextTexts...),
		ReceivedAt:  receivedAt,
	}
}

func SearchRuntimeDeltaFrom(agentSession *AgentSession) *SearchRuntimeDelta {
	if agentSession == nil {
		return nil
	}
	delta := &SearchRuntimeDelta{
		DirtyReason:  agentSession.Search.DirtyReason,
		Baseline:     cloneBaseline(agentSession.Search.Baseline),
		ActiveSearch: cloneSearchSnapshot(agentSession.Search.ActiveSearch),
		LastResults:  append([]VehicleResultRef(nil), agentSession.Search.LastResults...),
	}
	for _, value := range agentSession.Search.Requirements {
		delta.RequirementResolution = append(delta.RequirementResolution, RequirementResolutionUpdate{
			RequirementID: value.ID,
			Status:        value.Status,
			Reason:        value.ResolutionReason,
		})
	}
	return delta
}

func PendingDeltaFrom(value PendingStore) *PendingDelta {
	return &PendingDelta{Pending: clonePendingStore(value)}
}
