# Phase 4 — HTTP 服务化 + Session + 状态前缀 + history 回放

> 隶属 [技术方案总纲](../technical-plan.md)。**可独立执行。**
>
> **工时:** ~~5-6 天~~ 2-3 天(HTTP 基础已提前实现) · **PR 数:** ~~3~~ 2 · **前置依赖:** [P1](phase1-shopping-mvp.md) 已合入(P2/P3 可并行)
>
> **[演进 2026-06-30] 状态更新**：HTTP 基础已提前实现——`cmd/http/main.go`(服务入口)、`internal/httphandler/`(handler + SSE emitter + middleware)、`internal/session/`(Store 接口 + MemoryStore)、`web/index.html`(前端 SPA：User ID 登录 + 对话 + 历史回溯)。
> 本 spec 剩余未实现部分：**Redis session 持久化、history 回放工具调用(PR 4.2)、BuildStatePrefix 完善(PR 4.3,部分已在 NeedsStatePrefix 实现)**。
>
> **包名变更**：原设计 `internal/http/` 改为 `internal/httphandler/`，原因：`internal/http` 与 Go 标准库 `net/http` 同名，导致必须加 import 别名，违反项目"不加 import 别名"规范。

---

## 1. 目标

从 CLI 转 HTTP 服务,SSE 流式输出;ConversationState 落 Redis 跨进程持久化;同时把 tyche 借鉴清单 P0 的两件事(**结构化状态前缀 + history 回放**)做进来——它们正好需要 Redis 持久化的 state 才有意义。

---

## 2. PR 4.1 — HTTP + SSE + Session 持久化

- `cmd/http/main.go`:加载 config → 建 Deps/Factory/Agent → 起 HTTP server + 优雅停机
- ~~`internal/http/handler.go`~~ → **`internal/httphandler/handler.go`** **[已实现]**:`POST /api/chat`(SSE 流式) + `GET/POST/DELETE /api/sessions` + `/healthz` + 静态文件
- ~~`internal/http/sse.go`~~ → **`internal/httphandler/sse.go`** **[已实现]**:sseEmitter 实现 `agent.Emitter` 接口,`text`/`event`/`done`/`error` 类型化
- ~~`internal/http/middleware.go`~~ → **`internal/httphandler/middleware.go`** **[已实现]**:`recovery → cors → accessLog` 中间件链
  - auth：User ID 在请求 body/query 中传递(用户要求"只需要一个 user ID",无需密码登录)
  - ratelimit 留 TODO(P6 填实现)
- `internal/session/`:`Store` 接口 + `RedisStore`(JSON 序列化,key=`prefix:uid:sid`,TTL 24h)+ `MemoryStore`(dev 兜底)

**验收**:`curl -N -H "X-User-Id: u1" -d '{"session_id":"s1","message":"明天北京SUV"}' /agent/chat` 看到 SSE 流;跨请求续聊(Redis 持久化)。

---

## 3. PR 4.2 — history 回放工具调用

参考 tyche `v4_stage_decide.go` 的 buildDecideMessages。

- `internal/types/types.go`:`HistoryEntry` 增加 `ToolCall *ToolCallSnapshot{Name, Arguments, Result}`
- `ConversationState.AppendMessage`:Capability 跑完写 assistant 条目时,把本轮 ToolCall 快照一并存
- `internal/agent/history_replay.go`:构造 LLM messages 时,把 `Role=assistant && ToolCall!=nil` 的条目**还原成 `assistant(tool_calls) + tool(tool_call_id, result)` 两条消息**,而非拍平成文本——让模型看见上轮真发过工具调用(否则模型照抄"用文本回答"不再调工具)

**验收**:历史含 1 轮搜车 → 第 2 轮 LLM messages 里能看到 `assistant(tool_calls=[search_vehicles])` + `tool(content="已展示3辆车")`。

---

## 4. PR 4.3 — 状态前缀 BuildStatePrefix

参考 tyche `v4_stage_decide.go:buildStatePrefix`。

`internal/agent/state_prefix.go`:Decider 调 LLM 前,在末条 user 消息前拼 `## 当前会话状态`:

| 字段 | 来源 | 备注 |
|---|---|---|
| 当前时间(含中文星期) | time.Now() | 让模型按周末/工作日理解租期 |
| current_rental | state.Rental | 检测异地/改取还车 |
| last_quotes | state.LastQuotes | **只放车名/品牌/价格,绝不放 reference_id** |
| profile | state.Profile(占位) | P7 之前可空 |
| clarify_count | state.ClarifyCount | 反问软倾向 + 硬顶 |
| summary | state.Summary | P6 摘要器实现后才有内容 |

decide_system prompt 加提示:"用户消息前可能带 `## 当前会话状态`,据此判断;不要把 ID 写进答复"。

**验收**:末条 user 前能看到结构化前缀;last_quotes 不含 ID;单测覆盖空 state/有报价/过期报价(不出 last_quotes)。

---

## 5. 验收(整体)

- [x] HTTP + SSE 流式,RedisStore 代码路径已接入(真实 Redis 跨 pod 需环境验证)
- [x] dev 无 Redis 时降级 MemoryStore
- [x] history 回放:第 2 轮能看到上轮 tool_calls
- [x] 状态前缀拼接,ID 不外泄
- [ ] 首字节 < 1s
- [x] `go test ./...` 全绿

---

## 6. 风险

| 风险 | 应对 |
|---|---|
| 整段 history JSON 覆盖 + 并发覆盖 | P6 分布式锁解决;本期先单请求串行 |
| 前缀拼太长触顶 | 报价摘要 ≤3 条;summary 已压缩;history 回放保留最近 N 轮 |
| 鉴权空壳被绕过 | 本期最小实现 + TODO;P6 接公司 gateway 签名校验 |
