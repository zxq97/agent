package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/prompt"
)

// RulesFallbackText 是规则检索不可用/无命中/内容不适合外显时的统一兜底。
const RulesFallbackText = "这个规则我先不替你下结论了,建议以订单页/商家规则为准,或在 App 内联系在线客服获取准确解答。"

// RulesCapability 规则解读:AgentHub 检索 + grounded LLM 生成。
type RulesCapability struct{}

func (c *RulesCapability) Name() string { return "rules" }

func (c *RulesCapability) Run(ctx context.Context, in CapabilityInput) (*CapabilityResult, error) {
	query, _ := in.Decision.Args["rule_query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		query = strings.TrimSpace(in.UserInput)
	}
	if query == "" {
		return c.fallback(in), nil
	}
	if in.Deps == nil || in.Deps.AgentHub == nil {
		return c.fallback(in), nil
	}

	content, err := in.Deps.AgentHub.Retrieve(ctx, query)
	if err != nil || strings.TrimSpace(content) == "" || looksInternalOpsContent(content) {
		return c.fallback(in), nil
	}
	text := c.streamGroundedAnswer(ctx, in, query, content)
	return &CapabilityResult{
		Text:       text,
		ToolName:   ToolInterpretRules,
		ToolArgs:   query,
		ToolResult: "已基于 AgentHub 检索资料回答规则问题",
	}, nil
}

func (c *RulesCapability) fallback(in CapabilityInput) *CapabilityResult {
	emitText(in, RulesFallbackText)
	return &CapabilityResult{Text: RulesFallbackText}
}

func (c *RulesCapability) streamGroundedAnswer(ctx context.Context, in CapabilityInput, query, content string) string {
	if in.Factory == nil {
		return RulesFallbackText
	}
	model, err := in.Factory.Get("rules")
	if err != nil {
		emitText(in, RulesFallbackText)
		return RulesFallbackText
	}
	userMsg := formatRulesUserMessage(in, query, content)
	ch, err := model.ChatStream(llm.WithStage(ctx, "capability:rules"), llm.ChatRequest{
		System:      prompt.RulesSystem,
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: userMsg}},
		Temperature: llm.Float64Ptr(0.2),
		MaxTokens:   800,
	})
	if err != nil {
		emitText(in, RulesFallbackText)
		return RulesFallbackText
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
		emitText(in, RulesFallbackText)
		return RulesFallbackText
	}
	return b.String()
}

func formatRulesUserMessage(in CapabilityInput, query, content string) string {
	rental := "暂无"
	if in.State != nil {
		ctxID, quotes, _ := in.State.SnapshotQuotes()
		if ctxID != "" || len(quotes) > 0 {
			rental = fmt.Sprintf("context已存在:%t,最近报价数:%d", ctxID != "", len(quotes))
		}
	}
	return fmt.Sprintf("【检索到的知识资料】\n%s\n\n【当前取还车】\n%s\n\n【用户问题】\n%s", content, rental, query)
}

func looksInternalOpsContent(content string) bool {
	s := strings.ToLower(content)
	needles := []string{
		"prompt",
		"system prompt",
		"ai 话术",
		"话术规则",
		"应答口径",
		"对话模板",
		"内部运营",
		"内部规则",
	}
	for _, n := range needles {
		if strings.Contains(s, strings.ToLower(n)) {
			return true
		}
	}
	return false
}
