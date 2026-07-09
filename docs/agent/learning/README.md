# 业务 Agent 学习资料目录

本目录存放业务 Agent 相关外部文章/论文的学习笔记。定位不是翻译，而是从“生产业务 Agent 落地”的角度提炼：应该学什么、借鉴什么、容易踩什么坑，以及如何映射到当前租车 AI 导购 V4。

## 推荐阅读顺序

1. [Anthropic《Building Effective Agents》](./anthropic-building-effective-agents-summary.md)  
   先建立总判断：多数生产场景优先 workflow，不要一开始就上自治 Agent。

2. [12-Factor Agents](./12-factor-agents-summary.md)  
   学工程拆法：prompt、context、tool、state、control flow、人类审批、可回放。

3. [Prompt Engineering 学习笔记](./prompt-engineering-summary.md)  
   学怎么把业务规则、边界、输出格式写成模型可执行的软契约。

4. [Context Engineering 学习笔记](./context-engineering-summary.md)  
   学怎么给模型正确的信息、工具和格式；这通常比单纯改 prompt 更重要。

5. [Evals / Tracing / Guardrails 学习笔记](./evals-tracing-guardrails-summary.md)  
   学怎么从 demo 走向生产：评测、回放、观测、安全兜底。

6. [Lilian Weng《LLM Powered Autonomous Agents》](./lilian-weng-llm-agents-summary.md)  
   学 Agent 概念地图：planning、memory、tool use、reflection。

7. [ReAct 论文](./react-paper-summary.md)  
   学经典 loop 范式：reasoning + acting，理解它适合什么，也理解为什么业务主链路不能裸用。

## 一张总图

```text
业务 Agent 生产化能力

Prompt
  -> 规则、边界、输出格式、话术

Context
  -> 当前状态、历史、工具结果、用户画像、业务数据

Tool / ACI
  -> 结构化输出、参数 schema、白名单、执行前校验

Workflow / Control Flow
  -> Stage、状态机、暂停/恢复、审批、fallback

Harness
  -> 调用、解析、校验、trace、eval、回放、灰度

Loop
  -> 受控多轮、错误压缩、重试上限、人类介入
```

## 对当前 AI 导购最关键的学习点

- 不要让模型裸控业务流程，V4 Pipeline 的方向是对的。
- `DecideStage` 是 routing + structured tool call，不是完整自治 Agent。
- `buildStatePrefix` 是 context engineering，后续应该版本化和可回放。
- `v4_tools.go` 是 ACI，工具 schema 要像接口契约一样维护。
- `FilterCodeStage` 是“模型结构化意图 -> 确定性业务执行”的典型中间层。
- `ClarifyStage` 是把“问用户”作为结构化工具，而不是裸文本追问。
- 后续最大增量不一定是更强 prompt，而是 eval、trace、badcase replay 和工具执行前 guardrail。

## 资料来源

- Anthropic, Building effective agents: <https://www.anthropic.com/engineering/building-effective-agents>
- HumanLayer, 12-Factor Agents: <https://github.com/humanlayer/12-factor-agents>
- OpenAI Prompt Engineering: <https://platform.openai.com/docs/guides/prompt-engineering>
- Anthropic Prompt Engineering Overview: <https://docs.anthropic.com/en/docs/build-with-claude/prompt-engineering/overview>
- LangChain, The rise of context engineering: <https://www.langchain.com/blog/the-rise-of-context-engineering>
- Hamel Husain, Your AI Product Needs Evals: <https://hamel.dev/blog/posts/evals/>
- OpenAI Agents SDK Tracing: <https://openai.github.io/openai-agents-python/tracing/>
- OpenAI Agents SDK Guardrails: <https://openai.github.io/openai-agents-python/guardrails/>
- Lilian Weng, LLM Powered Autonomous Agents: <https://lilianweng.github.io/posts/2023-06-23-agent/>
- ReAct paper: <https://arxiv.org/abs/2210.03629>

