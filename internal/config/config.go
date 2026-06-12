// Package config 装载多环境 yaml + env var。
// 设计原则:
//   - pre/prod.yaml 用 ${ENV_NAME} 占位,装载时展开,生产敏感信息走 env。
//   - dev.yaml 允许写明文 key(本地开发便利),入库前自行评估。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// LLMProviderConf 单个 LLM provider 的配置。
type LLMProviderConf struct {
	Name    string `yaml:"name"`     // provider 名,如 "deepseek"
	Model   string `yaml:"model"`    // 模型名,如 "deepseek-chat" / "deepseek-reasoner"
	APIKey  string `yaml:"api_key"`  // 支持 ${DEEPSEEK_API_KEY} 占位
	BaseURL string `yaml:"base_url"` // 可选,留空走 provider 默认
	Timeout int    `yaml:"timeout"`  // 秒,默认 60
}

// LLMConf 全部 LLM provider 列表 + 各 agent 使用哪个 provider。
type LLMConf struct {
	Default   string                     `yaml:"default"`   // 默认 provider 名
	Providers map[string]LLMProviderConf `yaml:"providers"` // key 是 provider 名
	// AgentBindings 每个 agent 默认使用哪个 provider(P3+ 用)。
	AgentBindings map[string]string `yaml:"agent_bindings"`
}

// AgentConf agent 行为配置。
type AgentConf struct {
	// MaxStep React loop 的最大步数(每次 chatModel/toolsNode 切换算 1 步)。
	// 默认 12(eino 内部 node数+10)。典型场景:4个 tool 连调约需 9 步;
	// 留余量设 30,支持约 14 个 tool call 的复杂对话。
	MaxStep int `yaml:"max_step"`
}

// TycheMCPConf 接 tyche MCP server 的配置。
//
// tyche 是 C 端租车 API,自带一套 JSON-RPC 2.0 over HTTP 的 MCP server
// (controller/mcp/controller.go),提供 7 个开箱即用的 agent 工具:
//   rental_search_locations / rental_resolve_poi / rental_search_quotes /
//   rental_get_order_details / rental_create_order / rental_get_reservation /
//   rental_get_driver_list
// agent 直接调它,无需在 saas-api 里再造一套 MCP。
type TycheMCPConf struct {
	// Endpoint MCP 入口完整 URL,例如 http://10.78.133.4:8877/car/rental/inner/mcp
	Endpoint string `yaml:"endpoint"`
	// Phone Authorization: Bearer <phone> 鉴权用的手机号(需在 tyche Apollo
	// mcp_allowed_phones 白名单里)。dev 环境若白名单为空,这里也可空。
	Phone string `yaml:"phone"`
	// Timeout 单次 RPC 超时(秒)。
	Timeout int `yaml:"timeout"`
}

// Config 全局配置入口。
type Config struct {
	Env   string       `yaml:"env"` // dev / pre / prod
	LLM   LLMConf      `yaml:"llm"`
	Tyche TycheMCPConf `yaml:"tyche"`
	Agent AgentConf    `yaml:"agent"`
}

// Load 从指定 yaml 加载并展开 env 占位。
func Load(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve abs path: %w", err)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", abs, err)
	}
	expanded := expandEnv(string(raw))
	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}
	cfg.applyDefaults()
	return cfg, nil
}

// envRe 匹配 ${NAME} 或 ${NAME:-default}。
var envRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

func expandEnv(s string) string {
	return envRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := envRe.FindStringSubmatch(m)
		name := sub[1]
		def := sub[2]
		if v, ok := os.LookupEnv(name); ok && v != "" {
			return v
		}
		return def
	})
}

func (c *Config) applyDefaults() {
	if c.Tyche.Timeout == 0 {
		c.Tyche.Timeout = 30
	}
	if c.Agent.MaxStep == 0 {
		c.Agent.MaxStep = 30
	}
	for k, p := range c.LLM.Providers {
		if p.Timeout == 0 {
			p.Timeout = 60
			c.LLM.Providers[k] = p
		}
	}
}
