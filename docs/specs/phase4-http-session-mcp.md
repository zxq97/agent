# Phase 4: HTTP 服务化 + Session 持久化

> **目标:**
> 1. agent 从 CLI 转 HTTP 服务,SSE 流式输出,给 App / 小程序 / 客服系统接入
> 2. ConversationState 落 Redis,跨进程 / 跨 pod 持久化
> 3. 配套鉴权、限流、trace 等服务化基础设施

---

## 1. Context

P1-P3 在 CLI 验证了 agent 质量(导购、价格明细、保险推荐、supervisor 多 agent)。
P4 解决"上线对接"问题:
- 前端怎么用 agent?→ HTTP + SSE
- 跨设备 / 跨会话 / 跨 pod 怎么续聊?→ Redis session
- 怎么对接公司鉴权 / 限流 / trace?→ 中间件层

**后端能力沿用 tyche MCP**,P4 不引入新的后端 BFF。
若未来出现 tyche 不覆盖的能力(B 端运营、资金类),再按那时的场景单独评估,不在本 Phase 范围。

---

## 2. 验收标准

### 2.1 必过 demo
```bash
# 启动服务
$ go run ./cmd/http -env pre

# 客户端调用(SSE)
$ curl -N -X POST http://localhost:8080/agent/chat \
    -H "Content-Type: application/json" \
    -H "X-User-Id: u_001" -H "Authorization: Bearer ..." \
    -d '{"session_id":"s_001","message":"明天首都机场租 SUV,后天还"}'

data: {"type":"event","agent":"SupervisorAgent","action":"transfer","to":"ShoppingAgent"}
data: {"type":"event","agent":"ShoppingAgent","tool_call":"rental_search_locations"}
data: {"type":"event","agent":"ShoppingAgent","tool_result":"...摘要..."}
data: {"type":"message","content":"为您找到 5 款 SUV..."}
data: {"type":"done"}

# 断开重连,凭 session_id 续聊
$ curl -N -X POST http://localhost:8080/agent/chat \
    -H "Content-Type: application/json" \
    -H "X-User-Id: u_001" -H "Authorization: Bearer ..." \
    -d '{"session_id":"s_001","message":"那个 CR-V 怎么样"}'
# → 仍记得上一轮的 SUV 列表(从 Redis 拿历史)
```

### 2.2 检查清单
- [ ] SSE 流式响应,每条 event 含 agent 名 / 事件类型(message / tool_call / tool_result / done / error)
- [ ] `session_id` 跨请求保留完整 history(含 tool messages),Redis TTL 24h
- [ ] 鉴权中间件:校验公司鉴权 token / HMAC 签名
- [ ] 限流中间件:按 user_id 限流(默认 30 req / 分钟,可配置)
- [ ] 优雅停机:收到 SIGTERM 后等正在执行的 SSE 流跑完再退
- [ ] tyche MCP 鉴权 phone 按环境注入(pre/prod 走 env 占位 `${TYCHE_MCP_PHONE}`)

---

## 3. 分步实现

### Step 1 — Session Store `internal/session`
**文件:**
- `internal/session/store.go` 接口
- `internal/session/redis_store.go` 实现

```go
type Store interface {
    Get(ctx context.Context, sessionID string) ([]*schema.Message, error)
    Save(ctx context.Context, sessionID string, history []*schema.Message) error
    Touch(ctx context.Context, sessionID string) error  // 续 TTL
    Delete(ctx context.Context, sessionID string) error
}
```

- Redis 序列化:JSON(便于排查;message 量小,性能足够)
- Key: `agent:session:{userID}:{sessionID}`(用 userID 前缀做天然隔离)
- TTL: 24h,每次 Save / Touch 刷新
- 配置加 `session.redis_addr / session.password / session.db / session.ttl_hours`

### Step 2 — HTTP Handler + SSE `internal/http`
**文件:**
- `internal/http/handler.go`:主 handler,从 Redis 取 history → 调 adk.Runner → 边流边写 SSE
- `internal/http/sse.go`:SSE writer 封装
- `internal/http/middleware.go`:鉴权 / 限流 / trace

**接口设计:**
```
POST /agent/chat
  Headers: X-User-Id, Authorization, X-Trace-Id (optional)
  Body: { "session_id": "...", "message": "...", "metadata": {...} }
  Response: text/event-stream
    data: {"type":"message","content":"..."}
    data: {"type":"event","agent":"ShoppingAgent","tool_call":"rental_search_quotes","args_summary":"..."}
    data: {"type":"event","agent":"ShoppingAgent","tool_result":"找到 5 条报价"}
    data: {"type":"done"}
    data: {"type":"error","user_msg":"..."}  # 出错时

GET /agent/session/:id
  返回当前 session 摘要(最近 N 条消息,脱敏)

DELETE /agent/session/:id
  清空 session

POST /agent/feedback
  Body: { "session_id":..., "rating":1-5, "comment":"..." }
```

**流式逻辑:**
- 从 Redis 拿 history
- 调 `adk.Runner.Run(ctx, append(history, userMessage))` 返回 `AsyncIterator[*AgentEvent]`
- 逐 event 转 SSE 写出,同时收集所有 message 追加到 history
- 流结束后把新 history 写回 Redis

### Step 3 — cmd/http 入口
**文件:** `cmd/http/main.go`

```go
func main() {
    cfg := mustLoad("-env")
    factory := llm.NewFactory(&cfg.LLM)
    factory.SetLogger(logWriter)

    deps := tools.NewDeps(cfg, logWriter)
    allTools, _ := tools.All(ctx, deps)
    allTools = tools.WrapWithLogging(allTools, logWriter)

    runner, _ := agent.NewSupervisorSystem(ctx, agent.SystemDeps{
        ChatModelFactory: factory,
        AllTools:         allTools,
        MaxIterations:    cfg.Agent.MaxStep,
    })

    store := session.NewRedisStore(cfg.Session)
    srv := http.NewServer(cfg.HTTP, store, runner)

    go srv.Run()
    waitSignal()
    srv.Shutdown(ctx)
}
```

### Step 4 — 中间件
- **鉴权:**
  - C 端 App 来源:验公司 gateway 已下发的 token(具体方案与公司基础架构对齐)
  - 客服系统来源:HMAC-SHA256(secret, user_id + ts + body),过期窗口 5 分钟
- **限流:** 按 user_id token bucket,默认 30/min / 1000/day(Redis Cluster 实现)
- **Trace:** 提取 / 生成 `X-Trace-Id` 注入 ctx,落到所有日志、tyche RPC header、LLM API 调用 metadata

### Step 5 — 配置扩充
新增 yaml 字段:
```yaml
http:
  addr: ":8080"
  read_timeout: 30
  write_timeout: 600        # SSE 长连接

session:
  redis_addr: "redis.intra:6379"
  password: "${REDIS_PASSWORD}"
  db: 0
  ttl_hours: 24

ratelimit:
  per_minute: 30
  per_day: 1000
```

### Step 6 — 验收
- 写一个简单 web demo(static html + fetch SSE),手动跑通续聊
- 压测:100 并发 / 60s,p99 < 2s 首字节

---

## 4. 文件清单(P4 增量)

```
cmd/http/main.go                           # 新增
internal/session/store.go                  # 新增
internal/session/redis_store.go            # 新增
internal/http/handler.go                   # 新增
internal/http/sse.go                       # 新增
internal/http/middleware.go                # 新增
internal/config/config.go                  # 加 HTTPConf / SessionConf / RateLimitConf
conf/*.yaml                                # 增配置项
docs/specs/phase4-http-session-mcp.md      # 本文档
```

**不动 rental-saas-api、不动 tyche** —— 后端能力沿用 tyche MCP。

---

## 5. 关键决策

| # | 决策 | 理由 |
|---|---|---|
| P4-D1 | SSE 而非 WebSocket | LLM 是单向流式;SSE 实现简单、HTTP/1.1 / HTTP/2 通吃、代理友好 |
| P4-D2 | session 落 Redis,JSON 序列化 | 跨 pod 共享;JSON 便于直接 `redis-cli` 排查 |
| P4-D3 | session key 含 userID 前缀 | 天然隔离,防止误传 session_id 串号;userID 由鉴权层注入 ctx |
| P4-D4 | 后端能力沿用 tyche MCP | tyche 已是 C 端 API 的紧闭环,无需在 saas-api 再造一套 BFF |
| P4-D5 | tool/llm/event 全程在 logWriter 落日志 | 沿用 P1-P3 的诊断习惯,生产环境同样需要排障可视化 |

---

## 6. 已识别 TODO(P4 内必清)

- [ ] 鉴权方案与公司 gateway 对齐(C 端 token 校验入口、客服系统 HMAC secret 下发)
- [ ] 限流策略与公司基础设施(Redis Cluster / Sentinel)对齐
- [ ] SSE 响应被反向代理(nginx / 公司网关)缓冲的问题(`X-Accel-Buffering: no`)
- [ ] tyche MCP phone 在 prod 环境的获取方式(密钥管理 / 注入)
- [ ] 沉淀 5 条端到端评估 case(SSE 流是否完整、Redis 续聊是否一致)
