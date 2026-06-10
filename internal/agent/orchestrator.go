package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"

	"github.com/zxq97/agent/internal/session"
	"github.com/zxq97/agent/internal/skill"
	"github.com/zxq97/agent/internal/tool"
)

const orchestratorPrompt = `你是租车平台的智能客服助手。你可以帮助用户处理以下需求：

%s

## ⚠️ 行动优先，禁止空回复

收到用户请求后，如果匹配到某个能力域，必须**立即调用对应的 agent 工具**，不要先回复"好的我来帮您"之类的确认话术。
- ✅ 正确：用户说"我想租车" → 直接调用 vehicle_agent → 把结果返回给用户
- ❌ 错误：用户说"我想租车" → 回复"好的我来帮您查一下" → 等用户追问 → 才调用工具

唯一可以不调用工具的情况：用户的问题属于通用闲聊或不属于任何能力域。

## 重要约束
- 所有业务数据必须通过工具调用获取，不得凭记忆编造
- 如果工具返回空数据，应如实告知用户无法获取
- 不确定的业务规则应调用对应工具查询后再回答
- 如果用户的问题不属于以上任何一类，请直接用你的知识礼貌回答`

// Orchestrator 主 Agent，负责意图识别和 Skill 路由
type Orchestrator struct {
	chatModel     model.ToolCallingChatModel
	skillRegistry *skill.SkillRegistry
	toolRegistry  *tool.Registry
	sessions      *session.Manager
}

// NewOrchestrator 创建 Orchestrator
func NewOrchestrator(
	chatModel model.ToolCallingChatModel,
	skillRegistry *skill.SkillRegistry,
	toolRegistry *tool.Registry,
	sessions *session.Manager,
) *Orchestrator {
	return &Orchestrator{
		chatModel:     chatModel,
		skillRegistry: skillRegistry,
		toolRegistry:  toolRegistry,
		sessions:      sessions,
	}
}

// Run 执行一轮对话
func (o *Orchestrator) Run(ctx context.Context, sessionID string, userInput string) (string, error) {
	// 追加用户消息到会话
	userMsg := &schema.Message{
		Role:    schema.User,
		Content: userInput,
	}
	if err := o.sessions.AppendMessage(sessionID, userMsg); err != nil {
		return "", fmt.Errorf("追加用户消息失败: %w", err)
	}

	// 获取当前对话历史
	messages, err := o.sessions.GetMessages(sessionID)
	if err != nil {
		return "", fmt.Errorf("获取对话历史失败: %w", err)
	}

	// 构建 System Prompt
	routerPrompt := o.skillRegistry.BuildRouterPrompt()
	systemPrompt := fmt.Sprintf(orchestratorPrompt, routerPrompt)

	// 获取 Skill Agent 作为 Tool
	skillTools, err := o.skillRegistry.AsTools(ctx)
	if err != nil {
		return "", fmt.Errorf("获取 Skill Agent Tools 失败: %w", err)
	}

	// 创建 Orchestrator Agent
	orchestratorAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "orchestrator",
		Description: "租车平台智能客服主调度器",
		Instruction: systemPrompt,
		Model:       o.chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: skillTools,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("创建 Orchestrator Agent 失败: %w", err)
	}

	// 运行 Agent
	input := &adk.AgentInput{
		Messages: messages,
	}

	zap.L().Info("[Orchestrator] 开始处理用户请求",
		zap.String("session_id", sessionID),
		zap.Int("history_count", len(messages)),
	)

	iter := orchestratorAgent.Run(ctx, input)

	var result strings.Builder
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}

		// 日志：Agent 事件
		if event.Err != nil {
			zap.L().Warn("[Orchestrator] 事件错误",
				zap.String("agent", event.AgentName),
				zap.Error(event.Err),
			)
			continue
		}

		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, msgErr := event.Output.MessageOutput.GetMessage()
			if msgErr == nil && msg != nil {
				// 日志：LLM 输出
				zap.L().Info("[Orchestrator] LLM 输出",
					zap.String("agent", event.AgentName),
					zap.String("role", string(msg.Role)),
					zap.Int("tool_calls", len(msg.ToolCalls)),
					zap.String("content_preview", truncate(msg.Content, 200)),
				)

				// 日志：Tool 调用详情
				for _, tc := range msg.ToolCalls {
					zap.L().Info("[Orchestrator] Tool Call",
						zap.String("agent", event.AgentName),
						zap.String("tool_name", tc.Function.Name),
						zap.String("arguments_preview", truncate(tc.Function.Arguments, 500)),
					)
				}

				result.WriteString(msg.Content)
			}
		}
	}

	// 追加助手回复到会话
	assistantMsg := &schema.Message{
		Role:    schema.Assistant,
		Content: result.String(),
	}
	if err := o.sessions.AppendMessage(sessionID, assistantMsg); err != nil {
		zap.L().Warn("追加助手消息失败", zap.Error(err))
	}

	return result.String(), nil
}

// truncate 截断字符串用于日志，避免过长
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
