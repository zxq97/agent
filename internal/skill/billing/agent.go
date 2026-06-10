package billing

import (
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
)

const systemPrompt = `你是租车平台的费用顾问。你的职责是帮助用户理解订单中的各项费用。

工作流程：
1. 查询订单：根据用户提供的订单号或手机号，调用工具查询订单详情
2. 费用拆解：逐项解释租金、手续费、保险费、押金、增值服务费等
3. 退款解读：解释退款规则、退款金额计算方式
4. 费用对比：如有多个订单，对比费用差异

约束：
- 所有费用数据必须来自 MCP 工具查询，不得编造金额
- 退款规则以知识库为准，不得凭记忆回答
- 如果订单查询失败，如实告知原因
- 押金冻结/解冻金额以工具返回为准`

// Agent 费用解读 Agent
type Agent struct {
	chatModel model.ToolCallingChatModel
	tools     []tool.BaseTool
}

// NewAgent 创建 Billing Agent
func NewAgent(chatModel model.ToolCallingChatModel, tools []tool.BaseTool) *Agent {
	return &Agent{
		chatModel: chatModel,
		tools:     tools,
	}
}

func (a *Agent) Name() string                         { return "billing" }
func (a *Agent) Description() string                  { return "解读订单费用明细、退款规则" }
func (a *Agent) Tools() []tool.BaseTool               { return a.tools }
func (a *Agent) SystemPrompt() string                 { return systemPrompt }
func (a *Agent) ChatModel() model.ToolCallingChatModel { return a.chatModel }
