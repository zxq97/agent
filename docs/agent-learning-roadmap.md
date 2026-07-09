# Go 后端工程师 → Agent 开发者：速成学习路线

> **目标人群**：有 2+ 年 Go 后端经验，熟悉 Gin/Echo、gRPC、Redis、MQ
> 
> **总周期**：6 周（每天 2-3 小时）
> 
> **核心策略**：用 Go 构建生产级 Agent 系统，发挥 Go 的并发优势

---

## 为什么 Go 做 Agent 是个好选择

| 维度 | Go 优势 | 适用场景 |
|------|--------|---------|
| **并发模型** | goroutine + channel 天然适配 Agent Loop | 多工具并行调用、流式处理 |
| **性能** | 启动快、内存占用低 | 边缘节点、Serverless |
| **类型安全** | 编译期发现 Tool Schema 错误 | 大型 Agent 系统 |
| **后端生态** | 与微服务、K8s 无缝集成 | 企业级落地 |
| **部署简单** | 单二进制、无依赖 | Docker 镜像极小 |

**注意**：Go 生态在 Agent 工具链上不如 Python 丰富。**Eval、Embedding、向量库**等场景可能需要：
1. 用 Go 直接调 API（推荐）
2. 关键脚本用 Python 写（如 eval 跑批），Go 主服务调用

---

## 实战项目：智能客服 Agent（Go 版）

### 项目概述

```
┌─────────────────────────────────────────────────────────────────┐
│              智能客服 Agent 系统 (Go)                             │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  HTTP/gRPC ──▶ [Safety] ──▶ [Router] ──▶ [Agent Loop]          │
│                                              │                  │
│              goroutine                       │                  │
│              并发执行                  ┌──────┴──────┐           │
│                                       ▼              ▼           │
│                                  Tool Calls      LLM Call        │
│                                  (parallel)      (streaming)     │
│                                                                 │
│  特色:                                                           │
│  ✓ 高并发 (goroutine pool)    ✓ 多模型路由                       │
│  ✓ gRPC + HTTP 双协议         ✓ 类型安全的 Tool 定义              │
│  ✓ SSE 流式输出              ✓ Prometheus 指标                  │
│  ✓ Context 超时控制          ✓ OpenTelemetry 追踪                │
└─────────────────────────────────────────────────────────────────┘
```

### Go 技术栈

| 层 | 选型 | 备选 | 原因 |
|----|------|------|------|
| Web 框架 | Gin | Echo / Fiber | 社区最大、文档全 |
| LLM SDK | anthropic-sdk-go | 自封装 HTTP | 官方支持 |
| 向量库 | Qdrant Go Client | Milvus Go SDK | Qdrant Go 客户端成熟 |
| Embedding | API (Voyage/OpenAI) | 本地 ollama | 简单可靠 |
| Prompt 模板 | text/template | sprig | Go 原生，足够用 |
| 配置 | viper | koanf | 标准选择 |
| 日志 | slog (Go 1.21+) | zap / zerolog | 标准库 |
| 追踪 | OpenTelemetry | - | 业界标准 |
| 缓存 | redis/go-redis | - | LLM 响应缓存 |
| Eval | 自建 + Promptfoo(npm) | - | 主服务 Go，eval 工具用 npm |
| 部署 | Docker + K8s | Railway | 标准方案 |

---

## Week 1：基础认知 + 第一次对话

### 学习目标
- 理解 LLM API 调用机制
- 用 Go 完成第一次 LLM 调用
- 搭建项目骨架（Gin + 配置 + 日志）

### Day 1-2：LLM 基础概念

**学习内容：**
- Token 计算、模型参数（temperature/top_p/max_tokens）
- System / User / Assistant 消息角色
- 流式响应（SSE）机制
- API 错误码与限流

**学习资料：**

| 资料 | 类型 | 链接 |
|------|------|------|
| Anthropic Docs: Getting Started | 官方 | https://docs.anthropic.com/en/docs/initial-setup |
| Anthropic Go SDK | 官方 GitHub | https://github.com/anthropics/anthropic-sdk-go |
| Messages API Reference | 官方 | https://docs.anthropic.com/en/api/messages |
| 吴恩达 Prompt Engineering 课 | 视频(1h) | https://www.deeplearning.ai/short-courses/chatgpt-prompt-engineering-for-developers/ |

**动手练习：**

```go
// main.go - 第一次 LLM 调用
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
)

func main() {
    client := anthropic.NewClient(
        option.WithAPIKey("sk-ant-xxx"),
    )

    ctx := context.Background()
    message, err := client.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     anthropic.F(anthropic.ModelClaudeSonnet4),
        MaxTokens: anthropic.F(int64(1024)),
        System: anthropic.F([]anthropic.TextBlockParam{
            anthropic.NewTextBlock("你是一个客服助手"),
        }),
        Messages: anthropic.F([]anthropic.MessageParam{
            anthropic.NewUserMessage(anthropic.NewTextBlock("我想退货")),
        }),
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(message.Content[0].Text)
    fmt.Printf("Input tokens: %d\n", message.Usage.InputTokens)
    fmt.Printf("Output tokens: %d\n", message.Usage.OutputTokens)
}
```

```go
// 流式输出
stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
    Model:     anthropic.F(anthropic.ModelClaudeSonnet4),
    MaxTokens: anthropic.F(int64(1024)),
    Messages: anthropic.F([]anthropic.MessageParam{
        anthropic.NewUserMessage(anthropic.NewTextBlock("解释什么是 Agent")),
    }),
})

for stream.Next() {
    event := stream.Current()
    if delta, ok := event.Delta.(anthropic.ContentBlockDeltaEventDelta); ok {
        if delta.Text != "" {
            fmt.Print(delta.Text)
        }
    }
}
if err := stream.Err(); err != nil {
    log.Fatal(err)
}
```

### Day 3-4：Prompt Engineering

**学习内容：**
- Role Prompting / Few-shot / CoT
- 输出格式约束（JSON Mode）
- XML 标签结构化 prompt（Anthropic 推荐风格）

**学习资料：**

| 资料 | 类型 | 链接 |
|------|------|------|
| Anthropic Prompt Engineering Guide | 官方 | https://docs.anthropic.com/en/docs/build-with-claude/prompt-engineering/overview |
| Anthropic Prompt Library | 示例集 | https://docs.anthropic.com/en/prompt-library/overview |
| DAIR.ai Prompt Guide (中文) | 社区 | https://www.promptingguide.ai/zh |

**动手练习：**
- 用 Go 的 `text/template` 实现 prompt 模板渲染
- 对比 3 个版本的意图分类 prompt 的效果

### Day 5-7：项目骨架搭建

**项目结构：**

```
smart-cs-agent/
├── cmd/
│   └── server/
│       └── main.go               # 程序入口
├── internal/
│   ├── api/
│   │   ├── handler/
│   │   │   └── chat.go          # 聊天 handler
│   │   ├── middleware/
│   │   │   ├── auth.go
│   │   │   ├── logger.go
│   │   │   └── recovery.go
│   │   └── router.go            # Gin 路由
│   ├── config/
│   │   └── config.go            # viper 配置
│   ├── llm/
│   │   ├── client.go            # LLM 客户端封装
│   │   └── types.go             # 类型定义
│   ├── prompt/
│   │   ├── engine.go            # 模板引擎
│   │   └── loader.go            # YAML 加载器
│   └── session/
│       └── memory.go            # 会话内存
├── prompts/
│   └── cs_basic/
│       └── v1.yaml
├── configs/
│   └── config.yaml
├── go.mod
├── go.sum
└── Makefile
```

**核心代码：**

```go
// internal/llm/client.go
package llm

import (
    "context"
    "fmt"

    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
)

type Client struct {
    anthropic *anthropic.Client
}

type ChatRequest struct {
    Model       string
    System      string
    Messages    []Message
    MaxTokens   int64
    Temperature float64
    Stream      bool
}

type Message struct {
    Role    string // "user" | "assistant"
    Content string
}

type ChatResponse struct {
    Content      string
    InputTokens  int64
    OutputTokens int64
    Model        string
}

func NewClient(apiKey string) *Client {
    return &Client{
        anthropic: anthropic.NewClient(option.WithAPIKey(apiKey)),
    }
}

func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
    messages := make([]anthropic.MessageParam, 0, len(req.Messages))
    for _, m := range req.Messages {
        if m.Role == "user" {
            messages = append(messages, anthropic.NewUserMessage(
                anthropic.NewTextBlock(m.Content)))
        } else {
            messages = append(messages, anthropic.NewAssistantMessage(
                anthropic.NewTextBlock(m.Content)))
        }
    }

    resp, err := c.anthropic.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     anthropic.F(req.Model),
        MaxTokens: anthropic.F(req.MaxTokens),
        System: anthropic.F([]anthropic.TextBlockParam{
            anthropic.NewTextBlock(req.System),
        }),
        Messages: anthropic.F(messages),
    })
    if err != nil {
        return nil, fmt.Errorf("anthropic call: %w", err)
    }

    return &ChatResponse{
        Content:      resp.Content[0].Text,
        InputTokens:  resp.Usage.InputTokens,
        OutputTokens: resp.Usage.OutputTokens,
        Model:        resp.Model,
    }, nil
}

// 流式接口
func (c *Client) ChatStream(ctx context.Context, req ChatRequest, 
    onDelta func(text string)) error {
    // ... 流式实现
}
```

```go
// internal/api/handler/chat.go - SSE 流式输出
func (h *ChatHandler) StreamChat(c *gin.Context) {
    c.Header("Content-Type", "text/event-stream")
    c.Header("Cache-Control", "no-cache")
    c.Header("Connection", "keep-alive")

    var req ChatStreamRequest
    if err := c.BindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    err := h.llmClient.ChatStream(c.Request.Context(), llm.ChatRequest{
        Model:    "claude-sonnet-4-6",
        Messages: req.Messages,
    }, func(delta string) {
        // SSE 写入
        c.SSEvent("message", delta)
        c.Writer.Flush()
    })

    if err != nil {
        c.SSEvent("error", err.Error())
    }
    c.SSEvent("done", "")
}
```

**本周交付：**
- `make run` 启动服务
- `curl -X POST localhost:8080/chat -d '{"message":"你好"}'` 能收到 AI 回复
- `/chat/stream` 支持 SSE 流式输出

---

## Week 2：Tool Use + RAG

### 学习目标
- 用 Go 实现类型安全的 Tool 系统
- 实现 Agent Loop（ReAct）
- 接入向量数据库实现 RAG

### Day 1-2：Tool Use

**学习内容：**
- Tool Use 协议（输入 schema、输出格式）
- Agent Loop 的状态机模型
- 并行工具调用（Go 的天然优势）

**学习资料：**

| 资料 | 链接 |
|------|------|
| Anthropic Tool Use 文档 | https://docs.anthropic.com/en/docs/build-with-claude/tool-use/overview |
| Tool Use Best Practices | https://docs.anthropic.com/en/docs/build-with-claude/tool-use/best-practices |
| Building Agentic Systems | https://docs.anthropic.com/en/docs/build-with-claude/agentic-systems |
| anthropic-sdk-go Tool Use 示例 | https://github.com/anthropics/anthropic-sdk-go/tree/main/examples |

**动手练习 —— Go 风格的 Tool 系统：**

```go
// internal/agent/tool.go
package agent

import (
    "context"
    "encoding/json"
    "fmt"
    "reflect"
)

// Tool 接口
type Tool interface {
    Name() string
    Description() string
    Schema() map[string]any  // JSON Schema
    Execute(ctx context.Context, input json.RawMessage) (string, error)
}

// ToolRegistry 工具注册中心
type ToolRegistry struct {
    tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
    return &ToolRegistry{tools: make(map[string]Tool)}
}

func (r *ToolRegistry) Register(t Tool) {
    r.tools[t.Name()] = t
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
    t, ok := r.tools[name]
    return t, ok
}

// 转换为 Anthropic API 格式
func (r *ToolRegistry) ToAnthropicTools() []anthropic.ToolParam {
    tools := make([]anthropic.ToolParam, 0, len(r.tools))
    for _, t := range r.tools {
        tools = append(tools, anthropic.ToolParam{
            Name:        anthropic.F(t.Name()),
            Description: anthropic.F(t.Description()),
            InputSchema: anthropic.F(t.Schema()),
        })
    }
    return tools
}
```

```go
// internal/agent/tools/query_order.go
package tools

type QueryOrderTool struct {
    orderService OrderService  // 依赖注入业务服务
}

func (t *QueryOrderTool) Name() string {
    return "query_order"
}

func (t *QueryOrderTool) Description() string {
    return "查询用户订单信息。需要用户ID，可选订单号。"
}

func (t *QueryOrderTool) Schema() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "user_id":  map[string]any{"type": "string", "description": "用户ID"},
            "order_id": map[string]any{"type": "string", "description": "订单号（可选）"},
        },
        "required": []string{"user_id"},
    }
}

type queryOrderInput struct {
    UserID  string `json:"user_id"`
    OrderID string `json:"order_id,omitempty"`
}

func (t *QueryOrderTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
    var input queryOrderInput
    if err := json.Unmarshal(raw, &input); err != nil {
        return "", fmt.Errorf("invalid input: %w", err)
    }

    orders, err := t.orderService.Query(ctx, input.UserID, input.OrderID)
    if err != nil {
        return "", err
    }

    result, _ := json.Marshal(orders)
    return string(result), nil
}
```

```go
// internal/agent/loop.go - Agent Loop 核心
package agent

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"
)

type AgentLoop struct {
    llm      *llm.Client
    tools    *ToolRegistry
    maxSteps int
}

func (a *AgentLoop) Run(ctx context.Context, userMessage string) (string, error) {
    messages := []anthropic.MessageParam{
        anthropic.NewUserMessage(anthropic.NewTextBlock(userMessage)),
    }

    for step := 0; step < a.maxSteps; step++ {
        resp, err := a.llm.Anthropic.Messages.New(ctx, anthropic.MessageNewParams{
            Model:     anthropic.F(anthropic.ModelClaudeSonnet4),
            MaxTokens: anthropic.F(int64(1024)),
            Tools:     anthropic.F(a.tools.ToAnthropicTools()),
            Messages:  anthropic.F(messages),
        })
        if err != nil {
            return "", fmt.Errorf("step %d: %w", step, err)
        }

        // 如果没有工具调用，返回最终答案
        if resp.StopReason != anthropic.MessageStopReasonToolUse {
            return resp.Content[0].Text, nil
        }

        // 收集工具调用请求
        toolUses := []anthropic.ToolUseBlock{}
        for _, block := range resp.Content {
            if tu, ok := block.AsToolUse(); ok {
                toolUses = append(toolUses, tu)
            }
        }

        // ✨ Go 的优势：并行执行所有工具调用
        results := a.executeToolsParallel(ctx, toolUses)

        // 把结果加回 messages
        messages = append(messages, anthropic.NewAssistantMessage(resp.Content...))
        toolResultBlocks := make([]anthropic.MessageParamContentUnion, 0, len(results))
        for _, r := range results {
            toolResultBlocks = append(toolResultBlocks, 
                anthropic.NewToolResultBlock(r.ToolUseID, r.Content, r.IsError))
        }
        messages = append(messages, anthropic.NewUserMessage(toolResultBlocks...))
    }

    return "", fmt.Errorf("max steps exceeded")
}

// 并行执行多个工具调用
func (a *AgentLoop) executeToolsParallel(ctx context.Context, 
    toolUses []anthropic.ToolUseBlock) []ToolResult {
    
    results := make([]ToolResult, len(toolUses))
    var wg sync.WaitGroup

    for i, tu := range toolUses {
        wg.Add(1)
        go func(idx int, tu anthropic.ToolUseBlock) {
            defer wg.Done()
            
            tool, ok := a.tools.Get(tu.Name)
            if !ok {
                results[idx] = ToolResult{
                    ToolUseID: tu.ID,
                    Content:   fmt.Sprintf("tool %s not found", tu.Name),
                    IsError:   true,
                }
                return
            }

            output, err := tool.Execute(ctx, tu.Input)
            if err != nil {
                results[idx] = ToolResult{
                    ToolUseID: tu.ID,
                    Content:   err.Error(),
                    IsError:   true,
                }
                return
            }

            results[idx] = ToolResult{
                ToolUseID: tu.ID,
                Content:   output,
            }
        }(i, tu)
    }

    wg.Wait()
    return results
}
```

### Day 3-4：RAG with Qdrant

**学习内容：**
- Embedding 原理
- Qdrant Go Client 使用
- 文档切分策略

**学习资料：**

| 资料 | 链接 |
|------|------|
| Qdrant Go Client | https://github.com/qdrant/go-client |
| Anthropic RAG 教程 | https://docs.anthropic.com/en/docs/build-with-claude/retrieval-augmented-generation |
| Voyage Embeddings (Anthropic 推荐) | https://docs.voyageai.com/docs/embeddings |
| Chunking Strategies | https://www.pinecone.io/learn/chunking-strategies/ |

**动手练习：**

```go
// internal/rag/embedder.go
package rag

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
)

type Embedder struct {
    apiKey string
    model  string  // e.g. "voyage-3"
    client *http.Client
}

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
    body, _ := json.Marshal(map[string]any{
        "input": texts,
        "model": e.model,
    })

    req, _ := http.NewRequestWithContext(ctx, "POST", 
        "https://api.voyageai.com/v1/embeddings", bytes.NewReader(body))
    req.Header.Set("Authorization", "Bearer "+e.apiKey)
    req.Header.Set("Content-Type", "application/json")

    resp, err := e.client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result struct {
        Data []struct {
            Embedding []float32 `json:"embedding"`
        } `json:"data"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    embeddings := make([][]float32, len(result.Data))
    for i, d := range result.Data {
        embeddings[i] = d.Embedding
    }
    return embeddings, nil
}
```

```go
// internal/rag/retriever.go
package rag

import (
    "context"
    "github.com/qdrant/go-client/qdrant"
)

type Retriever struct {
    qdrant    *qdrant.Client
    embedder  *Embedder
    collection string
}

func (r *Retriever) Retrieve(ctx context.Context, query string, topK int) ([]Document, error) {
    // 1. 把 query 转向量
    embeddings, err := r.embedder.Embed(ctx, []string{query})
    if err != nil {
        return nil, err
    }

    // 2. 向量搜索
    results, err := r.qdrant.Query(ctx, &qdrant.QueryPoints{
        CollectionName: r.collection,
        Query:          qdrant.NewQuery(embeddings[0]...),
        Limit:          qdrant.PtrOf(uint64(topK)),
        WithPayload:    qdrant.NewWithPayload(true),
    })
    if err != nil {
        return nil, err
    }

    // 3. 转换为 Document
    docs := make([]Document, 0, len(results))
    for _, p := range results {
        docs = append(docs, Document{
            Text:  p.Payload["text"].GetStringValue(),
            Score: p.Score,
        })
    }
    return docs, nil
}
```

### Day 5-7：整合到项目

**本周交付：**
- 实现 5 个 Tool（查订单、查物流、创建退款单、查 FAQ、转人工）
- Agent 能根据用户问题自动选择工具
- RAG 知识库问答可用
- 多个工具调用能并行执行

---

## Week 3：多模型路由 + 成本优化

### 学习目标
- 实现统一的 LLM Provider 抽象
- 接入 3+ 模型
- 实现路由器 + Fallback + 熔断器

### Day 1-2：多 Provider 接入

**学习内容：**
- 不同模型 API 的差异（Anthropic / DeepSeek / 豆包）
- 用接口抽象屏蔽差异
- HTTP Client 复用与连接池

**学习资料：**

| 资料 | 链接 |
|------|------|
| DeepSeek API | https://platform.deepseek.com/api-docs |
| 火山引擎(豆包) API | https://www.volcengine.com/docs/82379/1263482 |
| OpenAI API Compatible 调用 | DeepSeek 兼容 OpenAI 格式 |
| Model Comparison | https://artificialanalysis.ai/ |

**动手练习 —— Provider 抽象：**

```go
// internal/llm/provider.go
package llm

import "context"

// Provider 统一接口
type Provider interface {
    Name() string
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    ChatStream(ctx context.Context, req ChatRequest, onDelta func(string)) error
}

// internal/llm/providers/anthropic.go
type AnthropicProvider struct {
    client *anthropic.Client
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
    // 适配到 anthropic SDK
}

// internal/llm/providers/openai_compat.go
// DeepSeek、豆包等都兼容 OpenAI 协议，用一个 Provider 即可
type OpenAICompatProvider struct {
    baseURL string
    apiKey  string
    client  *http.Client
}

func (p *OpenAICompatProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
    // OpenAI 兼容协议实现
}

// 注册多个 Provider
func NewProviderRegistry(cfg Config) *ProviderRegistry {
    r := &ProviderRegistry{providers: map[string]Provider{}}
    r.Register("claude-sonnet", NewAnthropicProvider(cfg.AnthropicKey, "claude-sonnet-4-6"))
    r.Register("claude-haiku",  NewAnthropicProvider(cfg.AnthropicKey, "claude-haiku-4-5"))
    r.Register("deepseek-chat", NewOpenAICompatProvider("https://api.deepseek.com", cfg.DeepSeekKey, "deepseek-chat"))
    r.Register("doubao-pro",    NewOpenAICompatProvider("https://ark.cn-beijing.volces.com/api/v3", cfg.DoubaoKey, "doubao-pro-32k"))
    return r
}
```

### Day 3-4：路由器实现

**学习内容：**
- 路由策略（规则 → 复杂度评估）
- Context 超时与取消
- 熔断器模式（参考 sony/gobreaker）

**学习资料：**

| 资料 | 链接 |
|------|------|
| sony/gobreaker (熔断器) | https://github.com/sony/gobreaker |
| 路由设计参考 | docs/agent-engineering-guide.md (第2章) |
| Go context 最佳实践 | https://go.dev/blog/context |

**动手练习：**

```go
// internal/router/router.go
package router

import (
    "context"
    "strings"
)

type RoutingContext struct {
    TaskType       TaskType
    InputText      string
    InputTokens    int
    NeedsTools     bool
    UserTier       string
    LatencyBudget  time.Duration
}

type RoutingDecision struct {
    Model    string
    Fallback string
    Reason   string
    EstCost  float64
}

type Router struct {
    registry  *llm.ProviderRegistry
    breakers  map[string]*gobreaker.CircuitBreaker
    costTracker *CostTracker
}

func (r *Router) Route(ctx context.Context, rc RoutingContext) RoutingDecision {
    // 1. 硬约束过滤（上下文长度、延迟要求）
    candidates := r.filterByConstraints(rc)

    // 2. 规则路由
    if d, ok := r.ruleBasedRoute(rc, candidates); ok {
        return d
    }

    // 3. 复杂度评估
    complexity := r.assessComplexity(rc)
    return r.complexityBasedRoute(rc, complexity, candidates)
}

func (r *Router) assessComplexity(rc RoutingContext) string {
    score := 0
    if rc.InputTokens > 5000 { score += 2 }
    if rc.NeedsTools { score += 2 }

    signals := []string{"分析", "比较", "为什么", "设计", "方案"}
    for _, s := range signals {
        if strings.Contains(rc.InputText, s) { score++; break }
    }

    switch {
    case score >= 4: return "high"
    case score >= 2: return "medium"
    default:         return "low"
    }
}
```

```go
// internal/router/executor.go - 带熔断的执行器
type Executor struct {
    router   *Router
    breakers map[string]*gobreaker.CircuitBreaker
}

func (e *Executor) Execute(ctx context.Context, req llm.ChatRequest, rc RoutingContext) (*llm.ChatResponse, error) {
    decision := e.router.Route(ctx, rc)
    
    // 尝试主模型
    resp, err := e.tryWithBreaker(ctx, decision.Model, req)
    if err == nil {
        return resp, nil
    }

    // Fallback 到备选模型
    if decision.Fallback != "" {
        return e.tryWithBreaker(ctx, decision.Fallback, req)
    }
    return nil, err
}

func (e *Executor) tryWithBreaker(ctx context.Context, model string, req llm.ChatRequest) (*llm.ChatResponse, error) {
    breaker := e.getBreaker(model)
    result, err := breaker.Execute(func() (any, error) {
        provider, _ := e.router.registry.Get(model)
        return provider.Chat(ctx, req)
    })
    if err != nil {
        return nil, err
    }
    return result.(*llm.ChatResponse), nil
}

func (e *Executor) getBreaker(model string) *gobreaker.CircuitBreaker {
    if b, ok := e.breakers[model]; ok { return b }
    
    b := gobreaker.NewCircuitBreaker(gobreaker.Settings{
        Name:        model,
        MaxRequests: 3,
        Interval:    60 * time.Second,
        Timeout:     30 * time.Second,
        ReadyToTrip: func(c gobreaker.Counts) bool {
            return c.ConsecutiveFailures >= 5
        },
    })
    e.breakers[model] = b
    return b
}
```

### Day 5-7：Prompt Caching + 语义缓存

**学习内容：**
- Anthropic Prompt Caching 用法
- 用 Redis 做语义缓存
- 成本追踪与告警

**学习资料：**

| 资料 | 链接 |
|------|------|
| Anthropic Prompt Caching | https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching |
| Anthropic Batch API | https://docs.anthropic.com/en/docs/build-with-claude/batch-processing |
| Redis Vector Search | https://redis.io/docs/latest/develop/interact/search-and-query/advanced-concepts/vectors/ |

**动手练习：**

```go
// internal/cache/semantic.go - 语义缓存
type SemanticCache struct {
    redis    *redis.Client
    embedder *rag.Embedder
    threshold float32
}

func (c *SemanticCache) Get(ctx context.Context, query string) (string, bool) {
    embeddings, err := c.embedder.Embed(ctx, []string{query})
    if err != nil {
        return "", false
    }

    // 用 Redis Vector Search 找最相似的缓存
    cmd := c.redis.Do(ctx, "FT.SEARCH", "cache_idx",
        fmt.Sprintf("*=>[KNN 1 @embedding $vec AS score]"),
        "PARAMS", "2", "vec", floatToBytes(embeddings[0]),
        "DIALECT", "2")
    
    // 解析结果...
    if score >= c.threshold {
        return cachedResponse, true
    }
    return "", false
}
```

**本周交付：**
- 3 个模型接入完成
- 路由器能根据任务复杂度自动选模型
- 熔断 + Fallback 工作正常
- Prompt Caching 命中率可观察

---

## Week 4：Eval 体系搭建

### 学习目标
- 用 Go 搭建 Eval 框架
- 集成 Promptfoo（npm 工具）做高级评估
- CI 集成

### Day 1-2：Eval 概念

**学习内容：**
- Eval 定位与价值（参考 [agent-engineering-guide.md](docs/agent-engineering-guide.md) 第4章）
- Scorer 类型与适用场景
- LLM-as-Judge 方法

**学习资料：**

| 资料 | 链接 |
|------|------|
| Anthropic Eval 指南 | https://docs.anthropic.com/en/docs/build-with-claude/develop-tests |
| Hamel's Blog: LLM Eval | https://hamel.dev/blog/posts/evals/ |
| Eugene Yan: Eval 实践 | https://eugeneyan.com/writing/evals/ |
| Promptfoo 文档 | https://www.promptfoo.dev/docs/intro/ |
| Braintrust 文档 | https://www.braintrust.dev/docs |

### Day 3-5：用 Go 自建 Eval 框架

```go
// internal/eval/types.go
package eval

type Case struct {
    ID              string   `json:"id"`
    Input           string   `json:"input"`
    ExpectedExact   string   `json:"expected_exact,omitempty"`
    ExpectedContains []string `json:"expected_contains,omitempty"`
    ExpectedSchema  map[string]any `json:"expected_schema,omitempty"`
    ReferenceAnswer string   `json:"reference_answer,omitempty"`
    Tags            []string `json:"tags,omitempty"`
}

type Score struct {
    Value  float64
    Passed bool
    Reason string
}

type Scorer interface {
    Name() string
    Weight() float64
    Score(output string, c Case) Score
}

// internal/eval/scorers/exact_match.go
type ExactMatch struct{ weight float64 }

func (s *ExactMatch) Name() string { return "exact_match" }
func (s *ExactMatch) Weight() float64 { return s.weight }

func (s *ExactMatch) Score(output string, c Case) Score {
    if c.ExpectedExact == "" {
        return Score{Value: 1.0, Passed: true}
    }
    matched := strings.TrimSpace(strings.ToLower(output)) == 
               strings.TrimSpace(strings.ToLower(c.ExpectedExact))
    return Score{
        Value:  boolToFloat(matched),
        Passed: matched,
        Reason: fmt.Sprintf("expected=%q got=%q", c.ExpectedExact, output[:min(50, len(output))]),
    }
}
```

```go
// internal/eval/runner.go
package eval

import (
    "context"
    "sync"
)

type Runner struct {
    llm     *llm.Client
    scorers []Scorer
    concurrency int
}

type CaseResult struct {
    CaseID       string
    Output       string
    Scores       map[string]Score
    OverallScore float64
    Passed       bool
    LatencyMs    int64
    Tokens       int64
}

type RunResult struct {
    RunID         string
    Results       []CaseResult
    OverallScore  float64
    PassRate      float64
    TotalCases    int
    PassedCases   int
}

func (r *Runner) Run(ctx context.Context, cases []Case, promptTemplate string, model string) (*RunResult, error) {
    sem := make(chan struct{}, r.concurrency)
    results := make([]CaseResult, len(cases))
    var wg sync.WaitGroup

    for i, c := range cases {
        wg.Add(1)
        sem <- struct{}{}
        go func(idx int, c Case) {
            defer wg.Done()
            defer func() { <-sem }()
            results[idx] = r.runSingle(ctx, c, promptTemplate, model)
        }(i, c)
    }
    wg.Wait()

    return r.aggregate(results), nil
}

func (r *Runner) runSingle(ctx context.Context, c Case, promptTpl string, model string) CaseResult {
    start := time.Now()
    
    // 渲染 prompt 并调用 LLM
    prompt := renderTemplate(promptTpl, c.Input)
    resp, err := r.llm.Chat(ctx, llm.ChatRequest{
        Model: model,
        Messages: []llm.Message{{Role: "user", Content: prompt}},
    })

    output := ""
    if err != nil {
        output = fmt.Sprintf("[ERROR] %v", err)
    } else {
        output = resp.Content
    }

    // 用所有 scorer 打分
    scores := map[string]Score{}
    var weightedSum, totalWeight float64
    for _, scorer := range r.scorers {
        s := scorer.Score(output, c)
        scores[scorer.Name()] = s
        weightedSum += s.Value * scorer.Weight()
        totalWeight += scorer.Weight()
    }

    overall := weightedSum / totalWeight
    return CaseResult{
        CaseID:       c.ID,
        Output:       output,
        Scores:       scores,
        OverallScore: overall,
        Passed:       overall >= 0.7,
        LatencyMs:    time.Since(start).Milliseconds(),
        Tokens:       resp.InputTokens + resp.OutputTokens,
    }
}
```

```go
// cmd/eval/main.go - Eval CLI
func main() {
    var (
        configPath    = flag.String("config", "eval/configs/intent.yaml", "eval config path")
        saveBaseline  = flag.Bool("save-baseline", false, "save current as baseline")
        compareBase   = flag.Bool("compare-baseline", false, "compare with baseline")
        failOnRegression = flag.Bool("fail-on-regression", false, "exit 1 if regressed")
    )
    flag.Parse()

    cfg := loadConfig(*configPath)
    cases := loadDataset(cfg.DatasetPath)
    
    runner := eval.NewRunner(/*...*/)
    result, _ := runner.Run(context.Background(), cases, cfg.Prompt, cfg.Model)
    
    printSummary(result)

    if *compareBase {
        cmp := compareBaseline(cfg.PromptKey, result)
        fmt.Println(cmp.Message)
        if *failOnRegression && !cmp.Passed {
            os.Exit(1)
        }
    }

    if *saveBaseline {
        saveBaselineFile(cfg.PromptKey, result)
    }
}
```

### Day 6-7：Promptfoo 集成 + CI

**为什么混用 Go + Promptfoo：**
- Go 自建 eval：业务集成测试（带工具、带 RAG 的完整 Agent Loop）
- Promptfoo：纯 prompt 评估，npm 工具开箱即用 LLM-as-Judge、A/B 对比

```yaml
# promptfooconfig.yaml
description: "客服意图分类 Eval"

prompts:
  - file://prompts/intent/v2.tmpl

providers:
  - id: anthropic:messages:claude-haiku-4-5
  - id: deepseek:chat

tests:
  - vars:
      user_input: "我要退货"
    assert:
      - type: equals
        value: "refund"
      - type: cost
        threshold: 0.001
```

```yaml
# .github/workflows/prompt-eval.yml
name: Prompt Eval

on:
  pull_request:
    paths: ['prompts/**', 'eval/**']

jobs:
  go-eval:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      
      - name: Run Go Eval (集成测试)
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        run: |
          go build -o bin/eval ./cmd/eval
          ./bin/eval -compare-baseline -fail-on-regression

  promptfoo-eval:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: '20' }
      
      - run: npm install -g promptfoo
      
      - name: Run Promptfoo (纯 prompt)
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
        run: promptfoo eval --ci --fail-on-regression
```

**本周交付：**
- 自建 eval 框架可用
- Promptfoo 配置完成
- CI pipeline 跑通
- 50+ eval case 覆盖核心场景

---

## Week 5：安全审核 + 可观测性

### 学习目标
- 实现安全审核管道
- OpenTelemetry 追踪 + LangFuse
- Prometheus 指标

### Day 1-2：安全审核

**学习内容：**
- Prompt Injection 攻击与防御
- PII 脱敏
- 输出过滤

**学习资料：**

| 资料 | 链接 |
|------|------|
| OWASP Top 10 for LLM | https://owasp.org/www-project-top-10-for-large-language-model-applications/ |
| Anthropic Safety Guide | https://docs.anthropic.com/en/docs/build-with-claude/guardrails |
| Prompt Injection 论文集 | https://github.com/jthack/PIPE |

```go
// internal/safety/pipeline.go
type Pipeline struct {
    inputCheckers  []Checker
    outputCheckers []Checker
}

type Checker interface {
    Name() string
    Check(ctx context.Context, text string) CheckResult
}

type CheckResult struct {
    Passed bool
    Reason string
    Severity Severity  // info / warn / block
}

// 并行执行所有检查（Go 优势）
func (p *Pipeline) CheckInput(ctx context.Context, text string) Result {
    results := make([]CheckResult, len(p.inputCheckers))
    var wg sync.WaitGroup
    for i, c := range p.inputCheckers {
        wg.Add(1)
        go func(idx int, checker Checker) {
            defer wg.Done()
            results[idx] = checker.Check(ctx, text)
        }(i, c)
    }
    wg.Wait()
    return mergeResults(results)
}
```

```go
// internal/safety/injection_guard.go
type InjectionGuard struct {
    patterns []*regexp.Regexp
    llm      *llm.Client  // 备选：用小模型判断
}

func (g *InjectionGuard) Check(ctx context.Context, text string) CheckResult {
    // Layer 1: 正则规则
    for _, p := range g.patterns {
        if p.MatchString(text) {
            return CheckResult{
                Passed: false,
                Reason: "injection pattern detected: " + p.String(),
                Severity: SeverityBlock,
            }
        }
    }

    // Layer 2: LLM 判断（异步、可选）
    // ...
    return CheckResult{Passed: true}
}

var injectionPatterns = []string{
    `(?i)ignore (?:all )?(?:previous|above) instructions`,
    `忽略(?:以上|之前|所有)(?:的)?(?:指令|要求|规则)`,
    `(?i)system:\s*`,
    `(?i)(?:pretend|act|roleplay)\s+(?:as|like)`,
}
```

### Day 3-4：可观测性

**学习内容：**
- OpenTelemetry Go SDK
- LangFuse Go 集成（用 HTTP API 或社区 SDK）
- Prometheus 指标

**学习资料：**

| 资料 | 链接 |
|------|------|
| OpenTelemetry Go | https://opentelemetry.io/docs/languages/go/ |
| LangFuse 文档 | https://langfuse.com/docs |
| LangFuse Go SDK (社区) | https://github.com/henomis/langfuse-go |
| Prometheus Go Client | https://github.com/prometheus/client_golang |

```go
// internal/observability/metrics.go
package observability

import "github.com/prometheus/client_golang/prometheus"

var (
    LLMRequestsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "llm_requests_total",
            Help: "Total LLM requests",
        },
        []string{"model", "status"},
    )

    LLMTokensTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "llm_tokens_total",
            Help: "Total tokens consumed",
        },
        []string{"model", "type"}, // type: input/output/cache_read
    )

    LLMLatencySeconds = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "llm_latency_seconds",
            Help:    "LLM call latency",
            Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30},
        },
        []string{"model"},
    )

    LLMCostUSD = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "llm_cost_usd_total",
            Help: "Total LLM cost in USD",
        },
        []string{"model"},
    )
)

func init() {
    prometheus.MustRegister(LLMRequestsTotal, LLMTokensTotal, LLMLatencySeconds, LLMCostUSD)
}
```

```go
// internal/observability/tracer.go - OpenTelemetry 追踪
func TraceLLMCall(ctx context.Context, model string, fn func(ctx context.Context) (*llm.ChatResponse, error)) (*llm.ChatResponse, error) {
    tracer := otel.Tracer("agent")
    ctx, span := tracer.Start(ctx, "llm.chat",
        trace.WithAttributes(
            attribute.String("llm.model", model),
        ),
    )
    defer span.End()

    start := time.Now()
    resp, err := fn(ctx)
    duration := time.Since(start)

    // 记录到 span
    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
    } else {
        span.SetAttributes(
            attribute.Int64("llm.input_tokens", resp.InputTokens),
            attribute.Int64("llm.output_tokens", resp.OutputTokens),
            attribute.Float64("llm.latency_ms", float64(duration.Milliseconds())),
        )
    }

    // 记录到 Prometheus
    status := "success"
    if err != nil { status = "error" }
    LLMRequestsTotal.WithLabelValues(model, status).Inc()
    LLMLatencySeconds.WithLabelValues(model).Observe(duration.Seconds())
    if resp != nil {
        LLMTokensTotal.WithLabelValues(model, "input").Add(float64(resp.InputTokens))
        LLMTokensTotal.WithLabelValues(model, "output").Add(float64(resp.OutputTokens))
    }

    return resp, err
}
```

### Day 5-7：异常处理完善

**学习内容：**
- 重试策略（cenkalti/backoff）
- Context 超时控制
- 优雅降级

**学习资料：**

| 资料 | 链接 |
|------|------|
| cenkalti/backoff (重试库) | https://github.com/cenkalti/backoff |
| Go Context 实践 | https://go.dev/blog/context |
| Effective Error Handling | https://go.dev/blog/error-handling-and-go |

```go
// internal/llm/retry.go
import "github.com/cenkalti/backoff/v4"

func WithRetry(fn func() error) error {
    b := backoff.NewExponentialBackOff()
    b.MaxElapsedTime = 30 * time.Second
    b.InitialInterval = 500 * time.Millisecond
    b.Multiplier = 2

    return backoff.Retry(func() error {
        err := fn()
        if err == nil { return nil }

        // 不可重试的错误直接返回
        if isNonRetryable(err) {
            return backoff.Permanent(err)
        }
        return err
    }, b)
}

func isNonRetryable(err error) bool {
    var apiErr *anthropic.Error
    if errors.As(err, &apiErr) {
        // 4xx 错误不重试（除了 429）
        if apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 && apiErr.StatusCode != 429 {
            return true
        }
    }
    return false
}
```

**本周交付：**
- 安全管道拦截注入攻击
- Prometheus 指标接入
- OpenTelemetry trace 接入 Jaeger/LangFuse
- 异常自动重试与降级

---

## Week 6：生产部署 + 端到端验证

### 学习目标
- Docker + K8s 部署
- 性能压测与优化
- 完整 Demo

### Day 1-2：部署

**学习内容：**
- Multi-stage Dockerfile
- 健康检查与优雅关闭
- K8s Deployment / HPA

```dockerfile
# Dockerfile - 多阶段构建
FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/agent ./cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/agent /agent
EXPOSE 8080
ENTRYPOINT ["/agent"]
```

```go
// cmd/server/main.go - 优雅关闭
func main() {
    cfg := config.Load()
    srv := newServer(cfg)

    go func() {
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatal(err)
        }
    }()

    // 监听信号
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    log.Println("shutting down...")
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // 等待进行中的 LLM 调用完成
    if err := srv.Shutdown(ctx); err != nil {
        log.Fatal(err)
    }
}
```

### Day 3-4：压测 + 优化

**学习内容：**
- vegeta / hey 压测工具
- pprof 性能分析
- Goroutine 池（panjf2000/ants）

```bash
# 压测命令
echo "POST http://localhost:8080/chat
Content-Type: application/json
@payload.json" | vegeta attack -duration=60s -rate=50 | vegeta report
```

```go
// 用 ants 限制并发，避免打爆下游 LLM
import "github.com/panjf2000/ants/v2"

var llmPool, _ = ants.NewPool(100)  // 最多 100 个并发 LLM 调用

func handleChat(c *gin.Context) {
    done := make(chan struct{})
    var result *llm.ChatResponse
    var err error

    _ = llmPool.Submit(func() {
        defer close(done)
        result, err = agentLoop.Run(c.Request.Context(), req.Message)
    })

    select {
    case <-done:
        // ...
    case <-c.Request.Context().Done():
        c.JSON(504, gin.H{"error": "timeout"})
    }
}
```

### Day 5-7：端到端验证

**最终项目结构（Go 版）：**

```
smart-cs-agent/
├── cmd/
│   ├── server/main.go             # HTTP 服务入口
│   └── eval/main.go               # Eval CLI
├── internal/
│   ├── api/
│   │   ├── handler/
│   │   │   ├── chat.go
│   │   │   └── admin.go
│   │   ├── middleware/
│   │   └── router.go
│   ├── agent/
│   │   ├── loop.go                # Agent Loop
│   │   ├── tool.go                # Tool 接口
│   │   ├── registry.go            # Tool 注册中心
│   │   └── tools/                 # 具体工具实现
│   │       ├── query_order.go
│   │       ├── query_logistics.go
│   │       ├── create_refund.go
│   │       └── search_kb.go
│   ├── llm/
│   │   ├── client.go
│   │   ├── provider.go            # Provider 接口
│   │   ├── providers/
│   │   │   ├── anthropic.go
│   │   │   └── openai_compat.go
│   │   └── retry.go
│   ├── router/
│   │   ├── router.go              # 路由器
│   │   ├── executor.go            # 带熔断的执行器
│   │   └── cost.go                # 成本追踪
│   ├── rag/
│   │   ├── embedder.go
│   │   ├── retriever.go
│   │   ├── indexer.go
│   │   └── splitter.go            # 文档切分
│   ├── prompt/
│   │   ├── engine.go              # text/template 引擎
│   │   └── loader.go              # YAML 加载
│   ├── cache/
│   │   ├── semantic.go            # 语义缓存
│   │   └── prompt_cache.go        # Anthropic Prompt Cache
│   ├── safety/
│   │   ├── pipeline.go
│   │   ├── injection_guard.go
│   │   ├── pii_detector.go
│   │   └── output_filter.go
│   ├── observability/
│   │   ├── metrics.go             # Prometheus
│   │   ├── tracer.go              # OpenTelemetry
│   │   └── langfuse.go            # LangFuse 集成
│   ├── session/
│   │   └── memory.go              # Redis 会话
│   ├── eval/
│   │   ├── runner.go
│   │   ├── scorers/
│   │   │   ├── exact_match.go
│   │   │   ├── contains.go
│   │   │   └── llm_judge.go
│   │   ├── dataset.go
│   │   └── baseline.go
│   └── config/
│       └── config.go
│
├── prompts/                       # Prompt 模板（YAML）
│   └── customer_service/
│       ├── intent/v2.yaml
│       └── rag_qa/v1.yaml
│
├── eval/                          # Eval 数据
│   ├── datasets/
│   │   ├── intent.jsonl
│   │   └── rag_qa.jsonl
│   ├── baselines/
│   └── configs/
│
├── deployments/
│   ├── docker-compose.yml         # 本地：Redis + Qdrant + Jaeger + LangFuse
│   ├── Dockerfile
│   └── k8s/
│       ├── deployment.yaml
│       ├── service.yaml
│       └── hpa.yaml
│
├── .github/workflows/
│   ├── ci.yml
│   ├── prompt-eval.yml
│   └── deploy.yml
│
├── promptfooconfig.yaml
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

**验证清单：**

```
功能验证：
  □ 多轮对话 + 流式输出
  □ 5+ 工具能并行调用
  □ RAG 知识库问答
  □ 模型路由按预期工作
  □ 注入攻击被拦截
  
性能验证：
  □ P95 延迟 < 3s
  □ 单实例 50 QPS 稳定
  □ goroutine 泄漏检查通过（用 pprof）
  □ 内存占用稳定（无累积）

质量验证：
  □ Eval 整体分数 > 85%
  □ Prompt 改动 CI 自动验证
  
工程验证：
  □ Docker 镜像 < 30MB
  □ K8s 部署成功
  □ Prometheus 指标可见
  □ Jaeger 链路可查
  □ 优雅关闭工作正常
```

---

## Go 生态学习资源补充

### Go + AI 相关库速查

| 用途 | 库 | 备注 |
|------|----|------|
| Anthropic SDK | github.com/anthropics/anthropic-sdk-go | 官方 |
| OpenAI SDK | github.com/sashabaranov/go-openai | 社区主流 |
| Qdrant | github.com/qdrant/go-client | 官方 |
| Milvus | github.com/milvus-io/milvus-sdk-go | 官方 |
| LangChain Go | github.com/tmc/langchaingo | 社区，对标 Python LangChain |
| LangFuse Go | github.com/henomis/langfuse-go | 社区 |
| Eino (字节) | github.com/cloudwego/eino | 国内大厂，Agent 框架 |
| 熔断器 | github.com/sony/gobreaker | 经典选择 |
| 重试 | github.com/cenkalti/backoff | 经典选择 |
| 协程池 | github.com/panjf2000/ants | 高性能 |

### Go Agent 项目参考

| 项目 | 学什么 | GitHub |
|------|--------|--------|
| Eino | 字节开源 Agent 框架的设计思路 | cloudwego/eino |
| langchaingo | LangChain Go 实现 | tmc/langchaingo |
| Ollama | 本地 LLM 服务（Go 写的） | ollama/ollama |
| Coze Studio | 字节 Coze 开源版（含 Go 服务） | coze-dev/coze-studio |
| go-openai 示例 | tools / streaming / functions 示例 | sashabaranov/go-openai/examples |

### 课程（语言无关，重点看思路）

| 课程 | 平台 | 时长 |
|------|------|------|
| Building AI Agents | DeepLearning.AI | 2h |
| Function Calling | DeepLearning.AI | 1h |
| Building Systems with ChatGPT | DeepLearning.AI | 1h |
| Building RAG Apps | DeepLearning.AI | 1h |
| Evaluating Generative AI | DeepLearning.AI | 1h |

> 课程虽然是 Python，但**核心思想都是语言无关的**。看完后用 Go 重写一遍才是真正学会。

### 必读文章（不分语言）

| 文章 | 链接 |
|------|------|
| Building effective agents (Anthropic) | https://docs.anthropic.com/en/docs/build-with-claude/agentic-systems |
| Patterns for Building LLM Systems | https://eugeneyan.com/writing/llm-patterns/ |
| What We Learned from a Year with LLMs | https://www.oreilly.com/radar/what-we-learned-from-a-year-of-building-with-llms/ |
| Compound AI Systems | https://bair.berkeley.edu/blog/2024/02/18/compound-ai-systems/ |

---

## Go vs Python 在 Agent 开发上的取舍

| 场景 | 推荐 | 原因 |
|------|------|------|
| 生产 Agent 服务 | **Go** | 性能、并发、部署、与微服务集成 |
| 数据预处理、批量 eval | Python | 生态丰富（pandas、sklearn） |
| Embedding 模型本地推理 | Python | PyTorch / sentence-transformers |
| 调外部 LLM API | **Go** | HTTP/JSON 而已，Go 完全够用 |
| 复杂 RAG（reranker、混合检索） | Python 略优 | 但 Qdrant、Weaviate 都有 Go SDK |
| 多 Agent 协作 | **Go** | goroutine 天然合适 |
| 算法原型验证 | Python | 迭代速度快 |
| 大规模并发处理 | **Go** | 内存效率高 10 倍 |

**推荐架构：Go 主服务 + Python 辅助脚本**
```
Go (主服务，对外提供 API)
  ├─ Agent Loop / Tool Use / Router  ← Go 实现
  ├─ Prometheus / OTel / LangFuse    ← Go 实现
  │
  └─ 通过 HTTP / gRPC 调用：
      └─ Python 服务（可选）
          ├─ 复杂 reranker
          ├─ 本地 embedding 服务
          └─ 离线 eval 大规模跑批
```

---

## 关键里程碑

| 周 | 里程碑 | 验证方式 |
|----|--------|---------|
| W1 | Gin 服务 + LLM 对话 + SSE 流式 | `curl /chat/stream` |
| W2 | 5 个 Tool 并行调用 + RAG 问答 | 跑 5 个场景手动验证 |
| W3 | 3 个模型路由 + 熔断 + 成本追踪 | 看 Prometheus 模型分布 |
| W4 | Go Eval CLI + Promptfoo + CI | PR 触发自动 eval |
| W5 | 注入拦截 + OTel trace + 优雅关闭 | 安全测试集 + Jaeger UI |
| W6 | Docker + K8s + 50 QPS 压测通过 | vegeta report + pprof |

---

## 最后的建议

1. **Go 的优势在工程，不在算法**。AI 算法/模型相关的概念学习（prompt、eval、RAG）和语言无关，看 Python 资料也没问题，但**落地用 Go**。

2. **不要重复造轮子**。如果只是简单 Chat Bot，用现有的 Dify/FastGPT 接 API 就够。**用 Go 自己写**是为了：① 学习 ② 性能 ③ 深度定制。

3. **借鉴 Eino**。字节的 [cloudwego/eino](https://github.com/cloudwego/eino) 是国内 Go 圈最成熟的 Agent 框架，源码值得读，能少走很多弯路。

4. **混合架构是常态**。不要纠结"全 Go 还是全 Python"，主服务 Go、离线脚本/eval/原型 Python，是工业界的常见组合。

5. **每周交付一个能跑的东西**。Agent 开发最快的进步方式就是：搭出来 → 用 eval 验证 → 改进 → 再验证。
