package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
)

// BuildStatePrefix 构造注入到末条 user 前的结构化会话状态。严禁放任何内部 ID。
func BuildStatePrefix(state *orchestration.ConversationState, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## 当前会话状态\n")
	fmt.Fprintf(&b, "- now: %s %s\n", now.Format("2006-01-02 15:04"), weekdayCN(now))
	if state == nil {
		b.WriteString("- current_rental: 暂无\n")
		return b.String()
	}
	writeRentalPrefix(&b, state)
	if state.Summary != "" {
		fmt.Fprintf(&b, "- summary: %s\n", state.Summary)
	}
	writeProfilePrefix(&b, state)
	needs := orchestration.NeedsStatePrefix(state.Constraints)
	if needs != "【当前需求状态】(空)" {
		b.WriteString("- needs:\n")
		for _, line := range strings.Split(strings.TrimSpace(needs), "\n") {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	writeQuotesPrefix(&b, state)
	return b.String()
}

func writeProfilePrefix(b *strings.Builder, state *orchestration.ConversationState) {
	p := state.Profile
	if p.TripScene == "" && p.Companions == "" && p.PriceSensitivity == "" && p.StylePreference == "" {
		return
	}
	b.WriteString("- profile:\n")
	if p.TripScene != "" {
		fmt.Fprintf(b, "  trip_scene: %s\n", p.TripScene)
	}
	if p.Companions != "" {
		fmt.Fprintf(b, "  companions: %s\n", p.Companions)
	}
	if p.PriceSensitivity != "" {
		fmt.Fprintf(b, "  price_sensitivity: %s\n", p.PriceSensitivity)
	}
	if p.StylePreference != "" {
		fmt.Fprintf(b, "  style_preference: %s\n", p.StylePreference)
	}
}

func writeRentalPrefix(b *strings.Builder, state *orchestration.ConversationState) {
	r := state.Rental
	pickupReady := r.PickupPOI.Valid()
	searchReady := rentalSearchReady(state)
	if r.PickupName == "" && r.DropoffName == "" && r.PickupTime.IsZero() && r.DropoffTime.IsZero() {
		b.WriteString("- current_rental: 暂无\n")
		fmt.Fprintf(b, "- pickup_ready: false  # 未解析出真实取车 POI,禁止调 search_vehicles\n")
		fmt.Fprintf(b, "- search_ready: false  # 未确认完整取还车地点/时间,禁止调 search_vehicles\n")
		fmt.Fprintf(b, "- missing_rental_slots: [pickup_location, pickup_time, dropoff_time]\n")
		if state.Slot.PickupText != "" {
			fmt.Fprintf(b, "- pending_pickup_text: %q  # 上一轮抽到但尚未解析成功\n", state.Slot.PickupText)
		}
		return
	}
	b.WriteString("- current_rental:\n")
	if r.PickupName != "" {
		fmt.Fprintf(b, "  pickup: %s\n", r.PickupName)
	}
	if !r.PickupTime.IsZero() {
		fmt.Fprintf(b, "  pickup_time: %s\n", r.PickupTime.Format("2006-01-02 15:04"))
	}
	if r.DropoffName != "" {
		fmt.Fprintf(b, "  dropoff: %s\n", r.DropoffName)
	}
	if !r.DropoffTime.IsZero() {
		fmt.Fprintf(b, "  dropoff_time: %s\n", r.DropoffTime.Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(b, "- pickup_ready: %v\n", pickupReady)
	fmt.Fprintf(b, "- search_ready: %v\n", searchReady)
	if !searchReady {
		fmt.Fprintf(b, "- missing_rental_slots: %v\n", missingRentalSlots(state))
	}
}

func writeQuotesPrefix(b *strings.Builder, state *orchestration.ConversationState) {
	_, quotes, age := state.SnapshotQuotes()
	if len(quotes) == 0 || age > tools.QuoteTTL {
		return
	}
	b.WriteString("- last_quotes:\n")
	limit := len(quotes)
	if limit > 3 {
		limit = 3
	}
	for i := 0; i < limit; i++ {
		q := quotes[i]
		name := q.CarName
		if name == "" {
			name = q.BrandName
		}
		if name == "" {
			name = fmt.Sprintf("第%d辆", q.Index)
		}
		price := ""
		if q.DailyPrice > 0 {
			price = fmt.Sprintf(", day_price=%.0f", q.DailyPrice)
		}
		if q.TotalPrice > 0 {
			price += fmt.Sprintf(", total=%.0f", q.TotalPrice)
		}
		fmt.Fprintf(b, "  - index=%d, name=%s%s\n", q.Index, name, price)
	}
}

func weekdayCN(t time.Time) string {
	switch t.Weekday() {
	case time.Sunday:
		return "周日"
	case time.Monday:
		return "周一"
	case time.Tuesday:
		return "周二"
	case time.Wednesday:
		return "周三"
	case time.Thursday:
		return "周四"
	case time.Friday:
		return "周五"
	case time.Saturday:
		return "周六"
	default:
		return ""
	}
}
