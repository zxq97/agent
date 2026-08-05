package rentalcontext

import "time"

type Input struct {
	SourceText string
	Command    *Command
	ReceivedAt time.Time
}
