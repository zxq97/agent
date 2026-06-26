# Phase 3.5.1 — 收口与勘误

> 隶属 [phase3.5 重构路线图](phase3.5-decide-capability-refactor.md) 的第 1 步。**可独立执行。**
>
> **工时:** 0.5 天 · **PR 数:** 1 · **前置依赖:** 无(基于当前 P3/P4 已合入代码)

---

## 1. 动机

把不安全的占位代码先关掉,避免错误数字流到生产 prompt;给后续阶段清场。

当前 [internal/tools/local_qualification.go](../../internal/tools/local_qualification.go)、[local_trip_cost.go](../../internal/tools/local_trip_cost.go)、[local_time_optimize.go](../../internal/tools/local_time_optimize.go) 三个本地 P5 工具的规则全是**占位经验常数**(驾龄门槛、油价、能耗、过路费、停车费),一旦注册给 LLM,模型会一本正经地引用错误数字给用户("油费约 540 元"),反而比没这个工具更糟。

---

## 2. 改动清单

| 文件 | 操作 |
|---|---|
| [internal/config/config.go](../../internal/config/config.go) | `AgentConf` 增加 `EnableLocalTools bool`(`yaml:"enable_local_tools"`),默认 false |
| [internal/tools/common.go](../../internal/tools/common.go) | `All()` 里 `localTools()` 改为受 `cfg.Agent.EnableLocalTools` 开关控制;关闭时不追加。`localTools()` 关联代码**保留不删** |
| [internal/agent/adk.go](../../internal/agent/adk.go) | `buildShoppingAgent` 的 `filterToolsByName` 名单里去掉 `check_qualification / estimate_trip_cost / optimize_pickup_time` |
| [internal/prompt/shopping_system.go](../../internal/prompt/shopping_system.go) | 删掉这三个本地 tool 的描述段(原模板里 7/8/9 条 + 扩展能力段对应描述) |
| [internal/orchestration/state.go](../../internal/orchestration/state.go) | 给 `LastQuoteIDs`/`SelectedQuoteID` 加注释 `// Deprecated: Phase 3.5.2 起改用 LastQuotes/SelectedRef`(物理保留,避免破坏 import) |
| [CLAUDE.md](../../CLAUDE.md) | 安全护栏 1、2 加 `<!-- TODO(phase3.5.2): 改为"Go 托管 ID,LLM 不经手" -->` 标记(本步只标,3.5.2 落地后改文案) |

---

## 3. 验收

- [ ] `go test ./...` 通过
- [ ] `go run ./cmd/cli -env dev` 原链路不挂(搜车/明细/保险/规则/闲聊)
- [ ] 启动时打日志确认:LLM 可见 toolset 里**没有** `check_qualification`/`estimate_trip_cost`/`optimize_pickup_time`
- [ ] `conf/dev.yaml` 不配 `enable_local_tools` 时,默认 false 生效

---

## 4. 风险

| 风险 | 应对 |
|---|---|
| 删描述段误删模板其它内容 | 删完跑 `RenderShoppingSystem` 单测,确认渲染不报错、关键工具描述仍在 |
| 有调用方依赖 `LastQuoteIDs` | 全局 grep 确认目前**无任何读写**(已确认是死字段),仅加注释不删 |
