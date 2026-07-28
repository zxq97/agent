// Package pendingresolver interprets deterministic answers to an existing
// PendingInteraction. It returns a hypothesis only; domain handlers still
// validate the selected provider-backed option.
package pendingresolver

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/zxq97/agent/internal/session"
)

var ordinalOptionPattern = regexp.MustCompile(`^\s*(?:第|选)?([1-9])(?:个|项)?(?:[，,。\s]|$)`)

type Event string

const (
	EventNotAddressed Event = "not_addressed"
	EventCancelled    Event = "cancelled"
	EventSelected     Event = "selected"
)

type Resolution struct {
	PendingID      string
	Event          Event
	SelectedOption *session.PendingOption
	ResidualText   string
	EvidenceText   string
}

type Resolver struct{}

func New() *Resolver {
	return &Resolver{}
}

func (r *Resolver) Resolve(active *session.PendingInteraction, sourceText string) Resolution {
	result := Resolution{Event: EventNotAddressed, ResidualText: sourceText}
	if active == nil {
		return result
	}
	result.PendingID = active.ID
	residual, cancelled := removeCancellation(sourceText)
	if cancelled {
		result.Event = EventCancelled
		result.ResidualText = residual
		result.EvidenceText = strings.TrimSpace(sourceText)
		return result
	}
	option, residual := selectOption(active.Options, sourceText)
	if option != nil {
		cloned := *option
		if option.Location != nil {
			location := *option.Location
			cloned.Location = &location
		}
		result.Event = EventSelected
		result.SelectedOption = &cloned
		result.ResidualText = residual
		result.EvidenceText = strings.TrimSpace(strings.TrimSuffix(sourceText, residual))
	}
	return result
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

func selectOption(options []session.PendingOption, text string) (*session.PendingOption, string) {
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
