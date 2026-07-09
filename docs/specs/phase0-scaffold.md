# Phase 0 — 项目骨架 + 基础设施

> 隶属 [技术方案总纲](../technical-plan.md)。**已完成。**
>
> **前置依赖:** 无（从 main 空仓起步）
>
> **[演进 2026-06-30] 范围扩大**：原 P0 只含 CLI 入口 + Pipeline 骨架。实际实现时，HTTP 服务化、前端页面、Session Store 一并纳入 P0（用户要求跳过 CLI 直接做 HTTP + 前端），UserNeed/NeedDelta 类型定义也提前到 P0 落地。

---

## 1. 目标

把"能编译、能起 HTTP 服务、浏览器打开能看到对话页面、Pipeline 接口骨架立起来、tyche MCP + guide storelist 连通"做出来。**不含 LLM 决策逻辑**（Decider 在 P1），只验证管道连通 + 服务可达。

---

## 2. 已实现清单

### 2.1 工程基建

| 文件 | 说明 |
|------|------|
| `go.mod` | `github.com/zxq97/rental-agent`，Go 1.24+，依赖仅 `gopkg.in/yaml.v3` |
| `conf/{dev,pre,prod}.yaml` | 多环境配置：LLM provider + agent_bindings + tyche + guide + HTTP + session + ratelimit |
| `internal/config/config.go` | yaml + `${ENV}` 占位展开 + 默认值；含 `LLMConf`/`TycheMCPConf`/`GuideConf`/`HTTPConf`/`SessionConf`/`RateLimitConf`/`AgentHubConf` |
| `.gitignore` | Go 二进制 + `.claude/` + 编辑器；**禁止保留编译产物**（`/http`、`/cli`、`/rental-agent`） |

### 2.2 共享类型与状态

| 文件 | 说明 |
|------|------|
| `internal/types/types.go` | `Phase`、`QuoteSlot`、`ToolCallSnapshot` |
| `internal/types/need.go` | **UserNeed**（Type/Value/Source/Hardness/Confidence/Negative/BornTurn/LastReinforced）、**NeedDelta**（Op/Type/Value/Hardness/Confidence/Factor）、**NeedState**（ActiveHard/ActiveSoft/Decaying/Dormant/Negated）、**SearchConstraints**（Hard/Soft/Negative）、**LastSearchState**、**MenuGroupView**/**MenuItemView** |
| `internal/orchestration/state.go` | **ConversationState 唯一定义**：SessionID/UserID/Rental/LastQuotes/QuoteAt/SelectedRef/Constraints/TurnCount/LastSearch/CachedMenu/History + mutex + `AppendMessage`/`SnapshotHistory`/`SetQuotes`/`SelectQuote`/`IsQuoteStale`/`SupplierOf` + `NeedsStatePrefix()` |

### 2.3 LLM 工厂（纯 Go，不依赖 eino）

| 文件 | 说明 |
|------|------|
| `internal/llm/model.go` | `ChatModel` 接口（`Chat` 同步 + `ChatStream` 流式）、`Message`/`ToolCall`/`ToolDef`/`ChatRequest`/`ChatResponse`/`StreamChunk`/`Usage` |
| `internal/llm/provider.go` | `Builder` + `Factory`（懒加载 + AgentBindings + 可选 LoggingChatModel 包装） |
| `internal/llm/openai_client.go` | 纯 Go OpenAI-compatible client：流式 SSE 解析 + tool_calls 分片累积 + finish_reason 终止 + syncFallback |
| `internal/llm/deepseek.go` | DeepSeek provider 注册（`init` 调 `Register("deepseek", ...)`) |
| `internal/llm/logging.go` | `LoggingChatModel` 包装层，落 model/stage/tokens/duration 日志 |

### 2.4 后端数据客户端

| 文件 | 说明 |
|------|------|
| `internal/tyche/client.go` | tyche MCP JSON-RPC over HTTP（`tools/list` + `tools/call`）、`fixDateTimeSeconds` 补秒兜底 |
| `internal/tyche/guide_client.go` | **rental-guide 集群 HTTP client**：调 `/car/rental/guide/store/list/agent`，返回 menu_group + veh_rates + context_id |
| `internal/tyche/datetime.go` | `fixDateTimeSeconds` 实现 |

### 2.5 工具层

| 文件 | 说明 |
|------|------|
| `internal/tools/common.go` | `Deps`（持有 `Tyche *Client` + `Guide *GuideClient` + `Logger`）、`isAllowedTool` 白名单（6 个只读工具）、`Call`（调 tyche 工具 → 统一 `Result`） |
| `internal/tools/tyche_wrap.go` | `Result` 结构 + `Call` 实现（错误包成 `{is_error, user_msg, debug}`，不向上层返 Go error） |

### 2.6 Pipeline / Stage / Capability 接口骨架

| 文件 | 说明 |
|------|------|
| `internal/agent/pipeline.go` | `Signal`（Continue/Stop）、`Stage` 接口、`ChatPipeline`（逐 stage 执行 + 日志） |
| `internal/agent/types.go` | `Emitter` 接口（Text/Event）、`Decision`、`Understanding`、`Clarification`、`CapabilityResult`、`CapabilityInput`、`ModelGetter`、`Capability` 接口 |
| `internal/agent/stages.go` | `DecideStage`、`CapabilityStage`、`FinalizeStage` 三个 Stage 实现 |
| `internal/agent/orchestrator.go` | `CapabilityOrchestrator`（按 Decision.Tool 分发）、`RentalAgent`（对外入口 `New` + `Run`）、`finalize`（落 history） |

### 2.7 HTTP 服务 + 前端

| 文件 | 说明 |
|------|------|
| `cmd/http/main.go` | HTTP 服务入口：config → factory → deps → agent → store → handler → `http.Server` + 优雅关机 |
| `internal/httphandler/handler.go` | 路由注册（Go 1.22+ ServeMux）：`POST /api/chat`（SSE 流式）+ `GET/POST/DELETE /api/sessions` + `/healthz` + 静态文件 |
| `internal/httphandler/sse.go` | `sseEmitter` 实现 `agent.Emitter` 接口：text/event/done/error 事件 + Flush |
| `internal/httphandler/middleware.go` | recovery + cors + accessLog 中间件链 |
| `internal/session/store.go` | `Store` 接口 + `MemoryStore`（sync.RWMutex + 两层 map + 惰性 TTL）+ `Summary`/`extractPreview` |
| `web/index.html` | 前端 SPA（vanilla HTML/CSS/JS）：User ID 登录屏 → 对话界面（侧边栏会话列表 + 主区域消息气泡 + 输入框）→ SSE 流式接收（fetch + ReadableStream） |

### 2.8 CLI 入口（保留，非主入口）

| 文件 | 说明 |
|------|------|
| `cmd/cli/main.go` | 加载 config → 建 deps → agent.New → 多轮 REPL，`cliEmitter` 直接 stdout |

### 2.9 Prompt 模板

| 文件 | 说明 |
|------|------|
| `internal/prompt/decide_system.go` | 决策 system prompt 模板 + `RenderDecideSystem`（注入时间/昵称） |
| `internal/prompt/capability_system.go` | SearchGuide/PriceDetail/Insurance/Compare 四个 Capability 的 system prompt |

---

## 3. 验收

- [x] `go build ./...` 通过
- [x] `go vet ./...` 通过
- [x] `go test ./...` 全绿
- [x] `go run ./cmd/http -env dev` 能启动，打印 banner（addr/tyche/model/webDir）
- [x] 浏览器打开 `http://localhost:8080` 看到 User ID 登录页
- [x] `isAllowedTool` 白名单只放 6 个只读工具
- [x] `curl /healthz` 返回 200
- [x] 仓库内无编译残留二进制文件
