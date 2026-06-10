package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/zxq97/agent/internal/config"
)

// MCPToolProvider 实现 ToolProvider 接口，从 tyche MCP 加载 Tool
type MCPToolProvider struct {
	client *MCPClient
}

// NewMCPToolProvider 创建 MCP ToolProvider
func NewMCPToolProvider(cfg config.MCPConfig) *MCPToolProvider {
	return &MCPToolProvider{
		client: NewMCPClient(cfg),
	}
}

// Client 返回内部 MCPClient，供语义化 Tool 使用
func (p *MCPToolProvider) Client() *MCPClient {
	return p.client
}

// Name 返回 ToolProvider 标识
func (p *MCPToolProvider) Name() string {
	return "mcp"
}

// LoadTools 从 MCP Server 加载所有工具并转换为 eino BaseTool
func (p *MCPToolProvider) LoadTools(ctx context.Context) ([]tool.BaseTool, error) {
	if err := p.client.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("MCP 初始化失败: %w", err)
	}

	defs := p.client.ListTools()
	tools := make([]tool.BaseTool, 0, len(defs))

	for _, def := range defs {
		t, err := newMCPTool(p.client, def)
		if err != nil {
			return nil, fmt.Errorf("封装 MCP 工具 %s 失败: %w", def.Name, err)
		}
		tools = append(tools, t)
	}

	return tools, nil
}

// mcpTool 将单个 MCP 工具封装为 eino InvokableTool
type mcpTool struct {
	client   *MCPClient
	def      MCPToolDefinition
	toolInfo *schema.ToolInfo
}

// newMCPTool 创建 eino Tool 封装
func newMCPTool(client *MCPClient, def MCPToolDefinition) (*mcpTool, error) {
	paramsOneOf, err := parseInputSchema(def.InputSchema)
	if err != nil {
		return nil, fmt.Errorf("解析 inputSchema 失败: %w", err)
	}

	toolInfo := &schema.ToolInfo{
		Name:        def.Name,
		Desc:        def.Description,
		ParamsOneOf: paramsOneOf,
	}

	return &mcpTool{
		client:   client,
		def:      def,
		toolInfo: toolInfo,
	}, nil
}

// Info 返回工具元信息
func (t *mcpTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.toolInfo, nil
}

// InvokableRun 执行工具调用
func (t *mcpTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args map[string]any
	if argumentsInJSON != "" {
		if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
			return "", fmt.Errorf("解析工具参数失败: %w", err)
		}
	}

	result, err := t.client.CallTool(ctx, t.def.Name, args)
	if err != nil {
		return "", fmt.Errorf("调用 MCP 工具失败: %w", err)
	}

	text := extractText(result.Content)
	if text == "" {
		b, _ := json.Marshal(result)
		text = string(b)
	}

	return text, nil
}

// parseInputSchema 将 MCP 工具的 JSON Schema 转换为 eino ParamsOneOf
func parseInputSchema(raw json.RawMessage) (*schema.ParamsOneOf, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	var schemaMap map[string]any
	if err := json.Unmarshal(raw, &schemaMap); err != nil {
		return nil, fmt.Errorf("解析 JSON Schema 失败: %w", err)
	}

	params := make(map[string]*schema.ParameterInfo)

	props, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		return schema.NewParamsOneOfByParams(params), nil
	}

	required := make(map[string]bool)
	if req, ok := schemaMap["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				required[s] = true
			}
		}
	}

	for name, prop := range props {
		propMap, ok := prop.(map[string]any)
		if !ok {
			continue
		}

		pi := &schema.ParameterInfo{
			Required: required[name],
		}

		if t, ok := propMap["type"].(string); ok {
			pi.Type = convertJSONSchemaType(t)
		}

		if desc, ok := propMap["description"].(string); ok {
			pi.Desc = desc
		}

		if enum, ok := propMap["enum"].([]any); ok {
			pi.Enum = make([]string, 0, len(enum))
			for _, e := range enum {
				if s, ok := e.(string); ok {
					pi.Enum = append(pi.Enum, s)
				}
			}
		}

		params[name] = pi
	}

	return schema.NewParamsOneOfByParams(params), nil
}

// convertJSONSchemaType 将 JSON Schema 类型转为 eino DataType
func convertJSONSchemaType(t string) schema.DataType {
	switch t {
	case "string":
		return schema.String
	case "number":
		return schema.Number
	case "integer":
		return schema.Integer
	case "boolean":
		return schema.Boolean
	case "array":
		return schema.Array
	case "object":
		return schema.Object
	default:
		return schema.String
	}
}
