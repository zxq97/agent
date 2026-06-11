package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/deepseek"

	"github.com/zxq97/agent/internal/config"
)

func init() {
	Register("deepseek", buildDeepSeek)
}

func buildDeepSeek(ctx context.Context, c config.LLMProviderConf) (ChatModel, error) {
	if c.APIKey == "" || strings.HasPrefix(c.APIKey, "REPLACE_ME") {
		return nil, fmt.Errorf("deepseek: api_key 未配置 (model=%s)", c.Model)
	}
	timeout := time.Duration(c.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cfg := &deepseek.ChatModelConfig{
		APIKey:  c.APIKey,
		Model:   c.Model,
		BaseURL: c.BaseURL,
		Timeout: timeout,
	}
	return deepseek.NewChatModel(ctx, cfg)
}
