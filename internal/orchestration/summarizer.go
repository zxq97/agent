package orchestration

import (
	"fmt"
	"strings"
)

const maxRawHistoryEntries = 12

// MaybeSummarize compresses old conversation entries using a deterministic template.
func MaybeSummarize(s *ConversationState) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.History) <= maxRawHistoryEntries {
		return
	}
	cut := len(s.History) - maxRawHistoryEntries
	if cut < 4 {
		cut = 4
	}
	if cut > len(s.History) {
		cut = len(s.History)
	}
	summary := summarizeEntries(s.History[:cut])
	if summary != "" {
		if s.Summary != "" {
			s.Summary += "\n"
		}
		s.Summary += summary
	}
	remaining := make([]HistoryEntry, len(s.History[cut:]))
	copy(remaining, s.History[cut:])
	s.History = remaining
}

func summarizeEntries(entries []HistoryEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var userParts, assistantParts []string
	for _, e := range entries {
		if e.Msg == nil || e.Msg.Content == "" {
			continue
		}
		switch e.Msg.Role {
		case "user":
			userParts = append(userParts, trimRunes(e.Msg.Content, 30))
		case "assistant":
			assistantParts = append(assistantParts, trimRunes(e.Msg.Content, 30))
		}
	}
	if len(userParts) == 0 && len(assistantParts) == 0 {
		return ""
	}
	rounds := len(entries) / 2
	if rounds < 1 {
		rounds = 1
	}
	return fmt.Sprintf("第 1-%d 轮:用户问 %s,助手 %s", rounds, strings.Join(userParts, " / "), strings.Join(assistantParts, " / "))
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
