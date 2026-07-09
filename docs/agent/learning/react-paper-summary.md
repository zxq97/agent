# ReAct 论文学习笔记：Reasoning + Acting 的经典 Agent Loop

论文：ReAct: Synergizing Reasoning and Acting in Language Models  
链接：<https://arxiv.org/abs/2210.03629>  
作者：Shunyu Yao 等  
提交时间：2022-10-06，ICLR camera-ready 版本：2023-03-10

## 1. 核心结论

ReAct 的核心思想是让模型交替产生：

```text
Thought -> Action -> Observation -> Thought -> Action -> ...
```

也就是把“推理”和“行动”放在同一个循环里：

- Thought 帮模型跟踪目标、更新计划、处理异常。
- Action 让模型查询外部环境或执行工具。
- Observation 把真实反馈带回模型，减少纯脑补。

ReAct 在问答、事实验证、交互决策环境中表现出价值，尤其适合需要边查边判断下一步的任务。

## 2. ReAct 解决了什么问题

纯 Chain-of-Thought 的问题：

- 模型只在脑内推理，容易幻觉。
- 没有外部事实反馈。
- 错误会一路传播。

纯 Act-only 的问题：

- 只有动作，没有显式推理轨迹。
- 难以解释为什么这么做。
- 遇到异常时不容易调整策略。

ReAct 把两者结合：

- 用 reasoning trace 保持计划和解释性。
- 用 action 获取外部事实。
- 用 observation 修正后续决策。

## 3. ReAct 适合什么

适合任务路径不固定、需要外部反馈的场景：

- 多跳问答。
- 事实验证。
- 网页/电商环境操作。
- 代码排障。
- 日志排查。
- 多源调研。
- 工具探索。

共同特征：

- 不知道要查几次。
- 每次工具结果会影响下一步。
- 中间过程可从环境获得 ground truth。

## 4. ReAct 不适合什么

不适合作为强业务链路的裸主控：

- 支付。
- 退款。
- 下单。
- 订单状态变更。
- 库存承诺。
- 高风险客服动作。
- 强前端协议场景。

原因：

- loop 次数不稳定。
- 延迟和成本不可控。
- 工具选择顺序可能漂。
- 错误会在多步中累积。
- 高风险动作需要审批和状态机。
- 前端需要稳定事件顺序。

业务 Agent 可以借鉴 ReAct 的思想，但不应照搬裸 loop。

## 5. 业务 Agent 应该如何借鉴 ReAct

### 5.1 借鉴“行动必须带真实反馈”

租车导购里，库存事实必须来自真实 search，而不是模型凭常识回答。

### 5.2 借鉴“工具结果回到上下文”

上轮搜索结果、报价摘要、规则检索结果都应该进入下一轮 context。

### 5.3 借鉴“遇到异常要能调整”

搜空后放宽、工具失败后 fallback、缺槽时反问，都是受控版本的 ReAct 思想。

### 5.4 不借鉴“模型完全自由控制 loop”

业务系统应该用代码控制：

- 最多几轮。
- 什么工具能执行。
- 什么时候暂停。
- 什么时候转人工。
- 哪些动作需要审批。

## 6. 和 AI 导购的映射

| ReAct 概念 | AI 导购 V4 做法 |
| --- | --- |
| Thought | 不暴露原始思考，用导购话术和 thinking tips 表达进度 |
| Action | function calling 选择 search / ask / modify / rules |
| Observation | 搜索结果、规则答案、前端回执、反问回答 |
| Loop | 不让模型自由 loop，用 Pipeline 和多轮会话控制 |
| Error recovery | 空结果放宽、fallback、clarification |

AI 导购其实是“受控 ReAct 精神”的业务化版本：

```text
模型做一次受控决策
代码执行工具
结果进入状态
下一轮再由模型基于状态决策
```

## 7. 一句话总结

ReAct 是理解 Agent loop 的经典论文，但业务 Agent 不应该默认裸用 ReAct。

正确借鉴方式是：

```text
用工具反馈减少幻觉
用中间状态承接多轮
用代码掌控 loop 和风险
```

