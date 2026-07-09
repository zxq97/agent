package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/zxq97/rental-agent/internal/prompt"
	"github.com/zxq97/rental-agent/internal/tools"
)

// chargeTypeInsurance tyche charges 里保险费的 Type(约定值)。
const chargeTypeInsurance = 3

// InsuranceCapability 保险建议:基于 get_order_details 的 charges(Type=3 保险费)+ 驾龄。
type InsuranceCapability struct{}

func (c *InsuranceCapability) Name() string { return "insurance" }

func (c *InsuranceCapability) Run(ctx context.Context, in CapabilityInput) (*CapabilityResult, error) {
	ref, _ := in.Decision.Args["vehicle_ref"].(string)
	// vehicle_ref 可空 → 用已选定的(SelectedRef);ResolveQuoteRef 对空串返回 0 命中,
	// 故这里若为空,直接尝试用 state.SelectedRef 对应的车。
	if strings.TrimSpace(ref) == "" {
		if _, quotes, _ := in.State.SnapshotQuotes(); len(quotes) == 1 {
			ref = quotes[0].CarName // 单候选直接用
		}
	}

	if _, clar, errText := resolveQuoteForDetails(in.State, ref); clar != nil {
		return &CapabilityResult{Clarification: clar}, nil
	} else if errText != "" {
		emitText(in, errText)
		return &CapabilityResult{Text: errText}, nil
	}
	driverAge := parseDriverAge(in.Decision.Args["driver_age"])
	if driverAge <= 0 {
		return &CapabilityResult{Clarification: &Clarification{
			Question: "你大概有几年驾龄?我好按风险给你建议保险档位。",
			Options:  []string{"2年以内", "2-5年", "5年以上"},
			Slot:     "driver_age",
		}}, nil
	}

	od, clar, errText := fetchOrderDetails(ctx, in, ref)
	if clar != nil {
		return &CapabilityResult{Clarification: clar}, nil
	}
	if errText != "" {
		emitText(in, errText)
		return &CapabilityResult{Text: errText}, nil
	}

	userMsg := formatInsuranceData(od, driverAge)
	text := streamCapabilityLLM(ctx, in, "insurance", prompt.InsuranceSystem, userMsg,
		"这辆车的保险信息我拿到了,具体保障内容请在 App 内查看。保障范围以保险合同条款为准。")
	return &CapabilityResult{
		Text:       text,
		ToolName:   tools.ToolGetOrderDetails,
		ToolResult: "已为用户讲解保险选项",
	}, nil
}

// formatInsuranceData 从 charges 抽保险费(Type=3),拼成给 LLM 的文本。
func formatInsuranceData(od *orderDetailsData, driverAge int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "用户驾龄:%d年\n", driverAge)
	b.WriteString("保险费项(来自费用明细):\n")
	found := false
	for _, ch := range od.PriceDetail.Charges {
		if ch.Type == chargeTypeInsurance {
			fmt.Fprintf(&b, "- %s: %.2f 元\n", ch.Name, ch.Amount)
			found = true
		}
	}
	if !found {
		b.WriteString("(未在明细中找到独立保险费项)\n")
	}
	b.WriteString("注:保障范围细节工具未透出,统一引导用户 App 内查看。\n")
	return b.String()
}

func parseDriverAge(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	case string:
		return parseIntFromValue(x)
	default:
		return 0
	}
}
