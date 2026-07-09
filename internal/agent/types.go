package agent

import (
	"context"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
	"github.com/zxq97/rental-agent/internal/types"
)

// Emitter 输出汇 —— CLI / SSE 各自实现。Decider 与 Capability 通过它流式吐字。
type Emitter interface {
	// Text 推一段文本增量(用户可见话术)。
	Text(delta string)
	// Event 推一条调度/进度事件(可选展示,如 tool_call / thinking)。
	Event(name, detail string)
}

// ClientAction 是前端 quick_action 点击回传的结构化动作。
type ClientAction struct {
	Label   string         `json:"label,omitempty"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload,omitempty"`
}

// Decision 一次 function-calling 决策的结构化产出。
type Decision struct {
	Tool     string         // "" = 没调 tool(闲聊/越界)
	Args     map[string]any // 工具入参(已解析)
	Reply    string         // content 流式吐的话术(供 PureReply / Clarify 复用)
	ArgsDiag *ToolArgsDiagnostics

	// search_vehicles 时由 LLM 产出的结构化需求增量。
	SearchMode         string            // initial/refine/page/negative_feedback/budget_down/budget_up/relax
	FeedbackRef        string            // 用户反馈指向的自然语言对象,如"第一辆"/"比亚迪"
	PickupText         string            // 用户在本轮/历史给出的取车地点自然语言(LLM 抽);空=未说
	DropoffText        string            // 还车地点自然语言;空=同点还车
	PickupTimeText     string            // 用户给出的取车时间(YYYY-MM-DD HH:MM[:SS])
	DropoffTimeText    string            // 用户给出的还车时间(YYYY-MM-DD HH:MM[:SS])
	NeedDelta          []types.NeedDelta // 需求变更增量
	StrongSearchIntent bool              // 用户"直接推/别问了"→ true
	Understanding      *Understanding    // 模型自评
	ProfilePatch       *ProfilePatch     // 本轮抽取的轻量画像补丁
}

type ToolArgsDiagnostics struct {
	Raw              string
	Repaired         bool
	RepairedArgs     string
	ParseError       string
	ValidationErrors []string
}

type ProfilePatch struct {
	TripScene        string `json:"trip_scene,omitempty"`
	Companions       string `json:"companions,omitempty"`
	PriceSensitivity string `json:"price_sensitivity,omitempty"`
	StylePreference  string `json:"style_preference,omitempty"`
}

// Understanding 模型对本单需求理解的自评。
type Understanding struct {
	Sufficiency float64  `json:"sufficiency"`            // 0~1
	CoveredDims []string `json:"covered_dims,omitempty"` // 已掌握的关键维度
	Rationale   string   `json:"rationale,omitempty"`    // 一句话依据
}

// Clarification 一次澄清反问(指代多义 / 信息不足)。
type Clarification struct {
	Question string
	Options  []string
	Slot     string // 追问的维度(可空)
}

// CapabilityResult 能力执行产出。
type CapabilityResult struct {
	Text          string         // 最终文字(若 Capability 内已流式吐过,这里是累计全文)
	Clarification *Clarification // 非空表示要反问
	ToolName      string         // 本轮调用的代表性工具名(history 回放快照用)
	ToolArgs      string         // 工具入参 JSON(快照用)
	ToolResult    string         // 工具结果摘要(快照用)
}

// CapabilityInput 能力执行上下文。
type CapabilityInput struct {
	State     *orchestration.ConversationState
	UserInput string
	Decision  *Decision
	Deps      *tools.Deps
	Factory   ModelGetter
	Emit      Emitter
}

// ModelGetter 按环节 binding 拿 ChatModel。
type ModelGetter interface {
	Get(bindingKey string) (llm.ChatModel, error)
}

// Capability 单个能力的执行接口。
type Capability interface {
	Name() string
	Run(ctx context.Context, in CapabilityInput) (*CapabilityResult, error)
}
