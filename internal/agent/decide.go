package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/types"
)

// logf 写日志到 writer（nil 安全）。
func logf(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format+"\n", args...)
	}
}

// Decider 单次流式 function-calling 决策。不进 ReAct 循环。
type Decider struct {
	model     llm.ChatModel
	sysPrompt string
}

// NewDecider 构造决策器。
func NewDecider(model llm.ChatModel, sysPrompt string) *Decider {
	return &Decider{model: model, sysPrompt: sysPrompt}
}

// Decide 一次流式决策:
//   - 收到首帧 content 增量立即通过 emit 实时下发(不等流结束)
//   - 流末把 tool_calls 拼成 Decision
//   - 流异常且一字未出 → syncFallback 回退非流式重试一次
func (d *Decider) Decide(ctx context.Context, state *orchestration.ConversationState, userInput string, emit Emitter, logger io.Writer) (*Decision, error) {
	msgs := d.buildMessages(state, userInput)
	req := llm.ChatRequest{
		System:   d.sysPrompt,
		Messages: msgs,
		Tools:    decideTools(),
	}
	if vs, err := BuildDecideVersionSet(d.sysPrompt); err == nil {
		logf(logger, "[llm_version] prompt_id=%s prompt_version=%s prompt_hash=%s context_version=%s tool_schema_hash=%s parser_version=%s",
			vs.PromptID, vs.PromptVersion, vs.PromptHash, vs.ContextVersion, vs.ToolSchemaHash, vs.ParserVersion)
	}

	logf(logger, "[llm] stage=decide mode=stream starting messages=%d tools=%d", len(msgs), len(req.Tools))
	start := time.Now()

	ctx = llm.WithStage(ctx, "decide")
	ch, err := d.model.ChatStream(ctx, req)
	if err != nil {
		logf(logger, "[llm] stage=decide mode=stream start_error err=%v, trying sync fallback", err)
		return d.syncFallback(ctx, req, emit, logger)
	}

	var reply string
	var toolCalls []llm.ToolCall
	var usage *llm.Usage
	produced := false
	for chunk := range ch {
		if chunk.Err != nil {
			if !produced {
				logf(logger, "[llm] stage=decide mode=stream error_no_output err=%v, trying sync fallback", chunk.Err)
				return d.syncFallback(ctx, req, emit, logger)
			}
			break
		}
		if chunk.Delta != "" {
			produced = true
			reply += chunk.Delta
			if emit != nil {
				emit.Text(chunk.Delta)
			}
		}
		if len(chunk.ToolCalls) > 0 {
			toolCalls = chunk.ToolCalls
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
	}

	dur := time.Since(start).Milliseconds()
	dec := buildDecision(toolCalls, reply)
	toolName := dec.Tool
	if toolName == "" {
		toolName = "(none)"
	}
	tokenInfo := ""
	if usage != nil {
		tokenInfo = fmt.Sprintf(" tokens=%d(prompt=%d,completion=%d)", usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens)
	}
	logf(logger, "[llm] stage=decide mode=stream status=ok dur_ms=%d tool=%s reply_len=%d%s", dur, toolName, len(reply), tokenInfo)
	if len(dec.NeedDelta) > 0 {
		logf(logger, "[decide] need_delta count=%d", len(dec.NeedDelta))
		for _, nd := range dec.NeedDelta {
			logf(logger, "[decide]   %s %s=%v (hardness=%s, conf=%.2f)", nd.Op, nd.Type, nd.Value, nd.Hardness, nd.Confidence)
		}
	}
	logToolArgsDiagnostics(logger, dec)
	return dec, nil
}

// syncFallback 流式失败时回退同步 Chat。
func (d *Decider) syncFallback(ctx context.Context, req llm.ChatRequest, emit Emitter, logger io.Writer) (*Decision, error) {
	logf(logger, "[llm] stage=decide mode=sync_fallback starting")
	start := time.Now()
	ctx = llm.WithStage(ctx, "decide")
	resp, err := d.model.Chat(ctx, req)
	if err != nil {
		logf(logger, "[llm] stage=decide mode=sync_fallback status=error dur_ms=%d err=%v", time.Since(start).Milliseconds(), err)
		return nil, fmt.Errorf("decide: sync fallback failed: %w", err)
	}
	if resp.Content != "" && emit != nil {
		emit.Text(resp.Content)
	}
	logf(logger, "[llm] stage=decide mode=sync_fallback status=ok dur_ms=%d tokens=%d", time.Since(start).Milliseconds(), resp.Usage.TotalTokens)
	dec := buildDecision(resp.ToolCalls, resp.Content)
	logToolArgsDiagnostics(logger, dec)
	return dec, nil
}

func logToolArgsDiagnostics(logger io.Writer, dec *Decision) {
	if logger == nil || dec == nil || dec.ArgsDiag == nil {
		return
	}
	diag := dec.ArgsDiag
	if diag.Repaired {
		logf(logger, "[tool_args] tool=%s status=repaired parse_error=%q raw=%s repaired=%s",
			dec.Tool, diag.ParseError, truncateForLog(diag.Raw, 1024), truncateForLog(diag.RepairedArgs, 1024))
	}
	if diag.ParseError != "" && !diag.Repaired {
		logf(logger, "[tool_args] tool=%s status=parse_error err=%q raw=%s",
			dec.Tool, diag.ParseError, truncateForLog(diag.Raw, 1024))
	}
	if len(diag.ValidationErrors) > 0 {
		logf(logger, "[tool_args] tool=%s status=validation_error errors=%v raw=%s",
			dec.Tool, diag.ValidationErrors, truncateForLog(diag.Raw, 1024))
	}
}

// buildDecision 把工具调用 + 话术拼成 Decision。无 tool_call = 纯回复。
// search_vehicles 时额外解析 need_delta / understanding / strong_search_intent。
func buildDecision(toolCalls []llm.ToolCall, reply string) *Decision {
	if len(toolCalls) == 0 {
		return &Decision{Tool: "", Reply: reply}
	}
	// 只取第一个工具调用(决策层一次一个动作)
	tc := toolCalls[0]
	args, diag := parseDecisionArgsWithDiagnostics(tc.Function.Arguments)
	validateToolArgs(tc.Function.Name, args, diag)
	dec := &Decision{
		Tool:     tc.Function.Name,
		Args:     args,
		Reply:    reply,
		ArgsDiag: diag,
	}
	// search_vehicles: 解析 need_delta / understanding / strong_search_intent
	if tc.Function.Name == ToolSearchVehicles {
		if v, ok := args["search_mode"].(string); ok {
			dec.SearchMode = normalizeSearchMode(v)
		} else {
			dec.SearchMode = SearchModeRefine
		}
		if v, ok := args["feedback_ref"].(string); ok {
			dec.FeedbackRef = v
		}
		if v, ok := args["pickup_text"].(string); ok {
			dec.PickupText = v
		}
		if v, ok := args["dropoff_text"].(string); ok {
			dec.DropoffText = v
		}
		if v, ok := args["pickup_time"].(string); ok {
			dec.PickupTimeText = v
		}
		if v, ok := args["dropoff_time"].(string); ok {
			dec.DropoffTimeText = v
		}
		dec.NeedDelta = parseNeedDelta(args)
		dec.Understanding = parseUnderstanding(args)
		dec.ProfilePatch = parseProfilePatch(args)
		if v, ok := args["strong_search_intent"].(bool); ok {
			dec.StrongSearchIntent = v
		}
	}
	return dec
}

// parseNeedDelta 从工具入参解析 need_delta 数组。
func parseNeedDelta(args map[string]any) []types.NeedDelta {
	raw, ok := args["need_delta"]
	if !ok || raw == nil {
		return nil
	}
	// args 是 json.Unmarshal 出来的 map,need_delta 是 []interface{}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var deltas []types.NeedDelta
	if err := json.Unmarshal(b, &deltas); err != nil {
		return nil
	}
	return deltas
}

// parseUnderstanding 从工具入参解析 understanding 对象。
func parseUnderstanding(args map[string]any) *Understanding {
	raw, ok := args["understanding"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var u Understanding
	if err := json.Unmarshal(b, &u); err != nil {
		return nil
	}
	return &u
}

// buildMessages 组装决策入参 messages:历史回放 + 状态前缀 + 本轮 user。
func (d *Decider) buildMessages(state *orchestration.ConversationState, userInput string) []llm.Message {
	const maxHistory = 12
	hist := state.SnapshotHistory()
	if len(hist) > maxHistory {
		hist = hist[len(hist)-maxHistory:]
	}

	msgs := make([]llm.Message, 0, len(hist)+1)
	for i, h := range hist {
		// 工具轮:还原成 assistant(tool_calls) + tool(result),让模型看到上轮真发过工具调用
		if h.ToolCall != nil {
			callID := fmt.Sprintf("call_%d", i)
			msgs = append(msgs,
				llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
					ID:       callID,
					Type:     "function",
					Function: llm.FunctionCall{Name: h.ToolCall.Name, Arguments: h.ToolCall.Arguments},
				}}},
				llm.Message{Role: llm.RoleTool, ToolCallID: callID, Content: h.ToolCall.Result},
			)
			continue
		}
		if h.Msg != nil {
			msgs = append(msgs, *h.Msg)
		}
	}
	prefix := BuildStatePrefix(state, time.Now())
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: prefix + "\n" + userInput})
	return msgs
}
