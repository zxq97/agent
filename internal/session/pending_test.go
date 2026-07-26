package session

import (
	"testing"
	"time"
)

func TestPendingStoreOffersOnlyOneActiveInteraction(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	store := PendingStore{}
	first := &PendingInteraction{ID: "location", Type: PendingSelectLocation, CreatedAt: now}
	if !store.Offer(first, nil) {
		t.Fatal("first pending was not activated")
	}
	second := &PendingInteraction{ID: "vehicle", Type: PendingSelectVehicleEntity, CreatedAt: now}
	deferred := &DeferredAction{Action: ActionExecuteVehicleSearch, EvidenceText: "预算300"}
	if store.Offer(second, deferred) {
		t.Fatal("second pending unexpectedly replaced the active pending")
	}
	if store.Active == nil || store.Active.ID != first.ID {
		t.Fatalf("active pending = %#v", store.Active)
	}
	if len(store.DeferredActions) != 1 || store.DeferredActions[0].BlockedByPendingID != first.ID || store.DeferredActions[0].EvidenceText != "预算300" {
		t.Fatalf("deferred actions = %#v", store.DeferredActions)
	}
}

func TestPendingStoreSuspendsAfterConfiguredMisses(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	store := PendingStore{}
	store.Offer(&PendingInteraction{ID: "location", Type: PendingSelectLocation, CreatedAt: now, MaxMissedUserTurns: 2}, nil)
	if got := store.MarkNotAddressed(now.Add(time.Minute)); got != nil {
		t.Fatalf("first miss suspended pending: %#v", got)
	}
	got := store.MarkNotAddressed(now.Add(2 * time.Minute))
	if got == nil || got.Status != PendingSuspended || store.Active != nil {
		t.Fatalf("second miss result=%#v active=%#v", got, store.Active)
	}
	if len(store.History) != 1 || store.History[0].Status != PendingSuspended {
		t.Fatalf("history = %#v", store.History)
	}
}

func TestPendingStoreExpiresAndStopsBlocking(t *testing.T) {
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	store := PendingStore{}
	store.Offer(&PendingInteraction{ID: "location", Type: PendingSelectLocation, CreatedAt: now, ExpireAt: now.Add(time.Minute), BlockingActions: []PendingAction{ActionExecuteVehicleSearch}}, nil)
	if !store.Blocks(ActionExecuteVehicleSearch) {
		t.Fatal("active location pending did not block search")
	}
	expired := store.Expire(now.Add(time.Minute))
	if expired == nil || expired.Status != PendingExpired || store.Blocks(ActionExecuteVehicleSearch) {
		t.Fatalf("expired=%#v active=%#v", expired, store.Active)
	}
}
