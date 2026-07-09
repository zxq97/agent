package llm

import "github.com/zxq97/rental-agent/internal/config"

// init 注册 deepseek provider 实现。
// DeepSeek 是 OpenAI-compatible,直接复用 openAIClient。
func init() {
	Register("deepseek", func(conf config.LLMProviderConf) (ChatModel, error) {
		return NewOpenAIClient(conf)
	})
}
