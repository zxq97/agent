# 业务 Agent 设计借鉴指南

本文面向三类场景：

- 租车导购 Agent：订前选车、筛选、反问、规则解释、贵必赔、车型对比。
- 租车其他业务 Agent：订单、售后、取还车、理赔、客服、运营辅助。
- 其他业务 Agent：电商、酒旅、金融、客服、企业内部流程、数据分析等。

目标不是总结“Agent 应该多智能”，而是说明：业务 Agent 应该借鉴哪些工程原则、如何设计、哪些地方不能照搬，以及每条建议来自哪些学习文档的哪一块。

## 1. 总原则：业务 Agent 优先做 Workflow，不默认做自治 Agent

多数业务 Agent 的主链路应该是：

```text
用户/事件输入
-> 构造业务上下文
-> LLM 做局部决策或结构化输出
-> 代码控制 Workflow / State Machine
-> 工具调用真实业务系统
-> 校验 / 审批 / Guardrail
-> 渲染 / 落库 / Trace / Eval
```

不建议默认做：

```text
LLM 自由思考
-> 自由选择工具
-> 自由循环
-> 自由决定什么时候结束
```

原因：

- 业务动作通常有权限、状态、库存、价格、风控、审计等硬约束。
- 用户可见链路要求稳定，尤其是前端事件、卡片、done、loading。
- 出错后要能定位、回放、归责。
- 生产系统需要可控延迟和成本。

适用结论：

- 租车导购：应继续采用 V4 Pipeline 这种受控 workflow。
- 租车订单/售后：更应该采用状态机和审批点，不应让模型自由执行高风险动作。
- 其他业务 Agent：除非是调研、研发、排障等开放任务，否则先设计 workflow。

参考：

- [Anthropic《Building Effective Agents》](./learning/anthropic-building-effective-agents-summary.md) §3「Workflow 与 Agent 的边界」、§9.1「优先选择 Workflow，而不是裸 Agent Loop」
- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 8「Own Your Control Flow」、§5「这些原则如何组合成生产架构」
- [ReAct 论文学习笔记](./learning/react-paper-summary.md) §4「ReAct 不适合什么」、§5「业务 Agent 应该如何借鉴 ReAct」

## 2. 借鉴一：用 LLM 做“自然语言到结构化意图”，不要让它直接执行业务

业务 Agent 最稳的第一步，是让 LLM 把用户自然语言转成结构化意图：

```json
{
  "tool": "search_vehicles",
  "args": {
    "need_delta": [
      {"op": "ADD", "type": "seat_num", "value": "5人", "hardness": "hard"}
    ]
  }
}
```

然后由代码判断：

- 这个工具是否允许。
- 参数是否合法。
- 是否需要二次确认。
- 是否可以执行。
- 是否要转成另一个业务流程。

### 对租车导购的设计建议

- `DecideStage` 继续只产 `need_delta` / `ask` / `modify_location` / `interpret_rules` 等结构化意图。
- 不让模型直接产最终 `filter_codes`，而是交给 `FilterCodeStage` 和白名单处理。
- 不让模型凭常识判断库存，必须进入 `SearchStage` 真搜。

### 对租车其他 Agent 的设计建议

订单/售后类 Agent 可以让模型输出：

- `query_order_status`
- `explain_cancel_policy`
- `request_refund_confirmation`
- `create_service_ticket`
- `modify_pickup_time_request`

但不要让模型直接：

- 退款。
- 取消订单。
- 改订单。
- 承诺赔付。
- 发起支付。

高风险动作应该进入审批/确认/状态机。

### 对其他业务 Agent 的设计建议

电商、酒旅、金融、企业流程同理：

- 模型负责理解“用户想做什么”。
- 代码负责判断“能不能做、怎么做、是否需要确认”。

参考：

- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 1「Natural Language to Tool Calls」、§Factor 4「Tools Are Just Structured Outputs」
- [Prompt Engineering 学习笔记](./learning/prompt-engineering-summary.md) §2.3「输出结构必须强约束」
- [AI 导购设计亮点总结](./ai-guide-design-highlights.md) §2「DecideStage 只做需求理解和工具决策」、§3「FilterCodeStage 单独生成筛选码」

## 3. 借鉴二：Prompt 是软契约，硬约束必须在代码里兜住

Prompt 应该写清：

- 业务身份。
- 服务范围。
- 决策树。
- 工具边界。
- 输出格式。
- 禁止编造的事实。
- 话术风格。

但 Prompt 不能成为唯一防线。

典型分工：

| 规则 | Prompt 中说明 | 代码中兜底 |
| --- | --- | --- |
| 不编库存 | 写库存事实铁律 | 搜车结果必须来自真实库存 |
| 不输出非法筛选码 | 写工具 schema | 菜单白名单校验 |
| 不裸问用户 | 写反问铁律 | `Clarification` 协议强制结构化 |
| 不执行高风险动作 | 写服务边界 | 权限、状态机、审批、二次确认 |

### 对租车导购的设计建议

- Prompt 中继续保留库存事实铁律、反问铁律、决策树。
- 所有工具输出进入 Go 侧校验。
- Prompt 版本、tool schema 版本、context builder 版本进入 trace。

### 对租车其他 Agent 的设计建议

订单/售后 Prompt 要明确：

- 哪些可以解释。
- 哪些必须查订单。
- 哪些必须让用户确认。
- 哪些必须转人工/工单。
- 哪些不能承诺结果。

但真正执行必须看订单状态和权限。

参考：

- [Prompt Engineering 学习笔记](./learning/prompt-engineering-summary.md) §1「核心结论」、§3.1「把硬约束只写在 prompt 里」
- [Anthropic《Building Effective Agents》](./learning/anthropic-building-effective-agents-summary.md) §8「文章的生产实践原则」
- [AI 导购设计亮点总结](./ai-guide-design-highlights.md) §6「库存事实必须真搜，避免模型空口承诺」

## 4. 借鉴三：Context Engineering 比单纯改 Prompt 更重要

业务 Agent 每轮要回答三个问题：

```text
模型本轮应该知道什么？
哪些历史仍有效？
哪些信息会污染判断？
```

不要把聊天历史全塞给模型。更好的方式是构造业务状态摘要：

- 当前用户输入。
- 当前业务上下文。
- 当前有效需求。
- 上次工具结果。
- 历史摘要。
- 用户画像。
- pending action。
- 已反问次数。

### 对租车导购的设计建议

`buildStatePrefix` 的方向是对的，应继续沉淀：

- `needs`
- `last_search`
- `last_quote_summary`
- `current_rental`
- `profile`
- `clarify_count`

后续建议：

- 给 context builder 加版本。
- 每轮 trace 记录 context 摘要。
- 对“旧条件污染新需求”建立专项 eval。

### 对租车其他 Agent 的设计建议

订单 Agent 的 context 不应该只是聊天历史，应包括：

- 当前订单状态。
- 支付状态。
- 取还车时间。
- 取消/退款规则。
- 用户权益。
- 是否存在工单。
- 是否已有 pending 审批。
- 当前允许动作。

售后/理赔 Agent 应包括：

- 事故/理赔阶段。
- 上传材料状态。
- 历史沟通摘要。
- 可执行动作和不可执行动作。

参考：

- [Context Engineering 学习笔记](./learning/context-engineering-summary.md) §1「核心结论」、§2「Context 包含什么」、§4「业务 Agent 常见 Context 坑」
- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 3「Own Your Context Window」
- [AI 导购设计亮点总结](./ai-guide-design-highlights.md) §4「多轮需求状态用 need_delta 和置信度衰减维护」、§9「用户画像单独沉淀」

## 5. 借鉴四：工具接口 ACI 要像业务 API 一样认真设计

工具不是随便给模型几个函数。工具 schema 是 Agent-Computer Interface。

一个好的工具定义应该清楚说明：

- 什么时候用。
- 什么时候不用。
- 参数含义。
- 参数格式。
- 是否允许为空。
- 错误如何返回。
- 和相似工具的边界。
- 执行前是否需要确认。

### 对租车导购的设计建议

已有工具：

- `search_vehicles`
- `ask`
- `modify_location`
- `interpret_rules`
- `price_protection_entry`

建议继续保持工具小而明确，不要把“搜车 + 改城市 + 查规则 + 贵必赔”合成一个大工具。

### 对租车其他 Agent 的设计建议

订单 Agent 工具建议拆小：

- `get_order_detail`
- `explain_cancel_policy`
- `request_cancel_confirmation`
- `submit_cancel_request`
- `create_after_sales_ticket`

不要设计一个 `handle_order_issue` 大工具，让模型自由填复杂参数。

### 对其他业务 Agent 的设计建议

高风险工具要拆成两段：

```text
模型选择意图
-> 代码校验
-> 人类/用户确认
-> 执行动作
```

参考：

- [Anthropic《Building Effective Agents》](./learning/anthropic-building-effective-agents-summary.md) §7「工具接口比想象中更重要」
- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 4「Tools Are Just Structured Outputs」、§Factor 7「Contact Humans with Tool Calls」
- [Prompt Engineering 学习笔记](./learning/prompt-engineering-summary.md) §2.3「输出结构必须强约束」

## 6. 借鉴五：把“问用户”做成结构化工具，而不是文本里裸问

业务 Agent 经常需要补信息，但补信息也要可承接。

不推荐：

```text
你大概几个人坐？
```

推荐：

```json
{
  "type": "clarification",
  "slot": "seat_num",
  "question": "带爸妈孩子的话，算上你几位一起坐？",
  "options": ["1-2人", "3-4人", "5人以上"],
  "allow_skip": true
}
```

### 对租车导购的设计建议

- 继续用 `ask -> Clarification`。
- 同一槽位不要连续逼问。
- 用户跳过后应进入 search，而不是反复问。
- 反问结果进入 needs 和 history。

### 对租车其他 Agent 的设计建议

订单/售后 Agent 可以结构化问：

- “是否确认取消？”
- “是否接受该退款规则？”
- “请补充取车门店/时间。”
- “请上传材料。”

这些都应进入 pending action，而不是普通文本。

参考：

- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 6「Launch/Pause/Resume with Simple APIs」、§Factor 7「Contact Humans with Tool Calls」
- [AI 导购设计亮点总结](./ai-guide-design-highlights.md) §5「反问结构化，不做裸问」
- [Prompt Engineering 学习笔记](./learning/prompt-engineering-summary.md) §2.2「把决策树写清楚」

## 7. 借鉴六：受控 Loop，而不是裸 ReAct

ReAct 的价值是：

- 工具反馈能减少幻觉。
- 每一步 observation 能影响下一步。
- 中间状态可以帮助恢复。

但业务 Agent 不应让模型自由 loop。

建议采用受控 loop：

```text
模型做一次决策
-> 代码执行一次工具/流程
-> 写入状态
-> 下一轮或下一 Stage 再决策
```

### 对租车导购的设计建议

当前 V4 已是受控 loop：

- 本轮 `DecideStage` 只选一个动作。
- 搜车空结果由代码最多有限放宽。
- 反问由结构化协议暂停。
- 最终由 `FinalizeStage` 收口。

### 对租车其他 Agent 的设计建议

订单类动作必须有明确停止条件：

- 最多重试几次。
- 什么时候转人工。
- 什么时候要求用户确认。
- 什么时候禁止继续。

参考：

- [ReAct 论文学习笔记](./learning/react-paper-summary.md) §5「业务 Agent 应该如何借鉴 ReAct」
- [Anthropic《Building Effective Agents》](./learning/anthropic-building-effective-agents-summary.md) §6「Autonomous Agent 适合什么」
- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 8「Own Your Control Flow」、§Factor 9「Compact Errors into Context Window」

## 8. 借鉴七：错误要压缩、限次、可回放

工具失败、模型解析失败、搜索失败，不要只打日志，也不要原样把大错误塞回模型。

建议错误事件结构：

```json
{
  "stage": "FilterCodeStage",
  "error_type": "selector_failed",
  "summary": "filter selector timeout",
  "fallback": "static_recall",
  "retryable": false
}
```

### 对租车导购的设计建议

可沉淀这些错误事件：

- LLM stream failed。
- tool call parse failed。
- menu schema failed。
- filter selector failed。
- search empty。
- guardrail blocked。
- SSE write skipped。

### 对租车其他 Agent 的设计建议

订单/售后错误需要更严肃：

- 上游系统失败。
- 订单状态不允许。
- 权限不足。
- 用户未确认。
- 风控拒绝。

这些应该能被 trace 和客服后台看到。

参考：

- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 9「Compact Errors into Context Window」
- [Evals / Tracing / Guardrails 学习笔记](./learning/evals-tracing-guardrails-summary.md) §4「Tracing 要记录什么」、§6.2「只存最终回答，不存中间过程」

## 9. 借鉴八：Eval / Trace / Guardrail 是生产化主线

业务 Agent 不要只靠 prompt 调参。

至少建立三类 eval：

| 类型 | 评估内容 |
| --- | --- |
| 单元级 | 工具选择、JSON 解析、必填字段、白名单 |
| 质量级 | 话术自然度、推荐理由、规则解释完整度 |
| 业务级 | 转化率、空结果率、反问跳过率、拦截率 |

Trace 至少记录：

- 用户输入。
- session/context 摘要。
- prompt/context/tool schema version。
- 模型输出。
- 工具参数。
- 工具结果。
- guardrail 结果。
- 最终前端事件。

### 对租车导购的设计建议

建议建立专项 eval：

- 决策工具选择 eval。
- `need_delta` 抽取 eval。
- `FilterCodeStage` 选码 eval。
- 库存事实 eval。
- 反问质量 eval。
- 空结果放宽 eval。
- SSE 顺序 eval。

### 对租车其他 Agent 的设计建议

订单/售后 eval 更要覆盖：

- 是否错误承诺退款。
- 是否越权执行动作。
- 是否遗漏确认。
- 是否错误解释规则。
- 是否正确转人工。

参考：

- [Evals / Tracing / Guardrails 学习笔记](./learning/evals-tracing-guardrails-summary.md) §2「Evals 为什么重要」、§3「三层 Eval」、§5「Guardrails 不是只有安全审核」
- [Anthropic《Building Effective Agents》](./learning/anthropic-building-effective-agents-summary.md) §9.4「Eval 和 Trace 是迭代核心」
- [12-Factor Agents](./learning/12-factor-agents-summary.md) §6.5「把 badcase 变成事件回放」

## 10. 分场景设计建议

### 10.1 租车导购 Agent

推荐架构：

```text
PreRoute
-> Decide
-> Rules / PriceProtect / Modify
-> FilterCode
-> Search
-> Guide
-> Clarify
-> Finalize
```

继续强化：

- Prompt 决策树。
- Context builder 版本化。
- 真实库存约束。
- 筛选码白名单。
- 结构化反问。
- 搜空放宽策略。
- Guide 个性化解释。
- SSE/审核/done 收口。

不要做：

- 让模型直接说有没有车。
- 让模型直接选最终筛选码。
- 让模型裸问用户。
- 让模型自由 loop 多次搜。

主要参考：

- [AI 导购设计亮点总结](./ai-guide-design-highlights.md) 全文
- [Anthropic《Building Effective Agents》](./learning/anthropic-building-effective-agents-summary.md) §10「和当前 AI 导购设计的映射」
- [12-Factor Agents](./learning/12-factor-agents-summary.md) §7「和 AI 导购 V4 的映射」

### 10.2 租车订单/售后 Agent

推荐架构：

```text
Intent
-> Load Order Context
-> Policy / Rule Lookup
-> Decide Next Action
-> Confirmation / Approval if needed
-> Execute via State Machine
-> Notify / Ticket / Trace
```

重点设计：

- 订单状态是最高优先级 context。
- 所有写操作必须走状态机。
- 退款、取消、改订单必须确认。
- 规则解释必须有知识库或配置来源。
- 高客诉/风控/复杂异常必须转人工。

可以借鉴：

- 导购的结构化反问机制。
- 导购的 Finalize 和 SSE 收口。
- 12-Factor 的 pause/resume 和 human tool call。

不要做：

- 让模型直接改订单。
- 让模型承诺退款金额。
- 让模型在无订单上下文时解释具体结果。

主要参考：

- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 6「Launch/Pause/Resume with Simple APIs」、§Factor 7「Contact Humans with Tool Calls」
- [Evals / Tracing / Guardrails 学习笔记](./learning/evals-tracing-guardrails-summary.md) §5「Guardrails 不是只有安全审核」
- [Context Engineering 学习笔记](./learning/context-engineering-summary.md) §2「Context 包含什么」

### 10.3 租车运营/客服辅助 Agent

适合 Agent 化的能力：

- 辅助坐席查订单和规则。
- 自动生成工单摘要。
- 多系统信息汇总。
- 规则问答。
- 异常原因归因建议。

设计重点：

- Agent 给建议，不直接替人执行高风险动作。
- 每个建议要附来源。
- 坐席确认后再执行。
- 所有工具调用和建议进入 trace。

主要参考：

- [Anthropic《Building Effective Agents》](./learning/anthropic-building-effective-agents-summary.md) §5.3「Parallelization」、§5.5「Evaluator-Optimizer」
- [Lilian Weng《LLM Powered Autonomous Agents》学习笔记](./learning/lilian-weng-llm-agents-summary.md) §5「Tool Use」
- [Evals / Tracing / Guardrails 学习笔记](./learning/evals-tracing-guardrails-summary.md) §4「Tracing 要记录什么」

### 10.4 其他业务 Agent

可以按风险分层：

| 类型 | 推荐形态 |
| --- | --- |
| 内容/知识问答 | RAG + 引用 + eval |
| 导购/推荐 | Workflow + 真实库存/价格 + 结构化反问 |
| 订单/交易 | State Machine + LLM 局部决策 + 确认/审批 |
| 数据分析 | ReAct/Plan 可适度使用，但要沙箱和 trace |
| 研发/排障 | 更适合 ReAct / autonomous agent |
| 法务/财务/风控 | Multi-step workflow + evaluator + 人审 |

参考：

- [Anthropic《Building Effective Agents》](./learning/anthropic-building-effective-agents-summary.md) §5「常见 Workflow 模式」、§6「Autonomous Agent 适合什么」
- [Lilian Weng《LLM Powered Autonomous Agents》学习笔记](./learning/lilian-weng-llm-agents-summary.md) §3「Planning」、§4「Memory」、§5「Tool Use」
- [ReAct 论文学习笔记](./learning/react-paper-summary.md) §3「ReAct 适合什么」

## 11. 推荐设计 Checklist

启动一个业务 Agent 前，先回答这些问题：

### 业务边界

- 这个 Agent 服务哪个明确场景？
- 哪些问题必须拒绝或转人工？
- 哪些动作有副作用？
- 哪些动作需要用户确认或审批？

参考：

- [Prompt Engineering 学习笔记](./learning/prompt-engineering-summary.md) §2.1「明确任务边界」
- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 10「Small, Focused Agents」

### Context

- 本轮模型必须知道哪些状态？
- 哪些历史会污染判断？
- 上次工具结果如何摘要？
- 是否有 pending action？
- context builder 是否版本化？

参考：

- [Context Engineering 学习笔记](./learning/context-engineering-summary.md) §3「Context Engineering 的四个重点」
- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 3「Own Your Context Window」

### Tool

- 工具是否小而明确？
- 参数 schema 是否清楚？
- 相似工具边界是否写清？
- 工具执行前是否需要 guardrail？
- 工具结果是否适合回填给模型？

参考：

- [Anthropic《Building Effective Agents》](./learning/anthropic-building-effective-agents-summary.md) §7「工具接口比想象中更重要」
- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 4「Tools Are Just Structured Outputs」

### Control Flow

- 哪些流程固定？
- 哪些地方允许模型决策？
- loop 上限是多少？
- 错误如何 fallback？
- 什么时候暂停、恢复、转人工？

参考：

- [12-Factor Agents](./learning/12-factor-agents-summary.md) §Factor 8「Own Your Control Flow」
- [ReAct 论文学习笔记](./learning/react-paper-summary.md) §5.4「不借鉴‘模型完全自由控制 loop’」

### Eval / Trace

- 是否有离线 eval？
- 是否有线上 trace？
- 是否能回放 badcase？
- 是否记录 prompt/context/tool schema version？
- 是否有 guardrail matrix？

参考：

- [Evals / Tracing / Guardrails 学习笔记](./learning/evals-tracing-guardrails-summary.md) §8「最小落地建议」
- [Anthropic《Building Effective Agents》](./learning/anthropic-building-effective-agents-summary.md) §9.4「Eval 和 Trace 是迭代核心」

## 12. 最终建议

对租车和多数业务 Agent，最推荐的落地范式是：

```text
LLM Decision
+ Business Context
+ Structured Tool Calls
+ Deterministic Workflow
+ State Machine / Guardrails
+ Trace / Eval / Replay
```

不要追求“看起来最 Agent”的架构，要追求：

- 用户体验稳定。
- 业务动作可控。
- 事实来源真实。
- 错误可定位。
- 迭代可评估。

这也是当前租车 AI 导购 V4 已经走出的方向。后续真正值得补强的，不一定是更复杂的 Agent loop，而是更体系化的 context、tool ACI、eval、trace、guardrail 和业务状态机。

