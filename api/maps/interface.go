// Package maps provides the map API used to update rental pickup and drop-off
// locations.
package maps

import (
	"context"
)

// Client is the transport-independent map API used by the AI guide.
type Client interface {
	Search(context.Context, *SearchRequest) (*SearchResponse, error)
}

// HTTPConfig configures the map HTTP implementation.
type HTTPConfig struct {
	Endpoint       string
	ProductID      string
	AccKey         string
	AppVersion     string
	Platform       string
	AppID          string
	MapType        string
	CoordinateType string
	RequesterType  string
	Lang           string
	CallerID       string
	PlaceType      string
}
