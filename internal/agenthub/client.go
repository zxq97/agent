package agenthub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const workflowPath = "/v1/workflows/run"

// Client 是规则知识检索的最小接口。
type Client interface {
	Retrieve(ctx context.Context, query string) (string, error)
}

// Config 是 AgentHub 检索 workflow 的连接配置。
type Config struct {
	Host            string
	RetrievalAPIKey string
	Timeout         int
}

type httpClient struct {
	host   string
	key    string
	client *http.Client
}

// New 构造 AgentHub 检索 client。key 未配置时 Retrieve 返回空内容且不报错。
func New(cfg Config) Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10
	}
	return &httpClient{
		host:   strings.TrimRight(cfg.Host, "/"),
		key:    cfg.RetrievalAPIKey,
		client: &http.Client{Timeout: time.Duration(timeout) * time.Second},
	}
}

func (c *httpClient) Retrieve(ctx context.Context, query string) (string, error) {
	if strings.TrimSpace(c.host) == "" || strings.TrimSpace(c.key) == "" {
		return "", nil
	}
	body, err := json.Marshal(map[string]any{
		"inputs": map[string]any{
			"input": query,
		},
		"response_mode": "blocking",
		"user":          "rental_agent",
	})
	if err != nil {
		return "", fmt.Errorf("agenthub: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+workflowPath, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("agenthub: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("agenthub: http do: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("agenthub: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("agenthub: http %d: %s", resp.StatusCode, truncate(respBody, 200))
	}

	var env struct {
		Data struct {
			Status  string `json:"status"`
			Outputs struct {
				Content string `json:"content"`
			} `json:"outputs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &env); err != nil {
		return "", fmt.Errorf("agenthub: unmarshal response: %w", err)
	}
	if env.Data.Status != "succeeded" {
		return "", fmt.Errorf("agenthub: workflow status=%s", env.Data.Status)
	}
	return strings.TrimSpace(env.Data.Outputs.Content), nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
