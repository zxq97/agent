package prompt

import (
	"bytes"
	"fmt"
	"text/template"
	"time"
)

// SupervisorSystemVars 渲染 Supervisor system prompt 的变量。
type SupervisorSystemVars struct {
	Now           string // 当前时间(含星期)
	AssistantName string // 客服昵称
}

// supervisorSystemTpl 是顶层 supervisor 的 system prompt。
//
// 设计原则:
//   - Supervisor 只做"任务分派",不直接答用户、不调业务工具
//   - 通过 transfer_to_agent 把任务转给合适的子 agent
//   - 子 agent 答完会自动转回 supervisor,supervisor 决定要不要再转(继续追问)还是直接结束回话
const supervisorSystemTpl = `你是租车客服「{{.AssistantName}}」的调度中枢。当前时间:{{.Now}}。

# 你的唯一职责
判断用户的当前消息属于哪类需求,然后用 transfer_to_agent 把任务转给对应的子 agent。
**你不应该直接答用户问题、不调业务工具(报价/明细/保险等)。**

# 子 agent 路由规则
- **ShoppingAgent**:用户在挑车 / 比价 / 报价 / 看车型 / 看门店 / 看价格明细 → 转给它。
  典型话术:"明天北京租 SUV"、"帮我看下报价"、"这辆车多少钱"、"附近有哪些店"、"这个价怎么算的"
- **InsuranceAgent**:用户在选保险 / 问保险范围 / 加购保障 → 转给它。
  典型话术:"要不要加全险"、"基础保障包含什么"、"我驾龄 3 年要选哪种保险"

# 路由判断要点
1. 看用户**当前消息**的明确意图,不要回过头解析很久之前的话
2. 模糊语义("有什么推荐") → 多数情况转 ShoppingAgent
3. 同一问题不要反复在多个子 agent 间倒腾;若子 agent 已经给了答复,你应该直接结束本轮(让最终输出回到用户),不要再次 transfer
4. 用户改变话题(比如从"挑车"切到"问保险")时再 transfer 给新 agent

# 最终输出
当子 agent 已经给出对用户的回复后,**直接将其内容作为最终答复输出,不要做额外加工或重复转发**。`

// RenderSupervisorSystem 用变量渲染 supervisor system prompt。
func RenderSupervisorSystem(v SupervisorSystemVars) (string, error) {
	if v.AssistantName == "" {
		v.AssistantName = "小租"
	}
	if v.Now == "" {
		now := time.Now()
		v.Now = now.Format("2006-01-02 15:04") + " " + weekdayCN(now)
	}
	tpl, err := template.New("supervisor_system").Parse(supervisorSystemTpl)
	if err != nil {
		return "", fmt.Errorf("parse supervisor system tpl: %w", err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, v); err != nil {
		return "", fmt.Errorf("execute supervisor system tpl: %w", err)
	}
	return buf.String(), nil
}
