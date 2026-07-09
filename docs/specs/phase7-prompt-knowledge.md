# Phase 7 — Prompt 工程化沉淀

> 隶属 [技术方案总纲](../technical-plan.md)。**可独立执行。**
>
> **工时:** 2-3 天 · **PR 数:** 1 · **前置依赖:** [P1](phase1-shopping-mvp.md);建议 [P4](phase4-http-session.md) 之后(BuildStatePrefix 已能带 profile)

---

## 1. 目标

把"踩过的坑"和"领域规则"从 prompt 字符串里抽成结构化 Go 资产,后续维护成本断崖下降。参考 tyche `scene_knowledge.go` 与 `v4_decide_prompt.go`。

---

## 2. 改动

### 2.0 Prompt / Context / Tool Schema 版本化

新增 `internal/versioning/` 作为 prompt/context/tool schema 的版本清单:

| 文件 | 说明 |
|---|---|
| `manifest.go` | `PromptVersionSet` / `PromptAsset` / `ToolSchemaAsset` 类型,统一生成 sha256 hash |
| `prompt_versions.go` | decide/search_guide/price_detail/insurance/compare/rules/filter_interpreter 的 `PromptID + Version` |
| `tool_schema_versions.go` | `search_vehicles/ask/get_price_detail/insurance/compare_vehicles/interpret_rules` 的 schema version/hash |
| `context_versions.go` | `NeedsStatePrefix` / `ContextManager.BuildMessages` 的 context builder version/policy hash |

规则:
- prompt 模板、context prefix 策略、tool schema JSON、output parser 白名单任一变更,必须更新对应 version 或在 PR 中说明仅 metadata 变更。
- `RenderDecideSystem`、`decideTools()`、`NeedsStatePrefix/ContextManager` 输出时同时返回/记录对应 version set。
- 对 JSON schema 做 canonical marshal 后 hash,避免字段顺序导致 hash 抖动。

### 2.1 领域规则抽成 Go 表 + render 函数

| 占位 | 来源 | 内容 |
|---|---|---|
| `{{REQUIRED_SLOTS}}` | `internal/agent/required_slots.go` | 关键参考维度(seat_num/vehicle_type/price_preference)+ "信息够不够推好车"自评指引;`renderRequiredSlots()` |
| `{{SCENE_KB}}` | `internal/agent/scene_knowledge.go` | `SceneRule{Match []string, Inference string, Tip string}` 表(带娃→SUV、雪天→四驱、商务接送→商务车...);命中落 `need_delta(soft)` + 话术带 Tip;`renderSceneKB()` |

`internal/prompt/decide_system.go` 用 `text/template` 占位,渲染时替换。

### 2.2 用户画像 profile_patch
- `state.Profile{TripScene, Companions, PriceSensitivity, StylePreference}`
- decide 工具入参加 `profile_patch` 字段(参考 tyche `v4_tools.go`),Decide 完抽到 `state.Profile`,BuildStatePrefix(P4)下一轮带上

### 2.3 补三条中文铁律(decide_system)
- **库存事实铁律**:"没有 X" 必须 search 真查,禁止凭历史臆断"没车"
- **改向必清旧车型**:vehicle_model 改口要 DELETE 旧值
- **跳过即作罢铁律**:已反问过的维度不再重复问

---

## 3. 验收

- [x] `go test ./internal/versioning` 能生成稳定 hash;相同 schema 顺序变化 hash 不变
- [x] PromptAsset hash 随 prompt 内容变化
- [x] 改 `decide_tools.go` schema → `ToolSchemaHash` 变化,decide version set 能感知
- [x] decide LLM 日志含 `prompt_id/prompt_version/context_version/tool_schema_hash/parser_version`
- [x] decide_system 渲染后占位符全替换,无 `{{` 残留
- [x] "带老人小孩" → Go 侧命中场景规则,落 `vehicle_type=SUV(soft)`;Tip 已在 scene KB 资产中
- [ ] "带老人小孩" 话术带 Tip、不再追问车型需接 eval/真实模型验证
- [ ] 改口"看更便宜的经济型" → 旧 vehicle_model 被 DELETE 需 decide eval 覆盖
- [x] profile_patch 落 state.Profile,下一轮前缀带上
- [x] scene_knowledge / required_slots 单测覆盖命中与核心维度渲染

---

## 4. 风险

| 风险 | 应对 |
|---|---|
| 场景规则误命中 | Inference 一律 soft,用户下句可改向;矛盾场景(纯电+高原)优先 ask 提醒不硬推 |
| 占位符替换遗漏 | 渲染后 assert 不含 `{{` |

**完成态:** 新增场景在表里加一行即可,不动 prompt 主体。
