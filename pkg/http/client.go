// Package httpclient provides shared HTTP transport primitives for service clients.
package httpclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/pkg/log"
)

// Client is a reusable JSON-over-HTTP transport client.
type Client struct {
	hc *http.Client
}

type streamBody struct {
	io.ReadCloser
	onClose func()
	once    sync.Once
}

func (b *streamBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.onClose)
	return err
}

// Config configures the shared HTTP transport.
type Config struct {
	TimeoutSec int
}

// NewClient constructs an HTTP client. A non-positive timeout uses 30 seconds.
func NewClient(cfg *Config) *Client {
	if cfg == nil {
		cfg = &Config{}
	}
	timeoutSec := cfg.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	return &Client{
		hc: &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
	}
}

// PostJSON sends a JSON request. bearerToken is optional and is never logged
// by this transport package. It returns only the response body and an error.
func (c *Client) PostJSON(ctx context.Context, operation, url, bearerToken string, body []byte) (responseBody []byte, err error) {
	start := time.Now()
	var statusCode int
	var responseForLog string
	defer func() {
		entry := log.Entry{
			Component:  "http",
			Operation:  operation,
			Request:    map[string]string{"method": http.MethodPost, "url": url, "body": string(body)},
			Response:   map[string]any{"status_code": statusCode, "body": responseForLog},
			DurationMS: time.Since(start).Milliseconds(),
		}
		if err != nil {
			entry.Error = err.Error()
		}
		log.Write(ctx, entry)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	statusCode = resp.StatusCode
	responseBody, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	responseForLog = string(responseBody)
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("http %s: unexpected status %d: %s", operation, resp.StatusCode, truncateResponse(responseBody, 1024))
	}
	return responseBody, nil
}

// PostJSONStream sends a JSON request and returns its successful response body
// for streaming consumption. Closing the body emits the request log.
func (c *Client) PostJSONStream(ctx context.Context, operation, requestURL, bearerToken string, body []byte) (io.ReadCloser, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		return nil, errors.Errorf("http %s: unexpected status %d: %s", operation, resp.StatusCode, truncateResponse(raw, 1024))
	}
	return &streamBody{ReadCloser: resp.Body, onClose: func() {
		log.Write(ctx, log.Entry{Component: "http", Operation: operation, Request: map[string]string{"method": http.MethodPost, "url": requestURL, "body": string(body)}, Response: map[string]any{"status_code": resp.StatusCode}, DurationMS: time.Since(start).Milliseconds()})
	}}, nil
}

func truncateResponse(body []byte, limit int) string {
	if len(body) <= limit {
		return string(body)
	}
	return string(body[:limit]) + "..."
}

// Get sends an HTTP GET request and returns its response body.
func (c *Client) Get(ctx context.Context, operation, requestURL string) (responseBody []byte, err error) {
	start := time.Now()
	var statusCode int
	var responseForLog string
	defer func() {
		entry := log.Entry{Component: "http", Operation: operation, Request: map[string]string{"method": http.MethodGet, "url": redactURL(requestURL)}, Response: map[string]any{"status_code": statusCode, "body": responseForLog}, DurationMS: time.Since(start).Milliseconds()}
		if err != nil {
			entry.Error = err.Error()
		}
		log.Write(ctx, entry)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	statusCode = resp.StatusCode
	responseBody, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	responseForLog = string(responseBody)
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("http %s: unexpected status %d", operation, resp.StatusCode)
	}
	return responseBody, nil
}

func redactURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	for _, name := range []string{"acc_key", "token"} {
		if query.Has(name) {
			query.Set(name, "***")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
