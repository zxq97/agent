package guide

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/pkg/http"
)

const storeListPath = "/car/rental/guide/store/list/agent"

type httpClient struct {
	endpoint string
	phone    string
	hc       *httpclient.Client
}

type responseEnvelope struct {
	Errno  int            `json:"errno"`
	Errmsg string         `json:"errmsg"`
	Data   SearchResponse `json:"data"`
}

// NewHTTPClient creates a rental-guide HTTP client.
func NewHTTPClient(cfg *HTTPConfig) Client {
	if cfg == nil {
		cfg = &HTTPConfig{}
	}
	timeout := cfg.TimeoutSec
	if timeout <= 0 {
		timeout = 30
	}
	return &httpClient{
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		phone:    cfg.Phone,
		hc: httpclient.NewClient(&httpclient.Config{
			TimeoutSec: timeout,
		}),
	}
}

func (c *httpClient) SearchQuotes(ctx context.Context, input *SearchRequest) (*SearchResponse, error) {
	if input == nil {
		return nil, errors.New("guide search_quotes: request is required")
	}
	if input.PageSize < 6 {
		input.PageSize = 6
	}
	if input.Page < 1 {
		input.Page = 1
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	responseBody, err := c.hc.PostJSON(ctx, "guide search_quotes", c.endpoint+storeListPath, c.phone, body)
	if err != nil {
		return nil, err
	}

	var result responseEnvelope
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, err
	}
	if result.Errno != 0 {
		return nil, errors.Errorf("guide search_quotes: errno=%d errmsg=%s", result.Errno, result.Errmsg)
	}
	return &result.Data, nil
}
