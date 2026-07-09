package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zxq97/rental-agent/internal/orchestration"
)

// FeedbackStore 保存用户对本轮回答的正/负反馈。
type FeedbackStore interface {
	Save(ctx context.Context, snap FeedbackSnapshot) error
}

type FeedbackSnapshot struct {
	TraceID   string                       `json:"trace_id,omitempty"`
	UserID    string                       `json:"user_id"`
	SessionID string                       `json:"session_id"`
	Rating    string                       `json:"rating"`
	Message   string                       `json:"message,omitempty"`
	History   []orchestration.HistoryEntry `json:"history,omitempty"`
	CreatedAt time.Time                    `json:"created_at"`
}

type FileFeedbackStore struct {
	mu   sync.Mutex
	path string
}

func NewFileFeedbackStore(path string) *FileFeedbackStore {
	return &FileFeedbackStore{path: path}
}

func (s *FileFeedbackStore) Save(ctx context.Context, snap FeedbackSnapshot) error {
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
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}
