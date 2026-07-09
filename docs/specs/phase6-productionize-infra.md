# Phase 6 — 生产化基建(审核 / 锁 / 摘要 / metric)

> 隶属 [技术方案总纲](../technical-plan.md)。**可独立执行。**
>
> **工时:** 4-5 天 · **PR 数:** 2 · **前置依赖:** [P1](phase1-shopping-mvp.md) 已合入(可与 P4/P5 并行)

---

## 1. 目标

进生产前补齐"安全 / 并发 / 可观测 / 可压缩"四件套。这一步把骨架占住,真上线(接 ISM / 公司 metric SDK / Redis 限流)只填实现,不改架构。

---

## 2. PR 6.1 — 异步审核 + 分布式锁 + 对话摘要

### 异步审核(参考 tyche `guardrail/audit_async.go`)
- `internal/guardrail/`:`AsyncAuditor` `AuditHandle` `SecurityClient` `PassThroughClient`
- handler 入口构造 `AuditHandle`,用户输入立即送 `kind=input`;每次 SendText 前/后把累积满 `segmentChars=300` 的段送 `kind=output`
- 命中阻断:写 `done{reason:guardrail}` 推 SSE,后续 stage `checkAborted(ctx,audit,sse)` 短路
- 默认 `SecurityClient=PassThroughClient`(全放行),`cfg.Audit.Enabled/Endpoint` 启用真实审核(P6+ 接 ISM 填实现)

### 分布式锁(参考 tyche `access_lock.go`)
- `internal/session/access_lock.go`:Redis SETNX + TTL,key=`agent_lock:{uid}:{sid}`,TTL=60s
- handler 中间件链最外层 `TryAcquireAgentLock`,失败返回 429
- `cfg.AccessLock.FailOpen=true`(默认)Redis 异常放行,只 warn

### 对话摘要(参考 tyche `conversation_summarizer.go`)
- `internal/orchestration/summarizer.go`:**纯模板不调 LLM**,超 6 轮把最早 2 条压成"第 1-2 轮:用户问 XX,助手 XX",追加 `state.Summary`,从 history 移除原文
- FinalizeStage 末尾调 `MaybeSummarize(state)`;BuildStatePrefix(P4)已会带 summary

**验收**:并发同 user 请求 → access_lock 串行化;输入越界 → 审核拦截写 done{guardrail};>6 轮 → summary 自动生成原文保留 6 轮窗口。

---

## 3. PR 6.2 — 可观测性(Trace + Logging + Metrics + Cost)

> 完整设计见 [技术方案总纲 §9 可观测性设计](../technical-plan.md)。本节列实现清单。

### 3.1 LLM 调用日志(流式收尾落一条完整日志)

`internal/llm/openai_client.go` 的 `ChatStream` 收尾(`defer + 累积 buffer`)：

| 字段 | 来源 |
|---|---|
| trace_id / session_id / user_id / stage / model | ctx 透传 |
| prompt_id / prompt_version / prompt_hash | `versioning.PromptVersionSet` |
| context_version / context_hash | ContextManager / state prefix builder |
| tool_schema_set / tool_schema_hash / parser_version | decide tools / output parser manifest |
| prompt_tokens / completion_tokens / cache_hit_tokens / total_tokens | response usage |
| duration_ms / finish_reason / status | 计时 + 解析 |
| prompt_preview | system 前 200 字 + messages 条数 + 末条 user 前 100 字 |
| response_content | completion 全文(cfg 可关) |
| response_tool_calls | `[{name, arguments}]` JSON |
| error_msg | 失败时 |

- 自写 `CopyCtxWithSameTraceID`(原 ctx 请求结束被 cancel,trace 用 copy ctx 保留)
- 同步 `Chat` 同样落相同格式

### 3.1.1 Replay Store + 回放对比

新增 `internal/replay/` 与 `cmd/replay`:

| 模块 | 说明 |
|---|---|
| `LLMCallSnapshot` | 保存一次 LLM 调用的 version set、model 参数、最终 system/messages/tools hash、输出、usage、trace 信息 |
| `ReplayStore` | dev=file,prod=Redis/对象存储;普通日志只存 hash/preview,负反馈或 debug 模式存完整 messages/tools |
| `cmd/replay` | 按 trace_id/session_id 找快照,支持 frozen replay 与 current compare |

回放模式:
- `frozen`:使用当时 snapshot 的 system/messages/tools/model 复跑,验证供应商/模型漂移。
- `current`:用当前 prompt/context/tool schema 重建 messages 复跑,与 frozen 输出对比。
- `dry`:不调 LLM,只输出 version/hash diff,用于 PR 快速检查。

对比报告字段:
- `version_diff`:prompt/context/tool/parser/model 是否变化
- `tool_selection_diff`:tool 名是否变化
- `args_diff`:tool arguments JSON diff,重点标红 `need_delta/search_mode/vehicle_ref`
- `text_diff`:回复文本摘要 diff,检测 ID 泄露/绝对化/越界
- `metric_diff`:tokens、duration、cost
- `decision_diff`:confidence_action、filter_interpreter_used、static_recall_coverage

验收:
- [x] `internal/replay` 已支持文件型 `LLMCallSnapshot` 保存/按 trace_id 查询
- [x] `cmd/replay --trace_id xxx --mode dry` 能读取 snapshot 并展示版本/hash diff 骨架
- [ ] 任意一条线上 trace 自动导出 LLMCallSnapshot(待接 LLM logging/replay store)
- [ ] `cmd/replay --trace_id xxx --mode compare --target current` 能输出 tool/args/text/metric diff
- [ ] P5 负反馈胶囊写入 replay store,可一键转 eval regression case

### 3.2 Tool 调用日志

`internal/tools/logging.go`(P0 已有骨架)补齐字段：trace_id / session_id / user_id / tool_name / capability / arguments(截断) / response_preview(前 500 字) / is_error / duration_ms / status

### 3.3 Metric 包

`internal/metric/metric.go`，Prometheus-compatible 文本(`/debug/metrics`)。

**LLM:** `llm_calls_total{stage,model,status}` / `llm_tokens_total{stage,model,type}` / `llm_duration_ms{stage,model}` / `llm_cost_yuan{stage,model}`
**Tool:** `tool_calls_total{name,capability,status}` / `tool_duration_ms{name}` / `tool_error_total{name,error_type}`
**Pipeline:** `stage_duration_ms{stage}` / `pipeline_duration_ms` / `first_byte_ms` / `session_tokens_total` / `cost_per_turn_yuan{stage}` / `cost_per_session_yuan` / `cost_daily_yuan`

### 3.4 成本估算

```go
// internal/metric/cost.go — 定价系数放 config,provider 切换只改系数
func EstimateCost(usage UsageRecord) float64 {
    promptCost := float64(usage.PromptTokens-usage.CacheHitTokens) * cfg.PricePerMInput / 1_000_000
    cacheCost  := float64(usage.CacheHitTokens) * cfg.PricePerMCache / 1_000_000
    completionCost := float64(usage.CompletionTokens) * cfg.PricePerMOutput / 1_000_000
    return promptCost + cacheCost + completionCost
}
```

### 3.5 BudgetChecker(预算三道闸)

```go
// internal/agent/budget.go
type BudgetChecker interface {
    Check(ctx, uid string) (allowed bool, remaining int, err error)  // Decide 前
    Consume(ctx, uid string, tokens int) error                       // Finalize
}
```
- 用户日限额 500k token → 超限友好提示不调 LLM
- 单会话限额 100k token → 引导新开会话
- 全局日熔断 → 降级:闲聊模板,搜车仍走

Redis INCRBY + TTL,key = `token_budget:{uid}:{date}` / `token_budget:global:{date}`

### 3.6 Pipeline / Stage 打点

`ChatPipeline.Run` 每 Stage 前后 `metric.StageLatency(name, elapsed)`;Decider/Capability 内调 LLM/tool 同理。

**关键文件:** `internal/llm/openai_client.go`、`internal/tools/logging.go`、`internal/metric/{metric,cost}.go`(新建)、`internal/agent/{budget,agent}.go`、`internal/http/handler.go`(加 `/debug/metrics`)

**验收**:
- [x] `internal/guardrail` 异步审核骨架 + PassThroughClient + 单测
- [x] `internal/session/access_lock.go` 分布式锁接口骨架 + fail-open 单测
- [x] `MaybeSummarize(state)` 纯模板摘要,Finalize 后自动压缩早期 history
- [x] `/debug/metrics` 输出 Prometheus-compatible 文本;Pipeline stage duration 已打点
- [x] BudgetChecker 内存实现与超限单测
- [x] 成本估算公式输出与手动计算一致(单测)
- [ ] 压测同 user 50 QPS,access_lock 工作,审核延迟 P95 <100ms 不阻塞(需真实 Redis/审核服务)
- [ ] `/debug/metrics` 能看到全部指标:LLM/Tool/Stage/Cost(当前先接 stage 指标与 registry)
- [ ] 流式收尾日志含完整 prompt_preview + response_content + response_tool_calls + tokens(当前日志已有 tokens/summary,完整 snapshot 待接 replay)
- [ ] BudgetChecker 超限时返回友好提示,不调 LLM(当前完成 checker,未接 RunWithEvent 前置闸)

---

## 4. 风险

| 风险 | 应对 |
|---|---|
| 流式与审核 race | 命中 `audit.Done()`,Decider/Capability `select{case <-audit.Done()}` 提前退出 |
| 锁 Redis 故障 | `FailOpen=true` 异常放行,只 warn |
| 摘要抹关键信息 | 只压最早 2 条,保留 6 轮原文;前缀同时带 summary + last_quotes 双保险 |

**完成态:** 可发预发、可灰度、有 badcase 闭环(配合 P5 反馈胶囊)、有 metric。
