package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/zxq97/rental-agent/internal/config"
)

// openAIClient 纯 Go OpenAI Chat Completions 协议 client。
// 覆盖 DeepSeek / 豆包 / 千问(都是 OpenAI 兼容)。
type openAIClient struct {
	apiKey  string
	model   string
	baseURL string
	hc      *http.Client
}

// NewOpenAIClient 构造一个 OpenAI-compatible client。
func NewOpenAIClient(conf config.LLMProviderConf) (ChatModel, error) {
	if conf.APIKey == "" {
		return nil, fmt.Errorf("llm: api_key not configured for model %q", conf.Model)
	}
	to := conf.Timeout
	if to <= 0 {
		to = 60
	}
	base := conf.BaseURL
	if base == "" {
		base = "https://api.deepseek.com"
	}
	return &openAIClient{
		apiKey:  conf.APIKey,
		model:   conf.Model,
		baseURL: strings.TrimRight(base, "/"),
		hc:      &http.Client{Timeout: time.Duration(to) * time.Second},
	}, nil
}

const chatPath = "/v1/chat/completions"

func (c *openAIClient) resolveModel(reqModel string) string {
	if reqModel != "" {
		return reqModel
	}
	return c.model
}

// ---- 协议结构 ----

type chatCompletionResp struct {
	Choices []struct {
		Message struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens         int `json:"prompt_tokens"`
		CompletionTokens     int `json:"completion_tokens"`
		TotalTokens          int `json:"total_tokens"`
		PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"` // DeepSeek 特有
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type streamChunkResp struct {
	Choices []struct {
		Delta struct {
			Content   string                `json:"content"`
			ToolCalls []streamToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens         int `json:"prompt_tokens"`
		CompletionTokens     int `json:"completion_tokens"`
		TotalTokens          int `json:"total_tokens"`
		PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
	} `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// streamToolCallDelta 流式 tool_calls 分片:首帧带 id/type/name,后续帧只带 arguments 碎片,按 index 区分。
type streamToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// buildBody 组装 OpenAI 请求体。
func (c *openAIClient) buildBody(req ChatRequest, stream bool) map[string]interface{} {
	messages := make([]map[string]interface{}, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, map[string]interface{}{"role": RoleSystem, "content": req.System})
	}
	for _, m := range req.Messages {
		msg := map[string]interface{}{"role": m.Role}
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			msg["content"] = nil
			msg["tool_calls"] = m.ToolCalls
		} else {
			msg["content"] = m.Content
		}
		if m.ToolCallID != "" {
			msg["tool_call_id"] = m.ToolCallID
		}
		if m.Name != "" {
			msg["name"] = m.Name
		}
		messages = append(messages, msg)
	}

	body := map[string]interface{}{
		"model":    c.resolveModel(req.Model),
		"messages": messages,
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
		choice := req.ToolChoice
		if choice == "" {
			choice = "auto"
		}
		body["tool_choice"] = choice
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]interface{}{"include_usage": true}
	}
	return body
}

func (c *openAIClient) headers() map[string]string {
	return map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + c.apiKey,
	}
}

// Chat 同步调用。
func (c *openAIClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(c.buildBody(req, false))
	if err != nil {
		return nil, fmt.Errorf("llm: marshal body: %w", err)
	}
	start := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+chatPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: new request: %w", err)
	}
	for k, v := range c.headers() {
		httpReq.Header.Set(k, v)
	}
	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: post: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("llm: read body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("llm: http %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var apiResp chatCompletionResp
	if err := json.Unmarshal(raw, &apiResp); err != nil {
		return nil, fmt.Errorf("llm: unmarshal: %w body=%s", err, truncate(string(raw), 300))
	}
	if apiResp.Error != nil {
		return nil, fmt.Errorf("llm: api error: %s", apiResp.Error.Message)
	}
	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("llm: empty choices")
	}
	ch := apiResp.Choices[0]
	return &ChatResponse{
		Content:      ch.Message.Content,
		ToolCalls:    ch.Message.ToolCalls,
		FinishReason: ch.FinishReason,
		DurationMs:   time.Since(start).Milliseconds(),
		Usage: Usage{
			PromptTokens:     apiResp.Usage.PromptTokens,
			CompletionTokens: apiResp.Usage.CompletionTokens,
			TotalTokens:      apiResp.Usage.TotalTokens,
			CacheHitTokens:   apiResp.Usage.PromptCacheHitTokens,
		},
	}, nil
}

// ChatStream 流式调用(SSE)。任一环节失败投一个 Err chunk 后关闭,调用方据此回退。
func (c *openAIClient) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	body, err := json.Marshal(c.buildBody(req, true))
	if err != nil {
		return nil, fmt.Errorf("llm: marshal stream body: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+chatPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm: new stream request: %w", err)
	}
	for k, v := range c.headers() {
		httpReq.Header.Set(k, v)
	}
	resp, err := c.hc.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("llm: post stream: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("llm: stream http %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	ch := make(chan StreamChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		var (
			toolDeltas []streamToolCallDelta
			gotAny     bool
		)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(line[len("data:"):])
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				break
			}
			var chunk streamChunkResp
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // 跳过无法解析的帧(心跳/注释)
			}
			if chunk.Error != nil {
				ch <- StreamChunk{Err: fmt.Errorf("llm: stream api error: %s", chunk.Error.Message)}
				return
			}
			// usage 帧(include_usage 时流末单独一帧 choices 为空)
			if chunk.Usage != nil {
				ch <- StreamChunk{Usage: &Usage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
					CacheHitTokens:   chunk.Usage.PromptCacheHitTokens,
				}}
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			choice := chunk.Choices[0]
			if len(choice.Delta.ToolCalls) > 0 {
				gotAny = true
				toolDeltas = append(toolDeltas, choice.Delta.ToolCalls...)
			}
			if choice.Delta.Content != "" {
				gotAny = true
				select {
				case ch <- StreamChunk{Delta: choice.Delta.Content}:
				case <-ctx.Done():
					return
				}
			}
			// finish_reason 非空即结束(部分网关不发 [DONE])
			if choice.FinishReason != "" {
				break
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			ch <- StreamChunk{Err: fmt.Errorf("llm: stream read: %w", scanErr)}
			return
		}
		// 流末把累积的 tool_calls 分片拼成完整调用,一次性投递
		if calls := accumulateToolCalls(toolDeltas); len(calls) > 0 {
			select {
			case ch <- StreamChunk{ToolCalls: calls}:
			case <-ctx.Done():
				return
			}
		}
		if !gotAny {
			ch <- StreamChunk{Err: fmt.Errorf("llm: stream produced no content")}
		}
	}()
	return ch, nil
}

// accumulateToolCalls 把流式 tool_calls 分片按 index 拼成完整工具调用。
// 首帧带 id/type/name,后续帧追加 arguments,按 index 升序返回。
func accumulateToolCalls(deltas []streamToolCallDelta) []ToolCall {
	if len(deltas) == 0 {
		return nil
	}
	acc := make(map[int]*ToolCall, len(deltas))
	argBufs := make(map[int]*strings.Builder, len(deltas))
	order := make([]int, 0, len(deltas))
	for _, d := range deltas {
		tc, ok := acc[d.Index]
		if !ok {
			tc = &ToolCall{Type: "function"}
			acc[d.Index] = tc
			argBufs[d.Index] = &strings.Builder{}
			order = append(order, d.Index)
		}
		if d.ID != "" {
			tc.ID = d.ID
		}
		if d.Type != "" {
			tc.Type = d.Type
		}
		if d.Function.Name != "" {
			tc.Function.Name = d.Function.Name
		}
		if d.Function.Arguments != "" {
			argBufs[d.Index].WriteString(d.Function.Arguments)
		}
	}
	sort.Ints(order)
	calls := make([]ToolCall, 0, len(order))
	for _, idx := range order {
		tc := acc[idx]
		tc.Function.Arguments = argBufs[idx].String()
		calls = append(calls, *tc)
	}
	return calls
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
