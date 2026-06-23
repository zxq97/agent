// Package agent: P3 起改用 eino ADK 多 agent (supervisor + 子 agent)。
//
// 关键概念:
//   - ChatModelAgent: 单个 LLM-driven agent,自带 ReAct loop,可绑定工具子集
//   - Supervisor: 顶层调度 agent,通过 transfer_to_agent 把任务派给子 agent
//   - Runner: 把多 agent 系统跑起来,流出 AgentEvent(每个事件含一段 message + 元信息)
//
// 对外接口保持简单:NewSupervisorSystem() 一次性把所有 agent 组装好返回 *adk.Runner,
// 上层 (CLI / HTTP) 只调 Run/Query 拿 AgentEvent stream。
package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/supervisor"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"

	"github.com/zxq97/agent/internal/llm"
	"github.com/zxq97/agent/internal/prompt"
)

// SystemDeps 构造 supervisor 多 agent 系统所需的依赖。
type SystemDeps struct {
	// ChatModelFactory 用于按 agent 类型(shopping/insurance/supervisor)获取 ChatModel。
	// 不同 agent 可以走不同 provider/模型(在 config.LLM.AgentBindings 里配置)。
	ChatModelFactory *llm.Factory

	// AllTools 是从 tyche MCP 拉到的全部 InvokableTool(已经过白名单过滤 + logging 包装)。
	// 内部会按子 agent 拆分子集。
	AllTools []tool.BaseTool

	// MaxIterations 单个子 agent 的 LLM 生成最大轮次,0 表示用 ADK 默认(20)。
	MaxIterations int

	// AssistantName 客服昵称(写入 prompt)。
	AssistantName string

	// DriverAge 已知用户驾龄(0 表示未知)。
	DriverAge int
}

// NewSupervisorSystem 组装"supervisor + shopping + insurance"三个 agent,
// 返回一个 adk.Runner,上层直接拿来跑。
//
// 工具拆分:
//   - ShoppingAgent: search_locations / resolve_poi / search_quotes / get_order_details / get_reservation
//   - InsuranceAgent: get_order_details (复用,因为保险数据在这个工具的 guarantee_list)
//   - Supervisor: 不绑定任何业务工具,只负责 transfer
func NewSupervisorSystem(ctx context.Context, d SystemDeps) (*adk.Runner, error) {
	if d.ChatModelFactory == nil {
		return nil, fmt.Errorf("nil ChatModelFactory")
	}
	if len(d.AllTools) == 0 {
		return nil, fmt.Errorf("empty tools")
	}

	shopping, err := buildShoppingAgent(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("build shopping agent: %w", err)
	}
	insurance, err := buildInsuranceAgent(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("build insurance agent: %w", err)
	}
	sup, err := buildSupervisorAgent(ctx, d)
	if err != nil {
		return nil, fmt.Errorf("build supervisor agent: %w", err)
	}

	multi, err := supervisor.New(ctx, &supervisor.Config{
		Supervisor: sup,
		SubAgents:  []adk.Agent{shopping, insurance},
	})
	if err != nil {
		return nil, fmt.Errorf("supervisor.New: %w", err)
	}

	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           multi,
		EnableStreaming: true,
	})
	return runner, nil
}

// buildShoppingAgent 构造导购子 agent,绑定地点/报价/明细工具。
func buildShoppingAgent(ctx context.Context, d SystemDeps) (adk.Agent, error) {
	chat, err := d.ChatModelFactory.Get(ctx, "shopping")
	if err != nil {
		return nil, fmt.Errorf("get shopping chat model: %w", err)
	}
	sysPrompt, err := prompt.RenderShoppingSystem(prompt.ShoppingSystemVars{
		AssistantName: d.AssistantName,
		DriverAge:     d.DriverAge,
	})
	if err != nil {
		return nil, fmt.Errorf("render shopping prompt: %w", err)
	}
	tools := filterToolsByName(d.AllTools, map[string]bool{
		"rental_search_locations":  true,
		"rental_resolve_poi":       true,
		"rental_search_quotes":     true,
		"rental_get_order_details": true,
		"rental_get_reservation":   true,
		// P5 本地决策辅助 / 资质工具
		"check_qualification":   true,
		"estimate_trip_cost":    true,
		"optimize_pickup_time":  true,
	})
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "ShoppingAgent",
		Description: "处理用户的挑车、报价、价格明细、订单查询、资质预检、行程估算、取还时间优化等导购类需求。",
		Instruction: sysPrompt,
		Model:       chat,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: d.MaxIterations,
	})
}

// buildInsuranceAgent 构造保险子 agent,只绑定 get_order_details(数据源)。
func buildInsuranceAgent(ctx context.Context, d SystemDeps) (adk.Agent, error) {
	chat, err := d.ChatModelFactory.Get(ctx, "insurance")
	if err != nil {
		return nil, fmt.Errorf("get insurance chat model: %w", err)
	}
	sysPrompt, err := prompt.RenderInsuranceSystem(prompt.InsuranceSystemVars{
		DriverAge: d.DriverAge,
	})
	if err != nil {
		return nil, fmt.Errorf("render insurance prompt: %w", err)
	}
	tools := filterToolsByName(d.AllTools, map[string]bool{
		"rental_get_order_details": true,
	})
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "InsuranceAgent",
		Description: "处理用户的保险相关问题:解读 guarantee_list、按驾龄推荐保险组合。",
		Instruction: sysPrompt,
		Model:       chat,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools},
		},
		MaxIterations: d.MaxIterations,
	})
}

// buildSupervisorAgent 构造顶层 supervisor,不绑定任何业务工具。
// transfer_to_agent 工具由 supervisor.New 自动注入。
func buildSupervisorAgent(ctx context.Context, d SystemDeps) (adk.Agent, error) {
	chat, err := d.ChatModelFactory.Get(ctx, "supervisor")
	if err != nil {
		return nil, fmt.Errorf("get supervisor chat model: %w", err)
	}
	sysPrompt, err := prompt.RenderSupervisorSystem(prompt.SupervisorSystemVars{
		AssistantName: d.AssistantName,
	})
	if err != nil {
		return nil, fmt.Errorf("render supervisor prompt: %w", err)
	}
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "SupervisorAgent",
		Description: "顶层路由 agent,根据用户消息把任务派给 ShoppingAgent 或 InsuranceAgent。",
		Instruction: sysPrompt,
		Model:       chat,
		// supervisor 不绑定业务工具;transfer_to_agent 由 supervisor.New 注入
		ToolsConfig:   adk.ToolsConfig{},
		MaxIterations: d.MaxIterations,
	})
}

// filterToolsByName 按 tool name 白名单从全集筛子集。
func filterToolsByName(all []tool.BaseTool, names map[string]bool) []tool.BaseTool {
	out := make([]tool.BaseTool, 0, len(names))
	for _, t := range all {
		info, err := t.Info(context.Background())
		if err != nil || info == nil {
			continue
		}
		if names[info.Name] {
			out = append(out, t)
		}
	}
	return out
}
