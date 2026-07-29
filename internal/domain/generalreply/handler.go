package generalreply

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/internal/llmharness"
	"github.com/zxq97/agent/internal/progress"
	"github.com/zxq97/agent/internal/session"
)

const maxRecentMessages = 6

// LLMTaskID is the stable identifier for read-only general replies.
const LLMTaskID = "general_reply.generate"

type LLMHandler struct {
	harness *llmharness.Harness[promptInput, Result]
}

func NewLLMHandler(client llm.Client, policies ...llmharness.Policy) (*LLMHandler, error) {
	if client == nil {
		return nil, errors.New("general reply: llm client is required")
	}
	policy, err := llmharness.ResolvePolicy(policies)
	if err != nil {
		return nil, err
	}
	harness, err := llmharness.New(client, generalReplyTask(), policy)
	if err != nil {
		return nil, err
	}
	return &LLMHandler{harness: harness}, nil
}

func (h *LLMHandler) Handle(ctx context.Context, agentSession *session.AgentSession, input *Input) (*Result, error) {
	if agentSession == nil {
		return nil, errors.New("general reply: session is required")
	}
	if input == nil || strings.TrimSpace(input.SourceText) == "" {
		return nil, errors.New("general reply: source text is required")
	}
	progress.Emit(ctx, "replying", "正在组织回复")
	result, err := h.harness.Run(ctx, &llmharness.RunRequest[promptInput]{
		Input: promptInputPointer(buildPromptInput(agentSession, input)),
	})
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

func generalReplyTask() llmharness.Task[promptInput, Result] {
	return llmharness.Task[promptInput, Result]{
		ID:               LLMTaskID,
		PromptVersion:    "1.0.0",
		SchemaVersion:    "general-reply-text/1",
		ValidatorVersion: "1.0.0",
		ValidateInput: func(input *promptInput) error {
			if input == nil || strings.TrimSpace(input.SourceText) == "" {
				return errors.New("general reply: prompt source text is required")
			}
			return nil
		},
		BuildRequest: func(input *promptInput) (*llm.ChatRequest, error) {
			payload, err := json.Marshal(input)
			if err != nil {
				return nil, err
			}
			return &llm.ChatRequest{
				System:   generalReplyPrompt,
				Messages: []llm.Message{{Role: llm.RoleUser, Content: string(payload)}},
			}, nil
		},
		DecodeStrict: func(content string) (*Result, error) {
			message := strings.TrimSpace(content)
			if message == "" {
				return nil, errors.New("general reply: model returned empty content")
			}
			return &Result{Message: message}, nil
		},
		ValidateOutput: func(_ *promptInput, _ *Result) error {
			return nil
		},
	}
}

func promptInputPointer(value promptInput) *promptInput {
	return &value
}

type promptInput struct {
	SourceText     string            `json:"source_text"`
	RecentMessages []Message         `json:"recent_messages"`
	Session        promptSessionView `json:"session"`
}

type promptSessionView struct {
	LocationName string                  `json:"location_name"`
	PickupTime   string                  `json:"pickup_time"`
	ReturnTime   string                  `json:"return_time"`
	Requirements []promptRequirementView `json:"requirements"`
	Pending      string                  `json:"pending_question"`
	LastVehicles []string                `json:"last_vehicle_names"`
}

type promptRequirementView struct {
	Facet     string `json:"facet"`
	Value     string `json:"value"`
	Important string `json:"importance"`
	Status    string `json:"status"`
}

func buildPromptInput(agentSession *session.AgentSession, input *Input) promptInput {
	result := promptInput{SourceText: input.SourceText, Session: promptSessionView{
		Requirements: []promptRequirementView{},
		LastVehicles: []string{},
	}}
	history := input.RecentMessages
	if len(history) > maxRecentMessages {
		history = history[len(history)-maxRecentMessages:]
	}
	result.RecentMessages = append([]Message(nil), history...)
	if agentSession.Search.Location != nil {
		result.Session.LocationName = agentSession.Search.Location.Name
	}
	if agentSession.Search.PickupTime != nil {
		result.Session.PickupTime = agentSession.Search.PickupTime.Format("2006-01-02 15:04:05")
	}
	if agentSession.Search.ReturnTime != nil {
		result.Session.ReturnTime = agentSession.Search.ReturnTime.Format("2006-01-02 15:04:05")
	}
	for _, requirement := range agentSession.Search.Requirements {
		result.Session.Requirements = append(result.Session.Requirements, promptRequirementView{
			Facet: requirement.DisplayType(), Value: requirement.DisplayValue(),
			Important: requirement.Importance, Status: requirement.Status,
		})
	}
	if agentSession.Pending.Active != nil {
		result.Session.Pending = agentSession.Pending.Active.Question
	}
	for _, vehicle := range agentSession.Search.LastResults {
		if vehicle.VehicleName != "" {
			result.Session.LastVehicles = append(result.Session.LastVehicles, vehicle.VehicleName)
		}
	}
	return result
}

const generalReplyPrompt = `你是租车智能体的只读通用回复模块。请直接回答 source_text，并可使用有限的 recent_messages 和 session 摘要理解上下文。

严格边界：
1. 不修改或声称修改地点、时间、车辆诉求、Pending 或搜索状态。
2. 不调用或声称调用地图、Guide、订单等外部服务。
3. 不编造车辆报价、库存、provider ID、菜单 code、筛选 code 或 context_id。
4. session 中的车辆只代表上次已展示结果；不能把旧报价说成当前实时结果。
5. 若问题需要实时搜索或修改条件，说明你能协助，并请用户明确提出操作；不要假装已经执行。
6. 若存在 pending_question 且用户没有回答，可以自然提醒，但不要阻止回答本轮普通问题。
7. 回复简洁、自然、使用中文；不要输出 JSON、Markdown 标题或内部处理过程。`
