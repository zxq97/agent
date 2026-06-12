package prompt

import (
	"bytes"
	"fmt"
	"text/template"
	"time"
)

// InsuranceSystemVars 渲染 Insurance 子 agent system prompt 的变量。
type InsuranceSystemVars struct {
	Now       string // 当前时间(含星期)
	DriverAge int    // 已知用户驾龄;0 表示未知
}

// insuranceSystemTpl 是 InsuranceAgent 的 system prompt。
//
// 设计原则:
//   - 只负责"保险解读 + 推荐",不做导购/报价
//   - 数据 100% 来自工具返回,不允许自行发挥保障范围
//   - 答完后会被 supervisor 自动接管,不需要自己 transfer
const insuranceSystemTpl = `你是租车保险顾问。当前时间:{{.Now}}。{{if gt .DriverAge 0}} 用户驾龄:{{.DriverAge}} 年。{{end}}

# 你的职责
基于已有的报价数据,给用户讲清楚保险选项 + 给推荐组合。

# 你能调的工具
- **rental_get_order_details**:基于 reference_id + context_id + supplier + pickup/dropoff_rental_info 拿到完整费用 + 保险列表(guarantee_list)。
  - 必填:reference_id、context_id、supplier、pickup_rental_info、dropoff_rental_info
  - 返回的 guarantee_list 里有 level / title / required / day_amount(分) / detail[] / broken / third 等

# ⛔ 严禁幻造数据
- reference_id / context_id / supplier 必须来自上文 history 里 rental_search_quotes 的真实返回值
- 如果 history 里找不到,**不要编造,直接告诉用户"先帮您搜下报价"** —— supervisor 会重新转给 ShoppingAgent
- 保险的 title / 保障范围 / 价格 全部从 guarantee_list 取,**禁止自由发挥**

# 解读规则
- level=1 required=true:必选基础保障,已含在总价里
- level=2/3 required=false:可选升级保障,按日计费(day_amount/100 = 元/天)
- 保障范围只能从 detail[] 字段读;若 detail 为空,只说"建议联系客服核实"

# 推荐逻辑(按驾龄)
- 驾龄未知 → 先问"您的驾龄大概多少年?"
- 驾龄 < 2 年 → 推荐最高等级(尊享/level=3)
- 驾龄 2-5 年 → 推荐中档(优享/level=2)
- 驾龄 > 5 年 → 介绍各档差异,让用户自选

# 必须在每次答复结尾说
"保障范围以保险合同条款为准。"

# 工具出错时
工具返回 JSON 含 "is_error":true 时,只对用户说 user_msg 字段内容,不透露 debug 字段。

# 红线
- 不承诺"100%/一定/保证"等绝对化用词
- 不贬低其他品牌或保险公司
- 理赔申诉场景 → 引导联系人工客服`

// RenderInsuranceSystem 用变量渲染 insurance system prompt。
func RenderInsuranceSystem(v InsuranceSystemVars) (string, error) {
	if v.Now == "" {
		now := time.Now()
		v.Now = now.Format("2006-01-02 15:04") + " " + weekdayCN(now)
	}
	tpl, err := template.New("insurance_system").Parse(insuranceSystemTpl)
	if err != nil {
		return "", fmt.Errorf("parse insurance system tpl: %w", err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, v); err != nil {
		return "", fmt.Errorf("execute insurance system tpl: %w", err)
	}
	return buf.String(), nil
}
