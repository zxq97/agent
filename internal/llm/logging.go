package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// LoggingChatModel 包一层 ToolCallingChatModel,把每次 Generate / Stream 的
// 输入(全部 messages)和输出(content + tool_calls)落到一个 io.Writer。
//
// 用途:定位 "LLM 到底有没有想调 tool / 调了什么"。
// React loop 每轮都会调一次 ChatModel,日志能完整回放对话:
//
//	[llm-in ] turn=1 messages=3
//	   [0] system: ...
//	   [1] user: ...
//	   [2] assistant: tool_calls=[...]
//	[llm-out] turn=1 finish=stop content_len=42 tool_calls=1
//	   tool_call#0 name=list_quotes args={...}
type LoggingChatModel struct {
	inner einomodel.ToolCallingChatModel
	w     io.Writer

	mu   sync.Mutex
	turn int
}

// NewLoggingChatModel 包一层。w 为 nil 时直接返回 inner(no-op)。
func NewLoggingChatModel(inner einomodel.ToolCallingChatModel, w io.Writer) einomodel.ToolCallingChatModel {
	if w == nil {
		return inner
	}
	return &LoggingChatModel{inner: inner, w: w}
}

func (l *LoggingChatModel) nextTurn() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.turn++
	return l.turn
}

// Generate 同步生成,落入参/出参日志。
func (l *LoggingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	turn := l.nextTurn()
	l.logInput(turn, input)
	start := time.Now()
	out, err := l.inner.Generate(ctx, input, opts...)
	l.logOutput(turn, "generate", time.Since(start), out, err)
	return out, err
}

// Stream 流式生成,落入参/累加出参日志。
//
// 注意:为了不破坏 eino 内部的流式契约,Stream 必须返回一个 StreamReader,
// 直接交给上层(react.Agent)处理流式累加。我们用 schema.StreamReader 的 Copy
// 拿到一份副本,在 goroutine 里累加完整消息再写日志,主 stream 不受影响。
func (l *LoggingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	turn := l.nextTurn()
	l.logInput(turn, input)
	start := time.Now()
	sr, err := l.inner.Stream(ctx, input, opts...)
	if err != nil {
		l.logOutput(turn, "stream", time.Since(start), nil, err)
		return nil, err
	}
	// Copy(2) 复制一份给我们日志用,主 stream 仍然返回给上层。
	copies := sr.Copy(2)
	go l.consumeAndLog(turn, start, copies[1])
	return copies[0], nil
}

// consumeAndLog 把日志副本流读完并累加,然后写一行汇总。
func (l *LoggingChatModel) consumeAndLog(turn int, start time.Time, sr *schema.StreamReader[*schema.Message]) {
	defer sr.Close()
	var (
		merged    schema.Message
		toolCalls []schema.ToolCall
		chunks    int
	)
	for {
		chunk, err := sr.Recv()
		if err != nil {
			break
		}
		if chunk == nil {
			continue
		}
		chunks++
		merged.Content += chunk.Content
		if merged.Role == "" && chunk.Role != "" {
			merged.Role = chunk.Role
		}
		if chunk.Name != "" && merged.Name == "" {
			merged.Name = chunk.Name
		}
		// ToolCalls 流式累加:同 index 的片段拼到一起。eino 内部也这么做。
		for _, tc := range chunk.ToolCalls {
			idx := -1
			if tc.Index != nil {
				idx = *tc.Index
			}
			merged := mergeToolCall(toolCalls, idx, tc)
			toolCalls = merged
		}
	}
	merged.ToolCalls = toolCalls
	l.logOutput(turn, fmt.Sprintf("stream chunks=%d", chunks), time.Since(start), &merged, nil)
}

// mergeToolCall 把同 index 的 stream 片段合并到 toolCalls 切片。
// index 为 -1 时按"非流式"处理,直接 append。
func mergeToolCall(cur []schema.ToolCall, idx int, frag schema.ToolCall) []schema.ToolCall {
	if idx < 0 {
		return append(cur, frag)
	}
	for len(cur) <= idx {
		cur = append(cur, schema.ToolCall{})
	}
	tgt := &cur[idx]
	if frag.ID != "" {
		tgt.ID = frag.ID
	}
	if frag.Type != "" {
		tgt.Type = frag.Type
	}
	if frag.Function.Name != "" {
		tgt.Function.Name = frag.Function.Name
	}
	tgt.Function.Arguments += frag.Function.Arguments
	return cur
}

func (l *LoggingChatModel) logInput(turn int, input []*schema.Message) {
	fmt.Fprintf(l.w, "\n[llm-in ] turn=%d messages=%d\n", turn, len(input))
	for i, m := range input {
		role := string(m.Role)
		content := truncate(m.Content, 600)
		switch m.Role {
		case schema.Tool:
			// tool 角色的 content 是工具返回值;有 Name 字段
			fmt.Fprintf(l.w, "   [%d] tool name=%s content=%s\n", i, m.Name, content)
		case schema.Assistant:
			if len(m.ToolCalls) > 0 {
				args := make([]string, 0, len(m.ToolCalls))
				for _, tc := range m.ToolCalls {
					args = append(args, fmt.Sprintf("%s(%s)", tc.Function.Name, truncate(tc.Function.Arguments, 200)))
				}
				fmt.Fprintf(l.w, "   [%d] %s tool_calls=%v\n", i, role, args)
				if content != "" {
					fmt.Fprintf(l.w, "        content=%s\n", content)
				}
			} else {
				fmt.Fprintf(l.w, "   [%d] %s: %s\n", i, role, content)
			}
		default:
			fmt.Fprintf(l.w, "   [%d] %s: %s\n", i, role, content)
		}
	}
}

func (l *LoggingChatModel) logOutput(turn int, mode string, dur time.Duration, out *schema.Message, err error) {
	if err != nil {
		fmt.Fprintf(l.w, "[llm-err] turn=%d mode=%s dur=%s err=%v\n", turn, mode, dur, err)
		return
	}
	if out == nil {
		fmt.Fprintf(l.w, "[llm-out] turn=%d mode=%s dur=%s (nil out)\n", turn, mode, dur)
		return
	}
	fmt.Fprintf(l.w, "[llm-out] turn=%d mode=%s dur=%s role=%s content_len=%d tool_calls=%d\n",
		turn, mode, dur, out.Role, len(out.Content), len(out.ToolCalls))
	if out.Content != "" {
		fmt.Fprintf(l.w, "          content=%s\n", truncate(out.Content, 800))
	}
	for i, tc := range out.ToolCalls {
		args := tc.Function.Arguments
		// 美化 JSON,便于一眼判断字段是否齐全。
		if compact, ok := tryCompactJSON(args); ok {
			args = compact
		}
		fmt.Fprintf(l.w, "          tool_call#%d id=%s name=%s args=%s\n",
			i, tc.ID, tc.Function.Name, truncate(args, 800))
	}
}

func tryCompactJSON(s string) (string, bool) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s, false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return s, false
	}
	return string(b), true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// WithTools 透传 — 同时把新返回的 model 也包一层日志。
func (l *LoggingChatModel) WithTools(tools []*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	newInner, err := l.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &LoggingChatModel{inner: newInner, w: l.w}, nil
}
