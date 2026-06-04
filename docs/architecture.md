# 架构设计详细文档

## 1. 系统总览

Car Rental Agent 是一个**对话式智能助手**，部署在租车平台的前端（App/小程序/Web）与后端业务系统之间。用户通过自然语言与 Agent 交互，Agent 通过 Tool Calling 调用业务 API 获取实时数据，通过 RAG 检索静态知识，最终生成准确的回答。

```
┌────────────┐     ┌──────────────┐     ┌──────────────────┐     ┌──────────────┐
│   Client   │────▶│  API Gateway │────▶│  Agent Service   │────▶│  Business    │
│ (App/Web)  │◀────│  (Nginx/K8s) │◀────│  (This Project)  │◀────│  APIs        │
└────────────┘     └──────────────┘     └──────────────────┘     └──────────────┘
                                                │
                                                ▼
                                         ┌──────────────┐
                                         │  LLM / RAG   │
                                         │  (Claude/DB) │
                                         └──────────────┘
```

## 2. 核心概念模型

### 2.1 对话会话 (Session)

```
Session {
    ID          string            // 会话唯一标识
    UserID      string            // 用户标识
    Messages    []Message         // 对话历史
    State       DialogueState     // 对话状态
    Skill       string            // 当前活跃 Skill
    Context     map[string]any    // 业务上下文（选中的车型、订单号等）
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

DialogueState {
    Phase       string            // idle / skill_active / tool_calling / responding
    Intent      string            // 当前识别的意图
    Slots       map[string]string // 意图槽位（车型偏好、订单号等）
    TurnCount   int               // 当前轮次
}

Message {
    Role        string            // user / assistant / tool
    Content     string
    ToolCalls   []ToolCall        // LLM 发起的工具调用
    ToolResults []ToolResult      // 工具返回结果
    Timestamp   time.Time
}
```

### 2.2 Skill 模型

```
Skill {
    Name        string            // skill 标识：vehicle / insurance / billing / fulfillment
    Description string            // 技能描述，用于意图路由
    SystemPrompt string           // 该 Skill 的 System Prompt
    Tools       []ToolDefinition  // 该 Skill 可用的 Tool 列表
    Examples    []Example         // 意图识别的示例对话
}

ToolDefinition {
    Name        string
    Description string
    Parameters  json.Schema       // 参数 JSON Schema
}

Example {
    UserQuery   string            // 用户可能的说法
    Intent      string            // 对应的意图
    Skill       string            // 路由到的 Skill
}
```

## 3. 请求处理流程

```
用户消息
   │
   ▼
[1] 会话加载 ─── 从存储加载 Session（不存在则创建）
   │
   ▼
[2] 意图检测 ─── LLM 判断用户意图 + 目标 Skill
   │              （首次消息或意图切换时触发）
   │
   ▼
[3] Skill 路由 ── 切换到目标 Skill，加载对应 System Prompt + Tools
   │
   ▼
[4] LLM 调用 ─── 携带 System Prompt + 对话历史 + Tools 定义
   │              发送给 LLM
   │
   ▼
[5] Tool 执行 ─── 如果 LLM 返回 Tool Call，执行对应 Tool
   │              （可能有多次 Tool Call 循环）
   │
   ▼
[6] 响应生成 ─── LLM 基于 Tool 结果生成最终回复
   │
   ▼
[7] 会话持久化 ── 保存对话历史和状态更新
   │
   ▼
返回用户（流式 / 非流式）
```

## 4. 意图检测与路由

### 意图分类

| 意图 | 描述 | 目标 Skill |
|------|------|-----------|
| `vehicle_recommend` | 想了解/推荐车型 | vehicle |
| `vehicle_compare` | 对比不同车型 | vehicle |
| `vehicle_detail` | 了解某车型详情 | vehicle |
| `insurance_recommend` | 咨询保险方案 | insurance |
| `insurance_detail` | 了解保险条款 | insurance |
| `billing_explain` | 解释费用明细 | billing |
| `refund_explain` | 解释退款规则 | billing |
| `pickup_rule` | 取车规则 | fulfillment |
| `return_rule` | 还车规则 | fulfillment |
| `violation_rule` | 违章处理 | fulfillment |
| `extension_rule` | 续租规则 | fulfillment |
| `accident_guide` | 事故处理指引 | fulfillment |
| `general_greeting` | 打招呼 | （默认 Skill） |
| `general_other` | 其他无关问题 | （默认 Skill） |

### 路由策略

1. **首条消息**：必定触发意图检测
2. **上下文延续**：如果当前 Skill 上下文内的追问，不切换 Skill
3. **意图切换**：用户明确转换话题时，重新检测并路由
4. **模糊识别**：置信度低时，追问用户确认

### 路由实现

```
func (r *Router) Route(session *Session, msg string) (*Skill, error) {
    // 1. 如果当前 Skill 可以处理（上下文延续），保持
    if session.State.Skill != "" {
        current := r.skills[session.State.Skill]
        if current.CanHandle(msg, session.State) {
            return current, nil
        }
    }

    // 2. 通过 LLM 做意图分类
    intent, confidence := r.detectIntent(session, msg)

    // 3. 置信度检查
    if confidence < 0.6 {
        return r.defaultSkill, nil  // 降级到默认
    }

    // 4. 路由到目标 Skill
    skill, ok := r.intentToSkill[intent]
    if !ok {
        return r.defaultSkill, nil
    }
    return skill, nil
}
```

## 5. Tool Calling 设计

### Tool 执行流程

```
LLM Response (with tool_calls)
   │
   ▼
[1] 解析 Tool Call（name + arguments）
   │
   ▼
[2] 参数校验（JSON Schema validation）
   │
   ▼
[3] 权限检查（Session 上下文中是否有必要信息）
   │
   ▼
[4] 执行 Tool（调用业务 API / 查询知识库）
   │
   ▼
[5] 结果格式化（转为 LLM 可理解的文本）
   │
   ▼
[6] 返回 Tool Result 给 LLM
   │
   ▼
[7] LLM 可能继续调用 Tool 或生成最终回复
```

### Tool 接口定义

```go
type Tool interface {
    // Name 返回工具名称
    Name() string

    // Description 返回工具描述（供 LLM 理解）
    Description() string

    // Parameters 返回参数 JSON Schema
    Parameters() map[string]any

    // Execute 执行工具调用
    Execute(ctx context.Context, params map[string]any, session *Session) (*ToolResult, error)
}

type ToolResult struct {
    Success bool
    Data    any    // 结构化数据
    Content string // 转为文本供 LLM 消费
    Error   string // 错误信息
}
```

## 6. 对话状态管理

### 状态流转

```
    ┌──────┐     意图检测     ┌──────────────┐
    │ idle │──────────────▶│ skill_active │
    └──────┘                └──────┬───────┘
       ▲                       │          │
       │                       │ Tool     │ 用户
       │                       │ Call     │ 追问
       │                       ▼          │
       │               ┌──────────────┐   │
       │               │ tool_calling │   │
       │               └──────┬───────┘   │
       │                      │           │
       │     意图切换          │ Tool      │
       │◀─────────────────────┘ Done      │
       │                                  │
       │           意图切换                │
       │◀─────────────────────────────────┘
```

### 上下文槽位 (Slots)

各 Skill 维护自己的业务槽位：

| Skill | 槽位 |
|-------|------|
| vehicle | `city`, `date_range`, `passenger_count`, `budget`, `car_type_preference` |
| insurance | `vehicle_id`, `quote_id`, `insurance_tier` |
| billing | `order_id`, `fee_type` |
| fulfillment | `order_id`, `issue_type` |

## 7. LLM Provider 抽象

```go
type Provider interface {
    // Chat 发送消息并获取回复（非流式）
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

    // ChatStream 发送消息并流式获取回复
    ChatStream(ctx context.Context, req *ChatRequest) (<-chan ChatStreamEvent, error)
}

type ChatRequest struct {
    Model       string
    System      string
    Messages    []Message
    Tools       []ToolDefinition
    Temperature float64
    MaxTokens   int
}

type ChatResponse struct {
    Content    string
    ToolCalls  []ToolCall
    StopReason string  // end_turn | tool_use
}

type ChatStreamEvent struct {
    Type    string  // content_start | content_delta | content_end | tool_call_start | tool_call_delta | tool_call_end
    Content string
    ToolCall *ToolCall
}
```

## 8. API 设计

### HTTP API

```
POST /api/v1/chat                    # 发送消息（非流式）
POST /api/v1/chat/stream             # 发送消息（SSE 流式）
GET  /api/v1/sessions/{id}           # 获取会话历史
DELETE /api/v1/sessions/{id}          # 关闭会话
GET  /api/v1/health                   # 健康检查
```

### SSE 流式响应格式

```
event: message_start
data: {"session_id": "xxx", "message_id": "xxx"}

event: content_delta
data: {"delta": "根据您的需求"}

event: content_delta
data: {"delta": "，我推荐以下车型"}

event: tool_call_start
data: {"tool_name": "search_vehicles", "call_id": "xxx"}

event: tool_call_end
data: {"tool_name": "search_vehicles", "result": "..."}

event: content_delta
data: {"delta": "1. 丰田卡罗拉..."}

event: message_end
data: {"message_id": "xxx"}
```

## 9. 错误处理策略

| 错误类型 | 处理方式 |
|---------|---------|
| LLM 超时 | 重试 1 次，仍失败则返回"系统繁忙" |
| LLM 限流 | 指数退避重试，最多 3 次 |
| Tool 执行失败 | 告知用户"暂时无法获取数据"，引导稍后重试 |
| 意图识别失败 | 降级到默认 Skill，让用户补充信息 |
| 业务 API 异常 | 返回友好提示，记录告警日志 |
| 参数校验失败 | 提示用户补充必要信息 |

## 10. 部署架构

```
                    ┌─────────────┐
                    │   Nginx/LB  │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ Agent-1  │ │ Agent-2  │ │ Agent-3  │
        └────┬─────┘ └────┬─────┘ └────┬─────┘
             │             │             │
             ▼             ▼             ▼
        ┌────────────────────────────────────┐
        │           Redis (Session)          │
        └────────────────────────────────────┘
             │             │             │
             ▼             ▼             ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ Claude   │ │ Business │ │ Vector   │
        │ API      │ │ APIs     │ │ DB       │
        └──────────┘ └──────────┘ └──────────┘
```

- **无状态服务**：Agent 实例本身无状态，会话数据存 Redis
- **水平扩展**：根据 QPS 动态增减实例
- **优雅关闭**：处理完当前请求再退出
