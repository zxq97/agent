package llm

import "context"

const (
	// ModelFlash is the low-latency DeepSeek V4 model used by routine tasks.
	ModelFlash = "deepseek-v4-flash"
	// ModelPro is the stronger DeepSeek V4 model used by complex semantic tasks.
	ModelPro = "deepseek-v4-pro"

	// ModelConversation is kept as a source-compatible alias.
	ModelConversation = ModelFlash
	// ModelReasoning is kept as a source-compatible alias.
	ModelReasoning = ModelPro
)

// Client is the model-independent LLM API used by agent orchestration.
type Client interface {
	Chat(context.Context, *ChatRequest) (*ChatResponse, error)
	ChatStream(context.Context, *ChatRequest) (<-chan StreamChunk, error)
}

// HTTPConfig configures an OpenAI-compatible LLM endpoint.
type HTTPConfig struct {
	Endpoint   string
	APIKey     string
	TimeoutSec int
}
