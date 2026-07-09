# Prompt Engineering 学习笔记：业务规则如何变成模型可执行的软契约

参考：

- OpenAI Prompt Engineering: <https://platform.openai.com/docs/guides/prompt-engineering>
- Anthropic Prompt Engineering Overview: <https://docs.anthropic.com/en/docs/build-with-claude/prompt-engineering/overview>
- 12-Factor Agents / Own your prompts: <https://github.com/humanlayer/12-factor-agents>

## 1. 核心结论

业务 Agent 里的 prompt 不是“提示词技巧”，而是模型侧业务协议。它告诉模型：

- 你是谁。
- 你服务哪个业务。
- 什么问题该处理。
- 什么问题该拒绝。
- 什么时候调用工具。
- 什么时候追问用户。
- 哪些事实不能编。
- 输出必须是什么结构。
- 话术应该是什么风格。

但 prompt 不能承担全部业务逻辑。高风险、强确定性的规则必须下沉到代码、白名单、状态机和 guardrail。

一句话：

```text
Prompt 负责软判断和表达，代码负责硬约束和兜底。
```

## 2. Prompt 应该学习什么

### 2.1 明确任务边界

业务 Agent 首先要告诉模型“不做什么”。

例如租车导购：

- 可以做车型筛选、价格比较、出行方案建议。
- 可以查规则。
- 可以改取还车。
- 不应该回答与租车无关的问题。
- 不应该处理订单后退款/投诉等超出导购能力的问题。

边界越清晰，模型越不容易“热心帮倒忙”。

### 2.2 把决策树写清楚

业务 Agent 的 prompt 不应该只是“你是一个专业助手”，而应该有决策树：

```text
安全/越界 -> 拒绝
异地/改时间 -> modify_location
规则问题 -> interpret_rules
贵必赔使用 -> price_protection_entry
信息不足 -> ask
信息足够 -> search
```

这类决策树不是为了让模型完全替代代码，而是为了降低模型在局部决策上的歧义。

### 2.3 输出结构必须强约束

业务 Agent 最怕模型“答得像对的，但系统接不住”。

所以 prompt 要明确：

- 输出 JSON / tool call。
- 字段名是什么。
- 枚举值有哪些。
- 不能自造字段。
- 哪些字段为空时如何处理。
- 什么时候纯文本，什么时候结构化。

### 2.4 示例比抽象规则更有效

Prompt 里应包含代表性 case：

- 正例：用户说“带老人小孩”，推断 SUV。
- 反例：用户说“特斯拉有没有”，必须先查库存，不能直接回答。
- 边界例：用户说“一嗨更便宜”是贵必赔，不是按供应商筛选。
- 恢复例：用户说“别问了直接推”，停止追问。

业务 case 越贴近真实线上表达，prompt 越稳。

## 3. 业务 Agent 常见 Prompt 坑

### 3.1 把硬约束只写在 prompt 里

例如：

- 不允许编库存。
- 不允许发起退款。
- 不允许输出不存在的筛选码。

这些规则写 prompt 是必要的，但不充分。还必须有：

- 库存必须来自真实 search。
- 筛选码必须白名单校验。
- 退款必须权限和状态机校验。

### 3.2 Prompt 越修越长

没有 eval 的 prompt 迭代很容易变成打补丁：

```text
多一个 badcase -> 加一条规则
又多一个 badcase -> 再加一条规则
最后 prompt 又长又互相冲突
```

正确方式是先定位错误层：

- 是意图分类错？
- 是 context 缺信息？
- 是 tool schema 模糊？
- 是后处理没校验？
- 是 prompt 规则冲突？

不要默认所有问题都靠 prompt 修。

### 3.3 Prompt 被框架隐藏

业务生产里要能看到最终输入模型的 prompt。否则 badcase 无法复盘。

建议：

- prompt 文件化。
- prompt 版本化。
- prompt 变更 code review。
- 线上日志记录 prompt version。
- 回放时恢复当时 prompt。

## 4. 和 AI 导购的映射

当前 AI 导购里，`logic/agent/v4_decide_prompt.go` 是典型 prompt 工程：

- 安全红线。
- 话术要求。
- 工具决策树。
- 库存事实铁律。
- 反问铁律。
- 搜车参数 schema。
- 场景推理。
- 预算处理。
- 改取还车参数规则。

亮点是 prompt 没有孤立存在，而是配合：

- `v4_tools.go` 的工具 schema。
- `FilterCodeStage` 的确定性选码。
- `SearchStage` 的真实库存。
- `ClarifyStage` 的结构化反问。
- `FinalizeStage` 的统一收尾。

这就是业务 Agent 应该走的方向：prompt 写清软规则，但关键结果必须被工程链路接住。

## 5. 后续可借鉴

- 给 prompt 增加版本号，并进入 trace。
- 把 prompt 中的关键规则拆成 eval case。
- 对工具边界类规则建立专项回归集。
- Prompt 变更后自动跑 badcase replay。
- 对“同一句话走不同工具”的边界场景建立对照测试。

