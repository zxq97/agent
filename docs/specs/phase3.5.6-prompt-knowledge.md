# Phase 3.5.6 — Prompt 工程化沉淀

> 隶属 [phase3.5 重构路线图](phase3.5-decide-capability-refactor.md) 的第 6 步。**可独立执行。**
>
> **工时:** 2-3 天 · **PR 数:** 1 · **前置依赖:** [Phase 3.5.2](phase3.5.2-decide-core.md) 已合入(decide_system 已存在);建议在 [Phase 3.5.3](phase3.5.3-context-streaming.md) 之后(BuildStatePrefix 已能带 profile)

---

## 1. 动机

把"踩过的坑"和"领域规则"从 prompt 字符串里抽出来,变成结构化 Go 资产,后续维护成本断崖下降。

参考 tyche [`scene_knowledge.go`](file:///Users/didi/work/tyche/logic/agent/scene_knowledge.go) 与 [`v4_decide_prompt.go renderRequiredSlots/renderSceneKB`](file:///Users/didi/work/tyche/logic/agent/v4_decide_prompt.go#L146)。

---

## 2. 改动

### 2.1 领域规则抽成 Go 表 + render 函数

| 占位 | 来源文件 | 内容 |
|---|---|---|
| `{{REQUIRED_SLOTS}}` | 新增 `internal/agent/required_slots.go` | 关键参考维度 = seat_num / vehicle_type / price_preference,以及"自评信息够不够推好车"的判定指引;`renderRequiredSlots() string` |
| `{{SCENE_KB}}` | 新增 `internal/agent/scene_knowledge.go` | `SceneRule{Match []string, Inference string, Tip string}` 表(带娃→SUV、雪天→四驱、商务接送→商务车 ...);命中触发词时落 `need_delta(soft)` + 话术带一句 Tip;`renderSceneKB() string` |

`internal/prompt/decide_system.go` 用 `text/template` 占位 `{{SCENE_KB}}` `{{REQUIRED_SLOTS}}`,渲染时替换。

### 2.2 用户画像 profile_patch

- `state.Profile`(`UserProfile{TripScene, Companions, PriceSensitivity, StylePreference}`)。
- Decision 工具入参增加 `profile_patch` 字段(参考 tyche [`v4_tools.go:57-66`](file:///Users/didi/work/tyche/logic/agent/v4_tools.go#L57)),Decide 完顺手抽到 `state.Profile`,`BuildStatePrefix`(Phase 3.5.3)下一轮带上。

### 2.3 补三条中文铁律(decide_system)

保留已有的"严禁幻造/反问铁律",补充:
- **库存事实铁律**:"没有 X" 必须 search 真查,禁止凭历史臆断"没车"
- **改向必清旧车型**:`vehicle_model` 改口要 DELETE 旧值(否则旧车型把筛选钉死)
- **跳过即作罢铁律**:已反问过的维度不再重复问

**关键文件:** `internal/agent/{scene_knowledge,required_slots}.go`、`internal/orchestration/profile.go`(若 Profile 单独成文件)、`internal/prompt/decide_system.go`、`internal/orchestration/state.go`。

---

## 3. 验收

- [ ] `decide_system` 渲染后 `{{SCENE_KB}}`/`{{REQUIRED_SLOTS}}` 被正确替换,无残留占位符
- [ ] 用户说"带老人小孩" → 命中场景规则,落 `vehicle_type=SUV(soft)`,话术带 Tip,**不再追问车型**
- [ ] 用户改口("看更便宜的经济型") → 旧 `vehicle_model` 被 DELETE
- [ ] profile_patch 能落 `state.Profile`,下一轮前缀带上
- [ ] scene_knowledge / required_slots 单测覆盖命中与未命中

---

## 4. 风险

| 风险 | 应对 |
|---|---|
| 场景规则误命中 | Inference 一律 soft,用户下一句可轻松改向;矛盾场景(纯电+高原)优先 ask 提醒不硬推 |
| 占位符替换遗漏 | 渲染后 assert 不含 `{{` 子串 |

**完成态:** 领域规则结构化沉淀,新增场景在表里加一行即可,不动 prompt 主体。
