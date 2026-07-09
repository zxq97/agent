package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type LLMCallSnapshot struct {
	TraceID        string         `json:"trace_id"`
	SessionID      string         `json:"session_id,omitempty"`
	UserID         string         `json:"user_id,omitempty"`
	Stage          string         `json:"stage"`
	Model          string         `json:"model"`
	PromptVersion  string         `json:"prompt_version,omitempty"`
	PromptHash     string         `json:"prompt_hash,omitempty"`
	ContextHash    string         `json:"context_hash,omitempty"`
	ToolSchemaHash string         `json:"tool_schema_hash,omitempty"`
	ToolHash       string         `json:"tool_hash,omitempty"`
	SystemPreview  string         `json:"system_preview,omitempty"`
	MessagesHash   string         `json:"messages_hash,omitempty"`
	ToolsHash      string         `json:"tools_hash,omitempty"`
	OutputText     string         `json:"output_text,omitempty"`
	OutputToolName string         `json:"output_tool_name,omitempty"`
	OutputArgs     map[string]any `json:"output_args,omitempty"`
	PromptTokens   int            `json:"prompt_tokens,omitempty"`
	OutputTokens   int            `json:"output_tokens,omitempty"`
	DurationMs     int64          `json:"duration_ms,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

type FileStore struct {
	mu   sync.Mutex
	path string
}

func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

func (s *FileStore) Save(ctx context.Context, snap LLMCallSnapshot) error {
	if snap.CreatedAt.IsZero() {
		snap.CreatedAt = time.Now()
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func (s *FileStore) FindByTraceID(ctx context.Context, traceID string) ([]LLMCallSnapshot, error) {
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []LLMCallSnapshot
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var snap LLMCallSnapshot
		if err := json.Unmarshal(scanner.Bytes(), &snap); err != nil {
			continue
		}
		if snap.TraceID == traceID {
			out = append(out, snap)
		}
	}
	return out, scanner.Err()
}

func DryReport(oldSnap, current LLMCallSnapshot) string {
	var b strings.Builder
	writeDiff := func(name, old, cur string) {
		if old != cur {
			fmt.Fprintf(&b, "%s: %s -> %s\n", name, old, cur)
		}
	}
	writeDiff("prompt_version", oldSnap.PromptVersion, current.PromptVersion)
	writeDiff("prompt_hash", oldSnap.PromptHash, current.PromptHash)
	writeDiff("context_hash", oldSnap.ContextHash, current.ContextHash)
	toolOld := oldSnap.ToolHash
	if toolOld == "" {
		toolOld = oldSnap.ToolSchemaHash
	}
	toolCur := current.ToolHash
	if toolCur == "" {
		toolCur = current.ToolSchemaHash
	}
	writeDiff("tool_hash", toolOld, toolCur)
	writeDiff("model", oldSnap.Model, current.Model)
	if b.Len() == 0 {
		return "no version/hash diff\n"
	}
	return b.String()
}
