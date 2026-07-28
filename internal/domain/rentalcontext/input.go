package rentalcontext

import "time"

type ModifyRentalContextInput struct {
	SourceText string
	Command    *ModifyRentalContextCommand
	ReceivedAt time.Time
}
