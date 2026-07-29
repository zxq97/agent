// Package agenthub calls the optional long-tail vehicle recall service.
package agenthub

import "context"

type Client interface {
	RecallVehicles(context.Context, *RecallRequest) (*RecallResponse, error)
}

type HTTPConfig struct {
	Endpoint   string
	Path       string
	APIKey     string
	TimeoutSec int
}
