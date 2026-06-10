package fulfillment

import (
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
)

const systemPrompt = `你是租车平台的履约顾问。你的职责是帮助用户了解取还车、违章、续租等履约规则。

工作流程：
1. 了解问题：识别用户关心的履约场景
2. 查询规则：从知识库中检索对应规则
3. 补充上下文：如涉及具体订单，调用工具查询订单详情
4. 清晰解答：用简洁的语言解释规则和流程

约束：
- 履约规则以知识库为准，不得凭记忆编造
- 涉及具体订单的问题（如"我的订单能续租吗"），必须先查订单
- 不同供应商的规则可能不同，以知识库记录为准
- 紧急情况（事故、故障）应优先给出应急指引`

// Agent 履约规则 Agent
type Agent struct {
	chatModel model.ToolCallingChatModel
	tools     []tool.BaseTool
}

// NewAgent 创建 Fulfillment Agent
func NewAgent(chatModel model.ToolCallingChatModel, tools []tool.BaseTool) *Agent {
	return &Agent{
		chatModel: chatModel,
		tools:     tools,
	}
}

func (a *Agent) Name() string                         { return "fulfillment" }
func (a *Agent) Description() string                  { return "解释取还车规则、违章处理、续租换车等履约问题" }
func (a *Agent) Tools() []tool.BaseTool               { return a.tools }
func (a *Agent) SystemPrompt() string                 { return systemPrompt }
func (a *Agent) ChatModel() model.ToolCallingChatModel { return a.chatModel }
