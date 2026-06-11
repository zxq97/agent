# Phase 4: HTTP 服务化 + Session 持久化 + agent-bff MCP

> **目标:**
> 1. agent 从 CLI 转 HTTP 服务,SSE 流式输出,给 App / 小程序 / 客服系统接入
> 2. ConversationState 落 Redis,跨进程 / 跨 pod 持久化
> 3. 在 rental-saas-api 内加 `/inner/agent_mcp/*` 子域,agent 侧切 MCP client

---

## 1. Context

P1-P3 在 CLI 验证了 agent 质量。P4 解决"上线对接"问题:
- 前端怎么用 agent?→ HTTP + SSE
- 跨设备 / 跨会话怎么续聊?→ Redis session
- inner 接口字段噪声大、契约不稳 → 通过 MCP BFF 解耦

P4 **不解决**:扩展能力(留 P5)、生产化的监控降级评估(留 P6)。

---

## 2. 验收标准

### 2.1 必过 demo
```bash
# 启动服务
$ go run ./cmd/http -env pre

# 客户端调用(SSE)
$ curl -N -X POST http://localhost:8080/agent/chat \
    -H "Content-Type: application/json" \
    -H "X-User-Id: u_001" \
    -d '{"session_id":"s_001","message":"北京周末租 SUV"}'

data: {"type":"thinking","content":"正在查询..."}
data: {"type":"tool_call","name":"list_quotes","args":{...}}
data: {"type":"tool_result","name":"list_quotes","summary":"找到 5 款车型"}
data: {"type":"message","content":"为您找到 5 款 SUV..."}
data: {"type":"done"}

# 断开重连,凭 session_id 续聊
$ curl -N -X POST http://localhost:8080/agent/chat \
    -H "Content-Type: application/json" \
    -H "X-User-Id: u_001" \
    -d '{"session_id":"s_001","message":"那个 CR-V 怎么样"}'
# → 仍记得上一轮的 SUV 列表
```

### 2.2 检查清单
- [ ] SSE 流式响应,每条消息一个 event,前端能边到边渲染
- [ ] `session_id` 跨请求保留 ConversationState(Redis TTL 24h)
- [ ] 鉴权中间件:校验 `X-User-Id` + `X-Sign`(对接公司鉴权)
- [ ] 限流中间件:按 user_id 限流(默认 30 req / 分钟)
- [ ] 优雅停机:收到 SIGTERM 后等正在执行的 SSE 流跑完再退
- [ ] rental-saas-api 内 `/inner/agent_mcp/*` 子域可用,字段精简,粒度对齐 agent
- [ ] agent 侧 `internal/tools/` 切到 MCP client,**关闭直连 inner 不影响**

---

## 3. 分步实现

### Step 1 — Session Store `internal/session`
**文件:**
- `internal/session/store.go` 接口
- `internal/session/redis_store.go` 实现

```go
type Store interface {
    Get(ctx context.Context, sessionID string) (*orchestration.ConversationState, error)
    Save(ctx context.Context, state *orchestration.ConversationState) error
    Touch(ctx context.Context, sessionID string) error  // 续 TTL
    Delete(ctx context.Context, sessionID string) error
}
```

- Redis 序列化:`encoding/gob` 或 JSON(选 JSON 便于排查)
- Key: `agent:session:{sessionID}`
- TTL:24h,每次 Save / Touch 刷新
- 配置加 `session.redis_addr / session.password / session.db / session.ttl_hours`

### Step 2 — HTTP Handler + SSE `internal/http`
**文件:**
- `internal/http/handler.go` 主 handler
- `internal/http/sse.go` SSE writer
- `internal/http/middleware.go` 鉴权 / 限流 / trace

**接口设计:**
```
POST /agent/chat
  Headers: X-User-Id, X-Sign, X-Trace-Id (optional)
  Body: { "session_id": "...", "message": "...", "metadata": {...} }
  Response: text/event-stream
    data: {"type":"thinking|tool_call|tool_result|message|error|done", ...}

GET /agent/session/:id
  返回当前 session 摘要(slot + history 末尾 N 条)

DELETE /agent/session/:id
  清空 session

POST /agent/feedback
  Body: { "session_id":..., "rating":1-5, "comment":"..." }
```

**流式逻辑:**
- 从 Supervisor 拿到 `agent.Stream(ctx, msgs)` 的 `StreamReader`
- 边读边 `flush` 写 SSE event
- tool call / tool result 也作为 event 推出去(可调试)

### Step 3 — cmd/http 入口
**文件:** `cmd/http/main.go`

```go
func main() {
    cfg := mustLoad("-env")
    redisStore := session.NewRedisStore(cfg.Session)
    llmFactory := llm.NewFactory(&cfg.LLM)
    toolDeps := tools.NewDeps(cfg)

    sup := agent.NewSupervisor(llmFactory, toolDeps)
    srv := http.NewServer(cfg.HTTP, redisStore, sup)

    go srv.Run()
    waitSignal()
    srv.Shutdown(ctx)
}
```

### Step 4 — rental-saas-api 加 `/inner/agent_mcp/*` 子域

**改动位置:** `/Users/didi/work/rental-saas-api`

- 新增 `controller/agent_mcp/*.go`(`price.go / store.go / vehicle.go / insurance.go / contract.go`)
- 复用现有 `logic/` 与 `dao/`(只在 controller 层做字段精简 + 入参归一)
- 新增 `router/agent_mcp_router.go` 注册 `prefix + "/inner/agent_mcp/*"`
- 接入 [trpc-mcp-go](https://github.com/trpc-group/trpc-mcp-go) 在子路由暴露 MCP server
- 字段精简原则:
  - 去掉所有 `*_img` / `*_url` / `button_*` / `tag_color` 等 App 渲染字段
  - 时间统一 string ISO,金额统一"元 float64"(后端原是"分 int64")

**入参语义化:**
- 新接口直接接受 `pickup_city_id / pickup_lng / pickup_lat / pickup_time` 等扁平字段
- 而不是嵌套 `pickup_info.{...}` —— 让 MCP client 写 schema 时更顺

### Step 5 — agent 侧切 MCP client
**文件:** `internal/tools/` 重构

- 引入 [eino-ext mcp tool](https://www.cloudwego.io/docs/eino/ecosystem_integration/tool/tool_mcp/)
- `tools.All(d)` 改为:
  - 若 `cfg.Backend.UseMCP = true`:用 `mcp.GetTools(ctx, &mcp.Config{Cli: mcpClient})` 直接拿一组 tool
  - 否则:走 P1-P3 的直连实现(fallback)
- 灰度切换:`use_mcp: true/false` 开关 + 单元测试两条路径

### Step 6 — 中间件
- **鉴权:** 校验 `X-Sign = HMAC-SHA256(secret, user_id + ts + body)`,过期窗口 5 分钟
- **限流:** 按 user_id token bucket,默认 30/min / 1000/day
- **Trace:** 提取 `X-Trace-Id` 注入 ctx,落到所有日志和 inner 调用 header

### Step 7 — 验收
- 写一个简单 web demo(static html + fetch SSE),手动跑通续聊
- 压测:100 并发 / 60s,p99 < 2s 首字节

---

## 4. 文件清单(P4 增量)

### agent 仓库
```
cmd/http/main.go                           # 新增
internal/session/store.go                  # 新增
internal/session/redis_store.go            # 新增
internal/http/handler.go                   # 新增
internal/http/sse.go                       # 新增
internal/http/middleware.go                # 新增
internal/tools/mcp_loader.go               # 新增(MCP client → eino tools)
internal/tools/common.go                   # 修改(支持 UseMCP 开关)
internal/config/config.go                  # 加 HTTPConf / SessionConf / Backend.UseMCP
conf/*.yaml                                # 增配置项
docs/specs/phase4-http-session-mcp.md      # 本文档
```

### rental-saas-api 仓库
```
controller/agent_mcp/price.go              # 新增
controller/agent_mcp/store.go              # 新增
controller/agent_mcp/vehicle.go            # 新增
controller/agent_mcp/insurance.go          # 新增
controller/agent_mcp/contract.go           # 新增
router/agent_mcp_router.go                 # 新增
```

---

## 5. 关键决策

| # | 决策 | 理由 |
|---|---|---|
| P4-D1 | SSE 而非 WebSocket | LLM 是单向流式;SSE 实现简单、HTTP/2 友好 |
| P4-D2 | session 落 Redis,JSON 序列化 | 跨 pod 共享;JSON 便于排查 |
| P4-D3 | agent-bff 寄在 rental-saas-api 子域而非独立仓库 | 复用 dao/logic;不增加新仓库 / 新部署 |
| P4-D4 | 用 MCP 而非自定义 RPC | 标准协议;后续多 agent 复用零成本;trpc-mcp-go 现成 |
| P4-D5 | 直连 inner 保留 fallback | 灰度切换;MCP server 故障可回滚 |

---

## 6. 已识别 TODO(P4 内必清)

- [ ] 鉴权方案与公司 gateway 对齐
- [ ] 限流策略与公司基础设施(Redis Cluster / Sentinel)对齐
- [ ] trpc-mcp-go 与现有 rental-saas-api 的 ngs httpserver 共存方案
- [ ] agent-bff 接口契约 review(命名 / 字段 / 错误码),拉后端同学评审
- [ ] 灰度切换的 metric / alert
