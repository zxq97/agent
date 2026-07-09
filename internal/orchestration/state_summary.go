package orchestration

import (
	"fmt"
	"strings"
	"time"

	"github.com/zxq97/rental-agent/internal/types"
)

// SummarizeForLog 生成一段可 grep 的 "[state] ..." 汇总日志,把内存里的 context
// 主字段一次性铺开:rental / profile / needs 三桶 / last_search / last_quotes / cached_menu / history / summary。
//
// 铁律:
//   - context_id / reference_id / supplier 这些内部 ID 一律用 <yes>/<no> 标记,不打原值
//   - 每条一行,前缀统一 "[state] session=... turn=..." 便于 grep 一个 session 的完整演化
//
// tag 用于标记打这份汇总的时机("pipeline_start" / "search_written" / ...),
// 便于同一轮内两次汇总对比 diff。
func SummarizeForLog(s *ConversationState, tag string) string {
	if s == nil {
		return "[state] tag=" + tag + " (nil state)"
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var b strings.Builder
	fmt.Fprintf(&b, "[state] tag=%s session=%s user=%s turn=%d phase=%s\n",
		tag, s.SessionID, s.UserID, s.TurnCount, s.Phase)

	writeRentalLine(&b, s)
	writeProfileLine(&b, s)
	writeNeedsLines(&b, s.Constraints)
	writeLastSearchLine(&b, s.LastSearch)
	writeLastQuotesLine(&b, s)
	writeMenuLine(&b, s.CachedMenu)
	writeHistoryLine(&b, s)

	return strings.TrimRight(b.String(), "\n")
}

func writeRentalLine(b *strings.Builder, s *ConversationState) {
	r := s.Rental
	pickupReady := r.PickupPOI.Valid()
	fmt.Fprintf(b, "  rental: pickup=%q city_id=%d pickup_poi=%s pickup_time=%s dropoff=%q dropoff_poi=%s dropoff_time=%s context_id=%s pickup_ready=%v pending_pickup_text=%q\n",
		r.PickupName, r.PickupCityID, presenceTagForBool(r.PickupPOI.Valid()), formatTime(r.PickupTime),
		r.DropoffName, presenceTagForBool(r.DropoffPOI.Valid()), formatTime(r.DropoffTime),
		presenceTag(r.ContextID), pickupReady, s.Slot.PickupText,
	)
}

func writeProfileLine(b *strings.Builder, s *ConversationState) {
	p := s.Profile
	if p.TripScene == "" && p.Companions == "" && p.PriceSensitivity == "" && p.StylePreference == "" {
		b.WriteString("  profile: (empty)\n")
		return
	}
	fmt.Fprintf(b, "  profile: trip_scene=%q companions=%q price_sensitivity=%q style_preference=%q\n",
		p.TripScene, p.Companions, p.PriceSensitivity, p.StylePreference)
}

func writeNeedsLines(b *strings.Builder, c types.SearchConstraints) {
	if len(c.Hard) == 0 && len(c.Soft) == 0 && len(c.Negative) == 0 {
		b.WriteString("  needs: (empty)\n")
		return
	}
	writeNeedsBucket(b, "needs.hard", c.Hard)
	writeNeedsBucket(b, "needs.soft", c.Soft)
	writeNeedsBucket(b, "needs.negative", c.Negative)
}

func writeNeedsBucket(b *strings.Builder, label string, ns []types.UserNeed) {
	if len(ns) == 0 {
		return
	}
	parts := make([]string, 0, len(ns))
	for _, n := range ns {
		parts = append(parts, fmt.Sprintf("%s=%v/%s/%.2f(born=%d,reinf=%d)",
			n.Type, n.Value, n.Hardness, n.Confidence, n.BornTurn, n.LastReinforced))
	}
	fmt.Fprintf(b, "  %s: [%s]\n", label, strings.Join(parts, ", "))
}

func writeLastSearchLine(b *strings.Builder, ls *types.LastSearchState) {
	if ls == nil {
		b.WriteString("  last_search: (none)\n")
		return
	}
	fmt.Fprintf(b, "  last_search: mode=%s filter_codes=%v page=%d page_size=%d has_more=%v shown=%d excluded=%d relax=%d price_range=[%.0f,%.0f]\n",
		ls.SearchMode, ls.FilterCodes, ls.Page, ls.PageSize, ls.HasMore,
		len(ls.ShownRefs), len(ls.ExcludedRefs), ls.RelaxLevel, ls.MinPrice, ls.MaxPrice)
}

func writeLastQuotesLine(b *strings.Builder, s *ConversationState) {
	if len(s.LastQuotes) == 0 {
		b.WriteString("  last_quotes: (none)\n")
		return
	}
	age := "?"
	if !s.QuoteAt.IsZero() {
		age = time.Since(s.QuoteAt).Round(time.Second).String()
	}
	limit := len(s.LastQuotes)
	if limit > 6 {
		limit = 6
	}
	parts := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		q := s.LastQuotes[i]
		name := q.CarName
		if name == "" {
			name = q.BrandName
		}
		parts = append(parts, fmt.Sprintf("[%d]%s ¥%.0f/日(total=%.0f,ref=%s,supplier=%s)",
			q.Index, name, q.DailyPrice, q.TotalPrice,
			presenceTag(q.ReferenceID), presenceTag(q.Supplier)))
	}
	fmt.Fprintf(b, "  last_quotes[%d/%d age=%s]: %s\n", limit, len(s.LastQuotes), age, strings.Join(parts, " "))
	fmt.Fprintf(b, "  selected_ref: %s\n", presenceTag(s.SelectedRef))
}

func writeMenuLine(b *strings.Builder, menu []types.MenuGroupView) {
	if len(menu) == 0 {
		b.WriteString("  cached_menu: (none)\n")
		return
	}
	codes := make([]string, 0, len(menu))
	for _, g := range menu {
		codes = append(codes, fmt.Sprintf("%s(%d)", g.GroupCode, len(g.Items)))
	}
	fmt.Fprintf(b, "  cached_menu: %d groups %v\n", len(menu), codes)
}

func writeHistoryLine(b *strings.Builder, s *ConversationState) {
	tool := 0
	for _, h := range s.History {
		if h.ToolCall != nil {
			tool++
		}
	}
	fmt.Fprintf(b, "  history: %d entries (%d tool_calls) summary=%s\n",
		len(s.History), tool, presenceTag(s.Summary))
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

// presenceTag 用于内部 ID / 摘要类字段:只反映"有没有",不打原值。
// 守 ID 铁律,同时避免炸屏。
func presenceTag(v string) string {
	if v == "" {
		return "<no>"
	}
	return "<yes>"
}

func presenceTagForBool(ok bool) string {
	if ok {
		return "<yes>"
	}
	return "<no>"
}
