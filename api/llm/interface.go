package llm

import "context"

const (
	// ModelConversation is the default DeepSeek model for standard conversation
	// and function calling.
	ModelConversation = "deepseek-chat"
	// ModelReasoning is the stronger reasoning model for complex analysis that
	// does not require function calling.
	ModelReasoning = "deepseek-reasoner"
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
