# 在线租车系统智能 Agent (Car Rental Agent)

## 项目概述

构建一个**多 Agent 协作**的在线租车智能客服系统，当前聚焦四大能力域：车辆推荐、保险推荐、合约规则解读、订单费用解读。架构预留交易类能力（下单、支付、续租）的扩展口子，后续可平滑接入。

技术栈：Go 1.22+ / eino (CloudWeGo) / eino-ext / DeepSeek / Viper / Zap

## 核心架构

- **Orchestrator Agent**：意图识别 + Agent 路由 + 上下文管理
- **4 个 Skill Agent**：Vehicle / Insurance / Billing / Fulfillment，各自作为 Orchestrator 的 Tool
- **Tool Layer**：tyche MCP（HTTP Client 封装为 eino Tool）+ 知识库
- **LLM Provider**：eino-ext ChatModel（DeepSeek，OpenAI 兼容 API）

详细架构见 [docs/technical-overview.md](docs/technical-overview.md)

## 项目结构

```
agent/
├── cmd/cli/                # CLI 终端入口（当前唯一入口）
├── internal/
│   ├── agent/              # Orchestrator Agent
│   ├── skill/              # Skill Agent（插件化注册）
│   │   ├── interface.go    # SkillAgent 接口
│   │   ├── registry.go     # 统一注册表
│   │   ├── vehicle/
│   │   ├── insurance/
│   │   ├── billing/
│   │   └── fulfillment/
│   ├── tool/               # Tool 注册 + ToolProvider 接口 + MCP 封装
│   │   ├── registry.go     # Tool 统一注册表 + ToolProvider
│   │   └── mcp/
│   ├── llm/                # LLM Provider
│   ├── session/            # 会话管理
│   └── config/             # 配置
├── configs/                # 配置文件
├── knowledge/              # 静态知识库（JSON/Markdown）
├── docs/                   # 设计文档
│   ├── technical-overview.md
│   └── phases/             # 分阶段详细计划
└── CLAUDE.md
```

## ⚠️ 关键约束（必须遵守）

### 1. 数据来源约束 — 禁止幻觉

**所有返回给用户的数据必须来自 MCP Tool 调用或知识库检索，严禁使用 LLM 训练数据编造业务信息。**

- 车型、报价、库存 → 必须通过 MCP Tool（`rental_search_quotes` 等）查询
- 保险产品、保障范围、免赔额 → 必须来自 `knowledge/insurance/` 知识库
- 订单费用、退款金额 → 必须通过 MCP Tool（`rental_get_order_details` 等）查询
- 履约规则（取还车、违章、续租） → 必须来自 `knowledge/fulfillment/` 知识库
- 若 Tool 返回空或异常 → Agent 必须如实告知"暂时无法获取"，不允许猜测或编造
- 每个 Agent 的 System Prompt 必须包含此约束

### 2. MCP 兼容性约束

- 当前 tyche MCP Server 使用 HTTP JSON-RPC 2.0，eino-ext MCP Client 不兼容
- Phase 1-5 使用自研 HTTP Client 封装为 eino Tool
- Phase 6 迁移到 eino-ext MCP Client（待兼容性验证）
- 不要在 Phase 1-5 尝试使用 `eino-ext/components/tool/mcp`

### 3. 入口约束

- 当前仅实现 `cmd/cli` 下的命令行终端交互
- HTTP/gRPC 入口暂不实现
- CLI 交互格式：`🚗 你: <输入>` / `🤖 助手: <回复>`

### 4. 代码规范

- Go 标准项目布局和命名规范
- 接口优先设计：所有外部依赖通过接口抽象
- 错误处理：`fmt.Errorf("xxx: %w", err)` 包装，保留错误链
- 日志：结构化日志（Zap），包含 session_id / agent_name
- 配置外置：环境相关配置通过环境变量或 config.yaml 注入
- **import 别名**：禁止使用 import 别名，除非遇到同名包冲突。包命名时应避免与其他包重名，从源头消除别名需求
- **编译产物清理**：`go build` 产生的二进制文件（如 `cli`）不得提交到仓库，编译验证后须删除；`.gitignore` 已忽略 `/cli`

### 5. 知识库管理

- 知识库文件放在 `knowledge/` 目录下，按能力域分子目录
- 格式仅限 JSON 和 Markdown
- 知识库内容需要与业务团队确认后方可使用
- 禁止在知识库中写入测试/临时数据

### 6. 可扩展性约束

- 新增 Skill Agent 必须实现 `SkillAgent` 接口（`internal/skill/interface.go`），然后在 `registry.go` 中注册
- 新增 Tool 来源必须实现 `ToolProvider` 接口（`internal/tool/registry.go`），然后调用 `RegisterProvider`
- Orchestrator 路由 Prompt 从 SkillRegistry 动态生成，禁止硬编码 Agent 列表
- 交易类 Tool（下单、支付等）必须内置二次确认机制，防止 LLM 误触发不可逆操作

## 分阶段计划

| Phase | 文档 | 核心交付 |
|-------|------|---------|
| 1 | [phase-1-foundation.md](docs/phases/phase-1-foundation.md) | 项目骨架、eino 集成、MCP Client、CLI 入口 |
| 2 | [phase-2-vehicle-skill.md](docs/phases/phase-2-vehicle-skill.md) | VehicleAgent + 车辆搜索/报价 Tool |
| 3 | [phase-3-insurance-skill.md](docs/phases/phase-3-insurance-skill.md) | InsuranceAgent + 保险知识库 |
| 4 | [phase-4-billing-skill.md](docs/phases/phase-4-billing-skill.md) | BillingAgent + 费用解读 Tool |
| 5 | [phase-5-fulfillment-skill.md](docs/phases/phase-5-fulfillment-skill.md) | FulfillmentAgent + 履约规则知识库 |
| 6 | [phase-6-eino-mcp-migration.md](docs/phases/phase-6-eino-mcp-migration.md) | eino MCP 迁移 + RAG 增强 |

每个 Phase 有独立的详细计划文件，可分步完成。按 Phase 顺序执行。

## tyche MCP 工具（现有可直接对接）

| 工具名 | 功能 | 对应 Agent |
|--------|------|-----------|
| `rental_search_locations` | 搜索取还车地点 | Vehicle |
| `rental_resolve_poi` | 解析地点到 POI | Vehicle |
| `rental_search_quotes` | 搜索车型报价 | Vehicle |
| `rental_get_order_details` | 订单详情及退改政策 | Billing / Fulfillment |
| `rental_create_order` | 创建订单 | 预留给 Order Agent（后续扩展） |
| `rental_get_reservation` | 查询订单状态 | Billing / Fulfillment |
| `rental_get_driver_list` | 驾驶员列表 | （辅助） |

MCP 端点：`POST /car/rental/inner/mcp`，需 Bearer token + 白名单手机号认证。

## 开发启动

```bash
# Phase 1 第一步
go mod init github.com/zxq97/agent
go get github.com/cloudwego/eino
go get github.com/cloudwego/eino-ext/components/model/openai  # DeepSeek 通过此组件接入
go get github.com/spf13/viper
go get go.uber.org/zap
```

## LLM 配置说明

| 配置项 | 值 | 说明 |
|--------|-----|------|
| `llm.provider` | `deepseek` | 标识当前使用的 LLM |
| `llm.api_key` | 环境变量 `DEEPSEEK_API_KEY` | DeepSeek API Key |
| `llm.base_url` | `https://api.deepseek.com` | DeepSeek API 地址 |
| `llm.model` | `deepseek-chat` | V3 模型，支持 function calling |
| ⚠️ 不可用 | `deepseek-reasoner` | R1 模型，**不支持** function calling |
