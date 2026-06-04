# 数据流设计文档

## 1. 核心数据流

### 1.1 用户消息处理流

```
Client                  API Gateway             Agent Service              LLM Provider           Business API
  │                         │                        │                         │                      │
  │  POST /chat/stream      │                        │                         │                      │
  │────────────────────────▶│                        │                         │                      │
  │                         │  转发请求               │                         │                      │
  │                         │───────────────────────▶│                         │                      │
  │                         │                        │                         │                      │
  │                         │                        │  加载 Session            │                      │
  │                         │                        │◀─── Redis ──────────────│                      │
  │                         │                        │                         │                      │
  │                         │                        │  意图检测 (首次)         │                      │
  │                         │                        │────────────────────────▶│                      │
  │                         │                        │◀────────────────────────│  intent: vehicle     │
  │                         │                        │                         │                      │
  │                         │                        │  构建 Prompt + Tools    │                      │
  │                         │                        │────────────────────────▶│                      │
  │                         │                        │                         │                      │
  │                         │                        │  tool_call: search_veh  │                      │
  │                         │                        │◀────────────────────────│                      │
  │                         │                        │                         │                      │
  │                         │                        │  GET /api/vehicles      │                      │
  │                         │                        │────────────────────────────────────────────────▶│
  │                         │                        │◀────────────────────────────────────────────────│
  │                         │                        │                         │                      │
  │                         │                        │  Tool Result → LLM      │                      │
  │                         │                        │────────────────────────▶│                      │
  │                         │                        │                         │                      │
  │                         │                        │  生成回复 (stream)      │                      │
  │                         │                        │◀═══════════════════════│                      │
  │                         │                        │                         │                      │
  │  SSE: content_delta     │                        │                         │                      │
  │◀═══════════════════════│◀══════════════════════│                         │                      │
  │  SSE: content_delta     │                        │                         │                      │
  │◀═══════════════════════│                        │  保存 Session           │                      │
  │                         │                        │───▶ Redis ─────────────▶│                      │
  │  SSE: message_end       │                        │                         │                      │
  │◀═══════════════════════│                        │                         │                      │
```

### 1.2 对话上下文传递

每次 LLM 调用时构建的消息结构：

```
[
  {"role": "system",   "content": "<Skill System Prompt>"},
  {"role": "system",   "content": "<当前对话状态：活跃Skill、已收集的槽位>"},
  {"role": "user",     "content": "我想租一辆车"},
  {"role": "assistant","content": "好的，请问您在哪个城市..."},
  {"role": "user",     "content": "北京"},
  {"role": "assistant","content": null, "tool_calls": [{"name":"search_vehicles","args":{...}}]},
  {"role": "tool",     "content": "{\"vehicles\":[...]}", "tool_call_id": "xxx"},
  {"role": "assistant","content": "为您找到以下车型..."},
  {"role": "user",     "content": "第二款是什么类型？"}   ← 新消息
]
```

### 1.3 会话数据存储

```
Redis Key 设计：
  session:{id}           → Session JSON（TTL: 30min，每次请求续期）
  session:{id}:lock      → 分布式锁（防止并发写入）
```

## 2. 业务数据流

### 2.1 车辆推荐数据流

```
Agent                    Vehicle Tool                Vehicle API
  │                          │                          │
  │  Execute(params)         │                          │
  │─────────────────────────▶│                          │
  │                          │  GET /vehicles?city=BJ   │
  │                          │─────────────────────────▶│
  │                          │◀─────────────────────────│
  │                          │                          │
  │                          │  数据转换 & 精简          │
  │                          │─── (internal) ────────▶  │
  │                          │                          │
  │  ToolResult              │                          │
  │◀─────────────────────────│                          │
```

**数据精简原则**：业务 API 返回完整数据，Tool 层精简为 LLM 需要的关键字段，避免 Token 浪费。

### 2.2 保险推荐数据流

```
Agent                    Insurance Tool              Insurance API
  │                          │                          │
  │  Execute({quote_id})     │                          │
  │─────────────────────────▶│                          │
  │                          │  GET /quotes/{id}/plans  │
  │                          │─────────────────────────▶│
  │                          │◀─────────────────────────│
  │                          │                          │
  │  ToolResult              │  (保险方案列表)           │
  │◀─────────────────────────│                          │
```

### 2.3 费用解读数据流

```
Agent                    Billing Tool                 Order API + Payment API
  │                          │                          │
  │  Execute({order_id})     │                          │
  │─────────────────────────▶│                          │
  │                          │  GET /orders/{id}/detail │
  │                          │─────────────────────────▶│
  │                          │◀─────────────────────────│
  │                          │                          │
  │                          │  GET /payments/{id}/refunds
  │                          │─────────────────────────▶│
  │                          │◀─────────────────────────│
  │                          │                          │
  │                          │  合并 & 格式化            │
  │  ToolResult              │                          │
  │◀─────────────────────────│                          │
```

## 3. 知识检索数据流 (Phase 6)

```
Agent                    Knowledge Tool              Vector DB + Doc Store
  │                          │                          │
  │  Execute({query})        │                          │
  │─────────────────────────▶│                          │
  │                          │  向量化 query             │
  │                          │─── Embedding API ──────▶ │
  │                          │◀── vector ────────────── │
  │                          │                          │
  │                          │  相似度检索               │
  │                          │─────────────────────────▶│
  │                          │◀─────────────────────────│
  │                          │  (top-k 文档片段)        │
  │                          │                          │
  │  ToolResult              │  格式化为文本             │
  │◀─────────────────────────│                          │
```

## 4. 错误传播流

```
Business API Error
       │
       ▼
  Tool Layer: 包装为 ToolResult{Success: false, Error: "xxx"}
       │
       ▼
  Agent: 将错误信息作为 Tool Result 返回给 LLM
       │
       ▼
  LLM: 生成友好的错误回复（如"暂时无法查询，请稍后重试"）
       │
       ▼
  User: 看到友好的提示，而非技术错误
```
