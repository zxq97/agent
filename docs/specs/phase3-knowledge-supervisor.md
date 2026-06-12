# Phase 3: Supervisor 多 Agent

> **目标:** 单 ReAct agent → 拆分为 supervisor + 多子 agent (`Shopping / Insurance`)。
> **范围调整:** 知识库 / RAG / `search_knowledge` tool 暂不实现(留到后续 phase)。

---

## 1. Context

P1+P2 单 agent 一手包办挑车、报价、价格明细、保险。问题是:
- prompt 越写越长(导购规则 + 保险解读 + 红线一锅炖),不同主题相互干扰
- 工具描述全暴露给一个 LLM,token 浪费,误调风险
- 后续扩展(售后、比价、知识库等)继续往里塞,prompt 会失控

P3 引入 eino ADK 的 supervisor 模式,把职责切开:
- **Supervisor**:只做意图分派,不调业务工具
- **ShoppingAgent**:挑车 / 报价 / 价格明细 / 订单查询
- **InsuranceAgent**:保险解读 / 按驾龄推荐

知识库延后是因为:tyche MCP 已经能覆盖 90% 用户问题的数据需求,条款类问答优先级较低,且接入向量检索/中文分词的成本远高于多 agent 拆分本身。

---

## 2. 验收标准

### 2.1 必过 demo
```
你: 明天下午6点首都机场取车,两天后同地点还
[supervisor 转 ShoppingAgent]
[ShoppingAgent: search_locations → resolve_poi → search_quotes]
小租: 给您找了 3 款...(以下单时为准)

你: 第一个 这个车 我驾龄3年要不要加保险?
[supervisor 转 InsuranceAgent]
[InsuranceAgent: 从 history 找 reference_id → get_order_details 拿 guarantee_list]
小租: 您驾龄 3 年,推荐优享保障 ¥38/天:...(保障范围以保险合同条款为准)

你: 帮我重新搜一下北京南站的
[supervisor 转 ShoppingAgent]
小租: ...
```

### 2.2 检查清单
- [x] 三类 prompt 拆开,各自聚焦
- [x] Supervisor 不绑定业务工具,只通过 transfer_to_agent 派发
- [x] ShoppingAgent 工具子集:search_locations / resolve_poi / search_quotes / get_order_details / get_reservation
- [x] InsuranceAgent 工具子集:get_order_details(复用,因为 guarantee_list 在它的返回里)
- [x] 跨 agent 切换时完整 history 透传(由 ADK supervisor 自动管理 transfer 消息)
- [x] `go build / vet / test` 全过

---

## 3. 分步实现(已完成)

### Step 1 — Prompt 拆分
**文件:**
- [internal/prompt/shopping_system.go](../../internal/prompt/shopping_system.go) (保留,删除保险段)
- [internal/prompt/insurance_system.go](../../internal/prompt/insurance_system.go) (新增)
- [internal/prompt/supervisor_system.go](../../internal/prompt/supervisor_system.go) (新增)

各自的关注点:
- Shopping:6 个工具的调用顺序、严禁幻造数据、时间格式、价格明细解读
- Insurance:guarantee_list 解读、驾龄推荐逻辑、合规红线
- Supervisor:路由判断、不直接答用户、不调业务工具

### Step 2 — ADK 多 agent 组装
**文件:** [internal/agent/adk.go](../../internal/agent/adk.go) (新增)

```go
func NewSupervisorSystem(ctx, d SystemDeps) (*adk.Runner, error) {
    shopping  := buildShoppingAgent(ctx, d)   // ChatModelAgent + 5 工具
    insurance := buildInsuranceAgent(ctx, d)  // ChatModelAgent + 1 工具
    supervisor := buildSupervisorAgent(ctx, d) // ChatModelAgent + 无工具

    multi, _ := supervisor.New(ctx, &supervisor.Config{
        Supervisor: supervisor,
        SubAgents:  []adk.Agent{shopping, insurance},
    })
    return adk.NewRunner(ctx, adk.RunnerConfig{
        Agent:           multi,
        EnableStreaming: true,
    })
}
```

关键点:
- `supervisor.New` 自动给 supervisor 和每个子 agent 注入 `transfer_to_agent` 工具
- 子 agent 答完会自动 transfer 回 supervisor
- 工具子集通过 `filterToolsByName` 从 `tools.All` 的全集筛选

### Step 3 — LLM Factory 加日志钩子
**文件:** [internal/llm/provider.go](../../internal/llm/provider.go)

加 `SetLogger(w io.Writer)`:让 Factory 在每次 `Get` 返回 model 时自动套 `LoggingChatModel`。
解决"多个 agent 各自取一份 model 都要手动套日志"的繁琐。

### Step 4 — CLI 接 adk.Runner
**文件:** [cmd/cli/main.go](../../cmd/cli/main.go) (重写主流程)

```go
runner, _ := agent.NewSupervisorSystem(ctx, agent.SystemDeps{
    ChatModelFactory: factory,    // 已 SetLogger(logW)
    AllTools:         allTools,
    MaxIterations:    cfg.Agent.MaxStep,
    AssistantName:    *assistant,
    DriverAge:        *driverAge,
})

// 每轮:
iter := runner.Run(ctx, history)
for event := iter.Next(); ok; event = iter.Next() {
    // 收集所有 message 进 history(含 supervisor 的 transfer 消息)
    // 把纯文字 assistant 消息实时打到 stdout
}
```

History 传递:每轮把完整 `history` 喂给 `runner.Run`;runner 输出的所有 AgentEvent 转换的消息一并追加到 history。这样下一轮:
- LLM 看到上次 supervisor transfer 给了谁
- LLM 看到上次子 agent 的工具调用和返回值(真实 reference_id 等)
- 不会出现幻造数据的问题

---

## 4. 文件清单(P3 已完成)

```
internal/prompt/shopping_system.go     # 修改(删保险段)
internal/prompt/insurance_system.go    # 新增
internal/prompt/supervisor_system.go   # 新增
internal/agent/adk.go                  # 新增(ADK 多 agent 组装)
internal/agent/shopping.go             # 保留(P1 单 agent 走法,仍可用但 CLI 已不调)
internal/llm/provider.go               # 加 SetLogger
cmd/cli/main.go                        # 重写主流程接 adk.Runner
docs/specs/phase3-knowledge-supervisor.md  # 本文档
```

---

## 5. 关键决策

| # | 决策 | 理由 |
|---|---|---|
| P3-D1 | 用 eino ADK supervisor 模式 | 框架内置,transfer 机制自动处理;比手写路由稳 |
| P3-D2 | 子 agent 各持工具子集 | 缩小 LLM 决策空间,避免 InsuranceAgent 误调 search_quotes |
| P3-D3 | InsuranceAgent 复用 get_order_details | guarantee_list 数据就在这个工具返回里,无需新增 tool |
| P3-D4 | Supervisor 不绑定业务工具 | 强制其只做调度,避免"自己回答而不 transfer" |
| P3-D5 | LLM Factory 自动套日志 | 多 agent 不必每个手动包装 |
| P3-D6 | 保留 internal/agent/shopping.go (P1 写法) | 单 agent 调试时仍可用;新功能走 adk.go |
| P3-D7 | 知识库 / RAG / search_knowledge **延后** | tyche MCP 已覆盖主要数据需求;条款类问答优先级较低 |

---

## 6. 已识别 TODO(后续 phase 处理)

- [ ] 知识库:租车计费 / 履约 / 保险条款的 RAG 检索(独立 phase)
- [ ] 售后 agent:改派 / 换车 / 延期 / 提前还车规则解读
- [ ] 比价 agent:用户贴竞品报价时的对比解读
- [ ] supervisor 输出聚合策略优化:目前每个子 agent 完成会有一次 supervisor 收尾回话,可能导致输出冗余
