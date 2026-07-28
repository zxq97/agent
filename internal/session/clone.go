package session

import (
	"time"

	"github.com/zxq97/agent/internal/requirement"
	"github.com/zxq97/agent/internal/searchruntime"
)

// Clone returns an independent working copy of an AgentSession. It is the
// transitional SessionDraft boundary: handlers may mutate the copy, while the
// loaded base session remains unchanged until the turn is committed.
func Clone(value *AgentSession) *AgentSession {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Search = cloneSearchState(value.Search)
	cloned.Pending = clonePendingStore(value.Pending)
	cloned.StateChanges = cloneStateChanges(value.StateChanges)
	cloned.Memory.RecentRentalContextTexts = append([]string(nil), value.Memory.RecentRentalContextTexts...)
	cloned.Memory.RecentSearchCarTexts = append([]string(nil), value.Memory.RecentSearchCarTexts...)
	return &cloned
}

func cloneStateChanges(values []StateChange) []StateChange {
	result := make([]StateChange, len(values))
	for index := range values {
		result[index] = values[index]
		result[index].OldValue = cloneStateChangeValue(values[index].OldValue)
		result[index].NewValue = cloneStateChangeValue(values[index].NewValue)
	}
	return result
}

func cloneStateChangeValue(value any) any {
	switch typed := value.(type) {
	case *LocationRef:
		return cloneLocation(typed)
	case *time.Time:
		return cloneTime(typed)
	default:
		return value
	}
}

func cloneSearchState(value SearchState) SearchState {
	cloned := value
	cloned.Location = cloneLocation(value.Location)
	cloned.PickupTime = cloneTime(value.PickupTime)
	cloned.ReturnTime = cloneTime(value.ReturnTime)
	cloned.Requirements = cloneRequirements(value.Requirements)
	cloned.Baseline = cloneBaseline(value.Baseline)
	cloned.ActiveSearch = cloneSearchSnapshot(value.ActiveSearch)
	cloned.LastResults = append([]VehicleResultRef(nil), value.LastResults...)
	return cloned
}

func cloneRequirements(values []SearchRequirementStateItem) []SearchRequirementStateItem {
	result := make([]SearchRequirementStateItem, len(values))
	for index := range values {
		result[index] = values[index]
		result[index].Value = cloneRequirementValue(values[index].Value)
	}
	return result
}

func cloneRequirementValue(value requirement.Value) requirement.Value {
	cloned := value
	if value.Number != nil {
		number := *value.Number
		cloned.Number = &number
	}
	if value.Range != nil {
		valueRange := *value.Range
		if value.Range.Min != nil {
			minimum := *value.Range.Min
			valueRange.Min = &minimum
		}
		if value.Range.Max != nil {
			maximum := *value.Range.Max
			valueRange.Max = &maximum
		}
		cloned.Range = &valueRange
	}
	if value.Entity != nil {
		entity := *value.Entity
		cloned.Entity = &entity
	}
	return cloned
}

func cloneBaseline(value *GuideBaselineCache) *GuideBaselineCache {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Menu = searchruntime.CloneMenus(value.Menu)
	cloned.BaseQuotes = searchruntime.CloneQuotes(value.BaseQuotes)
	return &cloned
}

func cloneSearchSnapshot(value *ActiveSearchSnapshot) *ActiveSearchSnapshot {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.SeenQuoteIDs = cloneSet(value.SeenQuoteIDs)
	cloned.SeenVehicleCodes = cloneSet(value.SeenVehicleCodes)
	cloned.Batches = make([]SearchResultBatch, len(value.Batches))
	for index := range value.Batches {
		cloned.Batches[index] = value.Batches[index]
		cloned.Batches[index].Vehicles = searchruntime.CloneQuotes(value.Batches[index].Vehicles)
	}
	return &cloned
}

func clonePendingStore(value PendingStore) PendingStore {
	cloned := value
	cloned.Active = clonePendingInteraction(value.Active)
	cloned.DeferredActions = append([]DeferredAction(nil), value.DeferredActions...)
	cloned.History = make([]PendingInteraction, len(value.History))
	for index := range value.History {
		item := clonePendingInteraction(&value.History[index])
		cloned.History[index] = *item
	}
	return cloned
}

// ClonePendingStore returns an independent copy for deterministic Pending
// transition calculation before a PendingDelta is applied by Reducer.
func ClonePendingStore(value PendingStore) PendingStore {
	return clonePendingStore(value)
}

func clonePendingInteraction(value *PendingInteraction) *PendingInteraction {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Options = make([]PendingOption, len(value.Options))
	for index := range value.Options {
		cloned.Options[index] = value.Options[index]
		cloned.Options[index].Location = cloneLocation(value.Options[index].Location)
	}
	cloned.BlockingActions = append([]PendingAction(nil), value.BlockingActions...)
	cloned.ResolvedAt = cloneTime(value.ResolvedAt)
	cloned.Context.PickupTime = cloneTime(value.Context.PickupTime)
	cloned.Context.ReturnTime = cloneTime(value.Context.ReturnTime)
	return &cloned
}

func cloneLocation(value *LocationRef) *LocationRef {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneSet(value map[string]struct{}) map[string]struct{} {
	if value == nil {
		return nil
	}
	cloned := make(map[string]struct{}, len(value))
	for key := range value {
		cloned[key] = struct{}{}
	}
	return cloned
}
