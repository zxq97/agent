// Package guide calls the rental-guide vehicle search service.
package guide

import (
	"context"
)

// Client is the transport-independent rental-guide API.
type Client interface {
	SearchQuotes(context.Context, *SearchRequest) (*SearchResponse, error)
}

// HTTPConfig configures the rental-guide HTTP implementation.
type HTTPConfig struct {
	Endpoint   string
	Phone      string
	TimeoutSec int
}
