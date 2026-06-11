# Phase 6: 生产化

> **目标:** 让 agent 真正能"上线接量"。监控告警、降级限流、风险词拦截、对话评估、A/B 实验,可选拆独立进程(套 trpc-a2a-go)。

---

## 1. Context

P1-P5 是"能用"。P6 是"敢用":
- 模型抽风 / API 限流 / Redis 闪断时不能崩
- 违法话术 / 投诉风险话术能拦截
- 上线前能用一套评估集打分
- 上线后能 A/B 调 prompt / 切 model 比效果

---

## 2. 验收标准

### 2.1 可观测性
- [ ] 每次对话埋点:trace_id / session_id / agent / tool_calls / latency / token_in/out / cost / model
- [ ] 监控面板:QPS / 首字节时延 / 完成时延 / tool 调用成功率 / LLM API 错误率 / token 成本日累计
- [ ] 关键报警:
  - LLM API 5xx > 5% / 持续 2 分钟
  - Redis 超时 > 1% / 持续 1 分钟
  - tool 调用错误率 > 10% / 持续 5 分钟
  - 单 user 调用 > 阈值(疑似刷量)

### 2.2 降级
- [ ] 主 LLM provider 故障 → 自动切备用 provider(配置 `fallback_providers`)
- [ ] Redis 故障 → session 退化为内存(单 pod 短期可用,损失多 pod 续聊)
- [ ] 后端 inner / MCP 故障 → tool 返回"系统繁忙,可稍后再问 / 直接联系客服"
- [ ] 知识库 RAG 故障 → KnowledgeAgent 答"建议联系客服"

### 2.3 风险词拦截 + 转人工
- [ ] 用户消息含违法关键词(改里程 / 伪造证件 / ...) → 拒答 + 转人工提示
- [ ] 用户消息含理赔申诉 / 投诉关键词 → 转人工(给出客服入口)
- [ ] agent 答复**输出过滤**:含"100% / 一定 / 保证"等绝对化用词时,改写为"通常 / 一般"

### 2.4 评估集
- [ ] 沉淀 100 条评估 case(覆盖 4 个核心能力 + 4 类扩展)
- [ ] 每 case 标注:期望调用的 tool / 期望含的关键词 / 期望含的来源引用
- [ ] 自动评估脚本:`go run ./cmd/eval -dataset eval/cases.jsonl`
- [ ] CI 每次合并前跑评估,通过率 ≥ 90% 才能合并

### 2.5 A/B
- [ ] 配置层支持按 user_id hash 切流(同 user 永远进同 group)
- [ ] 实验维度:system prompt / model / toolset 子集 / supervisor 路由策略
- [ ] 上线后能从监控看到分组的 转化 / 满意度 / 平均时延 对比

---

## 3. 分步实现

### Step 1 — 埋点与监控 `internal/observability`
- `internal/observability/trace.go`:把 eino callback(`OnStart / OnEnd / OnError / OnStream*`)接入公司 trace 系统
- `internal/observability/metrics.go`:Prometheus / 公司 metric
- 关键指标:
  - `agent_chat_total{agent,status}`
  - `agent_chat_latency_seconds{agent,p="0.5|0.9|0.99"}`
  - `tool_call_total{tool,status}`
  - `llm_token_total{provider,model,type=in|out}`
  - `llm_cost_yuan_total{provider,model}`

### Step 2 — 降级 `internal/llm/fallback.go`
- 包一个 `FallbackProvider`,wrap 主 + 多个 fallback
- 主调用失败(5xx / timeout / quota)→ 自动切下一个
- 配置:
  ```yaml
  llm:
    default: deepseek_chat
    fallback_chain:
      - deepseek_chat
      - claude_sonnet
      - qwen_max
  ```

### Step 3 — 风险词拦截 `internal/safety`
- `internal/safety/input_filter.go`:用户输入预检(关键词 + 简单 LLM 分类)
- `internal/safety/output_filter.go`:agent 输出后处理(绝对化用词改写、来源校验)
- `internal/safety/handoff.go`:转人工时返回的固定话术 + 客服入口

### Step 4 — 评估集 `eval/`
```
eval/
  cases.jsonl           # 100 条 case
  judge_prompt.md       # 评分 prompt(自动评估用)
  baseline.json         # 上一次 release 的得分快照
```
case 格式:
```jsonl
{"id":"shop-001","user":"周六上午 9 点北京机场租 SUV","expect_tools":["list_quotes"],"expect_contains":["SUV","推荐"],"expect_source":false}
{"id":"know-005","user":"超时怎么算钱","expect_tools":["search_knowledge"],"expect_contains":["超时"],"expect_source":true}
```

`cmd/eval/main.go`:
- 跑 case
- 用 LLM 当 judge 打分(命中 tool / 含期望关键词 / 来源引用率)
- 输出 markdown 报告 + 与 baseline 对比

### Step 5 — A/B 框架 `internal/experiment`
- 简单实现:配置 `experiments: [{key, traffic, variant_a, variant_b}]`
- `experiment.Pick(userID, key)` 按 user_id hash 分流
- agent 构造时根据 variant 切 prompt / model / toolset

### Step 6 — 可选:拆独立进程(套 trpc-a2a-go)
- 若到 P6 仍单进程跑得动 → 不拆,继续保持
- 若要拆:
  - 每个子 agent 起独立服务
  - 接口按 A2A skill 暴露([trpc-a2a-go](https://github.com/trpc-group/trpc-a2a-go))
  - Supervisor 通过 A2A client 调用
  - **代价:** 跨进程 state 同步 / 链路追踪 / 部署 → 仅当真有独立扩容需求时做

---

## 4. 文件清单(P6 增量)

```
internal/observability/trace.go              # 新增
internal/observability/metrics.go            # 新增
internal/llm/fallback.go                     # 新增
internal/safety/input_filter.go              # 新增
internal/safety/output_filter.go             # 新增
internal/safety/handoff.go                   # 新增
internal/experiment/experiment.go            # 新增
cmd/eval/main.go                             # 新增
eval/cases.jsonl                             # 新增
eval/judge_prompt.md                         # 新增
eval/baseline.json                           # 新增
.github/workflows/eval.yml (or 公司 CI)      # 新增
docs/specs/phase6-productionize.md           # 本文档
```

---

## 5. 关键决策

| # | 决策 | 理由 |
|---|---|---|
| P6-D1 | LLM fallback chain 而非主备双跑 | 双跑成本高;fallback 链能覆盖大部分 case |
| P6-D2 | 风险词用关键词 + LLM 分类双层 | 单层关键词漏召,单层 LLM 慢且贵 |
| P6-D3 | 评估集用 LLM judge 自动打分 | 人工评估不可持续;judge prompt 经过校准 |
| P6-D4 | A/B 自研而非接公司平台 | 简单 case;减少外部依赖 |
| P6-D5 | 默认不拆独立进程 | 单进程足够;拆分成本 > 收益 |

---

## 6. 已识别 TODO(P6 内必清)

- [ ] 关键词词表来源(从历史投诉 / 法务规则提)
- [ ] LLM judge 与人工标注的一致性校准(至少 50 条 case 双标)
- [ ] 公司监控平台对接方式(Prometheus / 自研 metric)
- [ ] 限流策略与公司 gateway 协调
- [ ] 上线门控:首次上线前必须跑通"100 case 通过率 ≥ 90%"
