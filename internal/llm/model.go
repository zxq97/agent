// Package llm 提供纯 Go 的 OpenAI-compatible LLM client(不依赖 eino)。
// 自带流式 SSE 解析 + tool_calls 分片累积 + syncFallback。
package llm

import "context"

// Role 常量。
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message 一条对话消息(OpenAI Chat 协议)。
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // assistant 发起的工具调用
	ToolCallID string     `json:"tool_call_id,omitempty"` // role=tool 时,对应哪个 call
	Name       string     `json:"name,omitempty"`         // 可选:工具名
}

// ToolCall 模型发起的一次工具调用。
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall 工具调用的函数名 + 入参(JSON 字符串)。
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef 提供给模型的工具定义(schema)。
type ToolDef struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
}

// FunctionDef 函数定义。Parameters 是 JSON Schema(原始字节)。
type FunctionDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"` // 通常是 json.RawMessage
}

// Usage token 用量(从 response 取,供成本/监控)。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheHitTokens   int `json:"cache_hit_tokens,omitempty"` // DeepSeek 前缀缓存命中
}

// ChatRequest 一次 LLM 调用入参。
type ChatRequest struct {
	System      string    // system prompt(单独拎出,便于前缀缓存)
	Messages    []Message // 历史 + 本轮
	Tools       []ToolDef // function calling 工具集;空则普通对话
	Model       string    // 覆盖 provider 默认 model(可空)
	Temperature *float64  // 可空
	MaxTokens   int       // 可空(0 = 不限)
	ToolChoice  string    // 可空,默认 auto
}

// ChatResponse 同步响应。
type ChatResponse struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	DurationMs   int64
	Usage        Usage
}

// StreamChunk 流式单帧。
//   - Delta 非空:content 文本增量
//   - ToolCalls 非空:流末投递的完整工具调用
//   - Usage 非空:流末帧带 usage
//   - Err 非空:流异常
type StreamChunk struct {
	Delta     string
	ToolCalls []ToolCall
	Usage     *Usage
	Err       error
}

// ChatModel 是 agent 层依赖的最小 LLM 接口。
type ChatModel interface {
	// Chat 同步调用,返回完整响应(含 ToolCalls)。
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	// ChatStream 流式调用,逐 token 返回增量;流末投递完整 ToolCalls + Usage。
	// channel 关闭表示流结束。
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
}

// Float64Ptr 便捷构造温度等可选指针参数。
func Float64Ptr(v float64) *float64 { return &v }
