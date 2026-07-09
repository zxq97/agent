package llm

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/zxq97/rental-agent/internal/config"
)

// Builder 把一份 provider 配置构造成 ChatModel。
type Builder func(conf config.LLMProviderConf) (ChatModel, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Builder{}
)

// Register 注册一个 provider 实现工厂。包级 init 调用。
// name 与 LLMProviderConf.Name 对齐(deepseek / ...)。
func Register(name string, b Builder) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = b
}

// Factory 维护"配置名 -> 构造好的 ChatModel"映射,带懒加载缓存。
type Factory struct {
	cfg    *config.LLMConf
	mu     sync.Mutex
	cache  map[string]ChatModel
	logOut io.Writer // 非 nil 时,Get 返回的 model 自动套 LoggingChatModel
}

// NewFactory 构造工厂(懒加载,不立即建 model)。
func NewFactory(cfg *config.LLMConf) *Factory {
	return &Factory{cfg: cfg, cache: map[string]ChatModel{}}
}

// SetLogger 打开/关闭 LLM 调用日志。设置后清缓存,重新 Get 时按新设置包装。
func (f *Factory) SetLogger(w io.Writer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logOut = w
	f.cache = map[string]ChatModel{}
}

// Get 拿一个 ChatModel:
//   - bindingKey 留空 => 用 cfg.Default
//   - 否则从 AgentBindings 找该环节对应的 provider 配置名,找不到回退 Default
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
		return nil, fmt.Errorf("llm: provider impl %q not registered (import internal/llm/<impl>?)", pc.Name)
	}
	m, err := build(pc)
	if err != nil {
		return nil, fmt.Errorf("llm: build %q: %w", providerCfgName, err)
	}
	if f.logOut != nil {
		m = NewLoggingChatModel(m, providerCfgName, f.logOut)
	}
	f.cache[providerCfgName] = m
	return m, nil
}
