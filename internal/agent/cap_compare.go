package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/prompt"
	"github.com/zxq97/rental-agent/internal/tools"
)

// CompareCapability 车型对比:并发取多辆 get_order_details + LLM 综合。
// 本质是 get_price_detail 的"并行多辆"版(印证:多车对比用并行 tool_calls + 一次综合,不需 planner)。
type CompareCapability struct{}

func (c *CompareCapability) Name() string { return "compare" }

func (c *CompareCapability) Run(ctx context.Context, in CapabilityInput) (*CapabilityResult, error) {
	refs := extractRefs(in.Decision.Args["vehicle_refs"])
	if len(refs) < 2 {
		msg := "你想对比哪几辆车呢?告诉我车名我来帮你比。"
		emitText(in, msg)
		return &CapabilityResult{Text: msg}, nil
	}

	resolved, clar, missing := ResolveMany(in.State, refs)
	if clar != nil {
		return &CapabilityResult{Clarification: clar}, nil
	}
	if len(resolved) < 2 {
		msg := "其中有车我这边没找到报价,先帮你搜一下再对比?"
		if len(missing) > 0 {
			msg = fmt.Sprintf("「%s」我这边还没有报价,先帮你搜一下再对比?", strings.Join(missing, "、"))
		}
		emitText(in, msg)
		return &CapabilityResult{Text: msg}, nil
	}

	// 并发取每辆明细
	details := c.fetchAll(ctx, in, resolved)
	if len(details) < 2 {
		msg := "对比所需的明细暂时没取全,稍后再试或在 App 内查看?"
		emitText(in, msg)
		return &CapabilityResult{Text: msg}, nil
	}

	userMsg := formatCompareData(details)
	text := streamCapabilityLLM(ctx, in, "compare", prompt.CompareSystem, userMsg,
		"这几辆的明细我都拿到了,但对比时出了点问题,你可以在 App 内逐辆查看。以下单时为准。")
	return &CapabilityResult{
		Text:       text,
		ToolName:   tools.ToolGetOrderDetails,
		ToolResult: fmt.Sprintf("已为用户对比 %d 辆车", len(details)),
	}, nil
}

type namedDetail struct {
	carName string
	od      *orderDetailsData
}

// fetchAll 并发取每个 ref 的明细。部分失败降级为只对比成功的。
func (c *CompareCapability) fetchAll(ctx context.Context, in CapabilityInput, refs []string) []namedDetail {
	ctxID, quotes, _ := in.State.SnapshotQuotes()

	type result struct {
		idx int
		nd  namedDetail
	}
	resCh := make(chan result, len(refs))
	for i, ref := range refs {
		go func(idx int, ref string) {
			args := map[string]any{
				"context_id":   ctxID,
				"reference_id": ref,
				"supplier":     in.State.SupplierOf(ref),
			}
			argsJSON, _ := json.Marshal(args)
			callRes := in.Deps.Call(ctx, tools.ToolGetOrderDetails, string(argsJSON))
			nd := namedDetail{carName: carNameOf(quotes, ref)}
			if !callRes.IsError {
				if data, ok := parseStdResp(callRes.Data); ok {
					var od orderDetailsData
					if json.Unmarshal(data, &od) == nil {
						nd.od = &od
					}
				}
			}
			resCh <- result{idx: idx, nd: nd}
		}(i, ref)
	}

	ordered := make([]namedDetail, len(refs))
	for range refs {
		r := <-resCh
		ordered[r.idx] = r.nd
	}

	out := make([]namedDetail, 0, len(refs))
	for _, nd := range ordered {
		if nd.od != nil {
			out = append(out, nd)
		}
	}
	return out
}

// formatCompareData 把多辆明细拼成给 LLM 的对比输入。
func formatCompareData(details []namedDetail) string {
	var b strings.Builder
	for _, nd := range details {
		fmt.Fprintf(&b, "【%s】\n", nd.carName)
		b.WriteString(formatPriceData(nd.od))
		b.WriteString("\n")
	}
	return b.String()
}

// extractRefs 从工具入参的 vehicle_refs(any)抽出字符串列表。
func extractRefs(v any) []string {
	if vals, ok := v.([]string); ok {
		out := make([]string, 0, len(vals))
		for _, s := range vals {
			if strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// carNameOf 查 reference_id 对应车名,查不到回退 ref。
func carNameOf(quotes []orchestration.QuoteRef, ref string) string {
	for _, q := range quotes {
		if q.ReferenceID == ref {
			if q.CarName != "" {
				return q.CarName
			}
			return q.BrandName
		}
	}
	return ref
}
