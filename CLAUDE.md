# CLAUDE.md — 租车智能体 v2

## 项目概述

面向 C 端租车用户的对话式 AI 助手，Go 1.24+，责任链 Pipeline 架构。

- **分支**：`feat/agent-v2-pipeline`
- **LLM**：默认 DeepSeek-chat，纯 Go OpenAI-compatible client（不依赖 eino）
- **后端数据**：tyche MCP（POI 解析）+ rental-guide 集群（报价 + 菜单）
- **服务形态**：HTTP + SSE（`cmd/http/main.go`），附带前端页面（`web/index.html`）

## 构建与运行

```bash
# 构建（不保留二进制文件）
go build ./...

# 本地运行
DEEPSEEK_API_KEY=sk-xxx go run ./cmd/http -env dev

# 测试
go test ./...

# 检查
go vet ./...
```

## 编码规范

### Go 编译产物

**禁止在仓库中保留编译后的二进制文件。** 使用 `go run` 运行，不要 `go build` 后留下可执行文件。`.gitignore` 已配置忽略常见的二进制文件名（`/http`、`/cli`、`/rental-agent`）。

### import 规范

**不加 import 别名**。唯一例外是同文件 import 两个同名包时消歧义。

```go
// ✅ 正确
import (
    "github.com/zxq97/rental-agent/internal/types"
)

// ❌ 错误（无必要别名）
import (
    mytypes "github.com/zxq97/rental-agent/internal/types"
)

// ✅ 例外：两个同名包消歧义
import (
    agenthttp "github.com/zxq97/rental-agent/internal/httphandler"
    "net/http"
)
```

### 文档变更规范

**方案/设计文档修改时必须留痕。** 在 `docs/technical-plan.md` 的附录 A（演进日志）追加一行，记录：日期、改了什么、为什么改、影响范围。Phase spec 同步更新时在变更处标注 `[演进 YYYY-MM-DD]` 并说明原因。禁止静默修改设计文档——事后回溯时需要知道"当初为什么这么改"。

### 其他规范

- **ConversationState 唯一定义**在 `internal/orchestration/state.go`，任何子包 import 它
- **Stage / Capability 统一签名**：`Stage.Handle(ctx, *AgentContext) (Signal, error)`；`Capability.Run(ctx, CapabilityInput) (*CapabilityResult, error)`
- **Tool 描述写给 LLM 看**：中文 OK，decide tool schema **不含 ID 字段**（ID 由 Go 从 state 注入）
- **错误处理**：tool 内 error 转 `{is_error, user_msg, debug}`，user_msg 是人话
- **工具结果入 history 必须降噪**：存精炼摘要，不存 tyche 原始 JSON
- **日志必含结构化字段**：LLM / tool / stage 日志带 trace_id / session_id

## 目录结构

```
cmd/
  http/main.go          HTTP + SSE 服务入口
  cli/main.go           本地调试 CLI（保留但非主入口）
internal/
  agent/                Pipeline + Stage + Decider + Capability + NeedState + FilterCode
  orchestration/        ConversationState 唯一定义
  tools/                tyche MCP tool wrap（Go 托管 ID）+ 白名单
  tyche/                tyche MCP JSON-RPC client + guide storelist HTTP client
  llm/                  provider 工厂 + 纯 Go OpenAI-compatible client
  httphandler/          HTTP handler + SSE emitter + middleware
  session/              Session Store 接口 + MemoryStore
  config/               yaml + env 装载
  types/                跨模块共享类型（UserNeed / NeedDelta / SearchConstraints 等）
  prompt/               decide_system / 各 Capability prompt
conf/                   dev / pre / prod 多环境 yaml
web/                    前端单页应用
docs/                   技术方案 + 各 Phase spec
```

## 关键架构

### Pipeline 流水线

```
DecideStage (LLM #1 流式 function-calling)
  → CapabilityStage (按 Decision.Tool 分发)
  → FinalizeStage (落 state、写 history)
```

### UserNeed 需求管理

LLM 输出 `need_delta`（ADD/UPDATE/DELETE/NEGATE/DECAY），Go 侧管理生命周期：
- `TickNeeds`：每轮自然衰减
- `ApplyDelta`：应用增量操作
- `ApplyConflictDecay`：冲突衰减（换品牌→衰减旧座位数）
- `FilterActiveNeeds`：过滤 Dormant 需求
- `StaticRecall`：needs → filter_codes 静态映射

### 数据源

- **取还车 POI**：tyche MCP（`rental_search_locations` + `rental_resolve_poi`）
- **报价 + 菜单**：rental-guide `/car/rental/guide/store/list/agent`（返回 menu_group + veh_rates）
- Guide 不可用时 fallback 到 MCP `rental_search_quotes`

### ID 安全铁律

`context_id` / `reference_id` / `supplier` 由 Go 从 state 注入，LLM 不经手，tool schema 不暴露 ID 字段。
