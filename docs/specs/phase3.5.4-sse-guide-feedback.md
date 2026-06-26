# Phase 3.5.4 — SSE 协议升级 + 引导胶囊 + 反馈采集

> 隶属 [phase3.5 重构路线图](phase3.5-decide-capability-refactor.md) 的第 4 步。**可独立执行。**
>
> **工时:** 3-4 天 · **PR 数:** 2(C1、C2) · **前置依赖:** [Phase 3.5.3](phase3.5.3-context-streaming.md) 已合入

---

## 1. 动机

把"对话深度"、"badcase 闭环"、"等待感"三件事一起做。tyche 的高用户粘性主要靠这三件,纯 prompt 工程做不到。

---

## 2. PR C1 — SSE 事件协议升级 + 思考头/思考框

**改动:** [internal/http/sse.go](../../internal/http/sse.go) 现有 `sseEvent{type,content,agent,detail,extra}` 扩展新 type:

| type | payload | 用途 |
|---|---|---|
| `text`(替代 `message`) | `{content, subtype: thinking\|vehicle\|final}` | content 流式增量 |
| `thinking_tips` | `{status: start\|done, type: msg\|vehicle, text}` | 灰字"小租正在思考" |
| `thinking_box` | `{box_type: search, step: initialize\|thinking\|done, words}` | 找车折叠区 |
| `card` | `{type, payload}` | 车型卡片(Phase 3.5.6/后续业务用) |
| `quick_action` | `{actions: [{label, type, payload}]}` | 引导胶囊 |

**下发时序:**
- DecideStage 进入 → `thinking_tips{start, msg}`;Decider 拿到首个 content delta 后保持开启。
  - 纯回复(无 tool)→ content 流完发 `thinking_tips{done, msg}` → done。
  - 调 search → 进 SearchCapability 前发 `thinking_box{search, initialize, "正在理解你的需求"}` + `thinking_box{search, thinking, "正在为你筛选车型"}`,搜完发 `done`。
  - price_detail / insurance / rules → 同 thinking_tips 套路,Capability 内 LLM #2 流式时切到 `text{vehicle}`。
- **向后兼容**:旧前端只识别 `message` 时,服务端按 `Accept: application/x-sse-v1` 头降级到旧字段名(过渡期 1 个版本)。

**关键文件:** `internal/http/sse.go`、`internal/http/handler.go`(`consumeIter` 改造)、各 Capability 入口。

---

## 3. PR C2 — 引导胶囊(quick_action / slot_patch)+ 反馈胶囊

参考 tyche [`v4_stage_guide.go:21-101`](file:///Users/didi/work/tyche/logic/agent/v4_stage_guide.go) 与 [`feedback.go`](file:///Users/didi/work/tyche/logic/agent/feedback.go)。

**引导胶囊(GuideStage):** 放在 `CapabilityStage` 之后,**仅在 Search 命中且有车**时跑:
1. 用 lite 模型(继续走 `deepseek-chat` 或新增 `guide` provider binding)流式调一次,system 要求两段输出:`引导语纯文本 + ```json {actions, compare}` 代码块。
2. 引导语 `text{vehicle}` 流式;末段 ```json` 解析出 2-3 个 `slot_patch` 胶囊(白名单 key:`vehicle_type/seats/energy/transmission/budget_max/brands/tags`),发 `quick_action`。
- 新增 `internal/agent/guide_stream.go` 实现 `streamTextThenJSON`(参考 tyche `v4_stream_json.go`)。
- 新增 `internal/prompt/guide_system.go`(参考 tyche `dynamicGuideStreamSystem`)。

**用户点击胶囊回传:** HTTP `chatRequest` 增加 `event_type` `action` 字段;`PreRouteStage` 识别 `event_type=action_click` 时把 `action.payload.slot_patch` 合并进 `state.RecentSlotPatch`,Decide 看到 slot_patch 时优先短路到 search。

**反馈胶囊:** 每个 Search/Rules 结果末尾追加 `feedback_positive`/`feedback_negative` 两个固定 quick_action;前端点击回传 `event_type=action_click&label=符合预期/不符合预期`,服务端把整段对话快照(state.History 最近 N 条 + 本轮 decision)写到 `internal/agent/feedback_store.go`(先用 file/Redis list,P6 后接公司日志平台)。

**关键文件:** `internal/agent/{guide_stream,feedback_store}.go`、`internal/prompt/guide_system.go`、`internal/http/handler.go`、`internal/orchestration/state.go`。

---

## 4. 验收

- [ ] SSE 流出 `thinking_tips/start` → content → `thinking_tips/done` 完整序列
- [ ] 搜车 → `thinking_box{search,initialize}` → `{search,thinking}` → 卡片 + 引导胶囊 → `done`
- [ ] 引导胶囊 slot_patch 点击回传后,下一轮 PreRouteStage 短路到 search,LLM 调用 ≤ 1 次
- [ ] 反馈胶囊点击 → 整段对话快照入 feedback_store
- [ ] 旧前端(只识别 `message`)按 `Accept: application/x-sse-v1` 仍正常工作

---

## 5. 风险

| 风险 | 应对 |
|---|---|
| GuideStage 第二段 ```json` 解析失败 | 流末没拿到合法 JSON → 回退不带胶囊的纯引导语,记 warn |
| 新事件类型打挂旧前端 | `Accept` 头降级 + 灰度;新事件默认只对新版本前端下发 |

**完成态:** 用户能持续点击胶囊深耕需求;运营拿到 badcase;等待感问题大幅缓解。
