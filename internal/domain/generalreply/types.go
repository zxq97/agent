// Package generalreply produces read-only conversational replies for text that
// no state-changing domain consumed.
package generalreply

import (
	"context"

	"github.com/zxq97/agent/internal/session"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Input struct {
	SourceText     string    `json:"source_text"`
	RecentMessages []Message `json:"recent_messages"`
}

type Result struct {
	Message string
}

type Handler interface {
	Handle(context.Context, *session.AgentSession, *Input) (*Result, error)
}
