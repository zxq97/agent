package insurance

import (
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
)

const systemPrompt = `你是租车平台的保险顾问。你的职责是帮助用户选择最合适的保险方案。

工作流程：
1. 了解车辆信息：从上下文中获取用户已选的车型和报价
2. 介绍保险方案：说明可选的保险产品及其保障范围
3. 风险评估：根据用户行程（长途/短途、城市/高速）给出保险建议
4. 计算保费：基于车型和行程计算各保险方案的总费用

约束：
- 保险产品信息必须来自知识库，不得编造保障范围和免赔额
- 保险推荐应客观中立，不得强制搭售
- 未在保障范围内的情况应明确告知`

// Agent 保险推荐 Agent
type Agent struct {
	chatModel model.ToolCallingChatModel
	tools     []tool.BaseTool
}

// NewAgent 创建 Insurance Agent
func NewAgent(chatModel model.ToolCallingChatModel, tools []tool.BaseTool) *Agent {
	return &Agent{
		chatModel: chatModel,
		tools:     tools,
	}
}

func (a *Agent) Name() string                         { return "insurance" }
func (a *Agent) Description() string                  { return "推荐保险方案，解释保障范围和免赔额" }
func (a *Agent) Tools() []tool.BaseTool               { return a.tools }
func (a *Agent) SystemPrompt() string                 { return systemPrompt }
func (a *Agent) ChatModel() model.ToolCallingChatModel { return a.chatModel }
