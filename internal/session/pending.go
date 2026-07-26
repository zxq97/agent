package session

import "time"

const (
	DefaultPendingMaxMissedUserTurns = 2
	maxPendingHistory                = 20
)

type PendingStatus string

const (
	PendingActive     PendingStatus = "active"
	PendingResolved   PendingStatus = "resolved"
	PendingCancelled  PendingStatus = "cancelled"
	PendingExpired    PendingStatus = "expired"
	PendingSuperseded PendingStatus = "superseded"
	PendingSuspended  PendingStatus = "suspended"
)

type PendingType string

const (
	PendingSelectLocation      PendingType = "select_location"
	PendingClarifyPickupTime   PendingType = "clarify_pickup_time"
	PendingClarifyReturnTime   PendingType = "clarify_return_time"
	PendingSelectVehicleEntity PendingType = "select_vehicle_entity"
	PendingResolveHardConflict PendingType = "resolve_hard_conflict"
)

// PendingAction identifies work that can be blocked or revalidated. It is a
// stable orchestration value, not a Handler implementation name.
type PendingAction string

const (
	ActionModifyRentalContext       PendingAction = "modify_rental_context"
	ActionUpdateVehicleRequirements PendingAction = "update_vehicle_requirements"
	ActionExecuteVehicleSearch      PendingAction = "execute_vehicle_search"
)

type PendingOption struct {
	ID       string
	Label    string
	Value    string
	Location *LocationRef
}

type PendingContext struct {
	LocationQuery        string
	LocationID           string
	PickupTimeExpression string
	ReturnTimeExpression string
	PickupTime           *time.Time
	ReturnTime           *time.Time
	AmbiguousField       string
	AmbiguousRaw         string
}

type PendingInteraction struct {
	ID                    string
	Type                  PendingType
	Status                PendingStatus
	Question              string
	Options               []PendingOption
	WorkflowName          string
	Priority              int
	CreatedAt             time.Time
	LastPresentedAt       time.Time
	ExpireAt              time.Time
	ResolvedAt            *time.Time
	MissedUserTurns       int
	MaxMissedUserTurns    int
	BaseVersion           int64
	DependencyFingerprint string
	BlockingActions       []PendingAction
	Context               PendingContext
}

// DeferredAction records work that must be re-planned against the latest
// Session. It deliberately does not retain a frozen question or candidate
// list, which may be stale after the user answers another Pending.
type DeferredAction struct {
	ID                    string
	Action                PendingAction
	WorkflowName          string
	Reason                string
	EvidenceText          string
	BlockedByPendingID    string
	BaseVersion           int64
	DependencyFingerprint string
	CreatedAt             time.Time
}

type PendingStore struct {
	Active          *PendingInteraction
	DeferredActions []DeferredAction
	History         []PendingInteraction
}

// Offer makes interaction active when the Session has no active question. If
// another question is active, only the supplied revalidation action is kept.
func (s *PendingStore) Offer(interaction *PendingInteraction, deferred *DeferredAction) bool {
	if s == nil || interaction == nil {
		return false
	}
	initializePending(interaction)
	if s.Active == nil {
		s.Active = interaction
		return true
	}
	if deferred == nil {
		return false
	}
	if deferred.ID == "" {
		deferred.ID = interaction.ID
	}
	deferred.BlockedByPendingID = s.Active.ID
	if deferred.CreatedAt.IsZero() {
		deferred.CreatedAt = interaction.CreatedAt
	}
	s.AddDeferred(*deferred)
	return false
}

func initializePending(interaction *PendingInteraction) {
	interaction.Status = PendingActive
	if interaction.MaxMissedUserTurns <= 0 {
		interaction.MaxMissedUserTurns = DefaultPendingMaxMissedUserTurns
	}
	if interaction.LastPresentedAt.IsZero() {
		interaction.LastPresentedAt = interaction.CreatedAt
	}
}

// AddDeferred adds a revalidation descriptor once. Repeated execution of the
// same request must not grow the queue.
func (s *PendingStore) AddDeferred(action DeferredAction) {
	if s == nil {
		return
	}
	for i := range s.DeferredActions {
		if action.ID != "" && s.DeferredActions[i].ID == action.ID {
			s.DeferredActions[i] = action
			return
		}
	}
	s.DeferredActions = append(s.DeferredActions, action)
}

// Finish removes the active interaction and retains a bounded terminal record.
func (s *PendingStore) Finish(status PendingStatus, now time.Time) *PendingInteraction {
	if s == nil || s.Active == nil {
		return nil
	}
	finished := *s.Active
	finished.Status = status
	finished.ResolvedAt = timePointer(now)
	s.Active = nil
	s.History = append(s.History, finished)
	if len(s.History) > maxPendingHistory {
		s.History = append([]PendingInteraction(nil), s.History[len(s.History)-maxPendingHistory:]...)
	}
	return &finished
}

// Expire closes an Active Pending whose candidate or confirmation TTL elapsed.
func (s *PendingStore) Expire(now time.Time) *PendingInteraction {
	if s == nil || s.Active == nil || s.Active.ExpireAt.IsZero() || now.Before(s.Active.ExpireAt) {
		return nil
	}
	return s.Finish(PendingExpired, now)
}

// MarkNotAddressed counts a real user turn that had an opportunity to answer
// the same Pending. Once the configured limit is reached, the question is
// suspended so the assistant stops repeatedly asking it.
func (s *PendingStore) MarkNotAddressed(now time.Time) *PendingInteraction {
	if s == nil || s.Active == nil {
		return nil
	}
	s.Active.MissedUserTurns++
	if s.Active.MissedUserTurns < s.Active.MaxMissedUserTurns {
		s.Active.LastPresentedAt = now
		return nil
	}
	return s.Finish(PendingSuspended, now)
}

func (s *PendingStore) Blocks(action PendingAction) bool {
	if s == nil || s.Active == nil || s.Active.Status != PendingActive {
		return false
	}
	for _, blocked := range s.Active.BlockingActions {
		if blocked == action {
			return true
		}
	}
	return false
}

func (s *PendingStore) RemoveDeferred(id string) {
	if s == nil || id == "" {
		return
	}
	kept := s.DeferredActions[:0]
	for _, action := range s.DeferredActions {
		if action.ID != id {
			kept = append(kept, action)
		}
	}
	s.DeferredActions = kept
}

func (s *PendingStore) RemoveDeferredByAction(action PendingAction) {
	if s == nil {
		return
	}
	kept := s.DeferredActions[:0]
	for _, deferred := range s.DeferredActions {
		if deferred.Action != action {
			kept = append(kept, deferred)
		}
	}
	s.DeferredActions = kept
}

func timePointer(value time.Time) *time.Time { return &value }
