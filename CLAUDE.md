# 租车智能体 (Rental Agent)

> 面向 C 端租车用户的对话式 AI 助手 —— 帮用户**推荐车型报价、推荐保险、解读价格明细、解读租车条款**,
> 以及决策辅助、驾照预检、下单 deeplink、售后 FAQ、比价异议等扩展能力。

## 核心能力

| 能力 | Phase | 描述 |
|---|---|---|
| 车型推荐 + 报价 | P1 | 调 tyche MCP `rental_search_locations`+`rental_resolve_poi`+`rental_search_quotes`,排序对比 |
| 价格明细解读 | P2 | 调 tyche MCP `rental_get_order_details`,讲清每一项费用 |
| 保险推荐 | P2 | 基于报价里的 `grantee_list` + 驾龄/场景给组合建议;tyche 不覆盖独立保险列表时走 saas-api 兜底 |
| 条款 RAG | P3 | 基于 knowledge/ 检索回答,答复带 `[来源]` |
| 决策辅助 / 资质 / 售后 / 比价 | P5 | 详见 [docs/specs/phase5-extensions.md](docs/specs/phase5-extensions.md) |

## 技术栈

- **语言**:Go 1.24+
- **Agent 框架**:[cloudwego/eino](https://github.com/cloudwego/eino) —— ReAct / ChatModelAgent + ADK 多 agent
- **LLM**:可插拔,**默认 DeepSeek**(`deepseek-chat` + `deepseek-reasoner`),通过 `internal/llm` 工厂可换 Claude / 千问 / 豆包
- **后端调用**:**tyche MCP**(JSON-RPC 2.0 over HTTP)是主路线 —— tyche 已暴露 7 个 C 端工具,字段质量好;agent 通过 `internal/tyche` 轻量 RPC client 接入,把每个工具包装成 eino InvokableTool
- **Session**:Redis(P4 上)
- **检索**:BM25(P3 起,`Retriever` 接口预留向量检索)
- **服务形态**:CLI(P1-P3 调试)→ HTTP+SSE(P4+ 生产)

## 项目结构

```
.
├── cmd/
│   ├── cli/main.go        # P1 起步:本地调试 CLI
│   └── http/main.go       # P4:HTTP + SSE 服务
├── internal/
│   ├── agent/             # 各子 agent(基于 eino ADK)
│   ├── orchestration/     # ConversationState 唯一定义
│   ├── tools/             # eino Tool 实现(把 tyche MCP tools 包装成 InvokableTool)
│   ├── tyche/             # tyche MCP server 的轻量 JSON-RPC client
│   ├── llm/               # provider 工厂(deepseek 默认,可扩展)
│   ├── rag/               # 知识库切片 + BM25(P3)
│   ├── session/           # Redis session(P4)
│   ├── http/              # SSE handler / 鉴权中间件(P4)
│   ├── config/            # yaml + env 装载
│   ├── types/             # 跨模块共享类型
│   └── prompt/            # 系统提示词、模板
├── conf/                  # dev / pre / prod 多环境 yaml
├── knowledge/             # 计费 / 履约 / 保险 三类条款
└── docs/                  # 设计文档(见下)
```

## 设计文档索引

- [docs/technical-plan.md](docs/technical-plan.md) —— 总纲(架构、Phase 拆分、风险)
- [docs/specs/phase1-shopping-mvp.md](docs/specs/phase1-shopping-mvp.md) —— P1 导购 MVP(CLI)
- [docs/specs/phase2-price-detail-insurance.md](docs/specs/phase2-price-detail-insurance.md) —— P2 价格明细 + 保险
- [docs/specs/phase3-knowledge-supervisor.md](docs/specs/phase3-knowledge-supervisor.md) —— P3 知识库 + supervisor 多 agent
- [docs/specs/phase4-http-session-mcp.md](docs/specs/phase4-http-session-mcp.md) —— P4 HTTP 服务化 + agent-bff MCP
- [docs/specs/phase5-extensions.md](docs/specs/phase5-extensions.md) —— P5 扩展能力
- [docs/specs/phase6-productionize.md](docs/specs/phase6-productionize.md) —— P6 生产化

## 编码规范

- **包结构**:agent 业务核心放 `internal/`,公共类型放 `internal/types`
- **ConversationState 唯一定义**:只在 `internal/orchestration/state.go`,任何子包想用就 import
- **Agent 统一签名**:由 eino ADK 强制(P3 起)
- **Tool 命名**:tyche MCP 的 7 个工具沿用 tyche 命名(`rental_search_locations` / `rental_search_quotes` / ...);本地补充工具用 snake_case
- **Tool 描述写给 LLM 看**:每个字段 `jsonschema:"description=..."` 必填,中文 OK,要让 LLM 知道何时调、传什么
- **错误处理**:tool 内 panic 一律转 error,error 信息要"人话"(LLM 会读)
- **配置**:`pre/prod.yaml` 强制走 `${DEEPSEEK_API_KEY}` 等 env 占位;`dev.yaml` 允许写明文 key(本地开发用,**入库前自行确认**)
- **写操作禁忌**:**严禁**注册 `rental_create_order` / `pay` / `refund` / `modify_order` 类 tool 到 LLM 可见 ToolSet([internal/tools/common.go:isAllowedTool](internal/tools/common.go) 维护白名单)
- **import 别名**:不要给 import 加别名。**唯一合理场景**:同一文件 import 了**两个相同包名**的不同包(例如两个 `client`),此时给其中一个加别名以消歧。**当前文件所在包名与 import 包名相同不算冲突** —— Go 编译器和阅读者都能区分(`http.X` 始终指 import 的那个,本包内符号 unqualified 访问)。除此以外用原包名访问,即便路径很长。

## 安全护栏

1. **严禁幻造数据**:以下字段必须且只能来自对应工具返回值原文:
   - `location_id` ← rental_search_locations
   - `poi.latitude / longitude / city_id / name` ← rental_resolve_poi
   - `reference_id / context_id / supplier` ← rental_search_quotes
   - `order_id` ← rental_create_order(P5)
   唯一可由 LLM 推断的字段是 `date_time`(从用户自然语言换算)。如果没有调用对应工具或工具未返回,**禁止猜测、拼凑、伪造**。找不到时应重新调工具,而不是编造。
2. **关键 ID 状态保留**:调用 rental_search_quotes 后,LLM 必须在 assistant 消息里明确保存 context_id + 每条报价的 reference_id + supplier + car_name,供后续轮次"看明细"时直接使用,不得重新捏造。
3. agent 不闭环下单,通过 `build_order_deeplink` 跳转 App
4. 报价答复必须显式说明"以下单时为准"(报价有时效)
5. 保险话术 100% 基于真实工具返回,不允许 LLM 自由发挥保障范围
6. 条款问答强制走 RAG,无来源命中时引导转人工客服
7. 工具返回错误时(is_error:true),只对用户说 user_msg 字段,严禁透露 debug 字段、技术错误信息、JSON 原文
8. 违法操作 / 理赔申诉等场景由 prompt 层 + 关键词拦截转人工(P6)
