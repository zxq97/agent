package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/prompt"
	"github.com/zxq97/rental-agent/internal/tools"
)

// fetchOrderDetails 解析 vehicle_ref → 锁定报价 → Go 注入 ID 调 get_order_details → 返回明细。
// 返回 (明细, 澄清反问, 错误提示文本)。三者最多一个非零。
func fetchOrderDetails(ctx context.Context, in CapabilityInput, vehicleRef string) (*orderDetailsData, *Clarification, string) {
	st := in.State
	ref, clar, errText := resolveQuoteForDetails(st, vehicleRef)
	if clar != nil {
		return nil, clar, ""
	}
	if errText != "" {
		return nil, nil, errText
	}

	// Go 注入 ID:context_id + reference_id + supplier 全从 state 取
	ctxID, _, _ := st.SnapshotQuotes()
	args := map[string]any{
		"context_id":   ctxID,
		"reference_id": ref,
		"supplier":     st.SupplierOf(ref),
	}
	argsJSON, _ := json.Marshal(args)
	res := in.Deps.Call(ctx, tools.ToolGetOrderDetails, string(argsJSON))
	if res.IsError {
		return nil, nil, res.UserMsg
	}
	data, ok := parseStdResp(res.Data)
	if !ok {
		return nil, nil, "抱歉,暂时没拿到这辆车的费用明细,稍后再试?"
	}
	var od orderDetailsData
	if err := json.Unmarshal(data, &od); err != nil {
		return nil, nil, "抱歉,费用明细解析出了点问题,稍后再试?"
	}
	return &od, nil, ""
}

func resolveQuoteForDetails(state *orchestration.ConversationState, vehicleRef string) (string, *Clarification, string) {
	if state.IsQuoteStale(tools.QuoteTTL) {
		return "", nil, "这批报价有点久了,我先帮你重新搜一下最新的价格哈。"
	}
	if strings.TrimSpace(vehicleRef) == "" {
		if selected := state.SelectedQuoteRef(); selected != "" {
			return selected, nil, ""
		}
	}
	ref, clar := ResolveQuoteRef(state, vehicleRef)
	if clar != nil {
		return "", clar, ""
	}
	if ref == "" {
		return "", nil, "没找到你说的这辆车,先帮你搜一下报价?"
	}
	state.SelectQuote(ref)
	return ref, nil, ""
}

// PriceDetailCapability 价格明细讲解。
type PriceDetailCapability struct{}

func (c *PriceDetailCapability) Name() string { return "price_detail" }

func (c *PriceDetailCapability) Run(ctx context.Context, in CapabilityInput) (*CapabilityResult, error) {
	ref, _ := in.Decision.Args["vehicle_ref"].(string)
	od, clar, errText := fetchOrderDetails(ctx, in, ref)
	if clar != nil {
		return &CapabilityResult{Clarification: clar}, nil
	}
	if errText != "" {
		emitText(in, errText)
		return &CapabilityResult{Text: errText}, nil
	}

	text := c.streamExplain(ctx, in, od)
	return &CapabilityResult{
		Text:       text,
		ToolName:   tools.ToolGetOrderDetails,
		ToolResult: "已为用户讲解费用明细",
	}, nil
}

func (c *PriceDetailCapability) streamExplain(ctx context.Context, in CapabilityInput, od *orderDetailsData) string {
	userMsg := formatPriceData(od)
	return streamCapabilityLLM(ctx, in, "price_detail", prompt.PriceDetailSystem, userMsg,
		"这辆车的费用明细我拿到了,但讲解时出了点问题,你可以在 App 内查看完整价格。以下单时为准。")
}

// formatPriceData 把明细数据拼成给 LLM 的文本。
func formatPriceData(od *orderDetailsData) string {
	var b strings.Builder
	b.WriteString("费用明细:\n")
	for _, ch := range od.PriceDetail.Charges {
		fmt.Fprintf(&b, "- %s: %.2f 元\n", ch.Name, ch.Amount)
	}
	if len(od.PriceDetail.Promotions) > 0 {
		b.WriteString("优惠:\n")
		for _, p := range od.PriceDetail.Promotions {
			fmt.Fprintf(&b, "- %s: -%.2f 元\n", p.Name, p.Amount)
		}
	}
	fmt.Fprintf(&b, "日均: %.2f 元\n合计: %.2f 元\n", od.PriceDetail.DailyPrice, od.PriceDetail.Total)
	return b.String()
}

// ---- 共享:Capability 内二次 LLM 流式调用 ----

func streamCapabilityLLM(ctx context.Context, in CapabilityInput, binding, sysPrompt, userMsg, fallback string) string {
	model, err := in.Factory.Get(binding)
	if err != nil {
		emitText(in, fallback)
		return fallback
	}
	ch, err := model.ChatStream(llm.WithStage(ctx, "capability:"+binding), llm.ChatRequest{
		System:   sysPrompt,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: userMsg}},
	})
	if err != nil {
		emitText(in, fallback)
		return fallback
	}
	var b strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			break
		}
		if chunk.Delta != "" {
			b.WriteString(chunk.Delta)
			emitText(in, chunk.Delta)
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		emitText(in, fallback)
		return fallback
	}
	return b.String()
}

func emitText(in CapabilityInput, s string) {
	if in.Emit != nil {
		in.Emit.Text(s)
	}
}
