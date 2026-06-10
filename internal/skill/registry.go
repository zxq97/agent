package skill

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"
)

// SkillRegistry Skill Agent 统一注册表
type SkillRegistry struct {
	agents map[string]SkillAgent
}

// NewSkillRegistry 创建 Skill 注册表
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		agents: make(map[string]SkillAgent),
	}
}

// Register 注册 Skill Agent
func (r *SkillRegistry) Register(agent SkillAgent) {
	r.agents[agent.Name()] = agent
	zap.L().Info("Skill Agent 已注册",
		zap.String("name", agent.Name()),
		zap.String("description", agent.Description()),
	)
}

// GetByName 按名称获取 Agent
func (r *SkillRegistry) GetByName(name string) (SkillAgent, bool) {
	a, ok := r.agents[name]
	return a, ok
}

// ListAll 返回所有注册的 Agent
func (r *SkillRegistry) ListAll() []SkillAgent {
	result := make([]SkillAgent, 0, len(r.agents))
	for _, a := range r.agents {
		result = append(result, a)
	}
	return result
}

// AsTools 将所有 Agent 包装为 Orchestrator 可调用的 Tool
func (r *SkillRegistry) AsTools(ctx context.Context) ([]tool.BaseTool, error) {
	var tools []tool.BaseTool

	for _, sa := range r.agents {
		toolsNodeConfig := compose.ToolsNodeConfig{
			Tools: sa.Tools(),
		}

		chatModelAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
			Name:        sa.Name(),
			Description: sa.Description(),
			Instruction: sa.SystemPrompt(),
			Model:       sa.ChatModel(),
			ToolsConfig: adk.ToolsConfig{
				ToolsNodeConfig: toolsNodeConfig,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("创建 Agent %s 失败: %w", sa.Name(), err)
		}

		agentTool := adk.NewAgentTool(ctx, chatModelAgent)
		tools = append(tools, agentTool)
	}

	return tools, nil
}

// BuildRouterPrompt 根据 Registry 中的 Agent 自动生成路由 Prompt
func (r *SkillRegistry) BuildRouterPrompt() string {
	var sb strings.Builder

	for _, agent := range r.agents {
		sb.WriteString(fmt.Sprintf("- %s → 调用 %s_agent 工具\n", agent.Description(), agent.Name()))
	}

	return sb.String()
}
