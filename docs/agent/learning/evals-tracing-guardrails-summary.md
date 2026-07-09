# Evals / Tracing / Guardrails 学习笔记：从 Demo 到生产的分水岭

参考：

- Hamel Husain, Your AI Product Needs Evals: <https://hamel.dev/blog/posts/evals/>
- OpenAI Evals Guide: <https://platform.openai.com/docs/guides/evals>
- OpenAI Agents SDK Tracing: <https://openai.github.io/openai-agents-python/tracing/>
- OpenAI Agents SDK Guardrails: <https://openai.github.io/openai-agents-python/guardrails/>

## 1. 核心结论

AI 产品能不能从 demo 变成生产系统，关键不只是 prompt，而是：

```text
能否评估质量
能否定位错误
能否快速迭代
能否防住高风险输出和动作
```

Hamel 的文章里有一个很重要的观察：很多失败的 AI 产品共同问题不是没有模型能力，而是没有 robust evaluation system。

## 2. Evals 为什么重要

没有 eval 的迭代通常是：

```text
线上一个 badcase
-> 改 prompt
-> 另一个 case 坏了
-> 再改 prompt
-> prompt 越来越长
-> 质量仍靠感觉
```

有 eval 后，迭代变成：

```text
收集 badcase
-> 分类成能力项
-> 写成测试集
-> 跑回归
-> 定位是哪一层错
-> 改 prompt/context/tool/code
-> 比较指标
```

这和传统软件测试非常像。

## 3. 三层 Eval

### 3.1 Level 1：单元测试

适合测确定性或半确定性行为：

- 意图分类是否正确。
- 工具是否选对。
- JSON 是否可解析。
- 必填字段是否存在。
- 筛选码是否白名单内。
- 安全红线是否拒绝。

这类测试应该跑得快、数量多、每次变更都能跑。

### 3.2 Level 2：Human / Model Eval

适合评估主观质量：

- 话术是否自然。
- 推荐理由是否贴合用户。
- 规则解释是否完整。
- 是否有幻觉。
- 是否应该追问。

可以人评，也可以用 LLM-as-judge，但评估标准必须清晰。

### 3.3 Level 3：A/B Testing

适合上线后看真实业务指标：

- 点击率。
- 搜车转化。
- 下单转化。
- 反问跳过率。
- 空结果率。
- 用户继续追问率。
- 安全拦截率。

注意：A/B 是最后一层，不应替代离线 eval。

## 4. Tracing 要记录什么

OpenAI Agents SDK 的 tracing 思路值得借鉴：一次 workflow run 应该有完整 trace，里面包含 LLM generation、tool call、handoff、guardrail、自定义事件等。

业务 Agent 建议至少记录：

- trace_id / session_id。
- 用户输入。
- prompt version。
- context version。
- 实际注入模型的上下文摘要。
- 模型输出。
- tool name。
- tool arguments。
- tool result 摘要。
- guardrail 结果。
- 最终 SSE/card/clarification/done。
- 延迟和错误。

这样 badcase 才能复现。

## 5. Guardrails 不是只有安全审核

Guardrail 可以放在多个点：

- 输入前：用户是否越界、安全、恶意、无关。
- 模型输出后：是否有敏感内容、幻觉、格式错误。
- 工具执行前：参数是否合法、权限是否允许。
- 工具执行后：结果是否可展示。
- 最终渲染前：是否符合前端协议。

业务 Agent 特别需要 tool guardrail。因为模型“选择工具”不等于可以执行工具。

## 6. 业务 Agent 常见坑

### 6.1 只看线上指标，不建离线回归

线上指标能告诉你“变差了”，但不能告诉你为什么变差。

### 6.2 只存最终回答，不存中间过程

Agent 的错误经常发生在中间：

- context 缺字段。
- 工具选错。
- 参数错。
- 工具结果误读。
- 后处理兜底错。

只存最终输出无法定位。

### 6.3 LLM-as-judge 标准模糊

如果评估器没有 rubric，它也会漂。

### 6.4 Guardrail 只做输入审核

真正危险的往往是工具执行前后。尤其订单、支付、退款、发消息这类动作。

## 7. 和 AI 导购的映射

当前 AI 导购已经有一些基础：

- Stage 日志。
- `AgentTrace`。
- 工具快照。
- SSE writer。
- 异步审核。
- 多个单元测试。

后续可继续加强：

- 按 Stage 建 eval：
  - Decide 工具选择。
  - need_delta 抽取。
  - FilterCode 选码。
  - Clarification 是否该问。
  - Search 空结果放宽。
  - Guide 推荐解释质量。
  - SSE 事件顺序。

- 建 badcase replay：
  - 固定 session/context。
  - 固定菜单 schema。
  - 固定模型输出 mock。
  - 重放完整链路。

- 建 guardrail matrix：
  - 输入安全。
  - 库存事实。
  - 筛选码白名单。
  - 规则口径。
  - 前端协议。
  - 高风险动作审批。

## 8. 最小落地建议

先不用一步到位搭复杂平台，可以从这些开始：

1. 建 `agent_eval_cases`，按意图和 badcase 分类。
2. 每个 case 保存用户输入、session state、期望工具、期望行为。
3. 给每个 Stage 加稳定 trace 字段。
4. 每次 prompt/context/tool schema 变更跑回归。
5. 每周从线上抽 badcase 进入回归集。

