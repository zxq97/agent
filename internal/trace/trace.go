// Package trace 提供请求级 trace_id 生成、ctx 注入/提取、日志收集。
// 每次 /api/chat 请求生成一个 trace_id，贯穿 handler → pipeline → decide → capability → tool → LLM。
package trace

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type ctxKey struct{}

// NewID 生成 8 字符 hex trace_id。
func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// WithTrace 把 trace_id 注入 ctx。
func WithTrace(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, traceID)
}

// FromCtx 从 ctx 取 trace_id，空则返回 "-"。
func FromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok && v != "" {
		return v
	}
	return "-"
}

// Entry 一条 trace 日志条目。
type Entry struct {
	Time string `json:"ts"`
	Tag  string `json:"tag"`
	Msg  string `json:"msg"`
}

// Log 请求级日志收集器，实现 io.Writer。
// 每次 Write 自动解析 [tag] 前缀、记录时间戳、收集 Entry、同时写 stderr。
type Log struct {
	TraceID string
	start   time.Time
	mu      sync.Mutex
	entries []Entry
	stderr  io.Writer
}

// NewLog 创建收集器。stderr 为 nil 时不写服务端日志。
func NewLog(traceID string, stderr io.Writer) *Log {
	return &Log{
		TraceID: traceID,
		start:   time.Now(),
		entries: make([]Entry, 0, 16),
		stderr:  stderr,
	}
}

// Write 实现 io.Writer。解析 "[tag] msg\n" 格式，收集 Entry + 写 stderr。
func (l *Log) Write(p []byte) (int, error) {
	raw := strings.TrimSpace(string(p))
	if raw == "" {
		return len(p), nil
	}

	// 解析多行（一次 Write 可能包含多条）
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		tag, msg := parseLine(line)
		ts := time.Since(l.start).Milliseconds()
		tsStr := fmt.Sprintf("+%dms", ts)

		l.mu.Lock()
		l.entries = append(l.entries, Entry{Time: tsStr, Tag: tag, Msg: msg})
		l.mu.Unlock()

		// 写 stderr（带 trace_id 前缀）
		if l.stderr != nil {
			fmt.Fprintf(l.stderr, "[trace=%s] %s\n", l.TraceID, line)
		}
	}
	return len(p), nil
}

// Entries 返回收集到的所有日志条目的拷贝。
func (l *Log) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, len(l.entries))
	copy(out, l.entries)
	return out
}

// parseLine 从 "[tag] msg" 格式解析 tag 和 msg。
// 无 [tag] 前缀时 tag 为 "info"。
func parseLine(line string) (tag, msg string) {
	if !strings.HasPrefix(line, "[") {
		return "info", line
	}
	end := strings.Index(line, "]")
	if end < 0 {
		return "info", line
	}
	tag = line[1:end]
	msg = strings.TrimSpace(line[end+1:])
	return tag, msg
}
