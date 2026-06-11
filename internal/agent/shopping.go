// Package agent 聚合所有 agent 工厂。
// P1 只有 ShoppingAgent(单 ReAct);P3 起拆 supervisor + 多子 agent。
package agent

import (
	"context"
	"fmt"
	"io"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"

	"github.com/zxq97/agent/internal/llm"
	"github.com/zxq97/agent/internal/prompt"
	"github.com/zxq97/agent/internal/tools"
)

// ShoppingAgent 包一层 react.Agent,把 system prompt 注入逻辑放进来,
// 调用方只需 Generate / Stream。
type ShoppingAgent struct {
	core        *react.Agent
	systemPromt string
}

// NewShoppingAgent 构造导购 agent。
//   - chatModel:支持 tool calling 的 ChatModel(从 llm.Factory.Get 拿)
//   - allTools:tools.All(d) 的返回
//   - vars:渲染 system prompt 用的上下文(now / city hint / 客服昵称)
func NewShoppingAgent(
	ctx context.Context,
	chatModel llm.ChatModel,
	allTools []tool.BaseTool,
	vars prompt.ShoppingSystemVars,
) (*ShoppingAgent, error) {
	if chatModel == nil {
		return nil, fmt.Errorf("nil chatModel")
	}
	if len(allTools) == 0 {
		return nil, fmt.Errorf("empty tools")
	}
	sys, err := prompt.RenderShoppingSystem(vars)
	if err != nil {
		return nil, fmt.Errorf("render system prompt: %w", err)
	}

	core, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: chatModel,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: allTools,
		},
		// DeepSeek(以及 Claude 等)在中文长 prompt 下,
		// tool_call 不会出现在流式第一个 chunk —— 通常是先吐一段思考文本,
		// 末尾才追加 tool_calls。eino 默认 firstChunkStreamToolCallChecker 只看第一帧,
		// 会把这种情况误判为"无 tool 调用"直接结束 loop。
		// 这里换成"扫完整个流再判定"的 checker。
		StreamToolCallChecker: scanAllStreamForToolCall,
	})
	if err != nil {
		return nil, fmt.Errorf("react.NewAgent: %w", err)
	}
	return &ShoppingAgent{core: core, systemPromt: sys}, nil
}

// Generate 同步生成,把 system prompt 拼到 history 前。
func (a *ShoppingAgent) Generate(ctx context.Context, history []*schema.Message) (*schema.Message, error) {
	msgs := a.prepend(history)
	return a.core.Generate(ctx, msgs)
}

// Stream 流式生成,把 system prompt 拼到 history 前。
func (a *ShoppingAgent) Stream(ctx context.Context, history []*schema.Message) (*schema.StreamReader[*schema.Message], error) {
	msgs := a.prepend(history)
	return a.core.Stream(ctx, msgs)
}

// SystemPrompt 暴露给调试 / 日志。
func (a *ShoppingAgent) SystemPrompt() string { return a.systemPromt }

// prepend 在 history 前面插一条 system message。
// 如果调用方已经手动塞了 system,我们不再追加,保留用户的。
func (a *ShoppingAgent) prepend(history []*schema.Message) []*schema.Message {
	if len(history) > 0 && history[0].Role == schema.System {
		return history
	}
	out := make([]*schema.Message, 0, len(history)+1)
	out = append(out, schema.SystemMessage(a.systemPromt))
	out = append(out, history...)
	return out
}

// MustToolsBase 把 tool.InvokableTool 列表转成 tool.BaseTool — 满足 react 的入参。
// 调用方拿到 tools.All 返回的就已经是 []tool.BaseTool,无需此 helper;
// 留作扩展点。
func MustToolsBase(in []tool.InvokableTool) []tool.BaseTool {
	out := make([]tool.BaseTool, 0, len(in))
	for _, t := range in {
		out = append(out, t)
	}
	return out
}

// scanAllStreamForToolCall 扫整条流,只要任意一帧出现 ToolCalls,
// 或最终累积消息里有 ToolCalls 字段(部分 provider 仅在末尾给出),即视为有 tool 调用。
//
// 替代 eino 默认 firstChunkStreamToolCallChecker —— 后者只看第一帧,
// 在 DeepSeek/Claude 这种"先思考、末尾追加 tool_calls"的流式场景会漏判。
func scanAllStreamForToolCall(_ context.Context, sr *schema.StreamReader[*schema.Message]) (bool, error) {
	defer sr.Close()
	for {
		msg, err := sr.Recv()
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if msg == nil {
			continue
		}
		if len(msg.ToolCalls) > 0 {
			return true, nil
		}
	}
}

// 让 tools 包出现在 imports 列表里,便于 IDE 跳转 — 实际是 NewShoppingAgent 的调用方依赖。
var _ = tools.NewDeps
