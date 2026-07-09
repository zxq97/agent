// Package httphandler 提供 HTTP + SSE 服务层。
// 包名用 httphandler 而非 http,避免与 net/http 同名(遵守项目不加 import 别名的规范)。
package httphandler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// sseEmitter 实现 agent.Emitter 接口,把流式增量写成 SSE 帧。
type sseEmitter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
	legacy  bool
}

// newSSEEmitter 创建 SSE emitter 并设置必要的响应头。
// 返回 nil 如果 ResponseWriter 不支持 Flush。
func newSSEEmitter(w http.ResponseWriter) *sseEmitter {
	return newSSEEmitterWithVersion(w, false)
}

func newSSEEmitterWithVersion(w http.ResponseWriter, legacy bool) *sseEmitter {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &sseEmitter{w: w, flusher: flusher, legacy: legacy}
}

// Text 推一段文本增量(agent.Emitter 接口)。
func (e *sseEmitter) Text(delta string) {
	if e.legacy {
		e.send("text", map[string]string{"content": delta})
		return
	}
	e.send("text", map[string]string{"content": delta, "subtype": "final"})
}

// Event 推一条调度/进度事件(agent.Emitter 接口)。
func (e *sseEmitter) Event(name, detail string) {
	if e.legacy {
		e.send("event", map[string]string{"name": name, "detail": detail})
		return
	}
	var payload json.RawMessage
	if json.Valid([]byte(detail)) {
		payload = json.RawMessage(detail)
	} else {
		data, _ := json.Marshal(map[string]string{"detail": detail})
		payload = data
	}
	e.send(name, payload)
}

// Done 推流结束事件。
func (e *sseEmitter) Done() {
	e.send("done", map[string]string{})
}

// Error 推错误事件。
func (e *sseEmitter) Error(msg string) {
	e.send("error", map[string]string{"message": msg})
}

func (e *sseEmitter) send(eventType string, payload any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var data []byte
	if raw, ok := payload.(json.RawMessage); ok {
		data = raw
	} else {
		data, _ = json.Marshal(payload)
	}
	fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", eventType, data)
	e.flusher.Flush()
}
