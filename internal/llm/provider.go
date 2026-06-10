package llm

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/zxq97/agent/internal/config"
)

// NewChatModel 根据 config 创建 eino ChatModel
// DeepSeek API 完全兼容 OpenAI 格式，直接使用 eino-ext OpenAI ChatModel
func NewChatModel(ctx context.Context, cfg *config.Config) (model.ToolCallingChatModel, error) {
	maxTokens := cfg.LLM.MaxTokens
	temperature := float32(cfg.LLM.Temperature)

	chatModel, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:      cfg.LLM.APIKey,
		BaseURL:     cfg.LLM.BaseURL,
		Model:       cfg.LLM.Model,
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 ChatModel 失败: %w", err)
	}

	return chatModel, nil
}
