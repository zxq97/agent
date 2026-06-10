package mcp

import (
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 请求/响应类型

const (
	jsonRPCVersion = "2.0"
	methodInitialize = "initialize"
	methodPing       = "ping"
	methodToolsList  = "tools/list"
	methodToolsCall  = "tools/call"
)

// JSONRPCRequest MCP JSON-RPC 2.0 请求
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse MCP JSON-RPC 2.0 响应
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError JSON-RPC 错误
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// InitializeParams MCP initialize 请求参数
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
}

// ClientInfo 客户端信息
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult MCP initialize 响应
type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      ServerInfo     `json:"serverInfo"`
}

// ServerInfo 服务端信息
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolsListParams tools/list 请求参数
type ToolsListParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// ToolsListResult tools/list 响应
type ToolsListResult struct {
	Tools    []MCPToolDefinition `json:"tools"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

// MCPToolDefinition MCP 工具定义
type MCPToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolsCallParams tools/call 请求参数
type ToolsCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// ToolsCallResult tools/call 响应
type ToolsCallResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent 工具调用结果内容
type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
