# Phase 3.5.5 — 生产化基建

> 隶属 [phase3.5 重构路线图](phase3.5-decide-capability-refactor.md) 的第 5 步。**可独立执行。**
>
> **工时:** 4-5 天 · **PR 数:** 2(D1、D2) · **前置依赖:** [Phase 3.5.2](phase3.5.2-decide-core.md) 已合入(D 不强依赖 3.5.3/3.5.4,可并行)

---

## 1. 动机

让这套 agent 进生产前具备"安全 / 并发 / 可观测 / 可压缩"四件套。这一步把骨架占住,P6 只需填实现,避免 P6 大改架构。

---

## 2. PR D1 — 异步内容审核管道 + 分布式锁 + 对话摘要

### 2.1 异步审核(参考 tyche [`guardrail/audit_async.go`](file:///Users/didi/work/tyche/logic/agent/guardrail/audit_async.go))
- 新增包 `internal/guardrail/`:`AsyncAuditor` `AuditHandle` `SecurityClient` `PassThroughClient`。
- `Server.handleChat` 入口构造 `AuditHandle`,用户输入立即送 `kind=input`;每次 `sw.SendText` 前/后把累积满 `segmentChars=300` 的字符段送 `kind=output`。
- 命中阻断:写 `done{reason: guardrail}` 推 SSE,后续 stage 通过 `checkAborted(ctx, audit, sse)` 自动短路。
- 默认 `SecurityClient = PassThroughClient`(全放行),`cfg.Audit.Enabled / cfg.Audit.Endpoint` 启用真实审核(P6 接 ISM 时填实现)。

### 2.2 分布式锁(参考 tyche [`access_lock.go`](file:///Users/didi/work/tyche/logic/agent/access_lock.go))
- 新增 `internal/session/access_lock.go`,Redis SETNX + TTL,key=`agent_lock:{user_id}:{session_id}`,TTL=60s。
- `handleChat` 中间件链最外层 `TryAcquireAgentLock`,失败返回 429。
- `cfg.AccessLock.FailOpen=true`(默认 true)时 Redis 异常放行,只 warn。

### 2.3 对话摘要(参考 tyche [`conversation_summarizer.go`](file:///Users/didi/work/tyche/logic/agent/conversation_summarizer.go))
- 新增 `internal/orchestration/summarizer.go`,纯模板拼接**不调 LLM**:超过 6 轮时把最早 2 条压成一句"第 1-2 轮:用户问 XX,助手 XX",追加到 `state.Summary`,从 history 移除原文。
- `FinalizeStage` 末尾调 `summarizer.MaybeSummarize(state)`。
- `BuildStatePrefix`(Phase 3.5.3)已会把 `state.Summary` 拼到前缀。

**关键文件:** `internal/guardrail/audit_async.go`、`internal/session/access_lock.go`、`internal/orchestration/summarizer.go`、`internal/http/handler.go`、`internal/agent/agent.go(FinalizeStage)`。

---

## 3. PR D2 — 可观测性(LLM/Tool/Stage 三层 metric + 流式收尾日志)

- [internal/llm/logging.go](../../internal/llm/logging.go) 已有日志层,扩展为 `LoggingChatModel` 在 `Stream` 收尾时(`defer logCtx + 累积 buffer`)落一条 `params + resp_content + resp_tool_calls + duration_ms + tokens` 完整日志,模仿 tyche [`llm/client.go:196-218`](file:///Users/didi/work/tyche/library/llm/client.go#L196)。trace_id 通过自写的 `CopyCtxWithSameTraceID` ctx helper 透传(原 ctx 在请求结束后会被 cancel)。
- 新增 `internal/metric/` 极简包(本地 `expvar` 或文本计数器,P6 换公司 metric SDK):`LLMCalls{provider,model,status} / LLMLatencyMs / ToolCalls{name,status} / ToolLatencyMs / StageLatencyMs{name}`。
- `ChatPipeline.Run` 每个 Stage 前后打点;`Decider`/`Capability` 内部调 LLM/tool 前后打点。
- Trace:`chatRequest` 入口生成 `trace_id`(已有)全链路透传,所有日志结构化字段必含 `trace_id / session_id / user_id / stage / capability`。

**关键文件:** `internal/llm/logging.go`、`internal/tools/logging.go`、`internal/metric/`(新建)、`internal/agent/agent.go`、`internal/http/handler.go`。

---

## 4. 验收

- [ ] 压测同 user 50 QPS,access_lock 工作,异步审核延迟 P95 < 100ms 不阻塞主流程
- [ ] 所有指标可见:LLMCalls / LLMLatencyMs / ToolCalls / ToolLatencyMs / StageLatencyMs
- [ ] LLM 流式调用收尾日志含:params + resp_content + resp_tool_calls + duration_ms + tokens
- [ ] history > 6 轮时 summary 自动生成,原文保留 6 轮窗口
- [ ] 关键日志结构化字段含:trace_id / session_id / user_id / stage / capability
- [ ] Redis 故障时 `FailOpen=true` 放行,只 warn

---

## 5. 风险

| 风险 | 应对 |
|---|---|
| 流式与异步审核交互 race | 命中时 `audit.Done()` 触发,Decider/Capability 内 `select { case <-audit.Done(): break }` 提前退出 stream;参考 tyche `checkAborted` |
| 分布式锁 Redis 故障 | `cfg.AccessLock.FailOpen=true`(默认 true)异常放行,只 warn |
| 摘要把关键信息抹掉 | 只压最早 2 条,保留 6 轮原文窗口;`BuildStatePrefix` 同时带 summary + last_quotes 双保险 |

**完成态:** 可发预发,可灰度,有 badcase 闭环,有 metric。
