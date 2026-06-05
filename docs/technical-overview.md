# 在线租车系统智能 Agent — 技术方案概览

## 1. 项目定位

构建一个**多 Agent 协作**的在线租车智能客服系统，聚焦四大能力域：

| 能力域 | Agent 名称 | 职责 |
|--------|-----------|------|
| 车辆推荐 | VehicleAgent | 根据场景/预算/人数推荐车型，对比车型参数 |
| 保险推荐 | InsuranceAgent | 推适配保险方案，解释保障范围/免赔额 |
| 合约规则 | FulfillmentAgent | 解释取还车规则、违章处理、续租换车等 |
| 费用解读 | BillingAgent | 解读订单各项费用明细、退款规则 |

**不负责交易流程**（下单/支付/取还车由 tyche 业务系统完成），仅做信息推荐和规则解读。架构预留交易类能力扩展口子（详见 4.5）。

## 2. 技术栈

| 领域 | 选择 | 说明 |
|------|------|------|
| 语言 | Go 1.22+ | 高性能、强类型、并发友好 |
| Agent 框架 | [eino](https://github.com/cloudwego/eino) | CloudWeGo 出品，Go 原生 AI 应用框架 |
| 扩展组件 | [eino-ext](https://github.com/cloudwego/eino-ext) | OpenAI/Ark ChatModel、MCP Tool 等 |
| LLM | DeepSeek (deepseek-chat) | OpenAI 兼容 API，支持 function calling，通过 eino-ext OpenAI ChatModel 接入 |
| MCP Server | tyche 已有 MCP | 7 个租车工具，JSON-RPC 2.0 over HTTP |
| MCP Client | 先用 HTTP 直接调用 | eino MCP Client 暂不兼容，后续迁移 |
| 配置 | Viper | Go 生态标准 |
| 日志 | Zap | 结构化高性能 |
| 知识库 | 本地 JSON/Markdown（Phase 1-5）→ 向量库（Phase 6） | 渐进式引入 |
| 入口 | cmd/cli | 先实现命令行终端交互 |

## 3. 多 Agent 协作架构

```
┌───────────────────────────────────────────────────────┐
│                     CLI (cmd/cli)                      │
│                  用户输入 / 输出展示                     │
├───────────────────────────────────────────────────────┤
│                  Orchestrator Agent                     │
│              (意图识别 + Agent 路由 + 上下文管理)          │
│                                                        │
│   ┌─────────────┐  ┌─────────────┐  ┌──────────────┐  │
│   │  Vehicle    │  │  Insurance  │  │  Fulfillment │  │
│   │  Agent      │  │  Agent      │  │  Agent       │  │
│   └──────┬──────┘  └──────┬──────┘  └──────┬───────┘  │
│          │                │                 │          │
│   ┌──────┴──────┐  ┌──────┴──────┐  ┌──────┴───────┐  │
│   │  Billing    │  │             │  │              │  │
│   │  Agent      │  │             │  │              │  │
│   └──────┬──────┘  │             │  │              │  │
│          │         │             │  │              │  │
├──────────┼─────────┼─────────────┼──┼──────────────┼──┤
│          │    Tool Layer (MCP / 知识库)              │  │
│   ┌──────┴──────────┴─────────────┴──┴──────────────┐ │
│   │           Tool Registry (统一注册表)              │ │
│   └──────┬──────────┬─────────────┬──┬──────────────┘ │
│          │          │             │  │                 │
│   ┌──────▼──┐ ┌─────▼─────┐ ┌────▼──┐ ┌────────────┐ │
│   │  Tyche  │ │ 知识库     │ │ 知识库 │ │ 知识库     │ │
│   │  MCP    │ │ (Vehicle) │ │(Insur)│ │(Fulfill)   │ │
│   │  Server │ │           │ │       │ │            │ │
│   └─────────┘ └───────────┘ └───────┘ └────────────┘ │
├───────────────────────────────────────────────────────┤
│               LLM Provider (eino ChatModel)            │
│          DeepSeek API (OpenAI Compatible)               │
└───────────────────────────────────────────────────────┘
```

## 4. 关键设计决策

### 4.1 eino 框架下的多 Agent 模式

eino 提供了 `Graph` / `Chain` / `StateGraph` 编排能力，多 Agent 协作采用以下模式：

- **Orchestrator 模式**：一个主 Agent 负责意图识别和路由，将用户请求分派到对应的专业 Agent
- **Tool-Delegation 模式**：每个专业 Agent 作为 Orchestrator 可调用的 Tool，Orchestrator 通过 tool calling 委派任务
- **Graph 编排**：使用 eino 的 `Graph` 将 Orchestrator → Skill Agent → Tool 串联为 DAG

### 4.2 MCP 兼容性策略

**当前问题**：tyche 的 MCP Server 实现 JSON-RPC 2.0 over HTTP，但 eino-ext 的 MCP Client 采用 SSE/stdio 传输，两者协议不完全兼容。

**解决策略**：

```
Phase 1-5（先跑起来）:
  直接用 HTTP Client 调用 tyche MCP 的 /car/rental/inner/mcp 端点
  在 agent 项目内封装为 eino Tool 接口
  调用链: Agent → eino Tool → HTTP Client → tyche MCP Server

Phase 6（迁移）:
  待 eino MCP Client 适配后，替换 HTTP Client 为 eino-ext/components/tool/mcp
  或改造 tyche MCP Server 支持 SSE 传输
```

### 4.3 数据来源约束（防幻觉）

**核心原则：所有返回给用户的数据必须来自 MCP Tool 调用或知识库检索，禁止使用 LLM 训练数据编造业务信息。**

实现方式：
1. 每个 Agent 的 System Prompt 明确要求：不确信时必须调用 Tool 查询
2. Tool 返回的结构化数据作为上下文注入，LLM 仅做"翻译"和"组织"
3. 知识库中的规则文档（保险条款、取还车规则等）通过 RAG Tool 检索后引用
4. 若 Tool 返回空或异常，Agent 应如实告知用户"暂时无法获取"，不猜测

### 4.4 入口先 CLI

先实现 `cmd/cli` 下的终端交互，验证核心链路后再加 HTTP/gRPC。

### 4.5 可扩展性设计

系统架构从三个层面预留扩展口子，确保新增能力（如下单、支付、续租）时无需改动核心框架。

#### 4.5.1 Skill Agent 插件化注册

```
新增 Skill Agent 只需 3 步：

1. 在 internal/skill/<name>/ 下实现 Agent，满足 SkillAgent 接口
2. 在 skill/registry.go 的 init() 中注册
3. Orchestrator 自动感知新 Agent，更新路由 Prompt

无需改动：
  - Orchestrator 核心逻辑
  - Tool Registry
  - CLI 入口
  - Session Manager
```

**SkillAgent 接口**（Phase 1 定义）：

```go
type SkillAgent interface {
    // Name 返回 Agent 唯一标识，如 "vehicle"、"order"
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

**注册机制**：

```go
// internal/skill/registry.go
var globalRegistry = NewSkillRegistry()

func init() {
    globalRegistry.Register(vehicle.NewAgent(...))
    globalRegistry.Register(insurance.NewAgent(...))
    globalRegistry.Register(billing.NewAgent(...))
    globalRegistry.Register(fulfillment.NewAgent(...))
    // 未来扩展：
    // globalRegistry.Register(order.NewAgent(...))
    // globalRegistry.Register(payment.NewAgent(...))
}

// Orchestrator 从 globalRegistry 获取所有 Agent
// 自动生成路由 Prompt："你可以帮助用户：\n1. 推荐车型 → 调用 vehicle_agent\n2. ..."
```

#### 4.5.2 Tool 动态挂载

```
新增 Tool 来源只需实现 ToolProvider 接口：

type ToolProvider interface {
    Name() string                          // 来源名，如 "mcp"、"local"
    LoadTools(ctx context.Context) ([]tool.Tool, error)
}
```

当前已实现 `MCPToolProvider`（从 tyche MCP 加载），后续可扩展：

| ToolProvider | 说明 | 扩展时机 |
|-------------|------|---------|
| `MCPToolProvider` | 从 tyche MCP 加载 Tool | Phase 1（已实现） |
| `LocalToolProvider` | 本地知识库检索 Tool | Phase 2-5 |
| `RAGToolProvider` | 向量库检索 Tool | Phase 6 |
| `PaymentToolProvider` | 支付相关 Tool | 下单能力扩展时 |

**Skill Agent 选择性地从 Tool Registry 订阅 Tool**：

```go
// 每个 Agent 声明自己需要的 Tool（按名称前缀或标签过滤）
vehicleAgent := vehicle.NewAgent(chatModel, registry.Select("rental_search_", "rental_resolve_"))
orderAgent := order.NewAgent(chatModel, registry.Select("rental_create_", "rental_get_driver_"))
```

#### 4.5.3 下单能力扩展路径（示例）

以"下单"为例，扩展时的具体改动：

```
1. 新增文件：
   internal/skill/order/agent.go           — Order Agent 实现
   knowledge/order/                        — 下单流程知识库
   internal/tool/order/confirm.go          — 订单确认 Tool（安全校验）
   internal/tool/order/payment.go          — 支付发起 Tool

2. 注册改动（1 行）：
   internal/skill/registry.go — 加 globalRegistry.Register(order.NewAgent(...))

3. 无需改动：
   Orchestrator 核心代码 — 自动从 registry 生成路由
   Tool Registry — rental_create_order 等 MCP Tool 已在 Phase 1 注册
   CLI 入口 — 无变化

4. 关键设计（安全门控）：
   下单类 Tool 需要"二次确认"机制：
   - Agent 调用 order_confirm Tool 时，不是直接下单
   - 而是返回确认摘要给用户，等用户明确确认后才真正调用 rental_create_order
   - 这通过 Tool 内部的状态机实现，无需框架改动
```

#### 4.5.4 扩展性约束

| 约束 | 原因 |
|------|------|
| 新 Skill Agent 必须实现 `SkillAgent` 接口 | 保证 Orchestrator 能统一调度 |
| 新 Tool 必须通过 `ToolProvider` 加载 | 保证 Registry 统一管理 |
| 交易类 Tool 必须内置二次确认 | 防止 LLM 误触发不可逆操作 |
| Orchestrator 路由 Prompt 不硬编码 | 从 Registry 动态生成，新 Agent 自动出现 |

## 5. tyche MCP 工具清单（现有可直接对接）

| 工具名 | 功能 | 对应 Agent |
|--------|------|-----------|
| `rental_search_locations` | 关键词搜索取还车地点 | Vehicle |
| `rental_resolve_poi` | 解析 location_id 到 POI | Vehicle |
| `rental_search_quotes` | 搜索可用车型及报价 | Vehicle |
| `rental_get_order_details` | 获取订单详情及退改政策 | Billing / Fulfillment |
| `rental_create_order` | 创建租车订单 | 预留给 Order Agent（后续扩展） |
| `rental_get_reservation` | 查询订单状态 | Billing / Fulfillment |
| `rental_get_driver_list` | 获取用户驾驶员列表 | （辅助信息） |

## 6. 分阶段计划总览

| Phase | 名称 | 核心交付 | 依赖 |
|-------|------|---------|------|
| 1 | 基础骨架 | 项目结构、eino 集成、MCP HTTP Client 封装、CLI 入口、Orchestrator 单轮对话 | 无 |
| 2 | 车辆推荐 Agent | VehicleAgent + 车辆搜索/报价 Tool + 车型知识库 | Phase 1 |
| 3 | 保险推荐 Agent | InsuranceAgent + 保险查询 Tool + 保险条款知识库 | Phase 1 |
| 4 | 费用解读 Agent | BillingAgent + 订单/退款 Tool + 费用规则知识库 | Phase 1 |
| 5 | 履约规则 Agent | FulfillmentAgent + 取还车/违章/续租 Tool + 规则知识库 | Phase 1 |
| 6 | eino MCP 迁移 + RAG | 替换 HTTP Client 为 eino MCP Client，引入向量库 RAG | Phase 1-5 |

每个 Phase 有独立的详细计划文件，可分步完成。
