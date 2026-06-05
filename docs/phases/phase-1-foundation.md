# Phase 1: 基础骨架 (Foundation)

## 目标

搭建可运行的多 Agent 骨架，实现 CLI 终端交互，能通过 Orchestrator Agent 进行单轮/多轮对话，并能调用 tyche MCP 工具获取真实数据。

## 前置条件

- Go 1.22+ 已安装
- DeepSeek API Key 已获取（`deepseek-chat` 模型，支持 function calling）
- tyche MCP Server 可访问（需要 Bearer token + 白名单手机号）

## 交付物清单

| # | 交付物 | 说明 |
|---|--------|------|
| 1 | Go Module + 目录结构 | `go.mod`，完整项目骨架 |
| 2 | Config 模块 | Viper 加载 YAML 配置 |
| 3 | LLM Provider | eino ChatModel 接入（DeepSeek） |
| 4 | MCP HTTP Client | 封装 tyche MCP 为 eino Tool |
| 5 | Orchestrator Agent | 意图识别 + Agent 路由 |
| 6 | Tool Registry | 统一 Tool 注册与查找 |
| 7 | Session Manager | 内存会话管理（先不引入 Redis） |
| 8 | CLI 入口 | `cmd/cli/main.go`，终端交互循环 |
| 9 | 端到端测试 | CLI 发起对话 → Agent 调用 MCP → 返回结果 |

---

## Step 1.1: 初始化 Go Module 和目录结构

```bash
cd /Users/didi/vscode/agent
go mod init github.com/zxq97/agent
```

目标目录结构：

```
agent/
├── cmd/
│   └── cli/
│       └── main.go                    # CLI 入口
├── internal/
│   ├── agent/
│   │   ├── orchestrator.go            # Orchestrator Agent（主 Agent）
│   │   └── router.go                  # Agent 路由逻辑
│   ├── skill/
│   │   ├── registry.go               # Skill Agent 统一注册表（插件化）
│   │   ├── interface.go              # SkillAgent 接口定义
│   │   ├── vehicle/
│   │   │   └── agent.go              # Vehicle Agent
│   │   ├── insurance/
│   │   │   └── agent.go              # Insurance Agent
│   │   ├── billing/
│   │   │   └── agent.go              # Billing Agent
│   │   └── fulfillment/
│   │       └── agent.go              # Fulfillment Agent
│   ├── tool/
│   │   ├── registry.go               # Tool 注册表 + ToolProvider 接口
│   │   └── mcp/
│   │       ├── client.go             # MCP HTTP Client
│   │       ├── tools.go              # MCP Tool 定义
│   │       └── types.go              # MCP 请求/响应类型
│   ├── llm/
│   │   └── provider.go              # LLM Provider 初始化
│   ├── session/
│   │   ├── session.go               # Session 模型
│   │   └── manager.go               # 内存 Session 管理
│   └── config/
│       └── config.go                # 配置结构 + 加载
├── configs/
│   └── config.yaml                  # 配置文件模板
├── knowledge/                        # 知识库（静态 JSON/Markdown）
│   ├── vehicle/
│   ├── insurance/
│   ├── billing/
│   └── fulfillment/
├── go.mod
├── go.sum
├── CLAUDE.md
└── README.md
```

### 验证标准
- `go build ./...` 编译通过
- 目录结构完整，所有 `.go` 文件有 package 声明

---

## Step 1.2: 配置模块

**文件**: `internal/config/config.go`

```go
type Config struct {
    LLM    LLMConfig    `mapstructure:"llm"`
    MCP    MCPConfig    `mapstructure:"mcp"`
    Agent  AgentConfig  `mapstructure:"agent"`
}

type LLMConfig struct {
    Provider    string  `mapstructure:"provider"`     // "deepseek"
    APIKey      string  `mapstructure:"api_key"`
    BaseURL     string  `mapstructure:"base_url"`     // DeepSeek: https://api.deepseek.com
    Model       string  `mapstructure:"model"`        // "deepseek-chat" (V3, 支持 function calling)
    MaxTokens   int     `mapstructure:"max_tokens"`
    Temperature float64 `mapstructure:"temperature"`
}

type MCPConfig struct {
    BaseURL string `mapstructure:"base_url"`  // tyche MCP 地址
    Token   string `mapstructure:"token"`      // Bearer token
    Phone   string `mapstructure:"phone"`      // 白名单手机号
}

type AgentConfig struct {
    MaxHistoryTurns int `mapstructure:"max_history_turns"` // 上下文轮次
}
```

**文件**: `configs/config.yaml`

```yaml
llm:
  provider: "deepseek"
  api_key: "${DEEPSEEK_API_KEY}"
  base_url: "https://api.deepseek.com"
  model: "deepseek-chat"
  max_tokens: 4096
  temperature: 0.7

mcp:
  base_url: "http://tyche-inner.xxx.com/car/rental/inner/mcp"
  token: "${MCP_TOKEN}"
  phone: "${MCP_PHONE}"

agent:
  max_history_turns: 10
```

### 验证标准
- `Viper` 能正确加载配置
- 支持环境变量覆盖（`${ENV_VAR}` 模式）

---

## Step 1.3: LLM Provider 接入

**文件**: `internal/llm/provider.go`

使用 eino-ext 的 ChatModel：

```go
import (
    "github.com/cloudwego/eino-ext/components/model/openai"
    "github.com/cloudwego/eino/schema"
)

// NewChatModel 根据 config 创建 eino ChatModel
// DeepSeek 的 API 完全兼容 OpenAI 格式，直接使用 eino-ext OpenAI ChatModel
// 只需将 BaseURL 指向 https://api.deepseek.com 即可
func NewChatModel(cfg *config.Config) (model.ChatModel, error) {
    return openai.NewChatModel(ctx, &openai.ChatModelConfig{
        APIKey:  cfg.LLM.APIKey,
        BaseURL: cfg.LLM.BaseURL,  // https://api.deepseek.com
        Model:   cfg.LLM.Model,    // deepseek-chat (V3)
    })
}
```

### DeepSeek 使用注意事项

| 项目 | 说明 |
|------|------|
| 模型选择 | `deepseek-chat`（V3）支持 function calling；`deepseek-reasoner`（R1）**不支持** function calling，不能用于本项目 |
| Tool Calling | DeepSeek V3 的 function calling 与 OpenAI 格式兼容，eino-ext OpenAI ChatModel 可直接使用 |
| 并行调用 | DeepSeek 的并行 tool call 行为可能与 OpenAI 略有差异，需在多 Tool 场景下测试 |
| 流式输出 | DeepSeek 支持流式，与 OpenAI SSE 格式兼容 |

### 验证标准
- 能成功调用 DeepSeek API 并获得回复
- 支持 streaming（流式输出）
- 支持 function calling（tool use）

---

## Step 1.4: MCP HTTP Client 封装

**核心**：将 tyche MCP Server 的 JSON-RPC 2.0 接口封装为 eino Tool 接口。

**文件**: `internal/tool/mcp/types.go`

```go
// MCP JSON-RPC 2.0 请求/响应类型
type JSONRPCRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      int64           `json:"id"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      int64           `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *RPCError       `json:"error,omitempty"`
}

// MCP Tool 定义（从 tools/list 获取）
type MCPToolDefinition struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"inputSchema"`
}
```

**文件**: `internal/tool/mcp/client.go`

```go
type MCPClient struct {
    baseURL    string
    authToken  string
    phone      string
    httpClient *http.Client
    tools      []MCPToolDefinition  // 缓存的工具列表
}

// 关键方法：
// - Initialize() — 调用 MCP initialize，获取服务端能力
// - ListTools() — 调用 tools/list，获取可用工具
// - CallTool(name, args) — 调用 tools/call，执行工具
```

**文件**: `internal/tool/mcp/tools.go`

```go
// WrapAsEinoTools 将 MCP 工具转换为 eino Tool 接口
func WrapAsEinoTools(mcpClient *MCPClient) ([]tool.Tool, error) {
    // 遍历 mcpClient.ListTools() 返回的 MCPToolDefinition
    // 为每个工具创建 eino Tool 实现：
    //   - Name() → MCP 工具名
    //   - Description() → MCP 工具描述
    //   - InputSchema() → MCP 工具的 inputSchema
    //   - InvokableRun(ctx, args) → 调用 mcpClient.CallTool()
}
```

### 验证标准
- 能成功调用 tyche MCP `initialize` + `tools/list`
- 能成功调用 `rental_search_locations` 工具
- eino Tool 接口正确封装，能被 Agent 调用

---

## Step 1.5: Tool Registry + ToolProvider 接口

**文件**: `internal/tool/registry.go`

```go
// ToolProvider Tool 来源的统一接口，支持多种来源扩展
type ToolProvider interface {
    Name() string                          // 来源标识，如 "mcp"、"local"、"rag"
    LoadTools(ctx context.Context) ([]tool.Tool, error)
}

type Registry struct {
    tools     map[string]tool.Tool
    providers map[string]ToolProvider       // 已注册的 ToolProvider
}

// Register 批量注册 Tool
func (r *Registry) Register(tools ...tool.Tool)

// RegisterProvider 注册 ToolProvider 并加载其 Tool
func (r *Registry) RegisterProvider(ctx context.Context, provider ToolProvider) error

// GetByName 按名称获取 Tool
func (r *Registry) GetByName(name string) (tool.Tool, bool)

// Select 按名称前缀筛选 Tool（供 Skill Agent 选择性订阅）
func (r *Registry) Select(prefix string) []tool.Tool

// ListAll 返回所有注册的 Tool
func (r *Registry) ListAll() []tool.Tool
```

**扩展点**：后续新增 Tool 来源（本地知识库、RAG、支付等），只需实现 `ToolProvider` 接口并调用 `RegisterProvider`，无需改动 Registry 核心逻辑。

### 验证标准
- Registry 能注册、查找、列举所有 MCP 工具
- `Select("rental_search_")` 能筛选出车辆相关 Tool
- `RegisterProvider` 能加载 MCP ToolProvider

---

## Step 1.6: Session Manager（内存版）

**文件**: `internal/session/session.go`

```go
type Session struct {
    ID        string
    Messages  []*schema.Message  // 对话历史
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

**文件**: `internal/session/manager.go`

```go
type Manager struct {
    sessions map[string]*Session
    mu       sync.RWMutex
    maxTurns int  // 最大保留轮次
}

func (m *Manager) Create() *Session
func (m *Manager) Get(id string) (*Session, bool)
func (m *Manager) AppendMessage(id string, msg *schema.Message) error
func (m *Manager) TrimHistory(id string)  // 裁剪超出 maxTurns 的历史
```

### 验证标准
- Session 创建、查找、追加消息正常
- 超出最大轮次时自动裁剪

---

## Step 1.7: Orchestrator Agent

**文件**: `internal/agent/orchestrator.go`

Orchestrator 是整个系统的入口 Agent，职责：
1. 识别用户意图（车辆/保险/费用/履约/通用闲聊）
2. 将请求路由到对应的专业 Agent
3. 管理对话上下文

采用 eino 的 **Tool-Delegation 模式**：

```go
// 每个 Skill Agent 被包装为 Orchestrator 的一个 Tool
// Orchestrator 通过 tool calling 决定委派给哪个 Agent
// Agent 列表从 SkillRegistry 动态获取，新增 Agent 无需改 Orchestrator

type Orchestrator struct {
    chatModel    model.ChatModel
    skillRegistry *skill.SkillRegistry  // 从 Registry 获取所有 Agent
    toolRegistry  *tool.Registry        // 通用 Tool
    sessions     *session.Manager
}

func NewOrchestrator(
    chatModel model.ChatModel,
    skillRegistry *skill.SkillRegistry,
    toolRegistry *tool.Registry,
    sessions *session.Manager,
) *Orchestrator

func (o *Orchestrator) Run(ctx context.Context, sessionID string, userInput string) (string, error)
```

**System Prompt 核心**（从 SkillRegistry 动态生成）：

```
你是租车平台的智能客服助手。你可以帮助用户：
1. 推荐车型和报价 → 调用 vehicle_agent 工具
2. 推荐保险方案 → 调用 insurance_agent 工具
3. 解释合约和履约规则 → 调用 fulfillment_agent 工具
4. 解读订单费用明细 → 调用 billing_agent 工具
// 5. 下单租车 → 调用 order_agent 工具  ← 未来扩展，自动出现在这里

重要约束：
- 所有业务数据必须通过工具调用获取，不得凭记忆编造
- 如果工具返回空数据，应如实告知用户无法获取
- 不确定的业务规则应调用对应工具查询后再回答
```

注：Prompt 中的 Agent 列表由 `skillRegistry.BuildRouterPrompt()` 动态生成，新增 Agent 后自动更新，无需手动维护。

### 验证标准
- Orchestrator 能正确识别"我要租车"→ 路由到 VehicleAgent
- Orchestrator 能处理通用闲聊（不路由）
- 多轮对话保持上下文

---

## Step 1.8: Skill Agent 骨架 + 插件化注册

每个 Skill Agent 在 Phase 1 只做骨架，Phase 2-5 逐步填充。

**核心接口**：`internal/skill/interface.go`

```go
// SkillAgent 定义所有 Skill Agent 必须满足的接口
// 新增 Skill Agent 只需实现此接口，然后在 registry.go 中注册
type SkillAgent interface {
    // Name 返回 Agent 唯一标识，如 "vehicle"、"insurance"、"order"
    Name() string
    // Description 返回 Agent 能力描述，注入 Orchestrator 的路由 Prompt
    Description() string
    // Tools 返回该 Agent 可调用的 Tool 列表
    Tools() []tool.Tool
    // SystemPrompt 返回该 Agent 的系统提示词
    SystemPrompt() string
    // Run 执行一次 Agent 对话
    Run(ctx context.Context, messages []*schema.Message) (*schema.Message, error)
}
```

**统一注册表**：`internal/skill/registry.go`

```go
type SkillRegistry struct {
    agents map[string]SkillAgent
}

// Register 注册 Skill Agent
func (r *SkillRegistry) Register(agent SkillAgent)

// GetByName 按名称获取 Agent
func (r *SkillRegistry) GetByName(name string) (SkillAgent, bool)

// ListAll 返回所有注册的 Agent
func (r *SkillRegistry) ListAll() []SkillAgent

// AsTools 将所有 Agent 包装为 Orchestrator 可调用的 Tool
// Orchestrator 不硬编码 Agent 列表，而是从此方法动态获取
func (r *SkillRegistry) AsTools() []tool.Tool

// BuildRouterPrompt 根据 Registry 中的 Agent 自动生成路由 Prompt
func (r *SkillRegistry) BuildRouterPrompt() string
```

**Agent 实现**：每个 Skill Agent 实现 `SkillAgent` 接口：

```go
// internal/skill/vehicle/agent.go
type VehicleAgent struct {
    chatModel model.ChatModel
    tools     []tool.Tool  // 从 Registry.Select("rental_search_") 订阅
}

func (a *VehicleAgent) Name() string        { return "vehicle" }
func (a *VehicleAgent) Description() string  { return "处理车辆推荐、车型查询、报价比较等请求" }
func (a *VehicleAgent) Tools() []tool.Tool   { return a.tools }
func (a *VehicleAgent) SystemPrompt() string { return "你是租车平台的车辆推荐顾问..." }
func (a *VehicleAgent) Run(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
    // eino Agent 循环：chatModel + tools
}
```

**扩展方式**（以下单为例）：

```go
// 未来新增 Order Agent，只需：
// 1. 创建 internal/skill/order/agent.go，实现 SkillAgent 接口
// 2. 在 registry.go 的 init() 中加一行：
//    globalRegistry.Register(order.NewAgent(chatModel, registry.Select("rental_create_")))
// 3. Orchestrator 自动感知，无需改动核心代码
```

Phase 1 骨架阶段，4 个 Skill Agent 各自有基础 System Prompt 和对应的 MCP Tool 子集，但业务逻辑较简单。

### 验证标准
- 4 个 Skill Agent 都实现了 `SkillAgent` 接口
- SkillRegistry 能注册、查找、列举所有 Agent
- `AsTools()` 能将所有 Agent 包装为 Orchestrator 可调用的 Tool
- VehicleAgent 能调用 `rental_search_quotes` 并返回结果

---

## Step 1.9: CLI 入口

**文件**: `cmd/cli/main.go`

```go
func main() {
    // 1. 加载配置
    cfg := config.Load("configs/config.yaml")

    // 2. 初始化 LLM
    chatModel := llm.NewChatModel(cfg)

    // 3. 初始化 Tool Registry + 注册 MCP ToolProvider
    toolRegistry := tool.NewRegistry()
    mcpProvider := mcp.NewMCPToolProvider(cfg.MCP)
    toolRegistry.RegisterProvider(ctx, mcpProvider)  // 加载 7 个 MCP Tool
    // 未来扩展：toolRegistry.RegisterProvider(ctx, local.NewLocalToolProvider(...))

    // 4. 创建 Skill Registry + 注册所有 Skill Agent
    skillRegistry := skill.NewSkillRegistry()
    skillRegistry.Register(vehicle.NewAgent(chatModel, toolRegistry.Select("rental_search_", "rental_resolve_")))
    skillRegistry.Register(insurance.NewAgent(chatModel, ...))
    skillRegistry.Register(billing.NewAgent(chatModel, toolRegistry.Select("rental_get_order_", "rental_get_reservation")))
    skillRegistry.Register(fulfillment.NewAgent(chatModel, ...))
    // 未来扩展：skillRegistry.Register(order.NewAgent(chatModel, toolRegistry.Select("rental_create_")))

    // 5. 创建 Orchestrator（从 Registry 动态获取 Agent，无需硬编码）
    orchestrator := agent.NewOrchestrator(chatModel, skillRegistry, toolRegistry, sessionMgr)

    // 6. 交互循环
    sessionID := sessionMgr.Create().ID
    for {
        fmt.Print("🚗 你: ")
        input := readLine()
        if input == "exit" { break }
        result := orchestrator.Run(ctx, sessionID, input)
        fmt.Printf("🤖 助手: %s\n", result)
    }
}
```

### 验证标准
- `go run cmd/cli/main.go` 启动终端
- 输入"我想在北京租车"→ Orchestrator 路由到 VehicleAgent → 调用 MCP → 返回车型列表
- 输入"退出"→ 程序正常退出

---

## Step 1.10: 端到端测试

手动验证流程：

1. 启动 CLI
2. 测试通用对话："你好" → 应正常回复
3. 测试车辆推荐："我想在北京租一辆5座车" → 应调用 `rental_search_locations` + `rental_search_quotes`
4. 测试多轮对话：追问"有更便宜的吗" → 应保持上下文继续推荐
5. 测试边界：输入无关问题 → 应正常回复但不编造租车信息

---

## 依赖清单

```bash
# eino 核心
go get github.com/cloudwego/eino

# eino-ext: OpenAI 兼容 ChatModel（DeepSeek 通过此组件接入）
go get github.com/cloudwego/eino-ext/components/model/openai

# 配置
go get github.com/spf13/viper

# 日志
go get go.uber.org/zap
```

## 文件清单

| 文件 | 核心职责 |
|------|---------|
| `cmd/cli/main.go` | CLI 入口，组装所有组件 |
| `internal/config/config.go` | 配置结构 + Viper 加载 |
| `internal/llm/provider.go` | eino ChatModel 初始化 |
| `internal/tool/mcp/client.go` | tyche MCP HTTP Client |
| `internal/tool/mcp/types.go` | MCP JSON-RPC 类型定义 |
| `internal/tool/mcp/tools.go` | MCP → eino Tool 适配器（MCPToolProvider） |
| `internal/tool/registry.go` | Tool 统一注册表 + ToolProvider 接口 |
| `internal/session/session.go` | Session 模型 |
| `internal/session/manager.go` | 内存 Session 管理器 |
| `internal/agent/orchestrator.go` | Orchestrator Agent |
| `internal/agent/router.go` | Agent 路由辅助 |
| `internal/skill/interface.go` | SkillAgent 接口定义 |
| `internal/skill/registry.go` | Skill Agent 统一注册表 |
| `internal/skill/vehicle/agent.go` | Vehicle Agent 骨架 |
| `internal/skill/insurance/agent.go` | Insurance Agent 骨架 |
| `internal/skill/billing/agent.go` | Billing Agent 骨架 |
| `internal/skill/fulfillment/agent.go` | Fulfillment Agent 骨架 |
| `configs/config.yaml` | 配置文件模板 |
