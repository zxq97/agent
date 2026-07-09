package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/prompt"
	"github.com/zxq97/rental-agent/internal/tools"
	"github.com/zxq97/rental-agent/internal/types"
)

// CapabilityOrchestrator 按 Decision.Tool 认领分发。
type CapabilityOrchestrator struct {
	caps map[string]Capability
}

// Handle 分发。Decision.Tool == "" → 纯回复。未知 tool → 占位提示。
func (o *CapabilityOrchestrator) Handle(ctx context.Context, ac *AgentContext) (*CapabilityResult, error) {
	in := CapabilityInput{
		State:     ac.State,
		UserInput: ac.UserInput,
		Decision:  ac.Decision,
		Deps:      ac.Deps,
		Factory:   ac.Factory,
		Emit:      ac.Emit,
	}
	if res := resultForInvalidToolArgs(ac); res != nil {
		logf(ac.Logger, "[capability] stage=Capability status=invalid_tool_args tool=%s parse_error=%q validation_errors=%v raw_args=%s",
			ac.Decision.Tool, ac.Decision.ArgsDiag.ParseError, ac.Decision.ArgsDiag.ValidationErrors, truncateForLog(ac.Decision.ArgsDiag.Raw, 1024))
		return res, nil
	}
	if ac.Decision.Tool == "" {
		logf(ac.Logger, "[capability] stage=Capability dispatch=pure_reply reply_len=%d", len(ac.Decision.Reply))
		return (&PureReplyCapability{}).Run(ctx, in)
	}
	// 全量打:哪个 tool、args、search_mode(若是 search)。
	argsJSON, _ := json.Marshal(ac.Decision.Args)
	logf(ac.Logger, "[capability] stage=Capability dispatch=%s search_mode=%s args=%s",
		ac.Decision.Tool, ac.Decision.SearchMode, truncateForLog(string(argsJSON), 4096))
	cap, ok := o.caps[ac.Decision.Tool]
	if !ok {
		// 工具 schema 已声明但 Capability 未实现(P2/P3 占位)
		msg := "这个功能即将上线,先帮你看看车吧~"
		if ac.Emit != nil {
			ac.Emit.Text(msg)
		}
		logf(ac.Logger, "[capability] stage=Capability status=unknown_tool tool=%s", ac.Decision.Tool)
		return &CapabilityResult{Text: msg}, nil
	}
	res, err := cap.Run(ctx, in)
	if err != nil {
		logf(ac.Logger, "[capability] stage=Capability status=error tool=%s err=%v", ac.Decision.Tool, err)
		return res, err
	}
	// 出口摘要:Clarification 还是 Text,Tool 有没有真调过,产物长度。
	kind := "text"
	extra := ""
	if res != nil && res.Clarification != nil {
		kind = "clarification"
		extra = fmt.Sprintf(" slot=%s question=%q", res.Clarification.Slot, res.Clarification.Question)
	}
	textLen := 0
	toolName := ""
	if res != nil {
		textLen = len(res.Text)
		toolName = res.ToolName
	}
	logf(ac.Logger, "[capability] stage=Capability status=ok tool=%s kind=%s text_len=%d representative_tool=%s%s",
		ac.Decision.Tool, kind, textLen, toolName, extra)
	return res, nil
}

func resultForInvalidToolArgs(ac *AgentContext) *CapabilityResult {
	if ac == nil || ac.Decision == nil || ac.Decision.ArgsDiag == nil {
		return nil
	}
	diag := ac.Decision.ArgsDiag
	if diag.ParseError == "" && len(diag.ValidationErrors) == 0 {
		return nil
	}
	if diag.ParseError != "" && diag.Repaired && len(diag.ValidationErrors) == 0 {
		return nil
	}
	switch ac.Decision.Tool {
	case ToolSearchVehicles:
		return &CapabilityResult{Clarification: clarificationForInvalidSearchArgs(ac)}
	case ToolUpdateRental:
		return &CapabilityResult{Clarification: &Clarification{
			Question: "我没理解清楚要改哪个取还车信息,可以再说一下新的地点或时间吗?",
			Slot:     "rental_update",
		}}
	default:
		return &CapabilityResult{Text: "我刚才没理解清楚,可以换个说法再发一次吗?"}
	}
}

func clarificationForInvalidSearchArgs(ac *AgentContext) *Clarification {
	errs := ac.Decision.ArgsDiag.ValidationErrors
	for _, err := range errs {
		if strings.Contains(err, "pickup_text") {
			return &Clarification{
				Question: "你说的是车型偏好,取车地点还没确认。你想在哪个城市、哪个位置取车?",
				Slot:     "pickup_location",
			}
		}
	}
	if ac.State != nil {
		if clar := rentalMissingClarification(ac.State); clar != nil {
			return clar
		}
	}
	return &Clarification{
		Question: "我没解析清楚这次搜车条件,可以再说一下取车地点和时间吗?",
		Slot:     "search_args",
	}
}

// factoryAdapter 把 llm.Factory 适配成 ModelGetter(注入固定 ctx)。
type factoryAdapter struct {
	f   *llm.Factory
	ctx context.Context
}

func (a factoryAdapter) Get(bindingKey string) (llm.ChatModel, error) {
	return a.f.Get(a.ctx, bindingKey)
}

// RentalAgent 对外入口:装配 Decider + Capability 编排 + Pipeline。
type RentalAgent struct {
	factory  *llm.Factory
	deps     *tools.Deps
	caps     map[string]Capability
	feedback FeedbackStore
	logger   io.Writer
}

// New 装配 RentalAgent。
func New(ctx context.Context, factory *llm.Factory, deps *tools.Deps, logger io.Writer) (*RentalAgent, error) {
	caps := map[string]Capability{
		ToolSearchVehicles: &SearchCapability{},
		ToolAsk:            &AskCapability{},
		ToolGetPriceDetail: &PriceDetailCapability{},
		ToolInsurance:      &InsuranceCapability{},
		ToolCompare:        &CompareCapability{},
		ToolInterpretRules: &RulesCapability{},
		ToolUpdateRental:   &UpdateRentalCapability{},
	}
	feedback := NewFileFeedbackStore(filepath.Join(os.TempDir(), "rental-agent-feedback.jsonl"))
	return &RentalAgent{factory: factory, deps: deps, caps: caps, feedback: feedback, logger: logger}, nil
}

// Run 处理一轮用户输入。emit 流式吐字;返回最终结果。使用构造时注入的 logger。
func (a *RentalAgent) Run(ctx context.Context, state *orchestration.ConversationState, userInput string, emit Emitter) (*CapabilityResult, error) {
	return a.RunWithLogger(ctx, state, userInput, emit, a.logger)
}

// RunWithLogger 处理一轮用户输入,使用指定的 logger(支持 per-request TraceLog)。
func (a *RentalAgent) RunWithLogger(ctx context.Context, state *orchestration.ConversationState, userInput string, emit Emitter, logger io.Writer) (*CapabilityResult, error) {
	return a.RunWithEvent(ctx, state, userInput, "", nil, emit, logger)
}

// RunWithEvent 处理普通文本或前端结构化事件(action_click)。
func (a *RentalAgent) RunWithEvent(ctx context.Context, state *orchestration.ConversationState, userInput, eventType string, action *ClientAction, emit Emitter, logger io.Writer) (*CapabilityResult, error) {
	model, err := a.factory.Get(ctx, "decide")
	if err != nil {
		return nil, err
	}
	sysPrompt, err := prompt.RenderDecideSystem(prompt.DecideSystemVars{
		RequiredSlots: RenderRequiredSlots(),
		SceneKB:       RenderSceneKB(),
	})
	if err != nil {
		return nil, err
	}

	ac := &AgentContext{
		State:     state,
		UserInput: userInput,
		Deps:      depsForRequest(a.deps, logger),
		Factory:   factoryAdapter{f: a.factory, ctx: ctx},
		Emit:      emit,
		EventType: eventType,
		Action:    action,
		Feedback:  a.feedback,
		Logger:    logger,
	}

	pipeline := NewChatPipeline(
		&PreRouteStage{},
		&DecideStage{decider: NewDecider(model, sysPrompt)},
		&CapabilityStage{orch: &CapabilityOrchestrator{caps: a.caps}},
		&GuideActionStage{},
		&FinalizeStage{},
	)
	if err := pipeline.Run(ctx, ac); err != nil {
		return nil, err
	}
	return ac.Result, nil
}

func depsForRequest(base *tools.Deps, logger io.Writer) *tools.Deps {
	if base == nil || logger == nil {
		return base
	}
	deps := *base
	deps.Logger = logger
	return &deps
}

// finalize 落 history:先记用户输入,再记 assistant 回复(带工具调用快照供回放)。
func finalize(ac *AgentContext) {
	// 用户消息
	ac.State.AppendMessage(&llm.Message{Role: llm.RoleUser, Content: ac.UserInput}, nil)

	res := ac.Result
	if res == nil {
		return
	}
	// assistant 消息:若本轮调过工具,带快照(history 回放用)
	var snap *types.ToolCallSnapshot
	if res.ToolName != "" {
		snap = &types.ToolCallSnapshot{
			Name:      res.ToolName,
			Arguments: res.ToolArgs,
			Result:    res.ToolResult,
		}
	}
	assistantText := res.Text
	if res.Clarification != nil && assistantText == "" {
		assistantText = res.Clarification.Question
	}
	ac.State.AppendMessage(&llm.Message{Role: llm.RoleAssistant, Content: assistantText}, snap)
	orchestration.MaybeSummarize(ac.State)
}
