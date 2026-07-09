package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// stageCtxKey 通过 ctx 传递本次 LLM 调用的 pipeline stage tag。
// 例:decide / capability:search_guide / capability:filter_interpreter / capability:rules。
type stageCtxKey struct{}

// WithStage 在 ctx 上打 stage tag,供 LoggingChatModel 落日志时区分。
func WithStage(ctx context.Context, stage string) context.Context {
	return context.WithValue(ctx, stageCtxKey{}, stage)
}

func stageFrom(ctx context.Context) string {
	if v, ok := ctx.Value(stageCtxKey{}).(string); ok && v != "" {
		return v
	}
	return "unknown"
}

// LoggingChatModel 给 ChatModel 套一层调用日志。
//
// 落全量:
//   - 入参:system / messages(含 role/content/tool_calls/tool_call_id) / tools schema
//   - 出参:content 全文 / tool_calls(name+arguments) / finish_reason / usage
//
// 顺序 & 格式设计成"一行 json + 少量 kv"混排,方便 grep 也方便机器解析。
type LoggingChatModel struct {
	inner    ChatModel
	provider string
	out      io.Writer
}

// NewLoggingChatModel 包装。
func NewLoggingChatModel(inner ChatModel, provider string, out io.Writer) ChatModel {
	return &LoggingChatModel{inner: inner, provider: provider, out: out}
}

func (m *LoggingChatModel) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	stage := stageFrom(ctx)
	m.logReq(stage, "sync", req)
	start := time.Now()
	resp, err := m.inner.Chat(ctx, req)
	dur := time.Since(start).Milliseconds()
	if err != nil {
		fmt.Fprintf(m.out, "[llm] stage=%s provider=%s mode=sync status=error dur_ms=%d err=%v\n", stage, m.provider, dur, err)
		return nil, err
	}
	m.logResp(stage, "sync", dur, resp.Content, resp.ToolCalls, resp.FinishReason, resp.Usage)
	return resp, nil
}

func (m *LoggingChatModel) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	stage := stageFrom(ctx)
	m.logReq(stage, "stream", req)
	start := time.Now()
	src, err := m.inner.ChatStream(ctx, req)
	if err != nil {
		fmt.Fprintf(m.out, "[llm] stage=%s provider=%s mode=stream status=start_error err=%v\n", stage, m.provider, err)
		return nil, err
	}
	out := make(chan StreamChunk, 16)
	go func() {
		defer close(out)
		var (
			contentBuf strings.Builder
			toolCalls  []ToolCall
			usage      Usage
			streamErr  error
		)
		for chunk := range src {
			if chunk.Err != nil {
				streamErr = chunk.Err
			}
			if chunk.Delta != "" {
				contentBuf.WriteString(chunk.Delta)
			}
			if len(chunk.ToolCalls) > 0 {
				toolCalls = chunk.ToolCalls
			}
			if chunk.Usage != nil {
				usage = *chunk.Usage
			}
			out <- chunk
		}
		dur := time.Since(start).Milliseconds()
		if streamErr != nil {
			fmt.Fprintf(m.out, "[llm] stage=%s provider=%s mode=stream status=error dur_ms=%d err=%v content_len=%d tool_calls=%d\n",
				stage, m.provider, dur, streamErr, contentBuf.Len(), len(toolCalls))
			return
		}
		m.logResp(stage, "stream", dur, contentBuf.String(), toolCalls, "", usage)
	}()
	return out, nil
}

// logReq 落一次 LLM 请求的完整入参。
func (m *LoggingChatModel) logReq(stage, mode string, req ChatRequest) {
	if m.out == nil {
		return
	}
	sys := trimForLog(req.System, 4096)
	msgs := renderMessages(req.Messages, 4096)
	tools := renderTools(req.Tools)
	fmt.Fprintf(m.out,
		"\n[llm] stage=%s provider=%s mode=%s status=start messages=%d tools=%d\n[llm]  system: %s\n[llm]  messages:\n%s\n[llm]  tools: %s\n",
		stage, m.provider, mode, len(req.Messages), len(req.Tools),
		sys, msgs, tools,
	)
}

// logResp 落一次 LLM 响应的完整出参(content 全文 + tool_calls)。
func (m *LoggingChatModel) logResp(stage, mode string, dur int64, content string, tcs []ToolCall, finish string, usage Usage) {
	if m.out == nil {
		return
	}
	tcInfo := renderToolCalls(tcs)
	finishTag := ""
	if finish != "" {
		finishTag = " finish=" + finish
	}
	fmt.Fprintf(m.out,
		"[llm] stage=%s provider=%s mode=%s status=ok dur_ms=%d tokens=%d(prompt=%d,completion=%d,cache=%d) tool_calls=%d%s\n[llm]  content: %s\n[llm]  tool_calls: %s\n",
		stage, m.provider, mode, dur,
		usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens, usage.CacheHitTokens,
		len(tcs), finishTag,
		trimForLog(content, 8192), tcInfo,
	)
}

func renderMessages(msgs []Message, perMax int) string {
	if len(msgs) == 0 {
		return "    (empty)"
	}
	var b strings.Builder
	for i, msg := range msgs {
		fmt.Fprintf(&b, "    [%d] role=%s", i, msg.Role)
		if msg.ToolCallID != "" {
			fmt.Fprintf(&b, " tool_call_id=%s", msg.ToolCallID)
		}
		if msg.Name != "" {
			fmt.Fprintf(&b, " name=%s", msg.Name)
		}
		if msg.Content != "" {
			fmt.Fprintf(&b, " content=%s", trimForLog(msg.Content, perMax))
		}
		if len(msg.ToolCalls) > 0 {
			fmt.Fprintf(&b, " tool_calls=%s", renderToolCalls(msg.ToolCalls))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderToolCalls(tcs []ToolCall) string {
	if len(tcs) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(tcs))
	for _, tc := range tcs {
		parts = append(parts, fmt.Sprintf("{name=%s args=%s}", tc.Function.Name, trimForLog(tc.Function.Arguments, 4096)))
	}
	return strings.Join(parts, " ")
}

func renderTools(tools []ToolDef) string {
	if len(tools) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Function.Name)
	}
	// 名字先给一眼看,再附完整 schema(JSON)。
	raw, _ := json.Marshal(tools)
	return fmt.Sprintf("[%s] schema=%s", strings.Join(names, ","), trimForLog(string(raw), 8192))
}

// trimForLog 单值截断,防炸屏但保留足够长度做定位。
func trimForLog(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("...(+%d)", len(s)-n)
}
