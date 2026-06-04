# 在线租车系统智能体 (Car Rental Agent)

## 项目概述

本项目的核心目标是构建一个**在线租车系统的智能客服 Agent**，作为用户与租车平台之间的智能交互桥梁。Agent 不负责交易流程本身（下单、支付、取还车等由现有业务系统完成），而是聚焦于**信息推荐、规则解读、费用答疑和履约支持**四大场景，帮助用户做出更好的决策并解决使用过程中的疑问。

## 核心能力域

### 1. 车辆推荐与介绍 (Vehicle Recommendation)
- 根据用户出行场景（商务、家庭、自驾游等）、人数、预算等条件推荐合适的车型
- 介绍车辆参数（座位数、排量、油电类型、行李空间等）和平台特色标签
- 对比不同车型的优劣势，帮助用户决策

### 2. 保险推荐与介绍 (Insurance Recommendation)
- 在用户选中车辆报价后，推荐适配的保险方案（基础险、尊享险、补充险等）
- 解释各保险产品的保障范围、免赔额、理赔流程
- 根据用户风险偏好和行程特征给出保险建议

### 3. 订单费用解读与答疑 (Billing Explanation)
- 解读订单中各项费用明细：租金、手续费、保险费、押金、增值服务费等
- 解释退款规则和退款明细：提前还车退款、违约金、保险退保等
- 回答费用相关的常见问题，消除用户对账单的疑惑

### 4. 履约规则解答与支持 (Fulfillment Support)
- 解释取还车规则（时间、地点、证件要求、验车流程）
- 解答违章处理规则和流程
- 解释续租、换车、异地还车等操作的规则和费用
- 处理事故、故障等紧急情况的指引

## 技术选型

| 领域 | 选择 | 理由 |
|------|------|------|
| 语言 | Go 1.22+ | 高性能、强类型、并发友好、适合后端服务 |
| 框架 | **不使用 Web 框架** | Go 标准库 `net/http` 足够；避免框架耦合，保持灵活 |
| LLM SDK | anthropic-sdk-go | 官方 SDK，类型安全，流式支持完善 |
| 对话管理 | 自研状态机 | 轻量可控，避免引入重型对话引擎 |
| 向量检索 | Milvus / pgvector | 知识库 RAG 检索（Phase 5 引入） |
| 配置管理 | Viper | Go 生态标准配置库 |
| 日志 | Zap | 结构化日志，高性能 |
| API 风格 | RESTful + SSE | 普通请求走 REST，流式响应走 SSE |
| 通信协议 | HTTP（外部）/ gRPC（内部） | 外部对接简单，内部服务间高效 |

## 架构设计思路

### 核心原则
1. **Skill-Based Architecture** — 每个能力域是一个独立的 Skill，Agent 负责意图识别和 Skill 路由
2. **Tool-Calling Pattern** — 每个 Skill 挂载一组 Tool，LLM 通过 function calling 调用业务数据
3. **知识分层** — 静态知识（规则、FAQ）走 RAG，动态数据（车型、订单）走 Tool 调用业务 API
4. **对话状态追踪** — 多轮对话中维护上下文状态，支持意图切换和话题回归
5. **渐进式构建** — 分 6 个 Phase 逐步交付，每个 Phase 可独立验证

### 架构分层

```
┌─────────────────────────────────────────────────┐
│                   API Gateway                    │  ← HTTP/gRPC 入口
├─────────────────────────────────────────────────┤
│               Agent Orchestrator                 │  ← 对话编排、意图路由
│  ┌──────────┐ ┌──────────┐ ┌──────────────────┐ │
│  │ Dialogue  │ │  Intent   │ │   Skill Router   │ │
│  │ Manager   │ │ Detector  │ │                  │ │
│  └──────────┘ └──────────┘ └──────────────────┘ │
├─────────────────────────────────────────────────┤
│                  Skill Layer                     │  ← 四大能力域
│  ┌──────────┐┌───────────┐┌─────────┐┌────────┐ │
│  │ Vehicle  ││ Insurance ││ Billing ││Fulfill-│ │
│  │ Rec.     ││ Rec.      ││ Expl.   ││ ment   │ │
│  └──────────┘└───────────┘└─────────┘└────────┘ │
├─────────────────────────────────────────────────┤
│                  Tool Layer                      │  ← 业务系统对接
│  ┌────────┐┌─────────┐┌────────┐┌─────────────┐ │
│  │Vehicle ││Insurance││ Order  ││ Fulfillment │ │
│  │  API   ││  API    ││  API   ││    API      │ │
│  └────────┘└─────────┘└────────┘└─────────────┘ │
├─────────────────────────────────────────────────┤
│              Knowledge Layer (RAG)               │  ← 知识检索
│  ┌──────────────┐  ┌──────────────────────────┐ │
│  │ Rule Documents│  │ FAQ & Policy Embeddings  │ │
│  └──────────────┘  └──────────────────────────┘ │
├─────────────────────────────────────────────────┤
│              LLM Provider Layer                  │  ← 大模型调用
│         Claude API / Other LLM Providers         │
└─────────────────────────────────────────────────┘
```

## 项目结构

```
agent/
├── CLAUDE.md                    # 本文件 — 项目全貌
├── docs/
│   ├── architecture.md          # 架构设计详细文档
│   ├── data-flow.md             # 数据流设计文档
│   └── phases/                  # 分阶段设计文档
│       ├── phase-1-foundation.md
│       ├── phase-2-vehicle-skill.md
│       ├── phase-3-insurance-skill.md
│       ├── phase-4-billing-skill.md
│       ├── phase-5-fulfillment-skill.md
│       └── phase-6-rag-enhancement.md
├── cmd/
│   ├── http/                    # HTTP 服务入口
│   ├── grpc/                    # gRPC 服务入口
│   └── cli/                     # CLI 调试入口
├── internal/
│   ├── agent/                   # Agent 核心：编排、意图、路由
│   ├── skill/                   # 四大 Skill 实现
│   │   ├── vehicle/
│   │   ├── insurance/
│   │   ├── billing/
│   │   └── fulfillment/
│   ├── tool/                    # Tool 定义与实现
│   ├── dialogue/                # 对话状态管理
│   ├── knowledge/               # RAG 知识库
│   ├── llm/                     # LLM Provider 抽象
│   └── config/                  # 配置加载
├── pkg/                         # 可复用公共包
├── configs/                     # 配置文件
├── api/                         # API 定义（proto / openapi）
└── scripts/                     # 构建、部署脚本
```

## 分阶段实施计划

### Phase 1: 基础骨架 (Foundation)
- 搭建 Go 项目结构，初始化模块
- 实现 LLM Provider 抽象层（Claude API 对接）
- 实现 Agent 核心框架：对话管理、意图检测、Skill 路由
- 实现 Tool 注册与调用机制
- 搭建 HTTP 服务 + SSE 流式响应
- **交付物**：可运行的骨架，能进行基本的多轮对话

### Phase 2: 车辆推荐 Skill (Vehicle Recommendation)
- 定义车辆相关 Tool（搜索车型、获取车型详情、对比车型）
- 实现车辆推荐 Skill 的 System Prompt 和 Tool Calling 逻辑
- 对接车辆业务 API（Mock / 真实）
- **交付物**：用户可通过对话获取车辆推荐

### Phase 3: 保险推荐 Skill (Insurance Recommendation)
- 定义保险相关 Tool（查询保险方案、计算保费、获取保障详情）
- 实现保险推荐 Skill 的 System Prompt 和 Tool Calling 逻辑
- 对接保险业务 API
- **交付物**：用户选车后可获得保险推荐和讲解

### Phase 4: 费用解读 Skill (Billing Explanation)
- 定义费用相关 Tool（查询订单明细、查询退款记录、计算退款金额）
- 实现费用解读 Skill 的 System Prompt
- 对接订单/支付业务 API
- **交付物**：用户可询问订单费用明细和退款规则

### Phase 5: 履约支持 Skill (Fulfillment Support)
- 定义履约相关 Tool（查询取还车规则、违章规则、续租规则等）
- 实现履约支持 Skill 的 System Prompt
- 对接履约业务 API
- **交付物**：用户可询问取还车、违章、续租等履约问题

### Phase 6: RAG 增强 (Knowledge Enhancement)
- 接入向量数据库，构建规则/FAQ 知识库
- 实现知识检索 Tool，各 Skill 可调用 RAG 补充背景知识
- 完善意图检测的准确性
- 性能优化与生产化（限流、监控、降级）
- **交付物**：具备完整知识增强能力的生产级 Agent

## 关键设计决策

1. **不用 Web 框架**：Go 标准库 `net/http` + 轻量路由（如 chi）即可，避免框架侵入
2. **Skill 而非 Monolith**：每个能力域独立，便于开发、测试、演进，符合单一职责
3. **Tool-Calling First**：优先使用 LLM 的 function calling 获取实时数据，减少幻觉
4. **RAG 作为补充**：动态数据走 Tool，静态知识走 RAG，各司其职
5. **对话状态外部化**：对话历史和状态存储在 Redis/DB，支持水平扩展
6. **渐进式交付**：每个 Phase 都是一个可验证的里程碑，降低风险

## 开发约定

- 所有代码遵循 Go 标准项目布局和命名规范
- 接口优先设计：所有外部依赖通过接口抽象，便于 Mock 和替换
- 错误处理：使用 `fmt.Errorf("xxx: %w", err)` 包装，保留错误链
- 日志规范：结构化日志，包含 trace_id / session_id / skill_name
- 配置外置：所有环境相关配置通过环境变量或配置文件注入
- 每个 Phase 的详细设计文档在 `docs/phases/` 下，按文档执行即可
