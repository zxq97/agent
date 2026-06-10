package skill

import (
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
)

// SkillAgent 定义所有 Skill Agent 必须满足的接口
// 新增 Skill Agent 只需实现此接口，然后在 registry.go 中注册
type SkillAgent interface {
	// Name 返回 Agent 唯一标识，如 "vehicle"、"insurance"、"order"
	Name() string
	// Description 返回 Agent 能力描述，注入 Orchestrator 的路由 Prompt
	Description() string
	// Tools 返回该 Agent 可调用的 Tool 列表
	Tools() []tool.BaseTool
	// SystemPrompt 返回该 Agent 的系统提示词
	SystemPrompt() string
	// ChatModel 返回该 Agent 使用的 ChatModel
	ChatModel() model.ToolCallingChatModel
}
