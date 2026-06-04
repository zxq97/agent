# Phase 1: 基础骨架 (Foundation)

## 目标

搭建可运行的 Agent 骨架，能进行基本的多轮对话。本阶段不涉及具体业务 Skill，重点验证架构的可行性。

## 前置条件

- Go 1.22+ 已安装
- Claude API Key 已获取
- Redis 已部署（用于会话存储）

## 交付物清单

| # | 交付物 | 说明 |
|---|--------|------|
| 1 | Go Module 初始化 | `go.mod`, 依赖管理 |
| 2 | 项目目录结构 | 按 CLAUDE.md 定义的结构创建 |
| 3 | Config 模块 | Viper 加载配置 |
| 4 | LLM Provider | Claude API 对接，支持流式 |
| 5 | Session Manager | 会话创建/加载/保存（Redis） |
| 6 | Agent Core | 对话编排、消息构建 |
| 7 | Tool Framework | Tool 接口、注册表、执行器 |
| 8 | Skill Framework | Skill 接口、路由器 |
| 9 | HTTP Server | REST + SSE 端点 |
| 10 | 集成测试 | 端到端对话测试 |

## 详细步骤

### Step 1.1: 初始化 Go Module

```bash
cd /Users/didi/vscode/agent
go mod init github.com/didi/agent
```

创建目录结构：

```
internal/
├── agent/
│   ├── agent.go           # Agent 核心结构
│   ├── orchestrator.go    # 对话编排器
│   └── router.go          # Skill 路由器
├── skill/
│   ├── skill.go           # Skill 接口定义
│   └── default/
│       └── default.go     # 默认 Skill（通用对话）
├── tool/
│   ├── tool.go            # Tool 接口定义
│   └── registry.go        # Tool 注册表
├── dialogue/
│   ├── session.go         # Session 模型
│   └── manager.go         # Session 管理器
├── llm/
│   ├── provider.go        # Provider 接口
│   └── claude/
│       ├── claude.go      # Claude 实现
│       └── stream.go      # 流式处理
├── config/
│   └── config.go          # 配置加载
└── server/
    ├── server.go           # HTTP Server
    ├── handler.go          # 请求处理器
    └── middleware.go       # 中间件
```

### Step 1.2: Config 模块

**文件**: `internal/config/config.go`

配置结构：

```go
type Config struct {
    Server   ServerConfig   `mapstructure:"server"`
    LLM      LLMConfig      `mapstructure:"llm"`
    Redis    RedisConfig    `mapstructure:"redis"`
    Session  SessionConfig  `mapstructure:"session"`
}

type ServerConfig struct {
    Port         int           `mapstructure:"port"`
    ReadTimeout  time.Duration `mapstructure:"read_timeout"`
    WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type LLMConfig struct {
    Provider     string        `mapstructure:"provider"`    // "claude"
    APIKey       string        `mapstructure:"api_key"`
    Model        string        `mapstructure:"model"`       // "claude-sonnet-4-20250514"
    BaseURL      string        `mapstructure:"base_url"`
    MaxTokens    int           `mapstructure:"max_tokens"`
    Temperature  float64       `mapstructure:"temperature"`
}

type RedisConfig struct {
    Addr     string `mapstructure:"addr"`
    Password string `mapstructure:"password"`
    DB       int    `mapstructure:"db"`
}

type SessionConfig struct {
    TTL time.Duration `mapstructure:"ttl"`  // 默认 30min
}
```

配置文件 `configs/config.yaml`：

```yaml
server:
  port: 8080
  read_timeout: 30s
  write_timeout: 60s

llm:
  provider: claude
  api_key: ${CLAUDE_API_KEY}
  model: claude-sonnet-4-20250514
  base_url: https://api.anthropic.com
  max_tokens: 4096
  temperature: 0.7

redis:
  addr: localhost:6379
  password: ""
  db: 0

session:
  ttl: 30m
```

### Step 1.3: LLM Provider 实现

**文件**: `internal/llm/provider.go`

```go
// Provider LLM 提供者接口
type Provider interface {
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    ChatStream(ctx context.Context, req *ChatRequest) (<-chan StreamEvent, error)
}
```

**文件**: `internal/llm/claude/claude.go`

使用 `github.com/anthropics/anthropic-sdk-go` 实现 Provider 接口。

关键点：
- 支持 Tool Use（function calling）
- 流式响应使用 SSE 解析
- 错误重试：超时重试 1 次，限流指数退避
- 请求/响应日志记录

### Step 1.4: Session Manager

**文件**: `internal/dialogue/session.go`

```go
type Session struct {
    ID        string         `json:"id"`
    UserID    string         `json:"user_id"`
    Messages  []Message      `json:"messages"`
    State     DialogueState  `json:"state"`
    CreatedAt time.Time      `json:"created_at"`
    UpdatedAt time.Time      `json:"updated_at"`
}

type DialogueState struct {
    Phase    string            `json:"phase"`     // idle / skill_active / tool_calling
    Skill    string            `json:"skill"`     // 当前活跃 Skill 名
    Intent   string            `json:"intent"`    // 当前意图
    Slots    map[string]string `json:"slots"`     // 意图槽位
    TurnCount int             `json:"turn_count"` // 当前轮次
}
```

**文件**: `internal/dialogue/manager.go`

```go
type Manager interface {
    Create(ctx context.Context, userID string) (*Session, error)
    Get(ctx context.Context, sessionID string) (*Session, error)
    Save(ctx context.Context, session *Session) error
    Delete(ctx context.Context, sessionID string) error
}
```

Redis 实现：
- Key: `session:{id}`
- 序列化: JSON
- TTL: 可配置（默认 30 分钟，每次 Save 续期）

### Step 1.5: Tool Framework

**文件**: `internal/tool/tool.go`

```go
// Tool 工具接口
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]any  // JSON Schema
    Execute(ctx context.Context, params map[string]any, session *dialogue.Session) (*ToolResult, error)
}

type ToolResult struct {
    Success bool   `json:"success"`
    Content string `json:"content"`  // 供 LLM 消费的文本
    Data    any    `json:"data"`     // 结构化数据（可选）
    Error   string `json:"error,omitempty"`
}
```

**文件**: `internal/tool/registry.go`

```go
type Registry struct {
    tools map[string]Tool
}

func (r *Registry) Register(tool Tool)
func (r *Registry) Get(name string) (Tool, bool)
func (r *Registry) List() []Tool
func (r *Registry) ToolsForSkill(skillName string) []Tool  // 返回 Skill 可用的 Tools
```

### Step 1.6: Skill Framework

**文件**: `internal/skill/skill.go`

```go
type Skill interface {
    Name() string
    Description() string          // 用于意图路由时的描述
    SystemPrompt() string         // 该 Skill 的 System Prompt
    Tools() []tool.Tool           // 该 Skill 可用的 Tools
    CanHandle(query string, state *dialogue.DialogueState) bool  // 上下文延续判断
}
```

**文件**: `internal/skill/default/default.go`

默认 Skill —— 处理问候和通用问题：

```go
// SystemPrompt 返回默认 Skill 的系统提示词
// 问候、简单问答、无法识别意图时的兜底
```

### Step 1.7: Agent Orchestrator

**文件**: `internal/agent/orchestrator.go`

核心编排逻辑：

```go
func (o *Orchestrator) HandleMessage(ctx context.Context, session *dialogue.Session, userMsg string) (<-chan StreamChunk, error) {
    // 1. 将用户消息追加到 Session
    session.AddMessage("user", userMsg)

    // 2. 意图检测 + Skill 路由（首次或意图切换时）
    skill := o.router.Route(session, userMsg)

    // 3. 更新 Session State
    session.State.Skill = skill.Name()

    // 4. 构建 LLM 请求
    req := o.buildRequest(session, skill)

    // 5. 调用 LLM（流式）
    stream, err := o.provider.ChatStream(ctx, req)

    // 6. 处理流式响应
    //    - content_delta: 直接转发
    //    - tool_use: 暂存，收集完参数后执行 Tool
    //    - Tool 执行完毕后，将结果追加到消息，再次调用 LLM
    //    - 重复直到 LLM 返回 end_turn

    // 7. 保存 Session
    o.sessionMgr.Save(ctx, session)

    // 8. 返回流式通道
    return outputCh, nil
}
```

### Step 1.8: HTTP Server

**文件**: `internal/server/handler.go`

```go
// POST /api/v1/chat/stream
func (h *Handler) ChatStream(w http.ResponseWriter, r *http.Request) {
    // 1. 解析请求
    var req ChatRequest
    json.NewDecoder(r.Body).Decode(&req)

    // 2. 加载/创建 Session
    session, _ := h.sessionMgr.GetOrCreate(r.Context(), req.SessionID, req.UserID)

    // 3. 调用 Orchestrator
    stream, err := h.orchestrator.HandleMessage(r.Context(), session, req.Message)

    // 4. SSE 写入
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    flusher := w.(http.Flusher)
    for chunk := range stream {
        // 写入 SSE 事件
        fmt.Fprintf(w, "event: %s\ndata: %s\n\n", chunk.Type, chunk.Data)
        flusher.Flush()
    }
}
```

### Step 1.9: 主入口

**文件**: `cmd/http/main.go`

```go
func main() {
    // 1. 加载配置
    cfg := config.Load()

    // 2. 初始化依赖
    redisClient := ...
    sessionMgr := dialogue.NewRedisManager(redisClient, cfg.Session)
    llmProvider := claude.NewProvider(cfg.LLM)
    toolRegistry := tool.NewRegistry()
    skillRouter := agent.NewRouter(/* 注册 Skills */)
    orchestrator := agent.NewOrchestrator(llmProvider, sessionMgr, toolRegistry, skillRouter)

    // 3. 启动 HTTP Server
    server := server.New(cfg.Server, orchestrator, sessionMgr)
    server.Run()
}
```

### Step 1.10: 集成测试

测试场景：
1. 创建新会话 → 发送问候 → 获得友好回复
2. 多轮对话 → 上下文保持
3. 流式响应 → SSE 事件正确
4. Tool 调用 → Mock Tool 被正确调用
5. 会话超时 → 自动清理

## 依赖列表

```
github.com/anthropics/anthropic-sdk-go   # Claude SDK
github.com/redis/go-redis/v9             # Redis 客户端
github.com/spf13/viper                   # 配置管理
go.uber.org/zap                          # 结构化日志
github.com/google/uuid                   # UUID 生成
github.com/stretchr/testify              # 测试断言
```

## 验收标准

- [ ] `go build ./...` 编译通过
- [ ] `go test ./...` 测试通过
- [ ] 启动服务后，通过 curl 发送消息能获得流式回复
- [ ] 多轮对话上下文保持正确
- [ ] Mock Tool 能被正确调用
- [ ] Session 在 Redis 中正确存储和读取
