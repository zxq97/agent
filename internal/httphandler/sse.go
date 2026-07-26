package httphandler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type sseEmitter struct {
	writer  http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
}

func newSSEEmitter(writer http.ResponseWriter) *sseEmitter {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		return nil
	}
	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &sseEmitter{writer: writer, flusher: flusher}
}

func (e *sseEmitter) send(event string, value any) {
	if e == nil {
		return
	}
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, _ = fmt.Fprintf(e.writer, "event: %s\ndata: %s\n\n", event, data)
	e.flusher.Flush()
}
