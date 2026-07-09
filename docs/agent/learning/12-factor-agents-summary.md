# 12-Factor Agents 深度摘要与业务 Agent 落地启发

原文：<https://github.com/humanlayer/12-factor-agents>  
作者/项目：HumanLayer / Dex Horthy  
整理日期：2026-07-01

## 1. 核心结论

`12-Factor Agents` 的核心观点非常适合业务 Agent 开发：生产级 LLM 应用不是“一个 prompt + 一堆工具 + loop 到完成”，而是一个大部分由确定性软件组成、在关键点嵌入 LLM 能力的系统。

原文作者观察了很多 SaaS 团队和 Agent 框架实践后，给出的判断是：

- 很多号称 AI Agent 的产品，其实并不那么“自治”。
- 可靠的生产系统通常是确定性代码为主，LLM 步骤在恰当位置提供语义理解和灵活性。
- 框架可以加速启动，但当质量卡在 70%-80% 时，团队往往需要反向理解 prompt、context、control flow，甚至重写核心链路。
- 最快把 AI 能力可靠放进产品的方法，不是重写成一个全新 Agent 框架，而是把 Agent 里的小型模块化概念嵌入现有产品。

一句话总结：

```text
不要把业务系统交给 Agent 裸奔。
把 Agent 能力拆成 prompt、context、tool、state、control flow、human approval、trace 等工程部件。
```

## 2. 它和 Anthropic《Building Effective Agents》的关系

Anthropic 的文章强调：

- 先用简单方案。
- Workflow 往往比 Autonomous Agent 更适合生产。
- 增加复杂度必须由评测指标证明。
- 工具接口要认真设计。

`12-Factor Agents` 则更进一步，从工程实践角度告诉你怎么拆：

- Prompt 要自己掌控。
- Context window 要自己掌控。
- Tool call 本质是结构化输出。
- 执行状态和业务状态最好统一。
- 控制流要自己掌控。
- 人类审批也可以建模成 tool call。
- Agent 应该小而专注。
- Agent 最好像 stateless reducer 一样可恢复、可回放。

两篇放在一起看，结论非常一致：业务 Agent 的关键不是“更自治”，而是“更可控”。

## 3. 作者指出的典型失败路径

原文里提到很多团队的常见路径：

```text
想做 Agent
-> 设计产品和 UX
-> 为了快，引入一个框架
-> 很快做到 70%-80% 质量
-> 发现 80% 对客户可见功能不够
-> 为了继续提升，被迫反向理解框架、prompt、flow
-> 最后重做核心链路
```

这段对业务 Agent 很有警示意义。

很多 demo 之所以容易，是因为用户还没真正依赖它；很多生产问题之所以难，是因为：

- 业务边界复杂。
- badcase 长尾多。
- 工具调用有副作用。
- 上下文会污染。
- 多轮状态会漂移。
- 用户可见链路要求稳定。
- 出错后需要解释、恢复和追责。

所以业务 Agent 不能只追求“能跑”，而要追求“可调、可测、可回放、可恢复”。

## 4. 12 个 Factor 深度摘要

### Factor 1：Natural Language to Tool Calls

把自然语言转成结构化工具调用，是 Agent 最常见也最有用的模式。

例如用户说“给某人创建 750 美元付款链接”，模型输出结构化对象：

```json
{
  "function": {
    "name": "create_payment_link",
    "parameters": {
      "amount": 750,
      "customer": "...",
      "product": "...",
      "memo": "..."
    }
  }
}
```

然后由确定性代码真正执行。

业务启发：

- LLM 最擅长把模糊自然语言转成结构化意图。
- 真正执行动作应该由代码控制。
- 工具调用不是“模型真的做了事”，只是模型产出了一个可解析的动作描述。

对应 AI 导购：

- 用户自然语言被 `DecideStage` 转成 `search_vehicles / ask / modify_location / interpret_rules`。
- 模型只决定意图和参数，真正搜车、改取还车、查规则由后端执行。

### Factor 2：Own Your Prompts

不要把 prompt 完全外包给框架。Prompt 是业务 Agent 的一等代码。

原文强调，框架的 `role / goal / personality / tools` 抽象很方便，但在生产调优时可能变成黑盒。你需要知道模型实际看到了哪些 token。

业务启发：

- Prompt 应该版本化。
- Prompt 应该可以测试。
- Prompt 应该支持快速迭代。
- Prompt 是应用逻辑和模型之间的主接口。

踩坑：

- 只知道配置了“角色”和“目标”，不知道最终 prompt 长什么样。
- 改 badcase 时只能堆规则，不能精准控制上下文。
- Prompt 变成不可测、不可 diff 的隐形配置。

对应 AI 导购：

- `v4_decide_prompt.go` 明确掌控决策 prompt。
- 规则、边界、库存事实铁律、反问铁律都写在项目代码里，便于 code review 和测试。

### Factor 3：Own Your Context Window

这是整篇最重要的 factor 之一。

原文观点：LLM 是无状态函数，输入什么上下文，就基于什么上下文输出。Agent 每一步本质上都是：

```text
这里是已经发生的事情，现在下一步该做什么？
```

Context 包括：

- system prompt。
- 用户输入。
- 历史消息。
- 工具调用和工具结果。
- RAG 文档。
- 业务状态。
- memory。
- 输出格式要求。

业务启发：

- 不一定要使用标准 chat message 格式。
- 可以为业务设计更高信息密度的上下文格式。
- 要决定哪些历史保留、哪些压缩、哪些丢弃。
- 错误信息也可以进入上下文，帮助模型恢复。
- 敏感信息必须过滤。

踩坑：

- 旧状态污染当前判断。
- 上下文太长，模型注意力漂移。
- 把无关历史塞给模型，导致误判。
- 只调 prompt，不调 context，永远修不稳。

对应 AI 导购：

- `DecideStage` 构造了当前需求状态、上次搜索、摘要、上轮报价、当前取还车、用户画像、反问次数。
- 这正是 context engineering，而不是普通 prompt engineering。

### Factor 4：Tools Are Just Structured Outputs

工具调用本质上只是模型输出结构化 JSON，然后确定性代码决定怎么处理。

这句话很重要：模型“调用了工具”不代表一定要原样执行某个函数。你可以：

- 执行 API。
- 暂停等待人类确认。
- 做权限校验。
- 转成异步任务。
- 拒绝执行。
- 改写成另一个业务流程。

业务启发：

- Tool schema 应该看成业务协议。
- 结构化输出和执行动作要解耦。
- 高风险工具必须有审批、校验、状态机。

对应 AI 导购：

- `search_vehicles` 的输出不是直接搜车参数，而是 `need_delta`。
- 后面还有 `FilterCodeStage`、白名单、搜索后处理、空结果放宽。
- 这就是“工具调用只是结构化意图”，而不是“模型说搜就裸搜”。

### Factor 5：Unify Execution State and Business State

原文建议尽量统一执行状态和业务状态。

执行状态包括：

- 当前步骤。
- 下一步。
- 是否等待。
- 重试次数。
- 暂停原因。

业务状态包括：

- 已发生的用户输入。
- 工具调用。
- 工具结果。
- 决策历史。
- 当前会话信息。

如果能从一条 thread / event history 推导出当前执行状态，就不要额外维护过多隐藏状态。

业务启发：

- 单一事实源更易 debug。
- 会话可序列化。
- 可暂停、恢复、fork。
- 人类也能读懂发生了什么。

踩坑：

- 状态散落在 Redis、DB、内存、框架 runtime、LLM history 里。
- 恢复时不知道到底卡在哪。
- badcase 无法完整重放。

对应 AI 导购：

- `Session` 里有 `History`、`Constraints`、`LastSearch`、`ShoppingContext`、`PendingClarification`。
- 后续可以进一步把 Stage trace、tool snapshot、clarification lifecycle 做成更统一的事件流。

### Factor 6：Launch/Pause/Resume with Simple APIs

Agent 是程序，应该能被简单启动、暂停、恢复、停止。

特别关键的是：Agent 应该能在工具选择之后、工具执行之前暂停。

这对高风险业务很重要。比如模型决定要：

- 发邮件。
- 部署生产。
- 退款。
- 改订单。
- 下单。

此时不能直接执行，而应该暂停，等待审批或外部事件。

业务启发：

- Agent 不应只存在于内存循环里。
- 每个中间态都应该可持久化。
- 外部 webhook / 用户回复 / 审批结果可以恢复 Agent。

对应 AI 导购：

- 反问机制就是一种 pause/resume。
- 改取还车拉起前端编辑器，也是暂停等待用户确认。
- 后续如果扩展到订单操作，必须强化这一点。

### Factor 7：Contact Humans with Tool Calls

人类介入也可以建模成工具调用。

例如：

```json
{
  "intent": "request_human_input",
  "question": "是否确认部署生产？",
  "options": {
    "format": "yes_no",
    "urgency": "high"
  }
}
```

这带来几个好处：

- 人类反馈进入结构化事件流。
- 审批、确认、补充信息都可以被状态机承接。
- Agent 可以从 Slack、邮件、短信等外部渠道恢复。
- 高风险动作可以先请求人类确认。

业务启发：

- 不要把“问人”当成普通文本。
- 问人也是业务工具。
- 审批结果必须可追踪。

对应 AI 导购：

- `ask` 转成 `Clarification`，本质就是 request human input。
- 选项、跳过、自由输入、pending action 都是结构化人类交互。

### Factor 8：Own Your Control Flow

这是另一条核心原则。

不要把控制流完全交给框架或模型。你应该自己决定：

- 哪些工具调用可以同步执行后继续 loop。
- 哪些工具调用必须暂停。
- 哪些工具调用需要审批。
- 哪些错误可以重试。
- 哪些状态必须停止。
- 哪些结果需要压缩或缓存。
- 何时限流、何时降级、何时转人工。

业务启发：

- Control flow 是生产 Agent 的安全边界。
- 自由 loop 只适合低风险探索任务。
- 高风险业务必须能在工具选择和工具执行之间插入 deterministic control。

对应 AI 导购：

- V4 Pipeline 就是明确 owned control flow。
- `Decide -> Rules -> Modify -> FilterCode -> Search -> Guide -> Clarify -> Finalize` 是代码控制，而不是模型自由循环。

### Factor 9：Compact Errors into Context Window

Agent 的一个优势是可以根据错误自我修复。工具失败后，可以把错误摘要放回上下文，让模型调整下一步。

但原文也提醒：不能无限把原始错误塞回去，否则 Agent 会 spin out，反复犯同样的错。

业务启发：

- 错误要结构化。
- 错误要压缩。
- 相同错误要有重试上限。
- 达到阈值后要转人工或 deterministic fallback。

对应 AI 导购：

- 搜索失败、菜单失败、模型失败目前多走 fallback。
- 可以进一步沉淀“错误事件摘要”，用于离线 eval 和 badcase replay，而不是全部暴露给模型。

### Factor 10：Small, Focused Agents

不要做一个什么都管的巨型 Agent。Agent 应该小而专注。

原因是：任务越复杂，步骤越长，上下文越大，模型越容易迷路。

原文建议把 Agent 控制在明确领域内，通常 3-10 步，最多 20 步左右更可靠。

业务启发：

- 一个 Agent 做一个清晰领域。
- 多能力可以由 workflow 编排，而不是一个 prompt 全吃。
- Agent 范围可以随着模型能力和 eval 结果逐渐扩大。

对应 AI 导购：

- 租车导购只聚焦订前导购，不直接处理订单退款、售后投诉、客服全域。
- 规则、贵必赔、车型对比等能力也被拆成单独 Stage / 能力。

### Factor 11：Trigger from Anywhere, Meet Users Where They Are

Agent 不一定只由聊天框触发。它可以由：

- Slack。
- 邮件。
- 短信。
- Webhook。
- 定时任务。
- 告警事件。
- 业务事件。

触发后也可以通过同一渠道联系用户或审批人。

业务启发：

- Agent 是业务流程参与者，不只是聊天机器人。
- 外部事件触发和人类审批结合后，可以支持更高价值动作。
- 前提是状态、权限、审计和恢复机制可靠。

对应 AI 导购：

- 当前主要由前端聊天触发。
- 未来如果结合订单事件、价格波动、取车提醒、贵必赔进度，可以向外部事件触发扩展。

### Factor 12：Make Your Agent a Stateless Reducer

原文这一节较短，但概念很关键：Agent 可以被看成一个 reducer。

也就是：

```text
new_state = reducer(old_state, event)
```

或者：

```text
next_step = LLM(context_from_events)
```

Agent 本身不应该依赖不可恢复的内存状态。只要有事件历史，就能重建上下文、恢复执行、回放问题。

业务启发：

- Agent 状态要可序列化。
- 每一步最好都是事件驱动。
- 回放和恢复应该是架构内建能力。

对应 AI 导购：

- 当前 `Session + History + ToolCallSnapshot + LastSearch` 已有这个方向。
- 若继续演进，可以把每轮 Stage 输出统一成事件日志，做到完整 replay。

## 5. 这些原则如何组合成生产架构

把 12 个 factor 合起来，其实是一个业务 Agent 架构：

```text
用户/事件输入
-> 构造业务上下文
-> LLM 输出结构化 next_step
-> deterministic control flow 判断是否执行/暂停/审批/拒绝
-> 工具执行或人类介入
-> 结果写入统一事件/会话状态
-> 必要时继续 loop
-> 最终渲染、落库、trace、eval
```

和常见“裸 Agent loop”的差异：

| 裸 Agent Loop | 12-Factor 思路 |
| --- | --- |
| 模型自由决定下一步 | 模型产出结构化意图，代码掌控控制流 |
| 工具调用后直接执行 | 工具调用只是结构化输出，可审批、可暂停、可拒绝 |
| 上下文由框架管理 | 自己管理 context window |
| prompt 黑盒 | prompt 一等代码 |
| 状态散落 runtime | 状态尽量统一成 thread/events |
| 错误原样回塞 | 错误压缩、限次、转人工 |
| 做一个大 Agent | 做小而专注的 Agent |

## 6. 对业务 Agent 的落地建议

### 6.1 从现有业务流程嵌入 LLM，而不是重写业务系统

先找最适合 LLM 的点：

- 意图识别。
- 槽位抽取。
- 工具参数生成。
- 复杂语义判断。
- 个性化话术。
- 结果总结。

不要一上来让 LLM 接管完整业务流程。

### 6.2 Prompt、Context、Tool Schema 都要版本化

这三类变更都可能改变模型行为。

建议至少记录：

- prompt 版本。
- context builder 版本。
- tool schema 版本。
- 模型版本。
- 输出解析版本。

### 6.3 把“问用户”做成结构化工具

业务 Agent 最常见的问题不是模型不会答，而是不知道什么时候该问。

结构化反问要包含：

- 问题。
- 槽位。
- 选项。
- 是否允许跳过。
- pending action。
- 回执解析规则。

### 6.4 设计工具选择和执行之间的拦截点

高风险动作必须可以在执行前暂停：

- 下单。
- 支付。
- 退款。
- 改订单。
- 发外部消息。
- 写生产数据。

模型选择工具不等于立刻执行工具。

### 6.5 把 badcase 变成事件回放

每个 badcase 最好能看到：

- 用户输入。
- 构造后的 context。
- 模型输出。
- 选中的 tool。
- 工具参数。
- 工具结果。
- 控制流决策。
- 最终输出。

这样才能从“感觉 prompt 不行”变成“明确是哪一层错了”。

## 7. 和 AI 导购 V4 的映射

| 12-Factor 原则 | AI 导购当前体现 |
| --- | --- |
| Natural Language to Tool Calls | `DecideStage` 把用户输入转成 `search_vehicles/ask/modify_location/...` |
| Own your prompts | `v4_decide_prompt.go` 明确维护决策 prompt |
| Own your context window | `buildStatePrefix` 注入 needs、last_search、summary、current_rental、profile |
| Tools are structured outputs | `search_vehicles` 只产 `need_delta`，后续代码再转筛选码 |
| Unify execution/business state | `Session` 存储 History、Constraints、LastSearch、PendingClarification |
| Launch/Pause/Resume | 反问、改取还车编辑器都是暂停/恢复形态 |
| Contact humans with tools | `ask` -> `Clarification` 是结构化人类输入 |
| Own your control flow | V4 Pipeline 固定控制 Stage 顺序 |
| Compact errors | 搜索/模型/菜单失败有 fallback，后续可加强错误事件摘要 |
| Small focused agents | 导购聚焦订前选车，不做全域客服/订单后操作 |
| Trigger from anywhere | 当前以聊天触发为主，未来可扩展订单/价格/提醒事件 |
| Stateless reducer | Session/History/ToolCallSnapshot 已具备 replay 雏形 |

## 8. 当前 AI 导购可继续借鉴的方向

1. **统一事件日志**
   将每个 Stage 的输入、输出、决策、工具结果统一成可回放事件。

2. **Prompt/Context/Tool 版本化**
   支持线上 badcase 回放时恢复当时的完整模型输入。

3. **工具执行前拦截**
   如果未来导购扩展到订单操作，需要支持工具选择后审批/确认。

4. **错误压缩策略**
   对菜单失败、搜索失败、模型解析失败生成结构化 error event，而不是只打日志。

5. **小 Agent 边界继续保持**
   导购、规则、售后、订单操作不要合成一个巨型 prompt，应该按能力拆分。

6. **可触发渠道扩展**
   订前导购之外，可以探索价格提醒、取车提醒、贵必赔进度等事件触发型 Agent。

## 9. 最适合内部分享的一页总结

`12-Factor Agents` 的价值在于，它把“Agent”从一个玄学概念拆回了软件工程。

它告诉我们：生产级业务 Agent 的关键，不是让模型更自由，而是把模型能力纳入可控的软件结构：

```text
Prompt 自己管
Context 自己管
Tool 当结构化输出
Control flow 自己管
State 可持久化可恢复
Human approval 是一等工具
Agent 小而专注
所有过程可回放
```

这和当前 AI 导购 V4 的方向非常一致：模型负责自然语言理解和局部决策，业务系统负责状态、库存、筛选码、前端协议、安全审核和收尾。

因此，业务 Agent 的竞争力不在于“用了多高级的 Agent 框架”，而在于团队能否掌控 prompt、context、tool、state、control flow、eval 这些关键工程面，并在真实 badcase 中持续迭代。

## 参考

- 12-Factor Agents: <https://github.com/humanlayer/12-factor-agents>
- Anthropic, Building effective agents: <https://www.anthropic.com/engineering/building-effective-agents>
