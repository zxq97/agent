// Package domain defines contracts shared by all routed domain handlers.
package domain

import "github.com/pkg/errors"

// ErrDomainMismatch means the routed input does not belong to the handler's
// domain. It is a normal routing outcome rather than a handler failure.
var ErrDomainMismatch = errors.New("input does not belong to routed domain")
