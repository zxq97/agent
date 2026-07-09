package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/types"
)

const filterInterpreterSystem = `你是租车筛选条件解析器。只输出 JSON,不要输出解释。
把用户自然语言归一化为 search_mode、need_delta、feedback_ref、confidence。
禁止输出 context_id/reference_id/supplier/filter_code。`

type FilterInterpretInput struct {
	Factory  ModelGetter
	Decision *Decision
	UserText string
	State    *orchestration.ConversationState
	Reason   string
}

type filterInterpretOutput struct {
	SearchMode  string            `json:"search_mode"`
	NeedDelta   []types.NeedDelta `json:"need_delta"`
	FeedbackRef string            `json:"feedback_ref"`
	Confidence  float64           `json:"confidence"`
	Rationale   string            `json:"rationale"`
}

func InterpretFilterIfNeeded(ctx context.Context, in FilterInterpretInput) *Decision {
	if in.Decision == nil {
		return in.Decision
	}
	if in.Factory == nil {
		return in.Decision
	}
	model, err := in.Factory.Get("filter_interpreter")
	if err != nil {
		return in.Decision
	}
	resp, err := model.Chat(llm.WithStage(ctx, "capability:filter_interpreter"), llm.ChatRequest{
		System: filterInterpreterSystem,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: buildFilterInterpreterUserMessage(in),
		}},
	})
	if err != nil || resp == nil {
		return in.Decision
	}
	out, ok := parseFilterInterpretOutput(resp.Content)
	if !ok || out.Confidence < 0.6 || !validNeedDeltas(out.NeedDelta) {
		return in.Decision
	}
	next := *in.Decision
	next.SearchMode = normalizeSearchMode(out.SearchMode)
	next.FeedbackRef = out.FeedbackRef
	next.NeedDelta = out.NeedDelta
	return &next
}

func buildFilterInterpreterUserMessage(in FilterInterpretInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "触发原因:%s\n", in.Reason)
	fmt.Fprintf(&b, "用户原话:%s\n", in.UserText)
	if in.Decision != nil {
		fmt.Fprintf(&b, "原 search_mode:%s\n", in.Decision.SearchMode)
		fmt.Fprintf(&b, "原 need_delta:%s\n", mustJSON(in.Decision.NeedDelta))
	}
	if in.State != nil {
		fmt.Fprintf(&b, "当前需求状态:\n%s\n", orchestration.NeedsStatePrefix(in.State.Constraints))
	}
	return b.String()
}

func parseFilterInterpretOutput(content string) (filterInterpretOutput, bool) {
	var out filterInterpretOutput
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		content = trimJSONFence(content)
	}
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return out, false
	}
	if strings.Contains(content, "context_id") || strings.Contains(content, "reference_id") || strings.Contains(content, "supplier") || strings.Contains(content, "filter_code") {
		return out, false
	}
	return out, true
}

func trimJSONFence(content string) string {
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	return strings.TrimSpace(content)
}

func validNeedDeltas(deltas []types.NeedDelta) bool {
	for _, d := range deltas {
		switch normalizeDeltaOp(d.Op) {
		case DeltaAdd, DeltaUpdate, DeltaNegate, DeltaDecay, DeltaDelete, DeltaReinforce:
		default:
			return false
		}
		switch d.Type {
		case "vehicle_type", "energy_type", "seat_num", "brand", "price_preference", "transmission", "scene", "car_age", "comfort_preference", "vehicle_model", "vehicle_series", "luggage", "license", "service":
		default:
			return false
		}
		if d.Hardness != "" && d.Hardness != "hard" && d.Hardness != "soft" {
			return false
		}
	}
	return true
}
