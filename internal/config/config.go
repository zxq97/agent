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
	Name    string `yaml:"name"`     // provider 实现名,如 "deepseek"
	Model   string `yaml:"model"`    // 模型名,如 "deepseek-chat"
	APIKey  string `yaml:"api_key"`  // 支持 ${DEEPSEEK_API_KEY} 占位
	BaseURL string `yaml:"base_url"` // OpenAI-compatible base url
	Timeout int    `yaml:"timeout"`  // 秒,默认 60
}

// LLMConf 全部 provider + 各环节绑定。
type LLMConf struct {
	Default   string                     `yaml:"default"`   // 默认 provider 配置名
	Providers map[string]LLMProviderConf `yaml:"providers"` // key 是 provider 配置名
	// AgentBindings 每个环节(decide/filter_interpreter/search_guide/price_detail/insurance/compare/rules)
	// 默认用哪个 provider 配置名。铁律:带 function calling 的 decide 必须 chat,绝不能绑 reasoner。
	AgentBindings map[string]string `yaml:"agent_bindings"`
}

// AgentConf agent 行为配置。
type AgentConf struct {
	// MaxStep 责任链/工具编排的最大步数余量(预留)。
	MaxStep int `yaml:"max_step"`
	// EnableLocalTools 是否注册本地占位工具(资质/油费等)给 LLM。默认 false:
	// 占位常数会被 LLM 当真报给用户,真业务接入前不上。
	EnableLocalTools bool `yaml:"enable_local_tools"`
	// Mode 决策架构:"pipeline"(默认)。预留灰度开关。
	Mode string `yaml:"mode"`
}

// TycheMCPConf 接 tyche MCP server 的配置。
type TycheMCPConf struct {
	// Endpoint MCP 入口完整 URL。
	Endpoint string `yaml:"endpoint"`
	// Phone Authorization: Bearer <phone> 鉴权用手机号。
	Phone string `yaml:"phone"`
	// Timeout 单次 RPC 超时(秒)。
	Timeout int `yaml:"timeout"`
}

// HTTPConf P4 HTTP 服务配置。
type HTTPConf struct {
	Addr         string `yaml:"addr"`
	ReadTimeout  int    `yaml:"read_timeout"`
	WriteTimeout int    `yaml:"write_timeout"`
}

// SessionConf P4 Session 持久化配置。
type SessionConf struct {
	RedisAddr string `yaml:"redis_addr"`
	Password  string `yaml:"password"`
	DB        int    `yaml:"db"`
	TTLHours  int    `yaml:"ttl_hours"`
	KeyPrefix string `yaml:"key_prefix"`
}

// RateLimitConf P4 限流配置。
type RateLimitConf struct {
	PerMinute int `yaml:"per_minute"`
	PerDay    int `yaml:"per_day"`
}

type AuditConf struct {
	Enabled      bool   `yaml:"enabled"`
	Endpoint     string `yaml:"endpoint"`
	SegmentChars int    `yaml:"segment_chars"`
}

type AccessLockConf struct {
	Enabled    bool `yaml:"enabled"`
	TTLSeconds int  `yaml:"ttl_seconds"`
	FailOpen   bool `yaml:"fail_open"`
}

// GuideConf rental-guide 集群配置(guide/store/list/agent 报价+菜单)。
type GuideConf struct {
	Endpoint string `yaml:"endpoint"`
	Phone    string `yaml:"phone"`
	Timeout  int    `yaml:"timeout"`
}

// AgentHubConf P3 规则检索平台配置。
type AgentHubConf struct {
	Host            string `yaml:"host"`
	RetrievalAPIKey string `yaml:"retrieval_api_key"`
	Timeout         int    `yaml:"timeout"`
}

// LogConf 落盘日志配置。默认写 .logs/ 目录 + 兼输出 stderr。
type LogConf struct {
	// Dir 日志目录,支持相对/绝对路径。空字符串禁用文件输出。
	Dir string `yaml:"dir"`
	// Stderr 是否同时把日志复制一份到 stderr。CLI/HTTP 入口的 -q/-v 会覆盖此项。
	Stderr bool `yaml:"stderr"`
	// FilePrefix 文件名前缀,默认 "agent"。产物为 <dir>/<prefix>-YYYY-MM-DD.log。
	FilePrefix string `yaml:"file_prefix"`
}

// Config 全局配置入口。
type Config struct {
	Env        string         `yaml:"env"`
	LLM        LLMConf        `yaml:"llm"`
	Tyche      TycheMCPConf   `yaml:"tyche"`
	Agent      AgentConf      `yaml:"agent"`
	HTTP       HTTPConf       `yaml:"http"`
	Session    SessionConf    `yaml:"session"`
	RateLimit  RateLimitConf  `yaml:"ratelimit"`
	Audit      AuditConf      `yaml:"audit"`
	AccessLock AccessLockConf `yaml:"access_lock"`
	Guide      GuideConf      `yaml:"guide"`
	AgentHub   AgentHubConf   `yaml:"agenthub"`
	Log        LogConf        `yaml:"log"`
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
	if c.Agent.Mode == "" {
		c.Agent.Mode = "pipeline"
	}
	for k, p := range c.LLM.Providers {
		if p.Timeout == 0 {
			p.Timeout = 60
			c.LLM.Providers[k] = p
		}
		if p.BaseURL == "" {
			p.BaseURL = "https://api.deepseek.com"
			c.LLM.Providers[k] = p
		}
	}
	if c.HTTP.Addr == "" {
		c.HTTP.Addr = ":8080"
	}
	if c.HTTP.ReadTimeout == 0 {
		c.HTTP.ReadTimeout = 30
	}
	if c.HTTP.WriteTimeout == 0 {
		c.HTTP.WriteTimeout = 600
	}
	if c.Session.TTLHours == 0 {
		c.Session.TTLHours = 24
	}
	if c.Session.KeyPrefix == "" {
		c.Session.KeyPrefix = "agent:session"
	}
	if c.RateLimit.PerMinute == 0 {
		c.RateLimit.PerMinute = 30
	}
	if c.RateLimit.PerDay == 0 {
		c.RateLimit.PerDay = 1000
	}
	if c.Audit.SegmentChars == 0 {
		c.Audit.SegmentChars = 300
	}
	if c.AccessLock.TTLSeconds == 0 {
		c.AccessLock.TTLSeconds = 60
	}
	if !c.AccessLock.Enabled {
		c.AccessLock.FailOpen = true
	}
	if c.Guide.Timeout == 0 {
		c.Guide.Timeout = 30
	}
	if c.AgentHub.Timeout == 0 {
		c.AgentHub.Timeout = 10
	}
	if c.Log.Dir == "" {
		c.Log.Dir = ".logs"
	}
	if c.Log.FilePrefix == "" {
		c.Log.FilePrefix = "agent"
	}
}
