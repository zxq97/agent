package session

import (
	"time"

	"github.com/cloudwego/eino/schema"
)

// Session 对话会话
type Session struct {
	ID        string
	Messages  []*schema.Message
	CreatedAt time.Time
	UpdatedAt time.Time
}
