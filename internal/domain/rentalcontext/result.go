package rentalcontext

import (
	"time"

	"github.com/zxq97/agent/api/maps"
	"github.com/zxq97/agent/internal/session"
)

type ResultStatus string

const (
	ResultSuccess     ResultStatus = "success"
	ResultWaitingUser ResultStatus = "waiting_user"
	ResultDeferred    ResultStatus = "deferred"
	ResultRejected    ResultStatus = "rejected"
)

type ModifiedField string

const (
	ModifiedLocation   ModifiedField = "location"
	ModifiedPickupTime ModifiedField = "pickup_time"
	ModifiedReturnTime ModifiedField = "return_time"
)

type Result struct {
	Status          ResultStatus
	Location        *maps.Candidate
	PickupTime      *time.Time
	ReturnTime      *time.Time
	ModifiedFields  []ModifiedField
	InteractionID   string
	LocationOptions []maps.Candidate
	Message         string
	Deltas          []session.StateDelta
}
