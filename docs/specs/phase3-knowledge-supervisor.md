# Phase 3: 知识库 + 条款解读 + Supervisor 多 Agent

> **目标:**
> 1. 把租车计费、履约、保险三类条款灌入 `knowledge/`,做 BM25 检索
> 2. 新增 `search_knowledge` tool,所有条款类答复带 `[来源]` 角标
> 3. 单 ReAct agent → 拆 supervisor + 多子 agent(`Shopping / Insurance / Knowledge`)

---

## 1. Context

P1+P2 单 agent 能处理"导购 + 价格 + 保险",但当用户问:
- "超时 1 小时怎么算钱?"
- "在外地剐蹭走哪种保险?"
- "押金多久能退?"

这类**条款类问题**需要查知识库,LLM 不能自由发挥(否则编错条款 → 投诉/资损)。
同时单 agent 的 prompt 已经在膨胀(导购 + 价格 + 保险 + 条款 → 容易互相干扰),引入 supervisor 做职责拆分。

---

## 2. 验收标准

### 2.1 必过 demo
```
用户: 超时 1 小时怎么算钱?
agent: [supervisor 路由 → KnowledgeAgent]
       [search_knowledge(query="超时还车 费用", category="billing")]
       超时 1 小时按 1 小时计:
       - 超时 < 4 小时:按小时计,价格 = 日租金 / 8(参考下方条款)
       - 超时 ≥ 4 小时:按 1 天计
       [来源: knowledge/billing/overtime.md#section-2]

用户: 押金什么时候退?
agent: [KnowledgeAgent] [search_knowledge(query="押金 退款", category="billing")]
       押金一般在还车后 3-7 个工作日原路退回...
       [来源: knowledge/billing/deposit.md#refund]

用户: 那帮我重新报个北京的价
agent: [supervisor 路由 → ShoppingAgent]
       [list_quotes ...]
```

### 2.2 检查清单
- [ ] `knowledge/` 下三类目录各至少 3 个 Markdown 片段
- [ ] BM25 检索:输入查询,返回 top-5 片段(含路径 + score)
- [ ] `search_knowledge` 出参含 `source` 字段(路径 + 锚点)
- [ ] 条款类答复**必须**含 `[来源: xxx]`,**无来源命中时** agent 回复"建议联系客服核实"
- [ ] supervisor 能按 phase + 关键字路由(不是关键词匹配,见 P3-D2)
- [ ] 跨 agent 切换时 `ConversationState.History` 完整保留

---

## 3. 分步实现

### Step 1 — 知识库内容整理
**目录:** `knowledge/{billing,fulfillment,insurance}/`

每个目录至少 3 个 .md 文件,每个文件按"小标题分段"组织。建议结构:
```
knowledge/billing/
  daily_rate.md           # 日租金计算
  overtime.md             # 超时费用
  mileage.md              # 超公里费
  deposit.md              # 押金规则
knowledge/fulfillment/
  pickup_return.md        # 取还车流程
  delay_return.md         # 延期还车
  early_return.md         # 提前还车
  violation.md            # 违章处理
knowledge/insurance/
  basic_insurance.md      # 基础保险
  add_on_insurance.md     # 加购保险
  claim_process.md        # 出险理赔流程
```

每段格式:
```markdown
## 超时费用 {#overtime}

超时 < 4 小时:按小时计,价格 = 日租金 / 8
超时 ≥ 4 小时:按 1 天计
夜间超时(22:00-6:00)有额外服务费 ...
```

**来源标记:** 在 search_knowledge 返回时拼成 `knowledge/billing/overtime.md#overtime`。

### Step 2 — Retriever 抽象 `internal/rag/retriever.go`
```go
type Doc struct {
    Path    string            // "knowledge/billing/overtime.md"
    Anchor  string            // "overtime"
    Title   string
    Content string
    Score   float64
    Meta    map[string]string
}

type Retriever interface {
    Search(ctx context.Context, query string, category string, topK int) ([]Doc, error)
}
```

### Step 3 — BM25 实现 `internal/rag/bm25.go`
- 启动时扫 `knowledge/` 全量切片
- 中文分词:用 [github.com/yanyiwu/gojieba](https://github.com/yanyiwu/gojieba) 或 简单按字符 + 标点
- BM25:实现一份或用 [github.com/blevesearch/bleve](https://github.com/blevesearch/bleve) 的 scoring 子模块
- 内存索引(知识量小,< 1MB)

**P3 内允许:** 先用最朴素实现(jieba + 自写 BM25,~150 行),拿到检索分就行,后续可换 bleve。

### Step 4 — `search_knowledge` tool
**文件:** `internal/tools/search_knowledge.go`

```go
type SearchKnowledgeInput struct {
    Query    string `json:"query"     jsonschema:"description=查询语句"`
    Category string `json:"category"  jsonschema:"description=类别:billing / fulfillment / insurance / all,默认 all"`
    TopK     int    `json:"top_k"     jsonschema:"description=返回条数,默认 5,最大 10"`
}
type KnowledgeHit struct {
    Source  string `json:"source"`  // "knowledge/billing/overtime.md#overtime"
    Title   string `json:"title"`
    Snippet string `json:"snippet"` // 命中段落正文(可截断)
    Score   float64 `json:"score"`
}
type SearchKnowledgeOutput struct {
    Hits     []KnowledgeHit `json:"hits"`
    HasMatch bool           `json:"has_match"`
    Note     string         `json:"note,omitempty"`
}
```

### Step 5 — Supervisor + 子 Agent 拆分
**目录:** `internal/agent/`

```
agent/
  shopping.go          # 已有,P3 重构成 ChatModelAgent
  insurance.go         # 新拆出来
  knowledge.go         # 新建
  supervisor.go        # 新建,基于 eino ADK Supervisor 模式
```

**Supervisor 路由策略**(基于 phase + slot 完整度,**不**做关键词匹配):
- `Phase == Shopping` 且 slot 未填满 → ShoppingAgent(继续追问/报价)
- `Phase == Shopping` 且用户问价格构成 → ShoppingAgent(它会调 get_price_detail)
- 用户消息含"超时/押金/违章/保险条款/取还车流程"等**意图信号**(由 LLM 分类,不靠正则)→ KnowledgeAgent
- 用户开始问"保险怎么选" → InsuranceAgent
- KnowledgeAgent / InsuranceAgent 答完后,**回到 supervisor**,根据后续话再路由

**eino ADK 实现路径:**
- 用 [Supervisor](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_implementation/) 模式
- 子 agent 都是 `ChatModelAgent`,各自绑定 **不同的 toolset 子集**:
  - ShoppingAgent:`list_quotes / list_stores / list_vehicles / get_price_detail`
  - InsuranceAgent:`list_insurances`
  - KnowledgeAgent:`search_knowledge`

### Step 6 — Prompt 拆分
**文件:** `internal/prompt/{shopping,insurance,knowledge,supervisor}_system.go`

- Supervisor prompt:只描述"如何分配任务给子 agent"
- ShoppingAgent prompt:聚焦"找车 / 报价 / 价格明细"
- InsuranceAgent prompt:聚焦"保险选择 / 风险讲解",含合规红线
- KnowledgeAgent prompt:**强制**"答复必须含 `[来源: 路径#锚点]`,若 has_match=false 则回复'建议联系客服核实'"

### Step 7 — ConversationState 接入
**关键:** P3 起 supervisor 必须从 `internal/orchestration/state.go` 读写状态。
- 各子 agent 通过 eino ADK 的 session/state 机制共享(具体 API 待 eino ADK 文档落地)
- 跨 agent 切换时 `History` 必须完整传递

### Step 8 — 验收
- 沿用 P2 评估集,加 10 条条款类问题
- 检查项:命中 `[来源]` 率 ≥ 95%,跨 agent 切换不丢上下文

---

## 4. 文件清单(P3 增量)

```
internal/rag/retriever.go              # 新增
internal/rag/bm25.go                   # 新增
internal/rag/loader.go                 # 新增(扫 knowledge/ 切片)
internal/tools/search_knowledge.go     # 新增
internal/agent/shopping.go             # 重构
internal/agent/insurance.go            # 新增
internal/agent/knowledge.go            # 新增
internal/agent/supervisor.go           # 新增
internal/prompt/{shopping,insurance,knowledge,supervisor}_system.go
knowledge/billing/*.md                 # 内容
knowledge/fulfillment/*.md
knowledge/insurance/*.md
docs/specs/phase3-knowledge-supervisor.md  # 本文档
```

---

## 5. 关键决策

| # | 决策 | 理由 |
|---|---|---|
| P3-D1 | BM25 而非向量检索 | 知识量小;省合规审批与外部依赖;`Retriever` 接口预留 |
| P3-D2 | 路由用 LLM 意图分类而非关键词正则 | 关键词不可靠(沿用旧 spec 审查结论);用一个 haiku 级 LLM 做轻量分类即可 |
| P3-D3 | KnowledgeAgent 必须带 `[来源]` | 合规硬约束;tool 输出强制带 source 字段,prompt 强制引用 |
| P3-D4 | 无命中时**不让 LLM 编**,统一回客服话术 | 防幻觉 |
| P3-D5 | 子 agent 各持子 toolset | 缩小 LLM 决策空间,提升准确率 |

---

## 6. 已识别 TODO(P3 内必清)

- [ ] 中文分词方案确定(jieba vs 字符切分)
- [ ] 知识内容来源(从已有 PDF / 内部 wiki 转 md)— **业务方提供原文**
- [ ] eino ADK Supervisor API 调研落到具体代码模板
- [ ] 路由 LLM 选型:用 deepseek-chat 还是新加更便宜的 haiku 级
- [ ] 评估集扩到 30 条
