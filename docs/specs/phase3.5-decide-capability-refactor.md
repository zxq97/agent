# Phase 3.5: 借鉴 tyche V4 的工程化重构路线图(总纲 / 索引)

> **目标:** 借鉴 tyche 当前 V4 主链路的成熟工程做法,把 P3 的 supervisor + 子 agent ReAct 全面重构为「Decider 单次流式 function-calling + 责任链流水线 + Capability 编排 + ID Go 托管」,并把 tyche 已经踩过坑沉淀下来的工程资产(状态前缀 / history 回放 / 流式单流 / 思考头 / 引导胶囊 / 异步审核 / 反馈胶囊 / 对话摘要 / 分布式锁 / 场景知识库 / 可观测性)一并纳入。
>
> **本文档是总纲 + 子阶段索引**。每个子阶段拆成一份独立可执行的 spec(见 §4),按需单独领取。
>
> **不做的:**
> - ❌ 不抄 tyche 自己已回退的 L1 路由 / PolicyStage / Belief 三件套(对单产品线过重)
> - ❌ 不纳入 P3 真 RAG 落地 / P5 真业务接入 / P6 完整生产化(各自独立立项)
>
> **范围与原则三条边界**:
> 1. 范围 = 只做"借鉴 tyche 的工程化改造"
> 2. 架构走向 = **改造为责任链流水线**,取消 ADK supervisor + ChatModelAgent ReAct
> 3. P5 占位 tool(check_qualification / estimate_trip_cost / optimize_pickup_time)= **从 LLM 可见 toolset 摘掉**,代码保留

---

## 1. 改造背景

### 1.1 当前架构问题(P3 实测)

| 问题 | 表现 |
|---|---|
| LLM 调用次数多 | 一次"明天北京租 SUV 报价"≈ 6 次:Supervisor 路由 + ShoppingAgent ReAct 内 search_locations→resolve_poi→search_quotes→话术 + Supervisor 收尾 |
| 闲聊/越界也走全套 | "你好"也要 Supervisor ReAct 一轮 + 子 agent 一轮,全是大模型 |
| ID 幻造风险高 | `context_id`/`reference_id` 靠 prompt 让 LLM 在 assistant 文本里"自己保存",ReAct 多轮易丢/改/编(护栏 1、2 实质是**软约束**) |
| transfer 来回倒腾 | Supervisor 与子 agent 之间反复 transfer 已在 prompt 里硬约束,但仍是软约束 |
| 话术不连贯 | Supervisor 与子 agent 各生成一段文字拼接,语气割裂 |
| 用户感知响应慢 | 调工具中间空档完全沉默,首字节通常 3-5s |
| 上下文质量差 | history 拍平后 LLM 看不到上轮"真发过工具调用"的先例;关键状态散落在自然语言里 |
| 无安全/反馈/可观测 | 无前置审核、无 badcase 通道、文本日志无 metric |
| 单 user 并发安全 | 同 uid+session 并发请求可能同时跑两路 LLM,Redis history 互相覆盖 |

### 1.2 对照 tyche V4 的 13 条借鉴

P0(三条最关键):
1. **后端结构化保存 quote 状态** + 拼成"## 当前会话状态"前缀给 LLM(取代 LLM 自己在 history 里存 ID)
2. **history 回放工具调用**:`assistant(tool_calls) + tool(content)` 还原,而非拍平成 assistant 文本
3. **流式 function-calling 单流**:content 边吐边出,流末 tool_calls 拼出 Decision

P1:
4. 责任链流水线(替代 ADK supervisor) 5. 异步内容审核管道 6. quick_action 引导胶囊 + slot_patch 协议
7. 分布式锁(同 uid+session 串行化) 8. 反馈胶囊 → badcase 落库 9. 对话摘要(模板化非 LLM)

P2:
10. 静态召回快路(本期不做,留给 P3 RAG) 11. 场景知识库表 12. 思考头/思考框 SSE 协议 13. LLM client 端 trace + metric + 流式收尾日志

### 1.3 明确不抄

- ❌ L1 小模型前置路由(tyche `v4_stage_l1route.go`,已在 master 删除)
- ❌ PolicyStage 纯函数裁决(已删除)
- ❌ Belief 置信度衰减/冲突衰减/归类校正(已删除)
- ❌ tyche 的多模态贵必赔、ISM 真审核、AB 实验框架(P6 单独立项)

---

## 2. 目标架构

```
HTTP /agent/chat
   │  middleware: recover → trace → auth → ratelimit → access_lock(3.5.5)
   ▼
[加载 ConversationState] ← Redis (RentalCtx / LastQuotes / Profile / Summary)
   │
   ▼
ChatPipeline(责任链,Stage 单一职责,Signal=Continue/Stop)
   ├─ PreRouteStage      Go 零LLM
   │                       · 反问解挂(Clarification.ProceedInput 覆盖 UserInput)
   │                       · action_click(slot_patch)协议短路 → 直接 search
   │                       · 安全红线快路(关键词命中)
   ├─ DecideStage        ★ LLM #1 一次流式 function-calling(DeepSeek)
   │                       content → SSE text(逐字),末帧 tool_calls → Decision
   │                       工具集 = {search_vehicles, ask, get_price_detail,
   │                                 insurance, interpret_rules}
   │                       tool_choice=auto;不调=纯回复(闲聊/越界)
   │   ├─ 不调 tool ──► PureReplyCapability  ← 0 个额外 LLM
   │   ├─ ask ────────► AskCapability        ← 0 个额外 LLM(话术复用 Decide content)
   │   ├─ search ─────► SearchCapability     ← Go 编排 locations→poi→quotes
   │   ├─ price_detail► PriceDetailCapability← Go 调 get_order_details + LLM #2 讲解
   │   ├─ insurance ──► InsuranceCapability  ← 同上 + LLM #2 保险话术
   │   └─ rules ──────► RulesCapability      ← RAG(P3) + LLM #2 grounded 答
   ├─ GuideStage         ★ 仅 Search 命中且有车:LLM #2 流式产引导语 + 引导胶囊
   ├─ ClarifyStage       反问渲染(question + options)
   └─ FinalizeStage      落 state、写 history、对话摘要、推 SSE done、异步落 Redis

   旁路:
   • AsyncAuditor   输入审 + 输出分段送审,命中 → done{guardrail}
   • TraceSink      LLM/Tool/Stage 三层 metric + 流式收尾日志
```

**预期效果**:
- 单轮 LLM 调用从 ~5-6 次降至 1-2 次
- 用户感受到的首字节延迟 < 1s(流式 content 与 tool_call 同流)
- 关键 ID(context_id/reference_id/supplier)不再依赖 LLM 自己写在历史里
- 具备生产化最小集合:异步审核 / 分布式并发锁 / 反馈采集 / 流式日志 + metric

---

## 3. 整体排期

总共 **6 个子阶段 × 1-4 PR = 12 个 PR**,顺序执行。每个 PR 独立可灰度,可随时停在任一阶段交付。

| 子阶段 | PR 数 | 工时 | 累计 | 关键产出 |
|---|---|---|---|---|
| 3.5.1 收口与勘误 | 1 | 0.5 天 | 0.5 | P5 占位 tool 摘掉,清场 |
| 3.5.2 决策核心重构 | 4 | 5-7 天 | 6-7.5 | Decider + Capability + ID 托管 + handler 切流量 |
| 3.5.3 上下文与流式提质 | 2 | 4-5 天 | 10-12.5 | history 回放 + 状态前缀 |
| 3.5.4 SSE 协议升级 + 引导/反馈 | 2 | 3-4 天 | 13-16.5 | thinking_tips/box + quick_action + 反馈胶囊 |
| 3.5.5 生产化基建 | 2 | 4-5 天 | 17-21.5 | 异步审核 + 分布式锁 + 摘要 + metric |
| 3.5.6 Prompt 工程化沉淀 | 1 | 2-3 天 | 19-24.5 | 场景知识库 + required_slots + Profile |
| **合计** | **12** | **19-24.5 天** | **~4-5 周** |

**最小可生产灰度建议** = 3.5.1 + 3.5.2 + 3.5.3 + 3.5.5-D1(分布式锁/审核/摘要),3.5.4 / 3.5.5-D2 / 3.5.6 可滚动加。

**依赖关系:** 3.5.1 → 3.5.2 是硬前置;3.5.3 / 3.5.4 / 3.5.6 都依赖 3.5.2;3.5.5 只依赖 3.5.2(可与 3.5.3/3.5.4 并行);3.5.6 建议在 3.5.3 之后(BuildStatePrefix 已能带 profile)。

**保留不动:**
- `internal/tools/local_*.go`(占位 P5 工具)—— 3.5.1 起从 LLM 可见 toolset 摘掉,代码保留以便未来真业务接回
- `internal/tools/logging.go` / `common.go` 白名单逻辑
- `internal/session/`(P4 Redis session)、`internal/rag/`(P3 BM25)

---

## 4. 子阶段 spec 索引

每个子阶段是一份**独立可执行**的 spec,含动机、改动清单、详细设计、验收、风险:

| 子阶段 | 文档 | 一句话 |
|---|---|---|
| **3.5.1** | [phase3.5.1-cleanup.md](phase3.5.1-cleanup.md) | 占位 tool 摘掉、死字段标 Deprecated、清场 |
| **3.5.2** | [phase3.5.2-decide-core.md](phase3.5.2-decide-core.md) | **承重墙**:Decider + 6 Capability + ResolveQuoteRef + ID 托管 + handler 切流量(A1~A4) |
| **3.5.3** | [phase3.5.3-context-streaming.md](phase3.5.3-context-streaming.md) | history 回放工具调用 + BuildStatePrefix 结构化状态前缀(B1、B2) |
| **3.5.4** | [phase3.5.4-sse-guide-feedback.md](phase3.5.4-sse-guide-feedback.md) | SSE 协议升级(thinking_tips/box/card/quick_action)+ 引导胶囊 + 反馈采集(C1、C2) |
| **3.5.5** | [phase3.5.5-productionize-infra.md](phase3.5.5-productionize-infra.md) | 异步审核 + 分布式锁 + 对话摘要 + 三层 metric/流式日志(D1、D2) |
| **3.5.6** | [phase3.5.6-prompt-knowledge.md](phase3.5.6-prompt-knowledge.md) | 场景知识库表 + required_slots + profile_patch + 中文铁律(E) |

---

## 5. 贯穿所有子阶段的设计原则

1. **ID 永不让 LLM 经手**:context_id / reference_id / supplier 一律 Go 注入,工具 schema 不暴露;`ResolveQuoteRef` 把"第一辆/朗逸"翻译成 ref。
2. **stage 单一职责 + Signal 流转**:Stage 加减不影响别人;每步打 `start/done` + `stage_info` 日志。
3. **流式优先**:DecideStage 用流式 function-calling,Capability 内二次 LLM 尽量也流式;非流式只用于规则化转换(ResolveQuoteRef / 状态前缀 / 摘要)。
4. **配置开关默认安全**:`Mode=pipeline`、`Audit.Enabled=false`(P6 再开)、`LocalTools.Enabled=false`、`AccessLock.Enabled=true`。
5. **向后兼容 SSE**:旧客户端只识别 `message` 仍能用;新事件类型增量加。
6. **prompt 即代码**:任何 ≥3 处用到的"领域规则"必须抽成 Go 表 + render 函数,不在 prompt 字符串里散落。

---

## 6. 通用验收(每个子阶段都做)

1. **新增/改动文件**:`go test ./...` 全绿;新增模块单测覆盖核心分支(尤其 `ResolveQuoteRef` / `BuildStatePrefix` / `SceneKnowledge` / `Summarizer` / `AccessLock`)。
2. **CLI 联调**:`go run ./cmd/cli -env dev`,8 类 case 全过(搜车、看明细、保险、规则、闲聊、越界、反问解挂、指代多义)。
3. **HTTP 联调**:`curl -N -H "X-User-Id: u1" -d '{"session_id":"s1","message":"明天北京 SUV"}' /agent/chat` 看 SSE 流;3.5.4 后模拟点击胶囊;3.5.5 后并发请求看 access_lock。
4. **Trace 抽检**:能从一条 trace_id 串起 stage + LLM + tool 的全部调用与耗时。

> 各子阶段的**专项验收清单**见各自文档的"验收"小节。

**关键里程碑**:
- **3.5.2 完成**:同一 case 双跑(supervisor vs pipeline),LLM 调用次数 ≥ 50% 下降;闲聊 = 1 次 LLM。
- **3.5.5 完成**:压测同 user 50 QPS,access_lock 工作,异步审核延迟 P95 < 100ms 不阻塞主流程,指标全在 metric 里。

---

## 7. 与现有 Phase 文档的关系

- 本文档对应 **Phase 3.5**,夹在 P3(supervisor 多 agent)和 P4(已交付的 HTTP/Session/MCP)的工程化补强之间。
- **P3 已交付的 RAG / clarification / session 草稿全部保留并复用**;3.5.2 RulesCapability 直接接 P3 的 BM25 RAG。
- **P4 的 HTTP/SSE/Session**:3.5.2 把 handler 内部从 ADK Runner 切到 ChatPipeline.Run;3.5.4 在 SSE 协议增量加新事件(向后兼容);3.5.5 给中间件链最外层加分布式锁。
- **P5 扩展能力改造后更顺**:本地工具改为 Capability 内部直调;3.5.1 已先摘掉占位 tool,P5 真业务接入时直接落到对应 Capability。
- **P6 生产化**:3.5.5 已把审核管道、分布式锁、metric 三个骨架占住,P6 只需接 ISM / 改 Redis 跨 pod 限流 / 接公司 metric SDK,**避免 P6 大改架构**。
- 同步更新(3.5.2 的 A4 落地时一并改):
  - [docs/technical-plan.md](../technical-plan.md) Phase 索引、架构图、关键决策、风险登记(已随方案搬入项目时更新)
  - [CLAUDE.md](../../CLAUDE.md) 安全护栏 1、2 文案(从"LLM 必须保存 ID"改为"Go 托管,LLM 不经手")

---

## 8. 不在本路线图范围(后续独立立项)

- ❌ tyche 的 L1 小模型分流(对单产品线过重,且 tyche 自己已回退)
- ❌ Belief 置信度衰减(短会话用不上,P6 再评估)
- ❌ P3 真 RAG 落地(灌库 + BM25 + `search_knowledge` tool)— 由 P3 自身收口
- ❌ P5 真业务接入(资质、比价异议、售后查询接 saas-api)— 取决于业务方
- ❌ P6 完整生产化(ISM 真审核、A/B、限流改 Redis 跨 pod、压测、灾备)
- ❌ 多模态贵必赔、WebSocket、把 rental-agent 自己作为 MCP server 暴露

---

**Owner:** TBD
**预计工期:** 12 PR × 1-2 天 ≈ 4-5 周(单人)
**前置依赖:** P3 已合入(eino ADK + RAG 骨架 + clarification);P4 已合入(HTTP/SSE/Session)
