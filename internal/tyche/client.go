// Package tyche 提供 tyche MCP server 的轻量 JSON-RPC over HTTP 客户端。
//
// 为什么不用官方 mcp/go-sdk:tyche 的 MCP server(controller/mcp/controller.go)
// 是简化实现 —— 纯 POST + JSON-RPC 2.0,没有 Streamable HTTP / SSE / session,
// 而 modelcontextprotocol/go-sdk 的 StreamableClientTransport 强依赖 SSE event-stream。
// 直接写一个最小客户端最稳。
//
// 协议:
//   - POST /car/rental/inner/mcp
//   - Header: Authorization: Bearer <phone>
//   - Body  : {"jsonrpc":"2.0","id":N,"method":"tools/call","params":{"name":"...","arguments":{...}}}
//   - Resp  : {"jsonrpc":"2.0","id":N,"result":{"content":[{"type":"text","text":"..."}],"isError":bool}}
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
	endpoint string // 完整 URL,如 http://10.78.133.4:8877/car/rental/inner/mcp
	phone    string // Authorization: Bearer <phone>
	hc       *http.Client
	idGen    atomic.Int64
	logOut   io.Writer // 非 nil 时打印请求/响应,便于排障
}

// New 构造 client。timeoutSec <=0 时取默认 30s。
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

// rpcReq / rpcResp 对齐 tyche/controller/mcp/controller.go 的 rpcRequest/rpcResponse。
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

// ToolListItem 对齐 tyche tools/list 返回的单条 tool 元数据。
type ToolListItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []ToolListItem `json:"tools"`
}

// CallToolResult 对齐 tyche toolCallResult。
type CallToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Initialize 走一遍标准 MCP handshake(可选,tyche 接受不带 init 直接 tools/call,
// 但保留以便发现协议升级)。
func (c *Client) Initialize(ctx context.Context) error {
	var raw json.RawMessage
	return c.callRaw(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "rental-agent", "version": "0.1.0"},
	}, &raw)
}

// ListTools 调 tools/list。
func (c *Client) ListTools(ctx context.Context) ([]ToolListItem, error) {
	var out toolsListResult
	if err := c.callRaw(ctx, "tools/list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// CallTool 调 tools/call。argumentsJSON 是 tool args 的 JSON 字符串(eino 给的就是这个形式)。
func (c *Client) CallTool(ctx context.Context, name, argumentsJSON string) (*CallToolResult, error) {
	var args json.RawMessage
	if argumentsJSON == "" {
		args = json.RawMessage("{}")
	} else {
		args = json.RawMessage(argumentsJSON)
	}
	var out CallToolResult
	if err := c.callRaw(ctx, "tools/call", struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}{Name: name, Arguments: args}, &out); err != nil {
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
		fmt.Fprintf(c.logOut, "\n[tyche] -> %s %s\n[tyche] req: %s\n", method, c.endpoint, truncate(string(buf), 1024))
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
		fmt.Fprintf(c.logOut, "[tyche] %d in %s, resp: %s\n", resp.StatusCode, time.Since(start), truncate(string(body), 1024))
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
