// Package llm 提供 LLM provider 的可插拔工厂。
// 默认实现:DeepSeek。后续加 Claude / 千问 / 豆包等只需新增一个 Build 函数并在 Registry 注册。
package llm

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/components/model"

	"github.com/zxq97/agent/internal/config"
)

// ChatModel 是 agent 层依赖的最小接口。
// 直接复用 eino 的 ToolCallingChatModel —— 它能 BindTools,
// ReAct agent / ChatModelAgent 内部要求的就是这个。
type ChatModel = model.ToolCallingChatModel

// Builder 把一份 provider 配置构造成一个 ChatModel。
type Builder func(ctx context.Context, conf config.LLMProviderConf) (ChatModel, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Builder{}
)

// Register 注册一个 provider 工厂。包级 init 调用。
// name 与 LLMProviderConf.Name 对齐(deepseek / claude / qwen ...)。
func Register(name string, b Builder) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = b
}

// Factory 维护"配置名 -> 构造好的 ChatModel"映射,带懒加载缓存。
type Factory struct {
	cfg   *config.LLMConf
	mu    sync.Mutex
	cache map[string]ChatModel
}

// NewFactory 构造工厂,不真正建立 model(懒加载)。
func NewFactory(cfg *config.LLMConf) *Factory {
	return &Factory{cfg: cfg, cache: map[string]ChatModel{}}
}

// Get 拿到一个 ChatModel:
//   - bindingKey 留空 => 用 cfg.Default
//   - 否则从 AgentBindings 找该 key 对应的 provider 配置名
func (f *Factory) Get(ctx context.Context, bindingKey string) (ChatModel, error) {
	providerCfgName := f.cfg.Default
	if bindingKey != "" {
		if v, ok := f.cfg.AgentBindings[bindingKey]; ok && v != "" {
			providerCfgName = v
		}
	}
	if providerCfgName == "" {
		return nil, fmt.Errorf("llm: no provider configured (binding=%q)", bindingKey)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.cache[providerCfgName]; ok {
		return m, nil
	}

	pc, ok := f.cfg.Providers[providerCfgName]
	if !ok {
		return nil, fmt.Errorf("llm: provider config %q not defined", providerCfgName)
	}
	registryMu.RLock()
	build, ok := registry[pc.Name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("llm: provider %q not registered (did you import internal/llm/<name>?)", pc.Name)
	}
	m, err := build(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("llm: build %q: %w", providerCfgName, err)
	}
	f.cache[providerCfgName] = m
	return m, nil
}
