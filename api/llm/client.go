package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/pkg/http"
)

const chatCompletionsPath = "/v1/chat/completions"

type httpClient struct {
	endpoint string
	apiKey   string
	hc       *httpclient.Client
}

type completionResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// NewHTTPClient creates an OpenAI-compatible LLM client.
func NewHTTPClient(cfg *HTTPConfig) (Client, error) {
	if cfg == nil {
		return nil, errors.New("llm: config is required")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("llm: endpoint is required")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("llm: api_key is required")
	}
	return &httpClient{endpoint: strings.TrimRight(cfg.Endpoint, "/"), apiKey: cfg.APIKey, hc: httpclient.NewClient(&httpclient.Config{TimeoutSec: cfg.TimeoutSec})}, nil
}

func (c *httpClient) Chat(ctx context.Context, input *ChatRequest) (*ChatResponse, error) {
	if input == nil {
		return nil, errors.New("llm chat: request is required")
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		return nil, errors.New("llm chat: model is required")
	}
	body, err := json.Marshal(c.requestBody(input, model, false))
	if err != nil {
		return nil, err
	}
	raw, err := c.hc.PostJSON(ctx, "llm chat", c.endpoint+chatCompletionsPath, c.apiKey, body)
	if err != nil {
		return nil, err
	}
	var response completionResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, errors.Errorf("llm chat: api error: %s", response.Error.Message)
	}
	if len(response.Choices) == 0 {
		return nil, errors.New("llm chat: empty choices")
	}
	choice := response.Choices[0]
	return &ChatResponse{Model: response.Model, Content: choice.Message.Content, ToolCalls: choice.Message.ToolCalls, FinishReason: choice.FinishReason, Usage: response.Usage}, nil
}

func (c *httpClient) ChatStream(ctx context.Context, input *ChatRequest) (<-chan StreamChunk, error) {
	if input == nil || strings.TrimSpace(input.Model) == "" {
		return nil, errors.New("llm chat_stream: model is required")
	}
	body, err := json.Marshal(c.requestBody(input, input.Model, true))
	if err != nil {
		return nil, err
	}
	stream, err := c.hc.PostJSONStream(ctx, "llm chat_stream", c.endpoint+chatCompletionsPath, c.apiKey, body)
	if err != nil {
		return nil, err
	}
	chunks := make(chan StreamChunk, 16)
	go func() {
		defer close(chunks)
		defer stream.Close()
		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				return
			}
			var event streamResponse
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			if event.Error != nil {
				chunks <- StreamChunk{Err: errors.Errorf("llm chat_stream: api error: %s", event.Error.Message)}
				return
			}
			if event.Usage != nil {
				chunks <- StreamChunk{Usage: event.Usage}
			}
			if len(event.Choices) == 0 {
				continue
			}
			choice := event.Choices[0]
			if choice.Delta.Content != "" || choice.FinishReason != "" {
				chunks <- StreamChunk{Delta: choice.Delta.Content, FinishReason: choice.FinishReason}
			}
		}
		if err := scanner.Err(); err != nil {
			chunks <- StreamChunk{Err: err}
		}
	}()
	return chunks, nil
}

func (c *httpClient) requestBody(input *ChatRequest, model string, stream bool) any {
	messages := make([]Message, 0, len(input.Messages)+1)
	if input.System != "" {
		messages = append(messages, Message{Role: RoleSystem, Content: input.System})
	}
	messages = append(messages, input.Messages...)
	var streamOptions any
	if stream {
		streamOptions = map[string]bool{"include_usage": true}
	}
	return struct {
		Model          string          `json:"model"`
		Messages       []Message       `json:"messages"`
		Tools          []Tool          `json:"tools,omitempty"`
		ToolChoice     string          `json:"tool_choice,omitempty"`
		ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
		Temperature    *float64        `json:"temperature,omitempty"`
		MaxTokens      int             `json:"max_tokens,omitempty"`
		Stream         bool            `json:"stream,omitempty"`
		StreamOptions  any             `json:"stream_options,omitempty"`
	}{model, messages, input.Tools, input.ToolChoice, input.ResponseFormat, input.Temperature, input.MaxTokens, stream, streamOptions}
}

type streamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
