// Package tyche 提供 tyche MCP server 的轻量 JSON-RPC over HTTP 客户端。
//
// tyche 的 MCP server 是简化实现 —— 纯 POST + JSON-RPC 2.0,没有 SSE/session,
// 直接写一个最小客户端最稳。
//
// 协议:
//
//	POST {endpoint}
//	Header: Authorization: Bearer <phone>
//	Body  : {"jsonrpc":"2.0","id":N,"method":"tools/call","params":{"name":"...","arguments":{...}}}
//	Resp  : {"jsonrpc":"2.0","id":N,"result":{"content":[{"type":"text","text":"..."}],"isError":bool}}
package tyche

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// Client tyche MCP server JSON-RPC 客户端。
type Client struct {
	endpoint string
	phone    string
	hc       *http.Client
	idGen    atomic.Int64
	logOut   io.Writer // 非 nil 时打印请求/响应
}

// New 构造 client。timeoutSec <= 0 取默认 30s。
func New(endpoint, phone string, timeoutSec int, logOut io.Writer) *Client {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	return &Client{
		endpoint: endpoint,
		phone:    phone,
		hc:       &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
		logOut:   logOut,
	}
}

type rpcReq struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolListItem tools/list 返回的单条 tool 元数据。
type ToolListItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []ToolListItem `json:"tools"`
}

// CallToolResult tools/call 返回。
type CallToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ContentItem 内容项。
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ListTools 调 tools/list。
func (c *Client) ListTools(ctx context.Context) ([]ToolListItem, error) {
	var out toolsListResult
	if err := c.callRaw(ctx, "tools/list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// CallTool 调 tools/call。argumentsJSON 是 tool args 的 JSON 字符串。
// 自动补秒:tyche 的 date_time 校验要求 "2006-01-02 15:04:05",LLM 常省秒位,发送前补 ":00"。
func (c *Client) CallTool(ctx context.Context, name, argumentsJSON string) (*CallToolResult, error) {
	raw := argumentsJSON
	if raw == "" {
		raw = "{}"
	}
	raw = fixDateTimeSeconds(raw)

	var out CallToolResult
	err := c.callRaw(ctx, "tools/call", struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}{Name: name, Arguments: json.RawMessage(raw)}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) callRaw(ctx context.Context, method string, params interface{}, out interface{}) error {
	id := c.idGen.Add(1)
	req := rpcReq{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	buf, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal rpc req: %w", err)
	}
	if c.logOut != nil {
		fmt.Fprintf(c.logOut, "\n[tyche] -> %s %s\n[tyche] req: %s\n", method, c.endpoint, truncate(string(buf), 8192))
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("new http req: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.phone != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.phone)
	}

	start := time.Now()
	resp, err := c.hc.Do(httpReq)
	if err != nil {
		if c.logOut != nil {
			fmt.Fprintf(c.logOut, "[tyche] ERR %s after %s: %v\n", method, time.Since(start), err)
		}
		return fmt.Errorf("do rpc req %s: %w", method, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read rpc resp: %w", err)
	}
	if c.logOut != nil {
		fmt.Fprintf(c.logOut, "[tyche] %d in %s, resp: %s\n", resp.StatusCode, time.Since(start), truncate(string(body), 8192))
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("tyche http %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var r rpcResp
	if err := json.Unmarshal(body, &r); err != nil {
		return fmt.Errorf("unmarshal rpc resp: %w body=%s", err, truncate(string(body), 200))
	}
	if r.Error != nil {
		return fmt.Errorf("tyche rpc error code=%d msg=%s", r.Error.Code, r.Error.Message)
	}
	if out != nil && len(r.Result) > 0 {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("unmarshal rpc result: %w result=%s", err, truncate(string(r.Result), 200))
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
