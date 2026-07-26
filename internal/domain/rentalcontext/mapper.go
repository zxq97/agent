package rentalcontext

import (
	"strings"
	"time"

	"github.com/pkg/errors"
)

func mapExtractResult(result *RentalContextExtractResult) (*ModifyRentalContextCommand, []AmbiguousField, error) {
	if result == nil {
		return nil, nil, errors.New("modify rental context: extraction result is required")
	}
	if !result.DomainMatched {
		return nil, nil, ErrDomainMismatch
	}
	command := &ModifyRentalContextCommand{LocationQuery: strings.TrimSpace(result.LocationQuery)}
	var ambiguous []AmbiguousField
	for _, item := range []struct {
		name   string
		value  ExtractedTime
		target **time.Time
	}{{"pickup_time", result.PickupTime, &command.PickupTime}, {"return_time", result.ReturnTime, &command.ReturnTime}} {
		switch item.value.Status {
		case ResolutionAbsent:
		case ResolutionAmbiguous:
			ambiguous = append(ambiguous, AmbiguousField{Field: item.name, Raw: item.value.Raw})
		case ResolutionResolved:
			if item.value.Value == nil {
				return nil, nil, errors.New("modify rental context: resolved time is missing value")
			}
			parsed, err := time.Parse(time.RFC3339, *item.value.Value)
			if err != nil {
				return nil, nil, err
			}
			*item.target = &parsed
		default:
			return nil, nil, errors.New("modify rental context: invalid time resolution status")
		}
	}
	return command, ambiguous, nil
}
