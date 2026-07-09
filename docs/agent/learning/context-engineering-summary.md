# Context Engineering 学习笔记：让模型拿到正确的信息、工具和格式

参考：

- LangChain, The rise of context engineering: <https://www.langchain.com/blog/the-rise-of-context-engineering>
- 12-Factor Agents / Own your context window: <https://github.com/humanlayer/12-factor-agents>

## 1. 核心结论

Context engineering 的核心问题是：

```text
为了让 LLM 有可能完成任务，本轮到底应该给它什么信息、什么工具、什么格式？
```

LangChain 的文章把 context engineering 定义为：构建动态系统，向 LLM 提供正确的信息和工具，并以正确格式呈现，使模型有可能完成任务。

这句话很适合业务 Agent。很多线上 badcase 不是模型“不会”，而是模型没有拿到正确上下文，或拿到了错误/污染/过期上下文。

## 2. Context 包含什么

业务 Agent 的 context 不只是聊天历史。

它包括：

- system prompt。
- 当前用户输入。
- 会话历史。
- 历史摘要。
- 当前业务状态。
- 上一次工具调用结果。
- 用户画像。
- 权限信息。
- 当前可用工具。
- 工具 schema。
- RAG 文档。
- 错误摘要。
- 输出格式要求。

对业务 Agent 来说，context builder 往往比 prompt 本身更重要。

## 3. Context Engineering 的四个重点

### 3.1 给正确的信息

模型不是读心术。它不知道当前城市、库存、订单状态、用户历史偏好，除非你给它。

但也不能什么都给。要区分：

- 当前轮必须知道的信息。
- 可选参考信息。
- 已过期信息。
- 不能暴露给模型的敏感信息。

### 3.2 给正确的工具

模型有时无法只靠输入完成任务，需要外部工具：

- 查询库存。
- 查询订单。
- 查规则库。
- 查用户权益。
- 发起审批。
- 请求用户补充信息。

工具本身也是 context。工具太少，模型做不了事；工具太多，模型容易选错。

### 3.3 给正确的格式

同样的信息，不同格式效果差很多。

例如：

- 一大坨原始 JSON 可能不如结构化摘要。
- 错误栈可能不如压缩后的错误原因。
- 历史消息可能不如“当前需求状态 + 上次搜索摘要”。

格式决定模型注意力落在哪里。

### 3.4 判断模型是否“有可能完成任务”

这是排查 badcase 的关键问题：

```text
模型失败，是因为能力不行？
还是因为我们没给它必要上下文？
还是给了错误/过期上下文？
还是工具描述不清？
```

这几种问题的修法完全不同。

## 4. 业务 Agent 常见 Context 坑

### 4.1 上下文污染

旧条件没有衰减，导致新请求被历史约束绑住。

例子：

```text
用户先说“7座丰田”
后面说“看小米”
系统仍搜“小米 + 7座”
```

### 4.2 上下文缺失

模型不知道上次搜了什么，就无法回答“第一辆多少钱”；不知道当前取还车城市，就会错误判断异地。

### 4.3 上下文太长

长历史全塞进去，模型会迷路。业务 Agent 更适合把历史压成状态：

- 当前有效 needs。
- last_search。
- last_quote_summary。
- pending clarification。
- profile。

### 4.4 工具结果格式不友好

工具返回大 JSON，模型未必能抓重点。需要结果摘要、字段裁剪、口径统一。

## 5. 和 AI 导购的映射

AI 导购的 `DecideStage.buildStatePrefix` 就是 context engineering：

- 当前时间：处理相对租期。
- 当前需求状态 needs：承接多轮偏好。
- 上次搜索 last_search：支持翻页。
- 对话摘要 summary：压缩历史。
- 上轮报价 last_quote：支持历史追问。
- 当前取还车 current_rental：支持异地和改地点判断。
- 用户画像 profile：支持个性化导购。
- 已反问次数 clarify_count：避免无限追问。

这比“把聊天历史全丢给模型”更适合业务生产。

## 6. 后续可借鉴

- 为 context builder 单独建立版本号。
- 记录每轮实际注入模型的 context 摘要。
- 对 needs 衰减、冲突清理做专项测试。
- 区分“给模型看的状态”和“系统内部状态”。
- 对 RAG/规则/库存工具结果做专门摘要格式。
- 建立 context regression：上下文格式变更后跑历史 badcase。

