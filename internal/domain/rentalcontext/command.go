package rentalcontext

import (
	"strings"
	"time"

	"github.com/pkg/errors"
)

type Command struct {
	LocationQuery string
	LocationID    string
	PickupTime    *time.Time
	ReturnTime    *time.Time
	InteractionID string
}

func ValidateExtractedCommand(command *Command) error {
	if command == nil {
		return errors.New("modify rental context: command is required")
	}
	command.LocationQuery = strings.TrimSpace(command.LocationQuery)
	if !command.hasModification() {
		return errors.New("modify rental context: command has no modification")
	}
	if len(command.LocationQuery) > 120 {
		return errors.New("modify rental context: command field exceeds maximum length")
	}
	return nil
}

func (c *Command) hasModification() bool {
	return c != nil && (c.LocationQuery != "" || c.LocationID != "" || c.PickupTime != nil || c.ReturnTime != nil)
}
