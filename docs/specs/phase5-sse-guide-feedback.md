# Phase 5 — SSE 协议升级 + 引导胶囊 + 反馈采集

> 隶属 [技术方案总纲](../technical-plan.md)。**可独立执行。**
>
> **工时:** 3-4 天 · **PR 数:** 2 · **前置依赖:** [P4](phase4-http-session.md) 已合入

---

## 1. 目标

把"对话深度 / badcase 闭环 / 等待感"三件事一起做——tyche 高粘性主要靠这三件,纯 prompt 工程做不到。

---

## 2. PR 5.1 — SSE 事件协议升级 + 思考头/思考框

`internal/http/sse.go` 在 P4 的 `text/event/done/error` 基础上扩展:

| type | payload | 用途 |
|---|---|---|
| `text` | `{content, subtype: thinking\|vehicle\|final}` | content 流式增量 |
| `thinking_tips` | `{status: start\|done, type: msg\|vehicle, text}` | 灰字"小租正在思考" |
| `thinking_box` | `{box_type: search, step: initialize\|thinking\|done, words}` | 找车折叠区 |
| `card` | `{type, payload}` | 车型卡片 |
| `quick_action` | `{actions:[{label,type,payload}]}` | 引导胶囊 |

下发时序:DecideStage 进入发 `thinking_tips{start}`;首个 content delta 后保持;纯回复流完发 `{done}`;搜车发 `thinking_box{initialize/thinking}`→搜完`{done}`。

**向后兼容**:旧前端只识别 `message` 时,按 `Accept: application/x-sse-v1` 头降级旧字段名(过渡 1 版本)。

**验收**:SSE 流出 `thinking_tips/start`→content→`thinking_tips/done` 完整序列;旧前端降级正常。

---

## 3. PR 5.2 — GuideStage 引导胶囊 + 反馈胶囊

参考 tyche `v4_stage_guide.go` 与 `feedback.go`。

**GuideStage**(放 CapabilityStage 之后,仅 Search 命中且有车时跑):
1. lite 模型流式调一次,system 要求两段:`引导语纯文本 + ```json {actions,compare}` 代码块
2. 引导语 `text{vehicle}` 流式;末段解析出:
   - 2-3 个 **slot_patch 胶囊**(白名单 key:`vehicle_type/seats/energy/transmission/budget_max/brands/tags`)→ `quick_action`
   - 结果含 ≥2 个不同车型时,额外出 **compare 胶囊**("对比朗逸和轩逸",payload 带两辆车名)→ 用户纠结时一键发起对比(对接 P2 的 CompareCapability)
- `internal/agent/guide_stream.go`:`streamTextThenJSON`
- `internal/prompt/guide_system.go`

**用户点击胶囊回传**:`chatRequest` 加 `event_type`/`action` 字段;PreRouteStage 识别 `action_click` 后按 payload 类型短路(**两类都不走 decide LLM**):
- `slot_patch` 胶囊 → 合并进 `state.RecentSlotPatch`,直接构造 search Decision
- **`compare` 胶囊 → 直接构造 `compare_vehicles` Decision(vehicle_refs = payload 里的车名)→ CompareCapability**

> 这就补齐了车型对比的"胶囊点击"路径:自然语言路径在 P2 已实现,本期补结构化点击路径,两条统一收口 CompareCapability。

**反馈胶囊**:Search/Rules 结果末尾追加 `feedback_positive/negative`;点击回传 → 整段对话快照写 `internal/agent/feedback_store.go`(先 file/Redis list,P6 后接公司日志平台)。

**验收**:
- 搜车 → `thinking_box`→卡片+引导胶囊(含 compare 胶囊,当结果有 ≥2 车型)→`done`
- 点 slot_patch 胶囊 → 下一轮 PreRoute 短路 search,LLM ≤1 次
- 点 compare 胶囊 → PreRoute 短路构造 compare Decision → CompareCapability,不走 decide LLM
- 点反馈胶囊 → 快照入 feedback_store

---

## 4. 实现状态

- [x] SSE `text` payload 增加 `subtype`;新增 typed event: `thinking_tips` / `thinking_box` / `card` / `quick_action`
- [x] `Accept: application/x-sse-v1` 旧协议降级
- [x] 搜车链路输出 `thinking_box`、车型 `card`、slot_patch/compare/feedback 胶囊
- [x] `chatRequest` 支持 `event_type` / `action`
- [x] `PreRouteStage` 支持 `action_click`:slot_patch 直达 search、compare 直达 CompareCapability,不走 decide LLM
- [x] 反馈胶囊写入 `FileFeedbackStore` JSONL
- [x] 前端支持渲染 thinking、card、quick_action,并回传 action_click
- [ ] GuideStage 第二段 JSON 由 lite 模型生成的高级胶囊文案尚未接入,当前先用 Go 确定性胶囊
- [ ] 首字节 <1s / SSE 时序需接真实 LLM + 浏览器压测确认

---

## 5. 风险

| 风险 | 应对 |
|---|---|
| Guide 第二段 JSON 解析失败 | 回退不带胶囊的纯引导语,记 warn |
| 新事件打挂旧前端 | Accept 头降级 + 灰度,新事件默认只对新前端下发 |
