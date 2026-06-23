// Package http 提供 P4 阶段的 HTTP + SSE 服务实现。
//
// 设计要点:
//   - 单 endpoint POST /agent/chat 支持多轮对话,每轮一个 SSE 流
//   - SSE event 类型化: message / event / done / error
//   - 反向代理透传:写 X-Accel-Buffering: no 防止 nginx 缓冲整个流
//   - 长连接超时由 HTTPConf.WriteTimeout 控制(默认 600s)
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// sseEvent 是 SSE 流上推送的单条事件。
//
// Type 取值:
//   - "message": 直接展示给用户的文本内容(对应最终 assistant 答复)
//   - "event":   后端调度细节(agent transfer / tool_call / tool_result),供前端展示进度条用
//   - "done":    本轮回复结束
//   - "error":   出错(只含 user_msg,debug 字段进日志不上 SSE)
type sseEvent struct {
	Type    string         `json:"type"`
	Content string         `json:"content,omitempty"`
	Agent   string         `json:"agent,omitempty"`
	Detail  string         `json:"detail,omitempty"`
	Extra   map[string]any `json:"extra,omitempty"`
}

// sseWriter 包装一次 SSE 响应。
type sseWriter struct {
	w  http.ResponseWriter
	fl http.Flusher
}

// newSSEWriter 准备 SSE 响应头并返回一个 writer。如果底层 ResponseWriter
// 不支持 Flusher(理论上 net/http 的都支持),返回错误让 handler 早报。
func newSSEWriter(w http.ResponseWriter) (*sseWriter, error) {
	fl, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing (need http.Flusher)")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// 关键:让 nginx / 公司网关不缓冲整段响应,否则用户看不到流式效果
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	return &sseWriter{w: w, fl: fl}, nil
}

// send 序列化并推一条事件,推完立即 flush。
func (s *sseWriter) send(ev sseEvent) error {
	buf, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal sse event: %w", err)
	}
	// SSE 格式: "data: <json>\n\n"
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", buf); err != nil {
		return fmt.Errorf("write sse: %w", err)
	}
	s.fl.Flush()
	return nil
}

// sendMessage 简便封装:推一条 message 类型(用户可见文本)。
func (s *sseWriter) sendMessage(content string) error {
	return s.send(sseEvent{Type: "message", Content: content})
}

// sendEvent 推一条 event(调度/工具事件,前端可选择展示)。
func (s *sseWriter) sendEvent(agentName, detail string, extra map[string]any) error {
	return s.send(sseEvent{Type: "event", Agent: agentName, Detail: detail, Extra: extra})
}

// sendError 推一条 error(只含用户友好提示,debug 详情进日志)。
func (s *sseWriter) sendError(userMsg string) error {
	return s.send(sseEvent{Type: "error", Content: userMsg})
}

// sendDone 推结束事件,前端据此关闭连接。
func (s *sseWriter) sendDone() error {
	return s.send(sseEvent{Type: "done"})
}
