package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/zxq97/agent/internal/config"
)

// MCPClient tyche MCP Server 的 HTTP JSON-RPC 2.0 客户端
type MCPClient struct {
	baseURL    string
	authToken  string
	phone      string
	httpClient *http.Client
	tools      []MCPToolDefinition
	idCounter  atomic.Int64
	initialized bool
}

// NewMCPClient 创建 MCP Client
func NewMCPClient(cfg config.MCPConfig) *MCPClient {
	return &MCPClient{
		baseURL:   cfg.BaseURL,
		authToken: cfg.Token,
		phone:     cfg.Phone,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Initialize 调用 MCP initialize，获取服务端能力
func (c *MCPClient) Initialize(ctx context.Context) error {
	params := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]any{},
		ClientInfo: ClientInfo{
			Name:    "rental-agent",
			Version: "1.0.0",
		},
	}

	result := &InitializeResult{}
	if err := c.call(ctx, methodInitialize, params, result); err != nil {
		return fmt.Errorf("MCP initialize 失败: %w", err)
	}

	c.initialized = true
	zap.L().Info("MCP 初始化成功",
		zap.String("server", result.ServerInfo.Name),
		zap.String("version", result.ServerInfo.Version),
	)

	// 初始化后自动拉取工具列表
	if err := c.fetchTools(ctx); err != nil {
		return fmt.Errorf("获取 MCP 工具列表失败: %w", err)
	}

	return nil
}

// ListTools 返回缓存的工具列表
func (c *MCPClient) ListTools() []MCPToolDefinition {
	return c.tools
}

// CallTool 调用 MCP 工具
func (c *MCPClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolsCallResult, error) {
	if !c.initialized {
		return nil, fmt.Errorf("MCP 客户端未初始化，请先调用 Initialize")
	}

	argsJSON, _ := json.Marshal(arguments)
	zap.L().Info("[MCP] 请求",
		zap.String("tool", name),
		zap.String("arguments", string(argsJSON)),
	)

	params := ToolsCallParams{
		Name:      name,
		Arguments: arguments,
	}

	result := &ToolsCallResult{}
	if err := c.call(ctx, methodToolsCall, params, result); err != nil {
		zap.L().Error("[MCP] 调用失败",
			zap.String("tool", name),
			zap.Error(err),
		)
		return nil, fmt.Errorf("调用 MCP 工具 %s 失败: %w", name, err)
	}

	if result.IsError {
		text := extractText(result.Content)
		zap.L().Warn("[MCP] 工具返回错误",
			zap.String("tool", name),
			zap.String("error", text),
		)
		return nil, fmt.Errorf("MCP 工具 %s 返回错误: %s", name, text)
	}

	respJSON, _ := json.Marshal(result)
	zap.L().Info("[MCP] 响应",
		zap.String("tool", name),
		zap.String("response_preview", truncateJSON(string(respJSON), 500)),
	)

	return result, nil
}

// fetchTools 获取并缓存工具列表
func (c *MCPClient) fetchTools(ctx context.Context) error {
	result := &ToolsListResult{}
	if err := c.call(ctx, methodToolsList, ToolsListParams{}, result); err != nil {
		return err
	}

	c.tools = result.Tools
	zap.L().Info("MCP 工具列表已获取", zap.Int("count", len(c.tools)))
	for _, t := range c.tools {
		zap.L().Debug("MCP 工具", zap.String("name", t.Name), zap.String("desc", t.Description))
	}

	return nil
}

// call 执行一次 JSON-RPC 调用
func (c *MCPClient) call(ctx context.Context, method string, params any, result any) error {
	id := c.idCounter.Add(1)

	var paramsJSON json.RawMessage
	if params != nil {
		p, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("序列化参数失败: %w", err)
		}
		paramsJSON = p
	}

	req := JSONRPCRequest{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Method:  method,
		Params:  paramsJSON,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	if c.phone != "" {
		httpReq.Header.Set("X-Phone", c.phone)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP 状态码 %d: %s", resp.StatusCode, string(respBody))
	}

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	if rpcResp.Error != nil {
		return rpcResp.Error
	}

	if result != nil && rpcResp.Result != nil {
		if err := json.Unmarshal(rpcResp.Result, result); err != nil {
			return fmt.Errorf("解析结果失败: %w", err)
		}
	}

	return nil
}

// extractText 从 ToolContent 列表中提取文本
func extractText(contents []ToolContent) string {
	var texts []string
	for _, c := range contents {
		if c.Type == "text" && c.Text != "" {
			texts = append(texts, c.Text)
		}
	}
	if len(texts) == 0 {
		return ""
	}
	return texts[0]
}

// truncateJSON 截断字符串用于日志
func truncateJSON(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
