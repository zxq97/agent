package webchat

import (
	"strings"
	"time"

	"github.com/zxq97/agent/internal/router"
	"github.com/zxq97/agent/internal/session"
)

const maxRouterHistoryMessages = 6

func buildRouterInput(state *session.AgentSession, history []Message, sourceText string) *router.Input {
	input := &router.Input{
		SourceText:          sourceText,
		CurrentRequirements: []router.RequirementView{},
		RecentMessages:      []router.ConversationMessage{},
	}
	if state == nil {
		return input
	}
	if state.Search.Location != nil {
		input.CurrentRental.LocationName = state.Search.Location.Name
	}
	input.CurrentRental.PickupTime = formatRouterTime(state.Search.PickupTime)
	input.CurrentRental.ReturnTime = formatRouterTime(state.Search.ReturnTime)
	for _, requirement := range state.Search.Requirements {
		input.CurrentRequirements = append(input.CurrentRequirements, router.RequirementView{
			Type:       requirement.DisplayType(),
			Value:      requirement.DisplayValue(),
			Importance: requirement.Importance,
			Status:     requirement.Status,
		})
	}
	if active := state.Pending.Active; active != nil {
		pending := &router.PendingView{Type: string(active.Type), Question: active.Question, Options: []string{}}
		for _, option := range active.Options {
			text := strings.TrimSpace(strings.Join([]string{option.Label, option.Value}, " "))
			if text != "" {
				pending.Options = append(pending.Options, text)
			}
		}
		input.ActivePending = pending
	}
	if len(history) > maxRouterHistoryMessages {
		history = history[len(history)-maxRouterHistoryMessages:]
	}
	for _, message := range history {
		input.RecentMessages = append(input.RecentMessages, router.ConversationMessage{Role: message.Role, Content: message.Content})
	}
	input.HasPreviousSearch = state.Search.ActiveSearch != nil || len(state.Search.LastResults) > 0
	return input
}

func formatRouterTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}
