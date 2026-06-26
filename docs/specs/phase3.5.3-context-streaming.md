# Phase 3.5.3 — 上下文与流式提质

> 隶属 [phase3.5 重构路线图](phase3.5-decide-capability-refactor.md) 的第 3 步。**可独立执行。**
>
> **工时:** 4-5 天 · **PR 数:** 2(B1、B2) · **前置依赖:** [Phase 3.5.2](phase3.5.2-decide-core.md) 已合入

---

## 1. 动机

Phase 3.5.2 解决了"LLM 调用次数",本阶段解决两件事(对应 tyche 借鉴清单 P0):
- **LLM 看到的上下文是否结构化** —— history 回放工具调用 + 结构化状态前缀
- **用户感受到的响应速度** —— 流式 content 边吐边出收口

---

## 2. PR B1 — history 回放工具调用 + 流式落地收口

参考 tyche [`v4_stage_decide.go:323-366 streamDecide`](file:///Users/didi/work/tyche/logic/agent/v4_stage_decide.go#L323) 与 [`:128-149 buildDecideMessages`](file:///Users/didi/work/tyche/logic/agent/v4_stage_decide.go#L128)。

**改动:**
- 新增 `internal/agent/history_replay.go`:构造 LLM 入参 messages 时,把 `state.History` 里 `Role=assistant && len(ToolCalls)>0` 的条目还原成 `(assistant{tool_calls}, tool{tool_call_id, content})` 两条消息,而不是拍平成 assistant 文本。**这是让模型"看见上轮真发过工具调用"的关键** —— 否则模型会照抄"用文本回答"而不再调工具。
- `internal/types/types.go` 新增 `ToolCallSnapshot{Name, Arguments, Result}`,放进 `HistoryEntry` 的 metadata。
- `ConversationState.AppendMessage` 在 Capability 跑完后写 assistant 条目时,把本轮 ToolCall 快照(Name/Args/ResultSummary)一并存。
- 收口 Phase 3.5.2 的流式实现:确认 `Decider` 在收到第一帧 `Delta.Content` 时就通过 `SSEWriter.SendText({type:"thinking"})` 实时下发,**不等流结束**。

**关键文件:** `internal/agent/{decide,history_replay}.go`、`internal/orchestration/state.go`、`internal/types/types.go`。

**单测:** 历史含 1 轮搜车 → 第 2 轮模型 messages 里能看到 `assistant(tool_calls=[search_vehicles])` + `tool(content="已展示 3 辆车")`。

---

## 3. PR B2 — 状态前缀拼接(BuildStatePrefix)

参考 tyche [`v4_stage_decide.go:188-241 buildStatePrefix`](file:///Users/didi/work/tyche/logic/agent/v4_stage_decide.go#L188)。

**核心思路:** Decider 调 LLM 前,在末条 user message 前面拼一段 `## 当前会话状态` 结构化前缀:

| 字段 | 来源 | 备注 |
|---|---|---|
| 当前时间(含星期,中文) | `time.Now()` | 让模型按"周末/工作日"理解租期 |
| 当前生效取还车 `current_rental` | `state.Rental` | 检测异地/改取还车 |
| 上一轮报价摘要 `last_quotes` | `state.LastQuotes` | **只放车名/品牌/价格,绝不放 reference_id 给 LLM** |
| 用户画像 | `state.Profile`(占位) | Phase 3.5.6 之前可全空 |
| 已反问次数 `clarify_count` | `state.ClarifyCount` | 喂模型软倾向 + 硬顶 |
| 对话摘要 `summary` | `state.Summary` | Phase 3.5.5 摘要器实现后才有内容 |

**改动:**
- 新增 `internal/agent/state_prefix.go`,实现 `BuildStatePrefix(state) string`。
- `Decider.buildMessages` 调用它,把前缀拼到末条 user 消息前。
- `internal/prompt/decide_system.go` 加一段提示:"用户消息前可能带 `## 当前会话状态` 结构化字段,据此判断;不要把 ID 类字段写进答复"。

**关键文件:** `internal/agent/{state_prefix,decide}.go`、`internal/prompt/decide_system.go`。

**单测:** 覆盖空 state、有报价、有画像、有摘要、过期报价(不出 last_quotes)等情况。

---

## 4. 验收

- [ ] 历史含 1 轮搜车 → 第 2 轮模型 messages 里能看到 `assistant(tool_calls)` + `tool(content)`
- [ ] 末条 user 消息前能看到结构化 `## 当前会话状态` 前缀
- [ ] last_quotes 不含 reference_id/context_id
- [ ] 首字节时间 < 1s(content 流式)
- [ ] `go test ./...` 全绿;state_prefix / history_replay 单测覆盖核心分支

---

## 5. 风险

| 风险 | 应对 |
|---|---|
| 前缀拼太长触顶 token | 报价摘要最多 3 条;summary 已压缩;history 回放只保留最近 N 轮 |
| 回放出的 tool_call_id 不合法 | 用固定格式 `call_{i}` 生成,assistant 与 tool 配对一致 |

**完成态:** LLM 上下文质量与 tyche 持平;用户感受首字节 < 1s。
