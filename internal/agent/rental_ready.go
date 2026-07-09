package agent

import (
	"time"

	"github.com/zxq97/rental-agent/internal/orchestration"
)

func applyDecisionRentalTimes(st *orchestration.ConversationState, dec *Decision) {
	if st == nil || dec == nil {
		return
	}
	if dec.PickupTimeText != "" {
		if t, err := parseRentalTime(dec.PickupTimeText); err == nil {
			st.Rental.PickupTime = t
		}
	}
	if dec.DropoffTimeText != "" {
		if t, err := parseRentalTime(dec.DropoffTimeText); err == nil {
			st.Rental.DropoffTime = t
		}
	}
}

func rentalSearchReady(st *orchestration.ConversationState) bool {
	return len(missingRentalSlots(st)) == 0
}

func missingRentalSlots(st *orchestration.ConversationState) []string {
	if st == nil {
		return []string{"pickup_location", "pickup_time", "dropoff_time"}
	}
	var slots []string
	if !st.Rental.PickupPOI.Valid() {
		slots = append(slots, "pickup_location")
	}
	if st.Rental.PickupTime.IsZero() {
		slots = append(slots, "pickup_time")
	}
	if st.Rental.DropoffTime.IsZero() {
		slots = append(slots, "dropoff_time")
	}
	return slots
}

func rentalMissingClarification(st *orchestration.ConversationState) *Clarification {
	missing := missingRentalSlots(st)
	if len(missing) == 0 {
		return nil
	}
	switch missing[0] {
	case "pickup_location":
		return &Clarification{
			Question: "先确认下取车地点吧,你想在哪个城市、哪个位置取车呢?",
			Slot:     "pickup_location",
		}
	case "pickup_time":
		return &Clarification{
			Question: "取车时间也得先定一下,你打算哪天几点取车?",
			Options:  []string{"明天 10:00", "明天 14:00", "后天 10:00"},
			Slot:     "pickup_time",
		}
	case "dropoff_time":
		return &Clarification{
			Question: "还车时间也确认下吧,你预计哪天几点还车?",
			Options:  []string{"取车后 1 天", "取车后 2 天", "取车后 3 天"},
			Slot:     "dropoff_time",
		}
	default:
		return nil
	}
}

func rentalTimeForGuide(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(tycheTimeLayout)
}
