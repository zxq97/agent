package agenthub

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/pkg/http"
)

const defaultRecallPath = "/v1/vehicle/recall"

type httpClient struct {
	endpoint string
	path     string
	apiKey   string
	hc       *httpclient.Client
}

type responseEnvelope struct {
	Code       int               `json:"code"`
	Message    string            `json:"message"`
	Data       *RecallResponse   `json:"data"`
	Candidates []RecallCandidate `json:"candidates"`
}

func NewHTTPClient(cfg *HTTPConfig) Client {
	if cfg == nil {
		cfg = &HTTPConfig{}
	}
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		path = defaultRecallPath
	}
	timeout := cfg.TimeoutSec
	if timeout <= 0 {
		timeout = 3
	}
	return &httpClient{
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		path:     "/" + strings.TrimLeft(path, "/"),
		apiKey:   cfg.APIKey,
		hc:       httpclient.NewClient(&httpclient.Config{TimeoutSec: timeout}),
	}
}

func (c *httpClient) RecallVehicles(ctx context.Context, input *RecallRequest) (*RecallResponse, error) {
	if input == nil {
		return nil, errors.New("agenthub recall_vehicles: request is required")
	}
	if strings.TrimSpace(input.Query) == "" || strings.TrimSpace(input.EntityType) == "" {
		return nil, errors.New("agenthub recall_vehicles: query and entity_type are required")
	}
	if input.TopK <= 0 || input.TopK > 10 {
		input.TopK = 8
	}
	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	responseBody, err := c.hc.PostJSON(ctx, "agenthub recall_vehicles", c.endpoint+c.path, c.apiKey, body)
	if err != nil {
		return nil, err
	}
	var envelope responseEnvelope
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return nil, err
	}
	if envelope.Code != 0 {
		return nil, errors.Errorf("agenthub recall_vehicles: code=%d message=%s", envelope.Code, envelope.Message)
	}
	result := envelope.Data
	if result == nil {
		result = &RecallResponse{Candidates: envelope.Candidates}
	}
	for _, candidate := range result.Candidates {
		if strings.TrimSpace(candidate.CandidateID) == "" ||
			strings.TrimSpace(candidate.Name) == "" ||
			strings.TrimSpace(candidate.EntityType) == "" {
			return nil, errors.New("agenthub recall_vehicles: response contains incomplete candidate")
		}
	}
	return result, nil
}
