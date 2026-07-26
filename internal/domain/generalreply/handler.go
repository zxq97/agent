package generalreply

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/internal/progress"
	"github.com/zxq97/agent/internal/session"
)

const maxRecentMessages = 6

type LLMHandler struct {
	client llm.Client
}

func NewLLMHandler(client llm.Client) (*LLMHandler, error) {
	if client == nil {
		return nil, errors.New("general reply: llm client is required")
	}
	return &LLMHandler{client: client}, nil
}

func (h *LLMHandler) Handle(ctx context.Context, agentSession *session.AgentSession, input *Input) (*Result, error) {
	if agentSession == nil {
		return nil, errors.New("general reply: session is required")
	}
	if input == nil || strings.TrimSpace(input.SourceText) == "" {
		return nil, errors.New("general reply: source text is required")
	}
	progress.Emit(ctx, "replying", "正在组织回复")
	payload, err := json.Marshal(buildPromptInput(agentSession, input))
	if err != nil {
		return nil, err
	}
	response, err := h.client.Chat(ctx, &llm.ChatRequest{
		Model:    llm.ModelConversation,
		System:   generalReplyPrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: string(payload)}},
	})
	if err != nil {
		return nil, err
	}
	if response == nil || strings.TrimSpace(response.Content) == "" {
		return nil, errors.New("general reply: model returned empty content")
	}
	return &Result{Message: strings.TrimSpace(response.Content)}, nil
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
			Facet: requirement.Facet, Value: requirement.CanonicalValue,
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
