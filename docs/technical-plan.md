# 租车智能体 - 技术方案总纲

## 1. 背景与目标

### 1.1 为什么做
C 端租车用户面对车型、套餐、保险、各类费用项时决策成本高;在线客服人力密集且响应慢。
一个能听懂自然语言、调真实报价接口、把价格条款讲明白的 agent,可同时降低用户决策门槛和客服成本。

### 1.2 目标用户
**C 端租车用户**(非客服坐席、非商家)——决定了能力边界:
- ✅ 可以:推荐、解读、查询、引导跳转
- ❌ 不能:在 agent 内闭环下单、修改订单、操作退款 / 理赔

### 1.3 不做什么(显式排除)
- 不在 agent 内执行写操作(create_order / pay / refund / modify_order)
- 不替代客服处理理赔申诉、人工审核
- 不做 B 端商家管理 / 资金对账
- P1-P3 不做向量检索(用 BM25 兜底,知识量小)

---

## 2. 架构总览

### 2.1 P3 形态(supervisor + ChatModelAgent ReAct,已交付)

```
┌──────────────────────────────────────────────────────────────┐
│                 App / 小程序 / 客服系统(P4+)                │
└──────────────────────┬───────────────────────────────────────┘
                       │ HTTP + SSE (session_id)
                       ▼
┌──────────────────────────────────────────────────────────────┐
│  rental-agent (Go, eino)                                     │
│  ┌──────────────────────────────────────────────────────────┐│
│  │  cmd/http (P4+) / cmd/cli (P1-P3 调试)                   ││
│  └────────────────────────┬─────────────────────────────────┘│
│                           ▼                                  │
│  ┌──────────────────────────────────────────────────────────┐│
│  │  Supervisor (P3+)                                        ││
│  │     ├─ ShoppingAgent      ┐                              ││
│  │     ├─ InsuranceAgent     │  eino ChatModelAgent         ││
│  │     ├─ KnowledgeAgent     │  (ReAct loop)                ││
│  │     ├─ AftersalesAgent    │                              ││
│  │     └─ ComparePriceAgent  ┘                              ││
│  └────────────────────────┬─────────────────────────────────┘│
│                           ▼                                  │
│  ┌──────────────────────────────────────────────────────────┐│
│  │  Tools (eino InvokableTool,LLM 可见)                    ││
│  │     rental_search_locations / resolve_poi / search_quotes││
│  │     rental_get_order_details / get_reservation / driver  ││
│  │     search_knowledge(P3) / build_order_deeplink(P5)      ││
│  └─────┬───────────────────────────────────────┬────────────┘│
│        ▼                                       ▼             │
│  ┌──────────────┐                       ┌───────────────┐    │
│  │ tyche MCP    │                       │  RAG (BM25)   │    │
│  │ JSON-RPC HTTP│                       └───────────────┘    │
│  └──────────────┘                                            │
└──────────────────────────────────────────────────────────────┘
```

### 2.2 P3.5 目标态(责任链流水线 + Decider + Capability)

```
HTTP /agent/chat
   │  middleware: recover → trace → auth → ratelimit → access_lock(P3.5)
   ▼
[加载 ConversationState] ← Redis (RentalCtx / LastQuotes / Profile / Summary)
   │
   ▼
ChatPipeline(责任链,Stage 单一职责,Signal=Continue/Stop)
   ├─ PreRouteStage      Go 零LLM:反问解挂、action_click(slot_patch)短路、安全红线快路
   ├─ DecideStage        ★ LLM #1 一次流式 function-calling
   │                      content → SSE text(逐字),末帧 tool_calls → Decision
   │                      工具集 = {search_vehicles, ask, get_price_detail,
   │                                insurance, interpret_rules}
   │                      tool_choice=auto,不调=纯回复(闲聊/越界)
   ├─ CapabilityStage    ★ Go 编排 Capability(Search/Ask/PriceDetail/Insurance/
   │                      Rules/PureReply);Capability 内按需做 LLM #2 流式
   │   │── tyche MCP tools(只在 Capability 内部调,LLM 不可见)
   │   └── RAG (P3 BM25)
   ├─ GuideStage         ★ 仅 Search 命中且有车:LLM #2 流式产引导语 + 引导胶囊
   ├─ ClarifyStage       反问渲染(question + options)
   └─ FinalizeStage      落 state、写 history、推 SSE done、异步落 Redis

   旁路:
   • AsyncAuditor   输入审 + 输出分段送审,命中 → done{guardrail}
   • TraceSink      LLM 调用 metric / tool 调用 metric / pipeline stage 耗时

LLM Factory(可插拔 provider,P3.5 起所有 LLM 调用走流式 + 收尾日志):
   DeepSeek(默认) / Claude / 千问 / 豆包
```

**关键变化**(详见 [specs/phase3.5-decide-capability-refactor.md](specs/phase3.5-decide-capability-refactor.md)):
- 取消 supervisor + ChatModelAgent ReAct,改为责任链 Pipeline
- ID(`context_id` / `reference_id` / `supplier`)由 Go 从 `state` 注入,不再让 LLM 经手
- 工具 schema 不暴露 ID 字段;`ResolveQuoteRef` 把"第一辆/朗逸"翻译成 ref
- ConversationState 扩展结构化字段(RentalCtx / LastQuotes / Profile / Summary)
- SSE 协议增量扩展:thinking_tips / thinking_box / card / quick_action / meta(向后兼容)

---

## 3. 技术选型

| 维度 | 选型 | 理由 |
|---|---|---|
| 语言 | Go 1.24 | 对齐 4 个后端服务的栈 |
| Agent 框架 | [eino](https://github.com/cloudwego/eino) | 字节开源 Go 版 LangGraph,内置 ReAct / ADK,自动流式;字节内部 Doubao/TikTok 已验证 |
| LLM | 默认 DeepSeek,工厂可换 | 中文好、便宜、有 reasoner 变体;通过 `internal/llm/provider.go` 抽象,新增 provider 只需一个 `Register` |
| 后端调用 | **tyche MCP**(JSON-RPC 2.0 over HTTP) | tyche 已有 `controller/mcp/controller.go` 暴露 7 个 C 端工具,字段质量(车名/品牌/分类/燃料/座位/图片)远好于 saas-api inner;agent 直接接,**不再造轮子** |
| Agent 间通信 | 单进程 + 内部接口按 A2A skill 形态预留 | 早期独立部署成本高 / 收益低;后续要拆套 [trpc-a2a-go](https://github.com/trpc-group/trpc-a2a-go) 零改动 |
| 知识检索 | BM25 → 后续可切向量 | 知识量预计几十到一两百片段,BM25 够用;`Retriever` interface 预留 |
| Session | Redis | 对齐 rental-saas-api;TTL 24h |
| 服务形态 | CLI(P1-P3) → HTTP+SSE(P4+) | 先在 CLI 调通 prompt 与 tool,再服务化 |

---

## 4. Phase 拆分与索引

| Phase | 范围 | 预估 | 文档 |
|---|---|---|---|
| **P1** | 骨架 + 导购 MVP(CLI):eino 项目骨架、DeepSeek 接入、**tyche MCP client**(6 个只读工具)、单 ReAct agent、CLI 跑通"首都机场周末 SUV 推荐" | ~16h | [phase1-shopping-mvp.md](specs/phase1-shopping-mvp.md) |
| **P2** | 价格明细 + 保险推荐(仍 CLI):`rental_get_order_details` 一次返回价格明细 + 保险列表；补充 prompt 解读规则、驾龄推荐逻辑、用户画像槽位 | ~16h | [phase2-price-detail-insurance.md](specs/phase2-price-detail-insurance.md) |
| **P3** | 知识库 + 条款解读 + supervisor:灌入 billing/fulfillment/insurance 知识、`Retriever` + BM25、`search_knowledge` tool、答复带 `[来源]`、拆分多子 agent | ~24h | [phase3-knowledge-supervisor.md](specs/phase3-knowledge-supervisor.md) |
| **P3.5** | **借鉴 tyche V4 工程化重构**:取消 supervisor + ChatModelAgent ReAct,改为「Decider 单次流式 function-calling + 责任链流水线 + Capability 编排 + ID Go 托管」+ 状态前缀 + history 回放 + 思考头/引导胶囊/反馈 + 异步审核/分布式锁/对话摘要/可观测性 + 场景知识库结构化 | ~4-5 周 | [phase3.5-decide-capability-refactor.md](specs/phase3.5-decide-capability-refactor.md) |
| **P4** | HTTP 服务化 + Session:`cmd/http` + SSE、Redis session、公司鉴权 / 限流 / trace 中间件。后端能力沿用 tyche MCP | ~20h | [phase4-http-session-mcp.md](specs/phase4-http-session-mcp.md) |
| **P5** | 扩展能力:决策辅助 / 资质闭环 / 售后 FAQ / 比价异议 + 对应子 agent | ~35h | [specs/phase5-extensions.md](specs/phase5-extensions.md) |
| **P6** | 生产化:监控降级限流、风险词拦截、转人工、对话评估集、A/B、可选拆独立进程套 trpc-a2a-go | ~30h | [specs/phase6-productionize.md](specs/phase6-productionize.md) |

> **P3.5 说明**:这一 phase 是 P3 完工后基于 tyche V4 主链路实战经验的工程化补强,把 P3 的 supervisor 多 agent 架构平迁到"单 LLM 决策 + Go 编排 + 责任链流水线",同时把 ID 托管、流式上下文、生产化基建一并落地。详见 phase 文档。

---

## 5. 关键设计决策(给后续 PR review 用)

| # | 决策 | 替代方案 | 选择理由 |
|---|---|---|---|
| 1 | 用 eino 而非 langchain-go / 手写 loop | 手写 / langchain-go | eino 字节内部验证、ReAct / ADK 开箱即用、流式自动处理 |
| 2 | LLM 抽象层(Provider/Factory) | 直接 import deepseek | 用户明确要求 LLM 可换;新增 provider 成本最低 |
| 3 | 不用 disf,直接 HTTP | disf 调用 | agent 是独立服务,disf 依赖重;HTTP/JSON 通用 |
| 4 | 单进程多 agent(P3) → **P3.5 改为责任链流水线** | 持续走 supervisor + ReAct | tyche V4 实战回退结论:supervisor 路由额外消耗 token + latency,经常路由错;责任链单 LLM 决策 + Go 编排,LLM 调用从 ~5-6 次降至 1-2 次 |
| 5 | P1 起就接 tyche MCP(已存在) | 在 saas-api 里再造一套 / 自己直连 | tyche `/controller/mcp/controller.go` 已有 7 个工具,数据质量好,字段稳;agent 复用现成的最省心 |
| 6 | BM25 而非向量检索 | OpenAI embedding / 内部向量服务 | 知识量小;省合规审批;`Retriever` 接口预留 |
| 7 | 不注册写操作 tool | create_order tool 给 LLM | 写操作风险高,沿用旧 spec 审查结论 |
| 8 | CLI 优先,P4 再 HTTP | P1 就出 HTTP | CLI 调 prompt 和 tool 更快;不阻塞 |
| 9 | **P3.5: ID 由 Go 从 state 注入,工具 schema 不暴露给 LLM** | LLM 在 prompt 里"自己保存 context_id/reference_id" | prompt 软约束在多轮 ReAct 下易漂移、丢字段、改格式;Go 托管硬保证不幻造,且工具 schema 收窄后 LLM 上下文也更短 |
| 10 | **P3.5: 流式 function-calling 单流(content + tool_calls)** | 等 LLM 调用结束后整段下发 | 用户首字节延迟从数秒降至 <1s;tool_call 决策末帧出来,中间不沉默 |
| 11 | **P3.5: SSE 协议增量扩展 + 向后兼容** | 大版本切换 | 旧客户端按 `Accept: application/x-sse-v1` 降级到旧字段名;新事件(thinking_tips/box/quick_action)增量加,前端可滚动接 |

---

## 6. 跨 Phase 复用规范

### ConversationState
- **唯一定义点**:`internal/orchestration/state.go`
- 任何模块需要读写 state 都 import 这里,**禁止重新定义同名结构**

### Agent 签名
- P3:子 agent 实现 eino ADK 的 `agent.Agent` 接口,子 agent 间调用走 agent-as-tool / transfer
- **P3.5 起改为责任链 Pipeline**:`PipelineStage` 接口 `Handle(ctx, *AgentContext) (Signal, error)`;新增能力实现 `Capability` 接口 `Run(ctx, CapabilityInput) (*CapabilityResult, error)`,由 `CapabilityOrchestrator` 按 `Decision.Tool` 认领分发,**禁止**自创签名

### Tool 描述
- 写给 LLM 看 → 中文 OK,要写"何时调、必填项、返回什么"
- 每个字段必须有 `jsonschema:"description=..."`

### Prompt
- 复杂 prompt 用 `text/template`,**禁止**和 const 字符串拼接(旧规划审查发现)

---

## 7. 风险登记

| 风险 | 影响 | 规避 |
|---|---|---|
| **LLM 幻造关键 ID** | 用错误 reference_id/context_id 调工具 → 返回错误,用户体验差 | **P3.5 起 Go 从 state 注入,工具 schema 不暴露 ID 字段**;`ResolveQuoteRef` 解析"第一辆/朗逸"为 ref;15 分钟过期主动重搜 |
| 写操作泄漏 | 资损 | 不注册写 tool;CI 检测 toolset 命名黑名单 |
| 报价时效 | 用户拿过期价 | tool 返回 note: 以下单时为准;前端二次校验;P3.5 `state.QuoteAt` 15 分钟时效硬判 |
| LLM 幻觉条款 | 误导 / 投诉 | 强制 RAG;无命中时引导客服 |
| 技术错误透露给用户 | 用户体验差 / 信息泄漏 | tool 错误包装为 is_error:true + user_msg + debug 结构;prompt 规定只透传 user_msg |
| 多 LLM provider 不一致 | 不同 provider 工具调用 schema 有差异 | `internal/llm/` 内归一;提供 cross-provider 测试集 |
| MCP 迁移 | 切换期不稳 | P4 切 MCP 时直连代码保留 fallback,灰度切换 |
| 合规话术 | 保险 / 比价用语不当 | prompt 红线 + 评估集中专项 case |
| **P3.5 改造期不稳** | 中间状态用户体验断档 | 灰度开关 `cfg.Agent.Mode = supervisor\|pipeline`(默认 pipeline),按 session_id hash 灰度;1 周稳定后再物理删 supervisor |
| **流式与异步审核 race** | 命中审核但话术已流出 | 命中时 `audit.Done()` 触发,Decider 内 `select { case <-audit.Done(): break }` 提前退出 stream |
| **GuideStage 第二段 JSON 解析失败** | 引导胶囊缺失 | 解析失败回退到不带胶囊的纯引导语,记 warn |
| **分布式锁 Redis 故障** | 主链路阻塞 | `cfg.AccessLock.FailOpen=true`(默认 true)时 Redis 异常放行,只 warn |
| **对话摘要把关键信息抹掉** | 多轮丢上下文 | 摘要只压最早 2 条,保留 6 轮原文窗口;`BuildStatePrefix` 同时带 summary + last_quotes,信息双保险 |
