package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// Config 全局配置
type Config struct {
	LLM   LLMConfig   `mapstructure:"llm"`
	MCP   MCPConfig   `mapstructure:"mcp"`
	Agent AgentConfig `mapstructure:"agent"`
}

// LLMConfig 大模型配置
type LLMConfig struct {
	Provider    string  `mapstructure:"provider"`
	APIKey      string  `mapstructure:"api_key"`
	BaseURL     string  `mapstructure:"base_url"`
	Model       string  `mapstructure:"model"`
	MaxTokens   int     `mapstructure:"max_tokens"`
	Temperature float64 `mapstructure:"temperature"`
}

// MCPConfig tyche MCP Server 配置
type MCPConfig struct {
	BaseURL string `mapstructure:"base_url"`
	Token   string `mapstructure:"token"`
	Phone   string `mapstructure:"phone"`
}

// AgentConfig Agent 运行配置
type AgentConfig struct {
	MaxHistoryTurns int           `mapstructure:"max_history_turns"`
	MaxIterations   int           `mapstructure:"max_iterations"`
	RequestTimeout  time.Duration `mapstructure:"request_timeout"`
}

// Load 加载配置文件，支持环境变量覆盖
func Load(path string) *Config {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	// 支持环境变量覆盖
	v.AutomaticEnv()
	v.SetEnvPrefix("AGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := v.ReadInConfig(); err != nil {
		zap.L().Warn("配置文件读取失败，使用环境变量和默认值", zap.Error(err))
	}

	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		zap.L().Fatal("配置解析失败", zap.Error(err))
	}

	// 环境变量覆盖（支持 ${ENV_VAR} 格式）
	cfg.resolveEnvVars()

	// 设置默认值
	cfg.setDefaults()

	return cfg
}

// resolveEnvVars 解析配置中的 ${ENV_VAR} 格式环境变量引用
func (c *Config) resolveEnvVars() {
	c.LLM.APIKey = resolveEnv(c.LLM.APIKey)
	c.LLM.BaseURL = resolveEnv(c.LLM.BaseURL)
	c.MCP.BaseURL = resolveEnv(c.MCP.BaseURL)
	c.MCP.Token = resolveEnv(c.MCP.Token)
	c.MCP.Phone = resolveEnv(c.MCP.Phone)
}

// resolveEnv 如果值是 ${VAR} 格式则从环境变量读取
func resolveEnv(val string) string {
	if strings.HasPrefix(val, "${") && strings.HasSuffix(val, "}") {
		envKey := val[2 : len(val)-1]
		if envVal := os.Getenv(envKey); envVal != "" {
			return envVal
		}
	}
	return val
}

// setDefaults 设置默认值
func (c *Config) setDefaults() {
	if c.Agent.MaxHistoryTurns <= 0 {
		c.Agent.MaxHistoryTurns = 10
	}
	if c.Agent.MaxIterations <= 0 {
		c.Agent.MaxIterations = 20
	}
	if c.Agent.RequestTimeout <= 0 {
		c.Agent.RequestTimeout = 60 * time.Second
	}
}

// Validate 校验配置完整性
func (c *Config) Validate() error {
	if c.LLM.APIKey == "" {
		return fmt.Errorf("llm.api_key 不能为空")
	}
	if c.LLM.BaseURL == "" {
		return fmt.Errorf("llm.base_url 不能为空")
	}
	if c.LLM.Model == "" {
		return fmt.Errorf("llm.model 不能为空")
	}
	if c.MCP.BaseURL == "" {
		return fmt.Errorf("mcp.base_url 不能为空")
	}
	return nil
}
