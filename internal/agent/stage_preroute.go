package agent

import (
	"context"
	"fmt"
)

// PreRouteStage 处理前端结构化事件,跳过 decide LLM。
type PreRouteStage struct{}

func (s *PreRouteStage) Name() string { return "PreRoute" }

func (s *PreRouteStage) Handle(ctx context.Context, ac *AgentContext) (Signal, error) {
	if ac.EventType != "action_click" || ac.Action == nil {
		logf(ac.Logger, "[preroute] stage=PreRoute status=pass event_type=%q action=(none)", ac.EventType)
		return SignalContinue, nil
	}
	logf(ac.Logger, "[preroute] stage=PreRoute status=hit action_type=%s label=%q payload=%v",
		ac.Action.Type, ac.Action.Label, ac.Action.Payload)

	switch ac.Action.Type {
	case "compare":
		refs := extractRefs(ac.Action.Payload["vehicle_refs"])
		if len(refs) == 0 {
			refs = extractRefs(ac.Action.Payload["vehicles"])
		}
		ac.Decision = &Decision{
			Tool: ToolCompare,
			Args: map[string]any{"vehicle_refs": refs},
		}
		logf(ac.Logger, "[preroute] stage=PreRoute injected_decision tool=%s vehicle_refs=%v", ToolCompare, refs)
		return SignalContinue, nil
	case "slot_patch":
		ac.Decision = decisionFromSlotPatch(ac.Action.Payload)
		logf(ac.Logger, "[preroute] stage=PreRoute injected_decision tool=%s search_mode=%s need_delta=%d",
			ac.Decision.Tool, ac.Decision.SearchMode, len(ac.Decision.NeedDelta))
		return SignalContinue, nil
	case "update_rental":
		ac.Decision = &Decision{
			Tool: ToolUpdateRental,
			Args: ac.Action.Payload,
		}
		logf(ac.Logger, "[preroute] stage=PreRoute injected_decision tool=%s payload=%v", ToolUpdateRental, ac.Action.Payload)
		return SignalContinue, nil
	case "feedback_positive", "feedback_negative":
		rating := "positive"
		if ac.Action.Type == "feedback_negative" {
			rating = "negative"
		}
		if ac.Feedback != nil {
			_ = ac.Feedback.Save(ctx, FeedbackSnapshot{
				UserID:    ac.State.UserID,
				SessionID: ac.State.SessionID,
				Rating:    rating,
				Message:   fmt.Sprint(ac.Action.Payload["message"]),
				History:   ac.State.SnapshotHistory(),
			})
		}
		msg := "收到反馈,我会继续优化推荐。"
		if ac.Emit != nil {
			ac.Emit.Text(msg)
		}
		ac.Result = &CapabilityResult{Text: msg}
		logf(ac.Logger, "[preroute] stage=PreRoute short_circuit rating=%s reply=%q", rating, msg)
		return SignalStop, nil
	default:
		logf(ac.Logger, "[preroute] stage=PreRoute unknown_action_type=%s (fall-through to Decide)", ac.Action.Type)
		return SignalContinue, nil
	}
}

func decisionFromSlotPatch(payload map[string]any) *Decision {
	mode := SearchModeRefine
	if v, ok := payload["search_mode"].(string); ok && v != "" {
		mode = normalizeSearchMode(v)
	}
	deltas := make([]map[string]any, 0, len(payload))
	add := func(t string, v any) {
		if v == nil {
			return
		}
		deltas = append(deltas, map[string]any{
			"op":         "ADD",
			"type":       t,
			"value":      v,
			"hardness":   "hard",
			"confidence": 0.9,
		})
	}
	add("vehicle_type", payload["vehicle_type"])
	add("energy_type", payload["energy"])
	add("seat_num", payload["seats"])
	add("transmission", payload["transmission"])
	add("brand", payload["brands"])
	add("price_preference", payload["budget_max"])
	args := map[string]any{
		"search_mode": mode,
		"need_delta":  deltas,
	}
	dec := &Decision{Tool: ToolSearchVehicles, Args: args, SearchMode: mode}
	dec.NeedDelta = parseNeedDelta(args)
	return dec
}
