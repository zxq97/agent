# Phase 6: RAG 增强 & 生产化 (Knowledge Enhancement & Production)

## 目标

接入向量数据库实现知识库检索增强，提升意图检测准确性，完善监控、限流、降级等生产化能力。

## 前置条件

- Phase 5 履约支持 Skill 已完成
- 向量数据库已选型并部署（Milvus / pgvector）
- 知识文档已整理（规则文档、FAQ、政策文件）

## 交付物清单

| # | 交付物 | 说明 |
|---|--------|------|
| 1 | 知识库构建管道 | 文档切分、向量化、入库 |
| 2 | RAG Tool 实现 | 知识检索 Tool |
| 3 | 各 Skill 集成 RAG | Skill 可调用知识检索补充背景 |
| 4 | 意图检测优化 | 基于示例库的准确率提升 |
| 5 | 监控指标 | Prometheus 指标暴露 |
| 6 | 限流降级 | API 限流 + LLM 降级策略 |
| 7 | 性能优化 | 缓存、并发、Token 优化 |
| 8 | 部署配置 | Docker + K8s 配置 |

## 详细步骤

### Step 6.1: 知识库构建管道

**文件**: `internal/knowledge/pipeline.go`

```
知识文档（PDF/Markdown/HTML）
       │
       ▼
[1] 文档解析 ─── 提取纯文本
       │
       ▼
[2] 文档切分 ─── 按段落/标题切分，每块 300-500 字
       │             添加元数据（来源、章节、更新时间）
       ▼
[3] 向量化 ──── 调用 Embedding API 生成向量
       │
       ▼
[4] 入库 ────── 写入向量数据库
                  向量 + 原文 + 元数据
```

知识库内容分类：

| 类别 | 来源 | 更新频率 | 示例 |
|------|------|---------|------|
| 租车规则 | 运营团队文档 | 月度 | 取还车规则、验车标准 |
| 保险条款 | 保险合作方文档 | 季度 | 各保险产品的详细条款 |
| 费用政策 | 财务团队文档 | 月度 | 退费政策、手续费标准 |
| FAQ | 客服团队积累 | 周度 | 用户高频问题及标准答案 |
| 违章处理 | 法务团队文档 | 季度 | 各城市违章处理差异 |

### Step 6.2: RAG Tool 实现

**文件**: `internal/tool/knowledge/search.go`

```go
// SearchKnowledge 知识库检索
// 参数:
//   - query: 检索问题（必填）
//   - category: 知识类别：rule / faq / policy（可选）
//   - top_k: 返回结果数量，默认 3（可选）
// 返回: 相关知识片段列表
```

**文件**: `internal/knowledge/store.go`

```go
type Store interface {
    // Search 向量检索
    Search(ctx context.Context, query string, opts SearchOptions) ([]Document, error)

    // Index 索引文档
    Index(ctx context.Context, docs []Document) error

    // Delete 删除文档
    Delete(ctx context.Context, ids []string) error
}

type Document struct {
    ID       string
    Content  string            // 原文
    Vector   []float64         // 向量
    Metadata map[string]string // 元数据
    Score    float64           // 相似度分数
}
```

### Step 6.3: 各 Skill 集成 RAG

RAG Tool 注册为所有 Skill 可用的公共 Tool，在各 Skill 的 System Prompt 中说明使用场景：

**Vehicle Skill 补充**：
```
当用户询问某车型的具体配置或特色时，可以调用 search_knowledge 查询更详细的介绍资料。
```

**Insurance Skill 补充**：
```
当用户询问保险条款的具体细节（如免责条款、理赔时效）时，可以调用 search_knowledge 查询保险条款原文。
```

**Billing Skill 补充**：
```
当用户对费用规则有深入疑问（如退费政策的具体条款）时，可以调用 search_knowledge 查询费用政策文档。
```

**Fulfillment Skill 补充**：
```
当用户询问特殊场景的规则（如跨国租车、特定城市政策差异）时，可以调用 search_knowledge 查询相关规则文档。
```

### Step 6.4: 意图检测优化

#### 方案：Few-shot 增强

在意图检测的 Prompt 中注入相似的历史对话作为 few-shot 示例：

```go
func (d *Detector) Detect(ctx context.Context, session *Session, query string) (string, float64) {
    // 1. 从示例库中检索与当前 query 相似的对话
    examples := d.exampleStore.Search(query, 5)

    // 2. 构建 few-shot prompt
    prompt := buildIntentPrompt(query, examples)

    // 3. 调用 LLM
    result := d.llm.Chat(ctx, prompt)

    // 4. 解析意图和置信度
    return result.Intent, result.Confidence
}
```

#### 方案：混合路由

```
用户消息
   │
   ├──[1] 关键词匹配（快速路径，0ms）
   │     "保险" → insurance
   │     "退款" → billing
   │     "违章" → fulfillment
   │
   ├──[2] LLM 意图检测（准确路径，~200ms）
   │     使用 few-shot 增强
   │
   └──[3] 置信度融合
         关键词匹配 + LLM 结果 → 最终意图
```

### Step 6.5: 监控指标

**文件**: `internal/server/metrics.go`

```go
// Prometheus 指标定义
var (
    // 请求维度
    RequestTotal     *prometheus.CounterVec   // method, path, status
    RequestDuration  *prometheus.HistogramVec // method, path

    // LLM 维度
    LLMCallTotal     *prometheus.CounterVec   // model, skill, stop_reason
    LLMCallDuration  *prometheus.HistogramVec // model, skill
    LLMTokenUsage    *prometheus.CounterVec   // model, type (input/output)
    LLMCallErrors    *prometheus.CounterVec   // model, error_type

    // Tool 维度
    ToolCallTotal    *prometheus.CounterVec   // tool_name, success
    ToolCallDuration *prometheus.HistogramVec // tool_name

    // 会话维度
    SessionActive    prometheus.Gauge         // 当前活跃会话数
    SessionTurnCount *prometheus.HistogramVec // skill

    // 意图检测维度
    IntentDetected   *prometheus.CounterVec   // intent, confidence_range
)
```

### Step 6.6: 限流降级

**限流策略**：

```
全局 QPS 限制: 1000 req/s
单用户 QPS 限制: 10 req/s
单会话并发限制: 1（同一会话同时只处理一个请求）
LLM API 调用限制: 根据 API 配额动态调整
```

**降级策略**：

```
降级级别   触发条件                         行为
─────────────────────────────────────────────────────────
L0 正常    LLM 响应正常                     全功能
L1 简化    LLM 延迟 > 5s 或限流             跳过意图检测，用关键词路由
                                              禁用 RAG Tool，仅用业务 API
L2 受限    LLM 不可用 (>50% 请求失败)       仅关键词匹配 + 预设回复模板
L3 熔断    LLM 完全不可用                   返回"系统维护中"友好提示
```

### Step 6.7: 性能优化

#### 缓存策略

```
[1] Tool 结果缓存
    - 车型搜索结果：5 分钟（车型列表变化不频繁）
    - 保险方案查询：10 分钟
    - 费用规则查询：30 分钟
    - 缓存 Key: tool_name + sorted_params_hash

[2] 对话摘要
    - 当对话历史超过 20 轮时，自动摘要压缩
    - 保留最近 5 轮完整消息 + 之前轮次的摘要
    - 减少每次 LLM 调用的 Token 消耗

[3] 流式首字延迟优化
    - 并行执行：意图检测和 Skill 路由并行
    - 预加载：用户发消息时，预判可能需要的 Tool 并预加载
```

#### Token 优化

```
[1] Tool Result 精简
    - 只返回 LLM 需要的关键字段
    - 大列表只返回 top-N
    - 数值保留必要精度

[2] System Prompt 压缩
    - 每个 Skill 的 System Prompt 精简到 < 500 Token
    - 动态注入：只注入当前 Skill 的 Prompt

[3] 对话窗口管理
    - 滑动窗口：保留最近 N 轮完整消息
    - 早期消息摘要压缩
```

### Step 6.8: 部署配置

**Dockerfile**：

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -o /agent ./cmd/http

FROM alpine:3.19
COPY --from=builder /agent /agent
COPY configs/ /configs
EXPOSE 8080
CMD ["/agent"]
```

**K8s Deployment**：

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: car-rental-agent
spec:
  replicas: 3
  selector:
    matchLabels:
      app: car-rental-agent
  template:
    spec:
      containers:
      - name: agent
        image: car-rental-agent:latest
        ports:
        - containerPort: 8080
        resources:
          requests:
            cpu: 500m
            memory: 512Mi
          limits:
            cpu: 2000m
            memory: 1Gi
        env:
        - name: CLAUDE_API_KEY
          valueFrom:
            secretKeyRef:
              name: agent-secrets
              key: claude-api-key
        livenessProbe:
          httpGet:
            path: /api/v1/health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /api/v1/health
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
```

### Step 6.9: 集成测试场景

1. **RAG 检索**：问"保险免赔条款具体内容" → 检索保险条款原文
2. **RAG + Tool**：问"我的违章怎么处理" → Tool 查订单 + RAG 补充规则
3. **意图优化**：模糊问题（"这个怎么办"）→ 结合上下文准确识别
4. **降级测试**：模拟 LLM 超时 → 降级到关键词路由
5. **限流测试**：并发请求 → 正确限流
6. **缓存测试**：重复查询 → 命中缓存，响应更快
7. **长对话**：20+ 轮 → 对话摘要压缩正常

## 验收标准

- [ ] RAG 检索返回相关知识片段
- [ ] 各 Skill 能正确调用 RAG Tool
- [ ] 意图检测准确率 > 90%
- [ ] Prometheus 指标正确暴露
- [ ] 限流策略生效
- [ ] 降级策略各级别正常工作
- [ ] Docker 镜像构建成功
- [ ] K8s 部署配置可用
- [ ] 长对话摘要压缩正常
- [ ] Tool 结果缓存生效
