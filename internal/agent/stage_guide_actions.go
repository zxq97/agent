package agent

import (
	"context"

	"github.com/zxq97/rental-agent/internal/tools"
)

// GuideActionStage 在能力执行后追加引导胶囊和反馈胶囊。
type GuideActionStage struct{}

func (s *GuideActionStage) Name() string { return "GuideAction" }

func (s *GuideActionStage) Handle(ctx context.Context, ac *AgentContext) (Signal, error) {
	if ac.Result == nil {
		return SignalContinue, nil
	}
	switch ac.Result.ToolName {
	case tools.ToolSearchQuotes:
		emitSearchQuickActions(ac)
	case ToolInterpretRules:
		emitFeedbackActions(ac)
	}
	return SignalContinue, nil
}

func emitSearchQuickActions(ac *AgentContext) {
	emitVehicleCards(ac)
	actions := []ClientAction{
		{Label: "便宜一点", Type: "slot_patch", Payload: map[string]any{"budget_max": "便宜一点"}},
		{Label: "换一批", Type: "slot_patch", Payload: map[string]any{"search_mode": SearchModePage}},
	}
	_, quotes, _ := ac.State.SnapshotQuotes()
	if len(quotes) >= 2 {
		a := quotes[0].CarName
		b := quotes[1].CarName
		if a == "" {
			a = quotes[0].BrandName
		}
		if b == "" {
			b = quotes[1].BrandName
		}
		if a != "" && b != "" {
			actions = append(actions, ClientAction{
				Label: "对比" + a + "和" + b,
				Type:  "compare",
				Payload: map[string]any{
					"vehicle_refs": []string{a, b},
				},
			})
		}
	}
	actions = append(actions, feedbackActionSet()...)
	emitEventPayload(ac.Emit, "quick_action", map[string]any{"actions": actions})
}

func emitVehicleCards(ac *AgentContext) {
	_, quotes, _ := ac.State.SnapshotQuotes()
	if len(quotes) == 0 {
		return
	}
	cards := make([]map[string]any, 0, len(quotes))
	limit := len(quotes)
	if limit > 6 {
		limit = 6
	}
	for i := 0; i < limit; i++ {
		q := quotes[i]
		name := q.CarName
		if name == "" {
			name = q.BrandName
		}
		cards = append(cards, map[string]any{
			"index":       q.Index,
			"name":        name,
			"brand":       q.BrandName,
			"daily_price": q.DailyPrice,
			"total_price": q.TotalPrice,
		})
	}
	emitEventPayload(ac.Emit, "card", map[string]any{"type": "vehicle_list", "payload": cards})
}

func emitFeedbackActions(ac *AgentContext) {
	emitEventPayload(ac.Emit, "quick_action", map[string]any{"actions": feedbackActionSet()})
}

func feedbackActionSet() []ClientAction {
	return []ClientAction{
		{Label: "有帮助", Type: "feedback_positive"},
		{Label: "不满意", Type: "feedback_negative"},
	}
}
