package session

import "time"

type StateChange struct {
	Field     string
	OldValue  any
	NewValue  any
	Operation string
	CreatedAt time.Time
}
