# Lilian Weng《LLM Powered Autonomous Agents》学习笔记

原文：<https://lilianweng.github.io/posts/2023-06-23-agent/>  
作者：Lilian Weng  
发布时间：2023-06-23

## 1. 核心结论

这篇文章适合用来建立 Agent 概念地图。它把 LLM-powered autonomous agent 拆成三个核心组件：

- Planning：规划、任务拆解、自我反思。
- Memory：短期记忆、长期记忆、检索。
- Tool Use：使用外部工具弥补模型自身知识和能力限制。

它偏学术和综述，不是业务落地手册。但读完能帮助你理解 ReAct、Reflexion、Toolformer、MRKL、HuggingGPT 等概念之间的关系。

## 2. Agent System Overview

文章把 LLM 看作 Agent 的“大脑”，但大脑之外还需要：

```text
Planning + Memory + Tool Use
```

这对业务 Agent 的启发是：模型本身不是完整 Agent。只有当它能：

- 基于目标拆任务。
- 记住和读取相关历史。
- 调用外部工具获取真实信息或执行动作。
- 根据反馈调整行为。

才接近 Agent 系统。

## 3. Planning

Planning 包括两个方向：

### 3.1 Task Decomposition

复杂任务需要拆成子目标。常见方式：

- Chain-of-Thought：一步步想。
- Tree-of-Thoughts：探索多个思路分支。
- LLM+P：让 LLM 转成规划语言，再交给传统 planner。

业务启发：

- 不是所有业务都需要显式 planning。
- 短链路业务更适合 workflow。
- 长任务、开放任务、研发/调研/报告类任务更适合 planning。

### 3.2 Self-Reflection

Self-reflection 让 Agent 根据过去失败经验改进下一步。

代表方法：

- ReAct：reason + act 交错。
- Reflexion：把失败轨迹总结成反思记忆。

业务启发：

- 错误反馈进入上下文确实有价值。
- 但业务生产里要限制重试次数。
- 反思不能替代 eval 和 deterministic fallback。

## 4. Memory

文章把记忆类比为：

- 短期记忆：context window。
- 长期记忆：外部存储和检索。

业务 Agent 中的 memory 不应只是向量库。它还包括：

- 会话状态。
- 用户画像。
- 上次搜索。
- 订单状态。
- 工具调用结果。
- 历史摘要。
- 失败经验。

业务坑：

- 长期记忆写入不加过滤，会污染后续判断。
- 召回不带来源和时间，会使用过期信息。
- 全量历史进入上下文，会让模型注意力漂移。

## 5. Tool Use

Tool use 的价值是让模型访问：

- 当前信息。
- 私有知识。
- 外部 API。
- 代码执行。
- 数据库。
- 专用模型。

文章介绍了 MRKL、Toolformer、HuggingGPT 等思路。

业务启发：

- Tool 是 LLM 接入真实世界的接口。
- Tool 描述和参数 schema 直接影响模型行为。
- 多工具系统需要路由、白名单和权限控制。

## 6. 文章中提到的挑战

这类 autonomous agent 常见问题：

- 上下文长度限制。
- 长任务中的错误累积。
- 工具调用可靠性。
- 规划不稳定。
- 自我反思可能引入新错误。
- 长期记忆召回质量难评估。

这些挑战在业务 Agent 里会变得更尖锐，因为业务动作有副作用。

## 7. 和 AI 导购的映射

| Lilian Weng 概念 | AI 导购体现 |
| --- | --- |
| Planning | 不走开放 planning，而是 V4 Pipeline 固定流程 |
| Memory | Session、History、Summary、LastSearch、Profile |
| Tool Use | 搜车、规则、贵必赔、改取还车 |
| Reflection | 目前不做模型自反思，更多依靠日志、测试和 badcase |
| ReAct | 没裸用 ReAct，而是受控 tool decision |

AI 导购属于强业务约束场景，因此更适合“workflow + 局部 LLM”，而不是完整 autonomous agent。

## 8. 应该学什么

- 学 Agent 的组件拆解：planning / memory / tool。
- 学 ReAct 和 Reflexion 为什么有用。
- 学工具和外部环境反馈的重要性。
- 学长期记忆的风险。

但要避免直接照搬：

- 不要让导购自由规划多步动作。
- 不要把自我反思当线上自动修复。
- 不要把所有历史都塞成 memory。

