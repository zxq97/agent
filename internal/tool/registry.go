package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"
)

// ToolProvider Tool 来源的统一接口，支持多种来源扩展
type ToolProvider interface {
	// Name 来源标识，如 "mcp"、"local"、"rag"
	Name() string
	// LoadTools 从该来源加载所有 Tool
	LoadTools(ctx context.Context) ([]tool.BaseTool, error)
}

// Registry Tool 统一注册表
type Registry struct {
	tools     map[string]tool.BaseTool
	providers map[string]ToolProvider
}

// NewRegistry 创建 Tool 注册表
func NewRegistry() *Registry {
	return &Registry{
		tools:     make(map[string]tool.BaseTool),
		providers: make(map[string]ToolProvider),
	}
}

// Register 批量注册 Tool
func (r *Registry) Register(tools ...tool.BaseTool) {
	for _, t := range tools {
		info, err := t.Info(context.Background())
		if err != nil {
			zap.L().Warn("获取 Tool 信息失败，跳过注册", zap.Error(err))
			continue
		}
		r.tools[info.Name] = t
		zap.L().Debug("Tool 已注册", zap.String("name", info.Name))
	}
}

// RegisterProvider 注册 ToolProvider 并加载其 Tool
func (r *Registry) RegisterProvider(ctx context.Context, provider ToolProvider) error {
	if _, exists := r.providers[provider.Name()]; exists {
		return fmt.Errorf("ToolProvider %s 已注册", provider.Name())
	}

	tools, err := provider.LoadTools(ctx)
	if err != nil {
		return fmt.Errorf("加载 ToolProvider %s 失败: %w", provider.Name(), err)
	}

	r.providers[provider.Name()] = provider
	r.Register(tools...)

	zap.L().Info("ToolProvider 已注册",
		zap.String("provider", provider.Name()),
		zap.Int("tool_count", len(tools)),
	)

	return nil
}

// GetByName 按名称获取 Tool
func (r *Registry) GetByName(name string) (tool.BaseTool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Select 按名称前缀筛选 Tool（供 Skill Agent 选择性订阅）
// 支持多个前缀，返回任一前缀匹配的 Tool
func (r *Registry) Select(prefixes ...string) []tool.BaseTool {
	var result []tool.BaseTool
	for name, t := range r.tools {
		for _, prefix := range prefixes {
			if strings.HasPrefix(name, prefix) {
				result = append(result, t)
				break
			}
		}
	}
	return result
}

// ListAll 返回所有注册的 Tool
func (r *Registry) ListAll() []tool.BaseTool {
	result := make([]tool.BaseTool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	return result
}
