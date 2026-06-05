# Phase 6: eino MCP 迁移 + RAG 增强

## 目标

1. 将自研的 MCP HTTP Client 替换为 eino-ext 的 MCP Client，实现框架原生集成
2. 引入向量数据库，为知识库增加 RAG 检索能力，提升回答准确性

## 前置条件

- Phase 1-5 全部完成
- eino-ext MCP Client 已适配 tyche MCP Server（或 tyche MCP Server 已支持 SSE 传输）
- 向量数据库（Milvus / pgvector）已部署

## 交付物

| # | 交付物 | 说明 |
|---|--------|------|
| 1 | eino MCP Client 替换 | 用 eino-ext/components/tool/mcp 替换自研 HTTP Client |
| 2 | 知识库向量化 | 将 Markdown/JSON 知识文档转为向量并入库 |
| 3 | RAG Tool 实现 | 基于 eino Retriever 的知识检索 Tool |
| 4 | 各 Agent 集成 RAG | 4 个 Skill Agent 接入知识检索 |
| 5 | 评估与调优 | 回答准确性评估 + Prompt 调优 |

---

## Part A: eino MCP Client 迁移

### Step 6.A.1: 评估 eino MCP Client 兼容性

需要验证：

1. `eino-ext/components/tool/mcp` 当前支持的传输方式：
   - SSE (Server-Sent Events)
   - stdio (标准输入输出)

2. tyche MCP Server 的传输方式：
   - HTTP JSON-RPC 2.0（POST `/car/rental/inner/mcp`）

3. 兼容方案（三选一）：
   - **方案 A**：改造 tyche MCP Server 增加 SSE 传输端点 → 最标准
   - **方案 B**：在 agent 项目中写一个 SSE 适配层，包装 tyche 的 HTTP 接口 → 中间方案
   - **方案 C**：等待 eino-ext MCP Client 支持 HTTP 传输 → 最省事但依赖上游

### Step 6.A.2: 迁移代码

替换前后对比：

```go
// Before (Phase 1-5): 自研 HTTP Client
mcpClient := mcp.NewMCPClient(cfg.MCP)
mcpClient.Initialize(ctx)
tools := mcp.WrapAsEinoTools(mcpClient)

// After (Phase 6): eino-ext MCP Client
import einoMcp "github.com/cloudwego/eino-ext/components/tool/mcp"

mcpTool, err := einoMcp.NewTool(ctx, &einoMcp.Config{
    // 根据兼容方案配置传输方式
    ServerURL: cfg.MCP.BaseURL,
})
```

### Step 6.A.3: 删除自研 MCP Client

迁移完成后删除：
- `internal/tool/mcp/client.go`
- `internal/tool/mcp/types.go`
- `internal/tool/mcp/tools.go`

保留 `internal/tool/registry.go`（仍然需要统一注册表）。

---

## Part B: RAG 增强

### Step 6.B.1: 知识库向量化

将 Phase 2-5 的知识文档转为向量存储：

```
knowledge/
├── vehicle/
│   ├── car_types.json     → 向量化
│   └── faq.md             → 向量化
├── insurance/
│   ├── products.json      → 向量化
│   └── faq.md             → 向量化
├── billing/
│   ├── fee_structure.json → 向量化
│   └── refund_policy.md   → 向量化
└── fulfillment/
    ├── pickup_rules.md    → 向量化
    ├── return_rules.md    → 向量化
    ├── violation_rules.md → 向量化
    ├── extension_rules.md → 向量化
    └── accident_guide.md  → 向量化
```

使用 eino-ext 的 Embedding + VectorStore：

```go
import (
    "github.com/cloudwego/eino-ext/components/embedding/openai"
    "github.com/cloudwego/eino-ext/components/indexer/simple"
    "github.com/cloudwego/eino-ext/components/retriever"
)
```

### Step 6.B.2: RAG Tool 实现

为每个 Skill Agent 提供知识检索 Tool：

```go
// 通用 RAG Tool 模式
type KnowledgeRetrieverTool struct {
    retriever retriever.Retriever
    domain    string  // "vehicle" | "insurance" | "billing" | "fulfillment"
}

func (t *KnowledgeRetrieverTool) Name() string {
    return fmt.Sprintf("search_%s_knowledge", t.domain)
}

func (t *KnowledgeRetrieverTool) Description() string {
    return fmt.Sprintf("从%s知识库中检索相关规则和FAQ", domainName[t.domain])
}

func (t *KnowledgeRetrieverTool) InvokableRun(ctx context.Context, query string, opts ...tool.Option) (string, error) {
    docs, err := t.retriever.Retrieve(ctx, query)
    // 格式化返回检索结果
}
```

### Step 6.B.3: Agent 集成 RAG

每个 Skill Agent 新增知识检索 Tool：

```go
// Vehicle Agent 工具列表扩展
vehicleTools := []tool.Tool{
    searchPickupLocations,    // MCP Tool
    searchVehicleQuotes,      // MCP Tool
    resolveLocation,          // MCP Tool
    searchVehicleKnowledge,   // RAG Tool (新增)
}
```

### Step 6.B.4: 评估与调优

1. **检索质量评估**：对知识库检索的召回率和准确率做评估
2. **回答质量评估**：对比 RAG 前后的回答准确性和完整性
3. **Prompt 调优**：根据评估结果调整各 Agent 的 System Prompt
4. **性能优化**：检索延迟、Token 消耗优化

---

## 迁移风险评估

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| eino MCP Client 不兼容 tyche | 无法直接替换 | 保留自研 Client 作为 fallback |
| 知识库向量化效果不佳 | RAG 回答不准确 | 优化分块策略 + 调整 embedding 模型 |
| 向量库运维成本 | 增加基础设施依赖 | 先用 pgvector（与现有 PG 共存） |
| 迁移期间功能回退 | 用户体验下降 | 灰度替换，新旧并行 |

## 时间预估

| 任务 | 预估时间 |
|------|---------|
| MCP 兼容性评估 + 方案选择 | 2 天 |
| MCP Client 迁移 | 2 天 |
| 知识库向量化 | 3 天 |
| RAG Tool 实现 | 2 天 |
| Agent 集成 + 测试 | 2 天 |
| 评估调优 | 3 天 |
| **合计** | **~14 天** |
