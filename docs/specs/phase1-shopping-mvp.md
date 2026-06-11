# Phase 1: 骨架 + 导购 MVP (CLI,接 tyche MCP)

> **目标:** 在 CLI 跑通 eino ReAct agent 全链路 —— 用户描述出行需求,agent 通过 **tyche MCP**(`rental_search_locations` → `rental_resolve_poi` → `rental_search_quotes`)给出 3 条候选报价。
> **不在范围:** HTTP 服务 / Session 持久化 / 知识库 / 价格明细深度展开 / 保险(后续 Phase)。

---

## 1. Context

P1 是项目的最小可工作单元。之前一版直连 saas-api `/inner/price/list` 的尝试已废弃 —— 字段缺(没车型可读名/没图片)、地理点要自己兜底,**等价于在 agent 侧重新造一遍 tyche 已经做过的事**。

[/Users/didi/work/tyche/controller/mcp/controller.go](file:///Users/didi/work/tyche/controller/mcp/controller.go) 已经把 7 个 C 端工具封装好(JSON-RPC 2.0),其中 6 个**只读**工具直接给 agent 用最划算。

P1 验收完成意味着:agent loop、LLM 工厂、tyche MCP client、CLI 交互、日志诊断 整条主链路通。

---

## 2. 验收标准

### 2.1 必过 demo
```
$ go run ./cmd/cli
你: 明天下午6点首都机场取车,两天后同地点还车

(后台:agent 依次发出 rental_search_locations → rental_resolve_poi → rental_search_quotes)

小租: 给您找了 3 款合适的:
1. 大众 朗逸 自动 5 座    日均 138  总价 276  含基础保险
2. 丰田 卡罗拉 自动 5 座  日均 158  总价 316  含基础保险
3. 本田 CR-V 自动 5 座    日均 268  总价 536  适合多人/行李多
推荐理由:...
最终价格以下单时为准。
```

### 2.2 检查清单
- [ ] `go build ./...` / `go vet ./...` 通过
- [ ] [conf/dev.yaml](../../conf/dev.yaml) 填了 DeepSeek API key + tyche endpoint + 白名单手机号
- [ ] CLI 启动后日志(`logs/agent.log`)能看到一条完整调用链:
  ```
  [llm-out] turn=1 ... tool_calls=1 (rental_search_locations)
  [tool-in ] rental_search_locations args={...}
  [tyche] -> tools/call ... req={...}
  [tyche] 200 ... resp={...}
  [tool-out] ...
  [llm-out] turn=2 ... tool_calls=1 (rental_resolve_poi)
  ...
  [llm-out] turn=N ... tool_calls=0  ← 最终回答
  ```
- [ ] 用户消息缺关键信息(只说"想租车"没说时间地点)→ agent 主动追问而非空跑工具

---

## 3. 已完成实现

### Step 1 — 项目骨架 ✅
`go.mod` (`github.com/zxq97/agent`),目录:
```
cmd/{cli,http}
internal/{agent, orchestration, tools, tyche, llm, rag, session, http, types, prompt, config}
conf/{dev,pre,prod}.yaml
```

### Step 2 — 配置层 [internal/config/config.go](../../internal/config/config.go) ✅
- `Config{Env, LLM, Tyche}`
- `Tyche{Endpoint, Phone, Timeout}` 描述 tyche MCP server 接入参数
- 支持 `${ENV_NAME:-default}` 占位

### Step 3 — 共享类型 [internal/types/types.go](../../internal/types/types.go) ✅
`AddressInfo / QuoteSlot / Phase` 给后续 supervisor 用。

### Step 4 — ConversationState [internal/orchestration/state.go](../../internal/orchestration/state.go) ✅
唯一定义点,带 mutex,P3 起 supervisor 必读。

### Step 5 — LLM 工厂 [internal/llm/](../../internal/llm/) ✅
- `provider.go`:Factory + Builder 注册表
- `deepseek.go`:eino-ext deepseek adapter
- `logging.go`:每轮 Generate / Stream 入参出参全落日志

### Step 6 — tyche MCP client [internal/tyche/client.go](../../internal/tyche/client.go) ✅
轻量 JSON-RPC over HTTP 客户端:
- `Initialize / ListTools / CallTool`
- 走 `Authorization: Bearer <phone>` 鉴权
- 每次调用打日志(请求/响应)
- **不**用 `modelcontextprotocol/go-sdk` 是因为它的 `StreamableClientTransport` 强依赖 SSE,而 tyche 是简化的纯 POST + JSON-RPC

### Step 7 — Tool 包装层 [internal/tools/](../../internal/tools/) ✅
- `common.go`:`Deps / NewDeps / All(ctx, deps)` —— 拉 tyche tools/list,白名单过滤,逐个包装
- `tyche_wrap.go`:把 tyche tool 元数据(name/desc/inputSchema)包装成 eino InvokableTool,保留 tyche 原始 JSON Schema
- `logging.go`:套一层 InvokableRun 入口/出口日志
- **白名单**(`isAllowedTool`):暴露 6 个只读工具
  - `rental_search_locations / rental_resolve_poi`
  - `rental_search_quotes / rental_get_order_details`
  - `rental_get_reservation / rental_get_driver_list`
- **拒绝**:`rental_create_order` 永不暴露给 LLM

### Step 8 — Prompt [internal/prompt/shopping_system.go](../../internal/prompt/shopping_system.go) ✅
text/template 渲染,显式描述 6 个工具 + 调用顺序(先 search_locations → resolve_poi → search_quotes)+ 红线 + 反例 few-shot。

### Step 9 — Shopping Agent [internal/agent/shopping.go](../../internal/agent/shopping.go) ✅
基于 eino `react.NewAgent`:
- `ToolCallingModel` 注入 LoggingChatModel
- `ToolsConfig.Tools` 注入 tyche 包装好的 tool 列表
- 自定义 `StreamToolCallChecker = scanAllStreamForToolCall` —— 修复 DeepSeek 流式末尾才追加 tool_call 时,默认 `firstChunkStreamToolCallChecker` 误判退出 loop 的问题

### Step 10 — CLI [cmd/cli/main.go](../../cmd/cli/main.go) ✅
- flags:`-env / -conf-dir / -log-file / -city / -name`
- 默认日志 `logs/agent.log`(stdout 只有对话)
- REPL:`:exit` 退出,`:reset` 清空历史
- 流式输出 LLM content 到 stdout

---

## 4. 关键决策记录(P1)

| # | 决策 | 理由 |
|---|---|---|
| P1-D1 | **接 tyche MCP 而非自己直连 saas-api** | tyche 已有 7 个 C 端工具,字段质量(车名/品牌/分类/燃料/座位/图片/POI)远好于 saas-api inner;省去自己写经纬度兜底、车型字典联表 |
| P1-D2 | **不**用 `modelcontextprotocol/go-sdk` 接 tyche | tyche 的 MCP 是简化版纯 POST + JSON-RPC,go-sdk 的 StreamableClientTransport 强依赖 SSE,不兼容;自写 100 行轻量 client 更稳 |
| P1-D3 | tool 白名单 isAllowedTool | 防止 `rental_create_order` 被 LLM 调用;P5 真要做下单跳转再单独评审 |
| P1-D4 | 自定义 `StreamToolCallChecker` 扫整流 | DeepSeek 中文场景下 tool_call 在流末尾,eino 默认只看第一帧会漏 |
| P1-D5 | tyche RPC 与 tool 入口/出口日志默认开启,无 flag | 排障刚需,关闭它毫无好处;只让用户选写 stderr 还是写文件 |

---

## 5. 文件清单

```
cmd/cli/main.go                       ✅
internal/config/config.go             ✅
internal/types/types.go               ✅
internal/orchestration/state.go       ✅
internal/llm/provider.go              ✅
internal/llm/deepseek.go              ✅
internal/llm/logging.go               ✅
internal/tyche/client.go              ✅
internal/tools/common.go              ✅
internal/tools/tyche_wrap.go          ✅
internal/tools/logging.go             ✅
internal/prompt/shopping_system.go    ✅
internal/agent/shopping.go            ✅
conf/{dev,pre,prod}.yaml              ✅
```

---

## 6. 已识别 TODO(P2 起处理)

- [ ] tyche MCP server 当前缺少 health check 接口;启动时 `Initialize` 失败要给清晰的错误指引(目前只在第一次 ListTools 才暴露)
- [ ] tyche `rental_search_quotes` 返回的 quote_item 已含车名/品牌/座位,但**保险**只有 `grantee_list` 简化版;P2 评估是否独立保险查询通道
- [ ] CLI 没有 session 持久化(每次启动 history 清零),P4 上 Redis
- [ ] 无单测(MCP wrapper 单测需要起一个假 tyche server,放 P2 做)
- [ ] tyche 的 `rental_create_order` 暂不暴露给 LLM;若后续要做"agent 协助下单",必须设计**人机确认**环节
