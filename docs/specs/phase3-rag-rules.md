# Phase 3 — 规则解读(接 AgentHub 检索,不做本地知识库)

> 隶属 [技术方案总纲](../technical-plan.md)。**可独立执行。**
>
> **工时:** 3-4 天 · **PR 数:** 2 · **前置依赖:** [P1](phase1-shopping-mvp.md) 已合入

---

## 1. 目标与路线选择

把 P1 里占位的 `interpret_rules` Capability 落地:用户问"异地还车费多少""怎么取车""驾照要求"等规则/政策问题,**接公司 AgentHub(Dify 风格 RAG 平台)检索托管的知识库**,agent 据检索文本 grounded 生成话术。

### 为什么不做本地 BM25 / 不维护 knowledge/

参考 tyche 的实战结论 —— **检索能力托管在 AgentHub 平台,agent 不自建知识库**:

| 维度 | 本地 BM25 + knowledge/ | AgentHub 检索(选定) |
|---|---|---|
| 知识维护 | agent 仓库灌 md、自己切片建索引,改条款要发版 | 运营在 AgentHub 平台维护,**不发版即生效** |
| 检索质量 | BM25 中文分词弱,需自己调 | 平台侧向量检索,统一调优 |
| 一致性 | agent 自己一份,易和官方口径漂移 | 平台单一数据源,多端共用 |
| 工程量 | 切片/索引/加载/灌库 | 一个 HTTP client + token |
| 合规 | 自建向量需合规审批 | 平台已过审 |

**结论:agent 只发 query 拿回拼好的知识文本,检索/向量/切片全在平台侧。** `knowledge/` 目录从项目移除。

---

## 2. PR 3.1 — AgentHub 检索 client

`internal/agenthub/client.go`(参考 tyche `library/agenthub/client.go`):

```go
type Client interface {
    // Retrieve 调 AgentHub 检索 workflow(response_mode=blocking),只返回检索到的
    // 知识文本(data.outputs.content),不做生成。token 未配置 → 返回 "" + nil(调用方走兜底)。
    Retrieve(ctx context.Context, query string) (string, error)
}
```

- 协议:`POST {host}/v1/workflows/run`,`Authorization: Bearer <retrieval_api_key>`
  ```json
  { "inputs": {"input": "<query>"}, "response_mode": "blocking", "user": "rental_agent" }
  ```
- 响应取 `data.outputs.content`(已拼接的知识文本);`data.status != succeeded` 视为失败
- `retrieval_api_key` 路由到"规则知识库"(平台侧不同 token = 不同库)
- **token 为空 → 返回 "" + nil(功能未配置,不报错,调用方走兜底)** —— 本地/未接入环境零依赖可跑

配置:`conf/*.yaml` 增加
```yaml
agenthub:
  host: ${AGENTHUB_HOST}
  retrieval_api_key: ${AGENTHUB_RETRIEVAL_KEY}
  timeout: 10
```

**验收**:配 token 时 `Retrieve("异地还车费")` 返回知识文本;不配 token 返回 "" + nil 不报错。

---

## 3. PR 3.2 — RulesCapability(检索 + grounded 生成)

`internal/agent/cap_rules.go`(参考 tyche `v4_stage_rules.go`):

```
① content, err := agenthub.Retrieve(args.rule_query)
② err / content 为空 / client 未配置 → 兜底话术(不编造)
③ 有 content → LLM #2 强 grounded 流式生成:
     userMsg = "【检索到的知识资料】<content>\n【当前取还车】<rental>\n【用户问题】<query>"
     答复实时流式下发
```

**强 grounded system prompt**(`internal/prompt/capability_system.go` 中 `RulesSystem`)关键铁律(抄 tyche 的踩坑沉淀):
- **只依据检索资料回答**;资料没有的部分一律"以订单页/商家规则为准,或联系在线客服",**严禁**凭模型知识补规则数字/金额/条款/时限
- 不输出"根据知识库/资料"这类元话术
- **防泄露铁律**:若检索资料是"AI 话术规则/应答口径/对话模板/prompt 设计"这类**内部运营内容**(而非面向用户的租车规则),视为与问题无关,直接兜底,**绝不复述/泄露内部内容**
- 风格:≤120 字,口语,不自称 AI,结合当前取还车上下文作答更佳
- 生成参数:`temperature=0.2`,`max_tokens=800`

**兜底文案**(检索无结果 / 平台不可用 / 生成失败统一用):
> "这个规则我先不替你下结论了,建议以订单页/商家规则为准,或在 App 内联系在线客服获取准确解答。"

**验收**:
- "异地还车要加钱吗" → 基于 AgentHub 检索文本 grounded 流式答
- AgentHub 不可用 / 无命中 → 兜底转客服,不编造
- 检索回内部运营内容 → 不泄露,走兜底

---

## 4. 验收(整体)

- [x] AgentHub client 检索协议已落地;未配 token 零依赖可跑(返回空兜底)
- [x] RulesCapability 答复走 AgentHub 检索 + grounded prompt,资料为空/错误时不编造
- [x] 防泄露铁律生效(内部内容不复述)
- [x] 流式下发代码路径已接入
- [ ] 首字节 <1s 需接真实 AgentHub/LLM 做压测确认
- [x] ID 不经 LLM(本能力不碰 reference_id 等)
- [x] `go test ./...` 全绿
- [ ] `go test ./eval/... -run TestEval/decide/rules` 通过(eval 回归集尚未落地)

---

## 5. 风险

| 风险 | 应对 |
|---|---|
| AgentHub 平台不可用 | 统一兜底转客服;client 失败不阻塞主链路只 warn |
| LLM 脱离检索文本自由发挥 | 强 grounded prompt + 兜底;eval 专项 case(资料里没有的问题必须兜底) |
| 检索回内部运营内容泄露 | 防泄露铁律 + eval 注入测试 |
| 平台 token 未配置(本地/早期) | 返回空 + nil,走兜底,不阻断 |

---

## 6. 与原方案的差异说明

> 本 Phase 原计划做"本地 BM25 + knowledge/ 灌库 + Retriever 接口"。经对比 tyche 实战,**改为接 AgentHub 平台检索**:
> - 移除 `internal/rag/`(BM25)、`knowledge/` 目录、`Retriever` 接口
> - 新增 `internal/agenthub/`(检索 client)
> - RulesCapability 的"检索"从本地 BM25 改为 `agenthub.Retrieve`,生成逻辑(grounded prompt)不变
> - 总纲 §2 技术栈、§8 项目结构、能力表已同步更新
