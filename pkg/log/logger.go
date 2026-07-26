// Package log provides process-wide structured logging with optional
// context-based trace correlation.
package log

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type contextKey string

const (
	traceIDKey contextKey = "trace_id"
	loggerKey  contextKey = "logger"
)

var defaultLogger struct {
	sync.RWMutex
	logger Logger
}

// Entry is one structured log record.
type Entry struct {
	Timestamp  time.Time `json:"timestamp"`
	TraceID    string    `json:"trace_id,omitempty"`
	Component  string    `json:"component"`
	Operation  string    `json:"operation"`
	Request    any       `json:"request,omitempty"`
	Response   any       `json:"response,omitempty"`
	DurationMS int64     `json:"duration_ms"`
	Error      string    `json:"error,omitempty"`
}

// Logger writes structured entries.
type Logger interface {
	Log(context.Context, Entry)
}

// JSONLogger writes one JSON object per line.
type JSONLogger struct {
	writer io.Writer
	mu     sync.Mutex
}

// NewJSONLogger constructs a JSON-lines logger.
func NewJSONLogger(writer io.Writer) *JSONLogger {
	return &JSONLogger{writer: writer}
}

// NewDailyFileLogger creates an append-only JSON-lines log file named
// <prefix>-YYYY-MM-DD.log in dir. The caller closes the returned file.
func NewDailyFileLogger(dir, prefix string) (*JSONLogger, io.Closer, error) {
	if prefix == "" {
		prefix = "agent"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, prefix+"-"+time.Now().Format("2006-01-02")+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return NewJSONLogger(file), file, nil
}

// Init configures the process-wide logger. Applications call it once during
// startup; passing nil disables logging, which is useful for tests.
func Init(logger Logger) {
	defaultLogger.Lock()
	defer defaultLogger.Unlock()
	defaultLogger.logger = logger
}

// WithTraceID attaches a trace ID to a request context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// WithLogger attaches the request-scoped logger used by this context and all
// downstream service calls. A nil logger leaves the context unchanged.
func WithLogger(ctx context.Context, logger Logger) context.Context {
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerKey, logger)
}

// TraceID returns the optional trace ID carried by ctx.
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if traceID, ok := ctx.Value(traceIDKey).(string); ok && traceID != "" {
		return traceID
	}
	return ""
}

// Write emits entry through the process-wide logger. ctx is used only to read
// an optional trace ID; callers do not attach loggers to request contexts.
func Write(ctx context.Context, entry Entry) {
	if ctx != nil {
		if logger, ok := ctx.Value(loggerKey).(Logger); ok && logger != nil {
			logger.Log(ctx, entry)
			return
		}
	}
	defaultLogger.RLock()
	logger := defaultLogger.logger
	defaultLogger.RUnlock()
	if logger == nil {
		return
	}
	logger.Log(ctx, entry)
}

// Log implements Logger.
func (l *JSONLogger) Log(ctx context.Context, entry Entry) {
	if l == nil || l.writer == nil {
		return
	}
	entry.Timestamp = time.Now()
	entry.TraceID = TraceID(ctx)
	encoded, err := json.Marshal(entry)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.writer.Write(append(encoded, '\n'))
}
