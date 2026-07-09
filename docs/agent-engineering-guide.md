# Agent 工程化实战方案：多模型路由 + Prompt 工程 + Eval 体系

> 面向后端工程师的 Agent 开发落地指南
> 
> 核心目标：让 Agent 开发像后端开发一样有**工程纪律** —— 可测试、可回滚、可观测、可控成本。

---

## 目录

- [1. 总体架构概览](#1-总体架构概览)
- [2. 多模型路由系统](#2-多模型路由系统)
- [3. Prompt 工程体系](#3-prompt-工程体系)
- [4. Eval 评估体系](#4-eval-评估体系)
- [5. 三者联动：闭环工作流](#5-三者联动闭环工作流)
- [6. 项目目录结构建议](#6-项目目录结构建议)
- [7. 快速开始 Checklist](#7-快速开始-checklist)

---

## 1. 总体架构概览

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Agent Application                             │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌──────────┐     ┌───────────────┐     ┌──────────────────┐       │
│  │  Prompt  │────▶│  Model Router │────▶│  LLM Provider(s) │       │
│  │  Engine  │     │               │     │                  │       │
│  │          │     │  - 规则路由    │     │  - Claude        │       │
│  │  - 模板  │     │  - 复杂度评估  │     │  - GPT-4o        │       │
│  │  - 版本  │     │  - 成本约束    │     │  - DeepSeek      │       │
│  │  - 变量  │     │  - Fallback   │     │  - 豆包           │       │
│  └──────────┘     └───────────────┘     └──────────────────┘       │
│       │                   │                       │                  │
│       ▼                   ▼                       ▼                  │
│  ┌─────────────────────────────────────────────────────────┐       │
│  │                    Eval System                           │       │
│  │                                                         │       │
│  │  Dataset ──▶ Runner ──▶ Scorer ──▶ Report ──▶ CI Gate  │       │
│  └─────────────────────────────────────────────────────────┘       │
│                                                                     │
├─────────────────────────────────────────────────────────────────────┤
│  Observability: Token Metrics │ Latency │ Cost │ Quality Score      │
└─────────────────────────────────────────────────────────────────────┘
```

**三者关系：**
- **Prompt Engine** 产出 prompt → 送入 **Model Router** 选择最优模型 → 调用 LLM
- **Eval System** 验证整个链路的输出质量
- 改动 Prompt 或 Router 配置 → 自动触发 Eval → 通过才能合并

---

## 2. 多模型路由系统

### 2.1 设计目标

| 目标 | 描述 |
|------|------|
| 成本最优 | 80% 的请求用便宜模型处理，总成本降低 60%+ |
| 质量保底 | 复杂任务自动升级到强模型，不因省钱而劣化体验 |
| 高可用 | 模型不可用时自动 fallback，用户无感 |
| 可观测 | 每次路由决策可追溯，可分析 |

### 2.2 路由策略分类

```
┌─────────────────────────────────────────────────────┐
│              路由策略金字塔                            │
│                                                     │
│          ┌─────────────┐                            │
│          │  ML Router  │  ← 训练一个小模型做路由     │
│          │  (高级)      │                            │
│          ├─────────────┤                            │
│          │  混合路由    │  ← 规则 + 轻量分类器       │
│          │  (推荐起步)  │                            │
│          ├─────────────┤                            │
│          │  规则路由    │  ← if/else，简单有效       │
│          │  (入门)      │                            │
│          └─────────────┘                            │
└─────────────────────────────────────────────────────┘
```

**建议路径**：从规则路由起步 → 积累数据后训练 ML Router

### 2.3 模型能力矩阵

```python
# models/registry.py

from dataclasses import dataclass
from enum import Enum

class ModelTier(Enum):
    PREMIUM = "premium"      # 复杂推理、关键决策
    STANDARD = "standard"    # 通用任务
    ECONOMY = "economy"      # 简单任务、高并发
    MICRO = "micro"          # 分类、提取、embedding

@dataclass
class ModelConfig:
    name: str
    provider: str
    tier: ModelTier
    input_price: float        # $/1M tokens
    output_price: float       # $/1M tokens
    max_context: int          # tokens
    strengths: list[str]      # 擅长领域
    latency_p50_ms: int       # 典型延迟
    rate_limit_rpm: int       # 每分钟请求上限

# 模型注册表 —— 根据实际情况维护
MODEL_REGISTRY = {
    "claude-opus": ModelConfig(
        name="claude-opus-4-8",
        provider="anthropic",
        tier=ModelTier.PREMIUM,
        input_price=15.0,
        output_price=75.0,
        max_context=200_000,
        strengths=["complex_reasoning", "code", "planning", "multi_step"],
        latency_p50_ms=3000,
        rate_limit_rpm=100,
    ),
    "claude-sonnet": ModelConfig(
        name="claude-sonnet-4-6",
        provider="anthropic",
        tier=ModelTier.STANDARD,
        input_price=3.0,
        output_price=15.0,
        max_context=200_000,
        strengths=["general", "code", "analysis", "tool_use"],
        latency_p50_ms=1500,
        rate_limit_rpm=500,
    ),
    "deepseek-r1": ModelConfig(
        name="deepseek-r1",
        provider="deepseek",
        tier=ModelTier.STANDARD,
        input_price=0.55,
        output_price=2.19,
        max_context=64_000,
        strengths=["reasoning", "math", "code", "chinese"],
        latency_p50_ms=2000,
        rate_limit_rpm=300,
    ),
    "doubao-pro": ModelConfig(
        name="doubao-pro-32k",
        provider="volcengine",
        tier=ModelTier.ECONOMY,
        input_price=0.8,
        output_price=2.0,
        max_context=32_000,
        strengths=["chinese_chat", "summarization", "simple_qa"],
        latency_p50_ms=800,
        rate_limit_rpm=1000,
    ),
    "claude-haiku": ModelConfig(
        name="claude-haiku-4-5",
        provider="anthropic",
        tier=ModelTier.ECONOMY,
        input_price=0.8,
        output_price=4.0,
        max_context=200_000,
        strengths=["classification", "extraction", "simple_generation"],
        latency_p50_ms=500,
        rate_limit_rpm=2000,
    ),
}
```

### 2.4 路由器核心实现

```python
# router/model_router.py

from dataclasses import dataclass
from enum import Enum
from typing import Optional
import re

class TaskType(Enum):
    COMPLEX_REASONING = "complex_reasoning"  # 复杂推理、规划
    CODE_GENERATION = "code_generation"      # 代码生成
    GENERAL_CHAT = "general_chat"            # 通用对话
    SIMPLE_QA = "simple_qa"                  # 简单问答
    CLASSIFICATION = "classification"        # 分类任务
    EXTRACTION = "extraction"               # 信息提取
    SUMMARIZATION = "summarization"          # 摘要
    TRANSLATION = "translation"             # 翻译

@dataclass
class RoutingContext:
    """路由上下文 —— 路由器的输入"""
    task_type: TaskType
    input_text: str
    input_tokens: int
    requires_tool_use: bool = False
    requires_structured_output: bool = False
    user_tier: str = "standard"             # 用户等级（VIP 用好模型）
    latency_requirement_ms: Optional[int] = None  # 延迟要求
    budget_remaining: Optional[float] = None      # 剩余预算

@dataclass 
class RoutingDecision:
    """路由决策 —— 路由器的输出"""
    model: str
    reason: str
    fallback_model: Optional[str] = None
    estimated_cost: Optional[float] = None

class ModelRouter:
    """多模型路由器"""
    
    def __init__(self, registry: dict, default_model: str = "claude-sonnet"):
        self.registry = registry
        self.default_model = default_model
        self.cost_tracker = CostTracker()
    
    def route(self, ctx: RoutingContext) -> RoutingDecision:
        """
        核心路由逻辑。
        
        路由优先级：
        1. 硬约束检查（上下文长度、预算）
        2. 任务类型匹配
        3. 复杂度评估
        4. 成本优化
        """
        # Step 1: 硬约束过滤
        candidates = self._filter_by_constraints(ctx)
        
        # Step 2: 任务类型路由（规则层）
        decision = self._rule_based_route(ctx, candidates)
        if decision:
            return decision
        
        # Step 3: 复杂度评估路由
        complexity = self._assess_complexity(ctx)
        decision = self._complexity_based_route(ctx, complexity, candidates)
        
        # Step 4: 附加 fallback
        decision.fallback_model = self._select_fallback(decision.model)
        decision.estimated_cost = self._estimate_cost(ctx, decision.model)
        
        return decision
    
    def _filter_by_constraints(self, ctx: RoutingContext) -> list[str]:
        """硬约束过滤：排除不满足基本要求的模型"""
        candidates = []
        for name, config in self.registry.items():
            # 上下文长度检查
            if ctx.input_tokens > config.max_context * 0.8:
                continue
            # 延迟要求检查
            if ctx.latency_requirement_ms and config.latency_p50_ms > ctx.latency_requirement_ms:
                continue
            # 预算检查
            if ctx.budget_remaining is not None:
                estimated = (ctx.input_tokens * config.input_price) / 1_000_000
                if estimated > ctx.budget_remaining * 0.5:  # 单次不超过剩余预算的50%
                    continue
            candidates.append(name)
        return candidates if candidates else [self.default_model]
    
    def _rule_based_route(self, ctx: RoutingContext, candidates: list) -> Optional[RoutingDecision]:
        """规则路由：简单明确的任务直接分配"""
        
        # 分类和提取 → 小模型
        if ctx.task_type in (TaskType.CLASSIFICATION, TaskType.EXTRACTION):
            model = "claude-haiku" if "claude-haiku" in candidates else candidates[0]
            return RoutingDecision(model=model, reason="分类/提取任务，使用轻量模型")
        
        # 复杂推理 → 强模型
        if ctx.task_type == TaskType.COMPLEX_REASONING:
            model = "claude-opus" if "claude-opus" in candidates else "deepseek-r1"
            return RoutingDecision(model=model, reason="复杂推理任务，需要强模型")
        
        # 简单中文闲聊 → 经济模型
        if ctx.task_type == TaskType.SIMPLE_QA and self._is_chinese(ctx.input_text):
            model = "doubao-pro" if "doubao-pro" in candidates else "claude-haiku"
            return RoutingDecision(model=model, reason="简单中文问答，使用经济模型")
        
        return None  # 无法通过规则判断，进入复杂度评估
    
    def _assess_complexity(self, ctx: RoutingContext) -> str:
        """
        复杂度评估 —— 不调用 LLM，纯规则判断。
        
        评估维度：
        - 输入长度
        - 是否需要工具调用
        - 是否需要多步骤推理
        - 问题领域
        """
        score = 0
        
        # 输入长度
        if ctx.input_tokens > 5000:
            score += 2
        elif ctx.input_tokens > 1000:
            score += 1
        
        # 工具调用需求
        if ctx.requires_tool_use:
            score += 2
        
        # 结构化输出需求
        if ctx.requires_structured_output:
            score += 1
        
        # 关键词检测（多步推理信号）
        reasoning_signals = ["分析", "比较", "为什么", "怎样", "设计", "方案", "优化",
                            "analyze", "compare", "design", "implement", "debug"]
        if any(signal in ctx.input_text.lower() for signal in reasoning_signals):
            score += 2
        
        if score >= 5:
            return "high"
        elif score >= 3:
            return "medium"
        else:
            return "low"
    
    def _complexity_based_route(self, ctx: RoutingContext, complexity: str, 
                                 candidates: list) -> RoutingDecision:
        """基于复杂度的路由"""
        tier_map = {
            "high": ModelTier.PREMIUM,
            "medium": ModelTier.STANDARD,
            "low": ModelTier.ECONOMY,
        }
        target_tier = tier_map[complexity]
        
        # 在候选中找最匹配 tier 的模型
        for name in candidates:
            if self.registry[name].tier == target_tier:
                return RoutingDecision(
                    model=name,
                    reason=f"复杂度={complexity}，选择 {target_tier.value} 级别模型"
                )
        
        # fallback to default
        return RoutingDecision(model=self.default_model, reason="默认路由")
    
    def _select_fallback(self, primary: str) -> str:
        """选择 fallback 模型（不同 provider，避免单点故障）"""
        primary_provider = self.registry[primary].provider
        for name, config in self.registry.items():
            if config.provider != primary_provider and config.tier.value <= self.registry[primary].tier.value:
                return name
        return self.default_model
    
    def _estimate_cost(self, ctx: RoutingContext, model: str) -> float:
        """估算本次调用成本（美元）"""
        config = self.registry[model]
        # 假设输出 tokens 约为输入的 0.5 倍（可根据历史数据调整）
        estimated_output = ctx.input_tokens * 0.5
        cost = (ctx.input_tokens * config.input_price + estimated_output * config.output_price) / 1_000_000
        return round(cost, 6)
    
    def _is_chinese(self, text: str) -> bool:
        """检测是否为中文文本"""
        chinese_chars = len(re.findall(r'[一-鿿]', text))
        return chinese_chars / max(len(text), 1) > 0.3
```

### 2.5 降级与 Fallback 机制

```python
# router/fallback.py

import asyncio
from typing import Optional
import time

class FallbackExecutor:
    """带降级策略的 LLM 调用执行器"""
    
    def __init__(self, router: ModelRouter, max_retries: int = 2):
        self.router = router
        self.max_retries = max_retries
        self.circuit_breakers: dict[str, CircuitBreaker] = {}
    
    async def execute(self, ctx: RoutingContext, prompt: str) -> LLMResponse:
        """执行 LLM 调用，带自动降级"""
        
        decision = self.router.route(ctx)
        models_to_try = [decision.model]
        if decision.fallback_model:
            models_to_try.append(decision.fallback_model)
        
        last_error = None
        for model in models_to_try:
            # 熔断器检查
            breaker = self._get_breaker(model)
            if breaker.is_open:
                continue
            
            for attempt in range(self.max_retries):
                try:
                    response = await self._call_llm(model, prompt, ctx)
                    breaker.record_success()
                    return response
                except RateLimitError:
                    await asyncio.sleep(2 ** attempt)  # 指数退避
                except ModelUnavailableError as e:
                    breaker.record_failure()
                    last_error = e
                    break  # 跳到下一个模型
                except TimeoutError:
                    last_error = TimeoutError(f"{model} timeout")
                    continue  # 重试当前模型
        
        raise AllModelsFailedError(f"所有模型不可用: {last_error}")
    
    def _get_breaker(self, model: str) -> "CircuitBreaker":
        if model not in self.circuit_breakers:
            self.circuit_breakers[model] = CircuitBreaker(
                failure_threshold=5,
                recovery_timeout=60,
            )
        return self.circuit_breakers[model]


class CircuitBreaker:
    """熔断器 —— 连续失败 N 次后短路，避免雪崩"""
    
    def __init__(self, failure_threshold: int = 5, recovery_timeout: int = 60):
        self.failure_threshold = failure_threshold
        self.recovery_timeout = recovery_timeout
        self.failure_count = 0
        self.last_failure_time = 0
        self.is_open = False
    
    def record_failure(self):
        self.failure_count += 1
        self.last_failure_time = time.time()
        if self.failure_count >= self.failure_threshold:
            self.is_open = True
    
    def record_success(self):
        self.failure_count = 0
        self.is_open = False
    
    @property
    def is_open(self) -> bool:
        if self._is_open and (time.time() - self.last_failure_time > self.recovery_timeout):
            self._is_open = False  # 恢复尝试
            self.failure_count = 0
        return self._is_open
    
    @is_open.setter
    def is_open(self, value: bool):
        self._is_open = value
```

### 2.6 成本监控与预算控制

```python
# router/cost_tracker.py

from collections import defaultdict
from datetime import datetime, timedelta

class CostTracker:
    """成本追踪器"""
    
    def __init__(self, daily_budget: float = 100.0, alert_threshold: float = 0.8):
        self.daily_budget = daily_budget
        self.alert_threshold = alert_threshold
        self.records: list[CostRecord] = []
    
    def record(self, model: str, input_tokens: int, output_tokens: int, 
               user_id: str, task_type: str):
        """记录一次调用的成本"""
        config = MODEL_REGISTRY[model]
        cost = (input_tokens * config.input_price + output_tokens * config.output_price) / 1_000_000
        
        self.records.append(CostRecord(
            timestamp=datetime.now(),
            model=model,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
            cost=cost,
            user_id=user_id,
            task_type=task_type,
        ))
        
        # 预算告警
        daily_total = self.get_daily_cost()
        if daily_total > self.daily_budget * self.alert_threshold:
            self._send_alert(daily_total)
    
    def get_daily_cost(self) -> float:
        """今日总成本"""
        today = datetime.now().date()
        return sum(r.cost for r in self.records if r.timestamp.date() == today)
    
    def get_cost_breakdown(self, days: int = 7) -> dict:
        """成本分析报告"""
        cutoff = datetime.now() - timedelta(days=days)
        recent = [r for r in self.records if r.timestamp > cutoff]
        
        return {
            "total_cost": sum(r.cost for r in recent),
            "by_model": self._group_cost(recent, "model"),
            "by_task_type": self._group_cost(recent, "task_type"),
            "by_user": self._group_cost(recent, "user_id"),
            "avg_cost_per_request": sum(r.cost for r in recent) / max(len(recent), 1),
            "total_requests": len(recent),
        }
```

---

## 3. Prompt 工程体系

### 3.1 核心理念

> **Prompt 是代码，不是随手写的字符串。**
> 它需要版本管理、测试、review、灰度发布。

### 3.2 Prompt 版本管理

**目录结构：**

```
prompts/
├── customer_service/              # 业务域
│   ├── intent_classification/     # 具体任务
│   │   ├── v1.yaml               # 版本1
│   │   ├── v2.yaml               # 版本2（当前线上）
│   │   ├── v3.yaml               # 版本3（实验中）
│   │   └── eval/                 # 该 prompt 专属测试集
│   │       ├── dataset.jsonl
│   │       └── config.yaml
│   ├── order_handling/
│   │   ├── v1.yaml
│   │   └── eval/
│   └── _shared/                  # 共享片段
│       ├── safety_guidelines.yaml
│       └── output_format.yaml
├── code_assistant/
│   └── ...
└── _base/                         # 基础模板
    ├── system_base.yaml
    └── few_shot_base.yaml
```

### 3.3 Prompt 模板格式

```yaml
# prompts/customer_service/intent_classification/v2.yaml

metadata:
  name: "intent_classification"
  version: "2.1.0"
  author: "zhangsan"
  created: "2024-03-15"
  updated: "2024-06-20"
  status: "production"            # draft | staging | production | deprecated
  model_requirement: "economy+"   # 最低模型等级
  description: "客服意图分类 - 识别用户意图并路由到对应处理流程"
  changelog:
    - "2.1.0: 增加退换货子类型识别"
    - "2.0.0: 重构为结构化输出"
    - "1.0.0: 初始版本"

# 模板变量定义
variables:
  - name: "categories"
    type: "list"
    description: "可用的意图类别"
    required: true
  - name: "conversation_history"
    type: "string"
    description: "历史对话（最近3轮）"
    required: false
    default: ""
  - name: "user_profile"
    type: "object"
    description: "用户画像信息"
    required: false

# Prompt 模板
system_prompt: |
  你是一个客服意图分类器。根据用户的输入，判断其意图类别。

  ## 可用类别
  {{categories}}

  ## 规则
  1. 只能输出上述类别中的一个
  2. 如果无法判断，输出 "unknown"
  3. 必须按 JSON 格式输出

  ## 输出格式
  ```json
  {
    "intent": "类别名",
    "confidence": 0.0-1.0,
    "reasoning": "一句话解释"
  }
  ```

  {% if user_profile %}
  ## 用户信息
  - 用户等级: {{user_profile.tier}}
  - 最近订单: {{user_profile.recent_order}}
  {% endif %}

user_prompt: |
  {% if conversation_history %}
  历史对话:
  {{conversation_history}}
  ---
  {% endif %}
  
  用户最新输入: {{user_input}}

# Few-shot 示例
few_shot_examples:
  - input: "我买的手机屏幕碎了"
    output: '{"intent": "after_sales_repair", "confidence": 0.95, "reasoning": "手机物理损坏，属于售后维修"}'
  - input: "订单什么时候到"
    output: '{"intent": "logistics_query", "confidence": 0.92, "reasoning": "询问物流进度"}'
  - input: "你好呀"
    output: '{"intent": "greeting", "confidence": 0.98, "reasoning": "简单打招呼"}'

# 输出约束
output_schema:
  type: "object"
  properties:
    intent:
      type: "string"
      enum: "{{categories}}"
    confidence:
      type: "number"
      minimum: 0
      maximum: 1
    reasoning:
      type: "string"
      maxLength: 100
  required: ["intent", "confidence"]
```

### 3.4 Prompt 引擎实现

```python
# prompt_engine/engine.py

import yaml
import jinja2
from pathlib import Path
from typing import Any, Optional

class PromptEngine:
    """Prompt 模板引擎"""
    
    def __init__(self, prompts_dir: str = "prompts"):
        self.prompts_dir = Path(prompts_dir)
        self.jinja_env = jinja2.Environment(
            undefined=jinja2.StrictUndefined,  # 未定义变量报错而非静默忽略
        )
        self._cache: dict[str, dict] = {}
    
    def load(self, domain: str, task: str, version: str = "latest") -> "PromptTemplate":
        """
        加载 prompt 模板。
        
        Args:
            domain: 业务域（如 "customer_service"）
            task: 任务名（如 "intent_classification"）
            version: 版本号或 "latest"（自动取最新的 production 版本）
        """
        cache_key = f"{domain}/{task}/{version}"
        if cache_key in self._cache:
            return PromptTemplate(self._cache[cache_key], self.jinja_env)
        
        task_dir = self.prompts_dir / domain / task
        
        if version == "latest":
            # 找最新的 production 版本
            yaml_file = self._find_latest_production(task_dir)
        else:
            yaml_file = task_dir / f"{version}.yaml"
        
        with open(yaml_file) as f:
            config = yaml.safe_load(f)
        
        self._cache[cache_key] = config
        return PromptTemplate(config, self.jinja_env)
    
    def _find_latest_production(self, task_dir: Path) -> Path:
        """找到最新的 production 状态版本"""
        versions = []
        for f in task_dir.glob("v*.yaml"):
            with open(f) as fp:
                meta = yaml.safe_load(fp).get("metadata", {})
                if meta.get("status") == "production":
                    versions.append((meta.get("version", "0"), f))
        
        if not versions:
            raise FileNotFoundError(f"No production prompt in {task_dir}")
        
        versions.sort(key=lambda x: x[0], reverse=True)
        return versions[0][1]


class PromptTemplate:
    """编译后的 Prompt 模板"""
    
    def __init__(self, config: dict, jinja_env: jinja2.Environment):
        self.config = config
        self.metadata = config.get("metadata", {})
        self.variables = config.get("variables", [])
        self.jinja_env = jinja_env
    
    def render(self, **kwargs) -> dict[str, str]:
        """
        渲染 prompt。
        
        Returns:
            {"system": "...", "user": "...", "few_shot": [...]}
        """
        # 校验必填变量
        self._validate_variables(kwargs)
        
        # 渲染 system prompt
        system_template = self.jinja_env.from_string(self.config["system_prompt"])
        system = system_template.render(**kwargs)
        
        # 渲染 user prompt
        user_template = self.jinja_env.from_string(self.config["user_prompt"])
        user = user_template.render(**kwargs)
        
        # Few-shot 示例
        few_shot = self.config.get("few_shot_examples", [])
        
        return {
            "system": system,
            "user": user,
            "few_shot": few_shot,
            "output_schema": self.config.get("output_schema"),
            "model_requirement": self.metadata.get("model_requirement", "standard"),
        }
    
    def _validate_variables(self, kwargs: dict):
        """校验必填变量"""
        for var in self.variables:
            if var["required"] and var["name"] not in kwargs:
                raise ValueError(f"Missing required variable: {var['name']}")


# 使用示例
engine = PromptEngine("prompts")
template = engine.load("customer_service", "intent_classification")
rendered = template.render(
    categories=["greeting", "order_query", "refund", "complaint"],
    user_input="我要退货",
    user_profile={"tier": "vip", "recent_order": "ORD-12345"},
)
# rendered["system"]  → 完整的 system prompt
# rendered["user"]    → 完整的 user message
```

### 3.5 Prompt 生命周期管理

```
┌────────┐     ┌─────────┐     ┌────────────┐     ┌────────────┐
│ Draft  │────▶│ Staging │────▶│ Production │────▶│ Deprecated │
└────────┘     └─────────┘     └────────────┘     └────────────┘
     │              │                │
     ▼              ▼                ▼
  人工 review    跑 Eval 通过      线上监控 OK
  + 同行审核     + A/B Test 验证   + 新版本就绪
```

**状态流转规则：**

| 从 | 到 | 条件 |
|----|-----|------|
| draft | staging | PR review 通过 |
| staging | production | Eval 分数 ≥ baseline 且 A/B test 无劣化 |
| production | deprecated | 新版本上线后保留 7 天 |

### 3.6 Prompt 变更的 Git 规范

```bash
# 提交信息格式
git commit -m "prompt(customer_service/intent): v2.1.0 - 增加退换货子类型

- 新增 return_exchange 意图类别
- 增加 3 个 few-shot 示例
- eval score: 0.87 → 0.91 (+4.6%)

Eval run: https://eval.internal/runs/run_abc123"
```

---

## 4. Eval 评估体系

### 4.1 核心概念

```
┌─────────────────────────────────────────────────────────────────┐
│                       Eval System 架构                           │
│                                                                 │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐ │
│  │ Dataset  │───▶│  Runner  │───▶│  Scorer  │───▶│  Report  │ │
│  │          │    │          │    │          │    │          │ │
│  │ 测试用例  │    │ 执行调用  │    │ 评分打分  │    │ 结果报告  │ │
│  └──────────┘    └──────────┘    └──────────┘    └──────────┘ │
│       │                                               │        │
│       │          ┌──────────┐                         │        │
│       └─────────▶│ Baseline │◀────────────────────────┘        │
│                  │ 基准分数  │                                   │
│                  └──────────┘                                   │
└─────────────────────────────────────────────────────────────────┘
```

| 概念 | 类比后端 | 说明 |
|------|---------|------|
| **Dataset** | Test fixtures | 测试用例集合 |
| **Runner** | Test runner | 执行 LLM 调用 |
| **Scorer** | Assert 断言 | 给输出打分 |
| **Metric** | Coverage % | 聚合评分指标 |
| **Run** | Test run | 一次完整评估 |
| **Baseline** | Snapshot test | 基准分数，用于对比 |

### 4.2 Dataset 设计

```python
# eval/dataset.py

from dataclasses import dataclass, field
from typing import Any, Optional
import json

@dataclass
class EvalCase:
    """单个测试用例"""
    id: str                          # 唯一标识
    input: str                       # 用户输入
    expected: Any                    # 期望输出（可以是多种形式）
    metadata: dict = field(default_factory=dict)  # 元信息
    tags: list[str] = field(default_factory=list)  # 标签（用于过滤子集）
    
    # 期望输出的多种形式
    expected_exact: Optional[str] = None      # 精确匹配
    expected_contains: list[str] = field(default_factory=list)  # 包含检查
    expected_not_contains: list[str] = field(default_factory=list)  # 不应包含
    expected_schema: Optional[dict] = None     # JSON Schema 校验
    expected_tool_calls: list[str] = field(default_factory=list)  # 应调用的工具
    reference_answer: Optional[str] = None     # 参考答案（用于相似度/LLM判分）
    
@dataclass
class EvalDataset:
    """测试数据集"""
    name: str
    description: str
    cases: list[EvalCase]
    version: str = "1.0"
    
    @classmethod
    def from_jsonl(cls, path: str, name: str = "") -> "EvalDataset":
        """从 JSONL 文件加载"""
        cases = []
        with open(path) as f:
            for line in f:
                data = json.loads(line)
                cases.append(EvalCase(**data))
        return cls(name=name or path, description="", cases=cases)
    
    def filter_by_tag(self, tag: str) -> "EvalDataset":
        """按标签过滤子集"""
        filtered = [c for c in self.cases if tag in c.tags]
        return EvalDataset(
            name=f"{self.name}[{tag}]",
            description=self.description,
            cases=filtered,
            version=self.version,
        )
```

**Dataset JSONL 文件示例：**

```jsonl
{"id": "refund_001", "input": "我买的衣服大了想退", "expected_exact": "refund", "tags": ["refund", "simple"]}
{"id": "refund_002", "input": "三个月前买的能退吗", "expected_contains": ["超出", "期限"], "tags": ["refund", "edge_case"]}
{"id": "chat_001", "input": "你好", "expected_exact": "greeting", "expected_not_contains": ["订单", "退款"], "tags": ["chat"]}
{"id": "complex_001", "input": "我在你们app上买了个手机，快递说送到了但我没收到，而且我之前已经申请过一次退款被拒了", "reference_answer": "...", "expected_tool_calls": ["query_order", "query_logistics"], "tags": ["complex", "multi_step"]}
```

### 4.3 Scorer（评分函数）实现

```python
# eval/scorers.py

from abc import ABC, abstractmethod
from dataclasses import dataclass
import json
import re
from typing import Any

@dataclass
class ScoreResult:
    """评分结果"""
    score: float          # 0.0 ~ 1.0
    passed: bool          # 是否通过（score >= threshold）
    reason: str           # 评分原因
    details: dict = None  # 详细信息

class BaseScorer(ABC):
    """评分器基类"""
    
    def __init__(self, weight: float = 1.0, threshold: float = 0.5):
        self.weight = weight
        self.threshold = threshold
    
    @abstractmethod
    def score(self, output: str, case: EvalCase) -> ScoreResult:
        pass


class ExactMatchScorer(BaseScorer):
    """精确匹配评分"""
    
    def score(self, output: str, case: EvalCase) -> ScoreResult:
        if not case.expected_exact:
            return ScoreResult(score=1.0, passed=True, reason="no expected_exact defined")
        
        match = output.strip().lower() == case.expected_exact.strip().lower()
        return ScoreResult(
            score=1.0 if match else 0.0,
            passed=match,
            reason=f"Expected '{case.expected_exact}', got '{output[:50]}'"
        )


class ContainsScorer(BaseScorer):
    """包含检查评分"""
    
    def score(self, output: str, case: EvalCase) -> ScoreResult:
        if not case.expected_contains:
            return ScoreResult(score=1.0, passed=True, reason="no expected_contains defined")
        
        hits = sum(1 for keyword in case.expected_contains if keyword in output)
        total = len(case.expected_contains)
        score = hits / total
        
        missing = [k for k in case.expected_contains if k not in output]
        return ScoreResult(
            score=score,
            passed=score >= self.threshold,
            reason=f"Contains {hits}/{total} keywords. Missing: {missing}"
        )


class NotContainsScorer(BaseScorer):
    """不应包含检查"""
    
    def score(self, output: str, case: EvalCase) -> ScoreResult:
        if not case.expected_not_contains:
            return ScoreResult(score=1.0, passed=True, reason="no expected_not_contains defined")
        
        violations = [k for k in case.expected_not_contains if k in output]
        score = 1.0 if not violations else 0.0
        
        return ScoreResult(
            score=score,
            passed=not violations,
            reason=f"Violations: {violations}" if violations else "No violations"
        )


class JsonSchemaScorer(BaseScorer):
    """JSON Schema 校验评分"""
    
    def score(self, output: str, case: EvalCase) -> ScoreResult:
        if not case.expected_schema:
            return ScoreResult(score=1.0, passed=True, reason="no schema defined")
        
        try:
            # 提取 JSON（可能在 markdown code block 中）
            json_str = self._extract_json(output)
            parsed = json.loads(json_str)
            
            # 简单 schema 校验（生产中用 jsonschema 库）
            errors = self._validate(parsed, case.expected_schema)
            
            if not errors:
                return ScoreResult(score=1.0, passed=True, reason="Valid JSON matching schema")
            else:
                return ScoreResult(score=0.5, passed=False, reason=f"Schema errors: {errors}")
                
        except json.JSONDecodeError as e:
            return ScoreResult(score=0.0, passed=False, reason=f"Invalid JSON: {e}")
    
    def _extract_json(self, text: str) -> str:
        """从文本中提取 JSON"""
        # 尝试 markdown code block
        match = re.search(r'```(?:json)?\s*\n?(.*?)\n?```', text, re.DOTALL)
        if match:
            return match.group(1)
        # 尝试直接解析
        return text.strip()
    
    def _validate(self, data: dict, schema: dict) -> list[str]:
        """简化的 schema 校验"""
        errors = []
        required = schema.get("required", [])
        for field in required:
            if field not in data:
                errors.append(f"Missing required field: {field}")
        return errors


class LLMJudgeScorer(BaseScorer):
    """
    LLM-as-Judge 评分。
    
    用另一个 LLM 对输出质量打分。
    适用于开放式生成，无法用规则评估的场景。
    """
    
    def __init__(self, judge_model: str = "claude-sonnet", **kwargs):
        super().__init__(**kwargs)
        self.judge_model = judge_model
        self.judge_prompt_template = """
你是一个严格的评估专家。请对以下AI助手的回答质量打分。

## 评分标准
- 准确性 (0-1): 信息是否正确，是否有幻觉
- 完整性 (0-1): 是否回答了用户的问题
- 相关性 (0-1): 是否紧扣问题，没有跑题
- 简洁性 (0-1): 是否简洁有效，没有废话

## 用户问题
{question}

## 参考答案（如有）
{reference}

## AI助手回答
{output}

## 输出格式（严格 JSON）
{{"accuracy": 0.0, "completeness": 0.0, "relevance": 0.0, "conciseness": 0.0, "overall": 0.0, "reasoning": "..."}}
"""
    
    def score(self, output: str, case: EvalCase) -> ScoreResult:
        if not case.reference_answer:
            return ScoreResult(score=1.0, passed=True, reason="no reference for LLM judge")
        
        judge_prompt = self.judge_prompt_template.format(
            question=case.input,
            reference=case.reference_answer or "无参考答案",
            output=output,
        )
        
        # 调用 judge 模型
        judge_response = call_llm(self.judge_model, judge_prompt)
        scores = json.loads(judge_response)
        
        overall = scores["overall"]
        return ScoreResult(
            score=overall,
            passed=overall >= self.threshold,
            reason=scores["reasoning"],
            details=scores,
        )


class ToolCallScorer(BaseScorer):
    """工具调用评分 —— 检查 Agent 是否调用了正确的工具"""
    
    def score(self, output: str, case: EvalCase, tool_calls: list[str] = None) -> ScoreResult:
        if not case.expected_tool_calls:
            return ScoreResult(score=1.0, passed=True, reason="no expected tools")
        
        if tool_calls is None:
            return ScoreResult(score=0.0, passed=False, reason="no tool calls recorded")
        
        expected = set(case.expected_tool_calls)
        actual = set(tool_calls)
        
        correct = expected & actual
        missing = expected - actual
        extra = actual - expected  # 多余的调用（可能无害但浪费 token）
        
        precision = len(correct) / max(len(actual), 1)
        recall = len(correct) / max(len(expected), 1)
        f1 = 2 * precision * recall / max(precision + recall, 0.001)
        
        return ScoreResult(
            score=f1,
            passed=f1 >= self.threshold,
            reason=f"Correct: {correct}, Missing: {missing}, Extra: {extra}",
            details={"precision": precision, "recall": recall, "f1": f1},
        )
```

### 4.4 Eval Runner（评估执行器）

```python
# eval/runner.py

import asyncio
import time
from dataclasses import dataclass, field
from datetime import datetime
from typing import Optional

@dataclass
class EvalRunConfig:
    """评估运行配置"""
    dataset: EvalDataset
    prompt_template: PromptTemplate
    model: str                         # 使用哪个模型跑 eval
    scorers: list[BaseScorer]          # 评分器列表
    concurrency: int = 5               # 并发数
    timeout_per_case: int = 30         # 单个用例超时（秒）
    tags_filter: Optional[str] = None  # 只跑某个 tag

@dataclass
class CaseResult:
    """单个用例的评估结果"""
    case_id: str
    input: str
    output: str
    scores: dict[str, ScoreResult]     # scorer_name → ScoreResult
    overall_score: float
    passed: bool
    latency_ms: int
    tokens_used: int

@dataclass
class EvalRunResult:
    """一次评估运行的完整结果"""
    run_id: str
    timestamp: datetime
    config_summary: str
    results: list[CaseResult]
    
    # 聚合指标
    overall_score: float = 0.0
    pass_rate: float = 0.0
    total_cases: int = 0
    passed_cases: int = 0
    failed_cases: int = 0
    avg_latency_ms: float = 0.0
    total_cost: float = 0.0
    
    def compute_aggregates(self):
        """计算聚合指标"""
        self.total_cases = len(self.results)
        self.passed_cases = sum(1 for r in self.results if r.passed)
        self.failed_cases = self.total_cases - self.passed_cases
        self.pass_rate = self.passed_cases / max(self.total_cases, 1)
        self.overall_score = sum(r.overall_score for r in self.results) / max(self.total_cases, 1)
        self.avg_latency_ms = sum(r.latency_ms for r in self.results) / max(self.total_cases, 1)


class EvalRunner:
    """评估执行器"""
    
    def __init__(self):
        self.history: list[EvalRunResult] = []
    
    async def run(self, config: EvalRunConfig) -> EvalRunResult:
        """执行一次完整的 eval"""
        
        dataset = config.dataset
        if config.tags_filter:
            dataset = dataset.filter_by_tag(config.tags_filter)
        
        print(f"🚀 Starting eval: {dataset.name} ({len(dataset.cases)} cases)")
        print(f"   Model: {config.model}")
        print(f"   Scorers: {[type(s).__name__ for s in config.scorers]}")
        
        # 并发执行所有用例
        semaphore = asyncio.Semaphore(config.concurrency)
        tasks = [
            self._run_single(case, config, semaphore) 
            for case in dataset.cases
        ]
        results = await asyncio.gather(*tasks)
        
        # 组装结果
        run_result = EvalRunResult(
            run_id=f"run_{int(time.time())}",
            timestamp=datetime.now(),
            config_summary=f"{dataset.name} | {config.model} | {len(config.scorers)} scorers",
            results=results,
        )
        run_result.compute_aggregates()
        
        self.history.append(run_result)
        self._print_summary(run_result)
        
        return run_result
    
    async def _run_single(self, case: EvalCase, config: EvalRunConfig, 
                          semaphore: asyncio.Semaphore) -> CaseResult:
        """执行单个用例"""
        async with semaphore:
            start = time.time()
            
            try:
                # 渲染 prompt
                rendered = config.prompt_template.render(user_input=case.input)
                
                # 调用 LLM
                response = await call_llm_async(
                    model=config.model,
                    system=rendered["system"],
                    user=rendered["user"],
                    timeout=config.timeout_per_case,
                )
                output = response.text
                tokens = response.total_tokens
                
            except Exception as e:
                output = f"[ERROR] {e}"
                tokens = 0
            
            latency = int((time.time() - start) * 1000)
            
            # 用所有 scorer 打分
            scores = {}
            weighted_sum = 0
            total_weight = 0
            
            for scorer in config.scorers:
                scorer_name = type(scorer).__name__
                result = scorer.score(output, case)
                scores[scorer_name] = result
                weighted_sum += result.score * scorer.weight
                total_weight += scorer.weight
            
            overall = weighted_sum / max(total_weight, 0.001)
            
            return CaseResult(
                case_id=case.id,
                input=case.input,
                output=output,
                scores=scores,
                overall_score=overall,
                passed=overall >= 0.7,  # 全局通过阈值
                latency_ms=latency,
                tokens_used=tokens,
            )
    
    def _print_summary(self, result: EvalRunResult):
        """打印结果摘要"""
        print(f"\n{'='*60}")
        print(f"📊 Eval Run: {result.run_id}")
        print(f"{'='*60}")
        print(f"  Overall Score:  {result.overall_score:.2%}")
        print(f"  Pass Rate:      {result.pass_rate:.2%} ({result.passed_cases}/{result.total_cases})")
        print(f"  Avg Latency:    {result.avg_latency_ms:.0f}ms")
        print(f"  Total Cost:     ${result.total_cost:.4f}")
        print(f"{'='*60}")
        
        # 打印失败用例
        failures = [r for r in result.results if not r.passed]
        if failures:
            print(f"\n❌ Failed cases ({len(failures)}):")
            for f in failures[:5]:  # 只显示前5个
                print(f"  [{f.case_id}] score={f.overall_score:.2f}")
                print(f"    Input: {f.input[:80]}...")
                print(f"    Output: {f.output[:80]}...")
                for name, score in f.scores.items():
                    if not score.passed:
                        print(f"    ❌ {name}: {score.reason}")
```

### 4.5 Baseline 管理与对比

```python
# eval/baseline.py

import json
from pathlib import Path
from datetime import datetime

class BaselineManager:
    """
    基准线管理器。
    
    类比：snapshot testing。
    - 每次 eval 跑完，可以选择"保存为 baseline"
    - 下次跑完后，自动与 baseline 对比
    - 只有分数 >= baseline 才允许合并
    """
    
    BASELINE_DIR = Path("eval/baselines")
    
    def save_baseline(self, prompt_key: str, run_result: EvalRunResult):
        """保存基准线"""
        self.BASELINE_DIR.mkdir(parents=True, exist_ok=True)
        
        baseline = {
            "prompt_key": prompt_key,
            "saved_at": datetime.now().isoformat(),
            "run_id": run_result.run_id,
            "overall_score": run_result.overall_score,
            "pass_rate": run_result.pass_rate,
            "per_case_scores": {
                r.case_id: r.overall_score for r in run_result.results
            },
            "avg_latency_ms": run_result.avg_latency_ms,
        }
        
        path = self.BASELINE_DIR / f"{prompt_key}.json"
        with open(path, "w") as f:
            json.dump(baseline, f, indent=2, ensure_ascii=False)
        
        print(f"✅ Baseline saved: {path} (score={run_result.overall_score:.2%})")
    
    def compare(self, prompt_key: str, new_result: EvalRunResult) -> "ComparisonReport":
        """与基准线对比"""
        path = self.BASELINE_DIR / f"{prompt_key}.json"
        
        if not path.exists():
            return ComparisonReport(
                has_baseline=False,
                message="No baseline found. Run with --save-baseline to create one.",
            )
        
        with open(path) as f:
            baseline = json.load(f)
        
        # 整体对比
        score_diff = new_result.overall_score - baseline["overall_score"]
        pass_rate_diff = new_result.pass_rate - baseline["pass_rate"]
        
        # 逐用例对比（找出回退的用例）
        regressions = []
        improvements = []
        
        for case_result in new_result.results:
            old_score = baseline["per_case_scores"].get(case_result.case_id)
            if old_score is None:
                continue
            
            diff = case_result.overall_score - old_score
            if diff < -0.1:  # 分数下降超过 10%
                regressions.append({
                    "case_id": case_result.case_id,
                    "old_score": old_score,
                    "new_score": case_result.overall_score,
                    "diff": diff,
                })
            elif diff > 0.1:
                improvements.append({
                    "case_id": case_result.case_id,
                    "old_score": old_score,
                    "new_score": case_result.overall_score,
                    "diff": diff,
                })
        
        passed = score_diff >= -0.02  # 允许 2% 的波动
        
        return ComparisonReport(
            has_baseline=True,
            passed=passed,
            overall_score_old=baseline["overall_score"],
            overall_score_new=new_result.overall_score,
            score_diff=score_diff,
            pass_rate_diff=pass_rate_diff,
            regressions=regressions,
            improvements=improvements,
            message=self._format_message(score_diff, regressions, improvements, passed),
        )
    
    def _format_message(self, score_diff, regressions, improvements, passed) -> str:
        status = "✅ PASSED" if passed else "❌ FAILED"
        msg = f"\n{status} Baseline Comparison\n"
        msg += f"  Score change: {score_diff:+.2%}\n"
        
        if improvements:
            msg += f"  📈 Improvements: {len(improvements)} cases\n"
            for imp in improvements[:3]:
                msg += f"     [{imp['case_id']}] {imp['old_score']:.2f} → {imp['new_score']:.2f}\n"
        
        if regressions:
            msg += f"  📉 Regressions: {len(regressions)} cases\n"
            for reg in regressions[:3]:
                msg += f"     [{reg['case_id']}] {reg['old_score']:.2f} → {reg['new_score']:.2f}\n"
        
        return msg


@dataclass
class ComparisonReport:
    has_baseline: bool
    passed: bool = True
    overall_score_old: float = 0
    overall_score_new: float = 0
    score_diff: float = 0
    pass_rate_diff: float = 0
    regressions: list = field(default_factory=list)
    improvements: list = field(default_factory=list)
    message: str = ""
```

### 4.6 CI 集成：Prompt 改动自动跑 Eval

**GitHub Actions 配置：**

```yaml
# .github/workflows/prompt-eval.yml

name: Prompt Eval

on:
  pull_request:
    paths:
      - 'prompts/**'          # prompt 文件变更触发
      - 'eval/datasets/**'    # 测试集变更也触发

jobs:
  eval:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Setup Python
        uses: actions/setup-python@v5
        with:
          python-version: '3.11'
      
      - name: Install dependencies
        run: pip install -r requirements-eval.txt
      
      - name: Detect changed prompts
        id: changes
        run: |
          changed=$(git diff --name-only origin/main...HEAD -- prompts/ | grep '.yaml$' | head -20)
          echo "changed_prompts=$changed" >> $GITHUB_OUTPUT
      
      - name: Run Eval
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          DEEPSEEK_API_KEY: ${{ secrets.DEEPSEEK_API_KEY }}
        run: |
          python -m eval.cli run \
            --changed-only \
            --compare-baseline \
            --output eval_report.json
      
      - name: Check Baseline
        run: |
          python -m eval.cli check \
            --report eval_report.json \
            --fail-on-regression
      
      - name: Comment PR with Results
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const report = JSON.parse(fs.readFileSync('eval_report.json', 'utf8'));
            
            let body = '## 📊 Eval Results\n\n';
            body += `| Metric | Baseline | Current | Diff |\n`;
            body += `|--------|----------|---------|------|\n`;
            body += `| Score | ${report.baseline_score.toFixed(2)} | ${report.current_score.toFixed(2)} | ${report.diff > 0 ? '📈' : '📉'} ${report.diff.toFixed(2)} |\n`;
            body += `| Pass Rate | ${report.baseline_pass_rate.toFixed(2)} | ${report.current_pass_rate.toFixed(2)} | |\n\n`;
            
            if (report.regressions.length > 0) {
              body += '### ⚠️ Regressions\n\n';
              for (const reg of report.regressions) {
                body += `- \`${reg.case_id}\`: ${reg.old_score.toFixed(2)} → ${reg.new_score.toFixed(2)}\n`;
              }
            }
            
            await github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: body
            });
```

### 4.7 Eval CLI 工具

```python
# eval/cli.py

"""
Eval 命令行工具 —— 类似 pytest 的使用体验。

用法:
    python -m eval.cli run                            # 跑所有 eval
    python -m eval.cli run --prompt customer_service/intent  # 跑指定 prompt
    python -m eval.cli run --tag edge_case            # 只跑某个标签
    python -m eval.cli run --compare-baseline         # 与 baseline 对比
    python -m eval.cli run --save-baseline            # 保存当前结果为 baseline
    python -m eval.cli report --run-id run_xxx        # 查看历史报告
    python -m eval.cli diff run_abc run_def           # 对比两次运行
"""

import argparse
import asyncio

def main():
    parser = argparse.ArgumentParser(description="LLM Eval CLI")
    subparsers = parser.add_subparsers(dest="command")
    
    # run 子命令
    run_parser = subparsers.add_parser("run", help="执行评估")
    run_parser.add_argument("--prompt", help="指定 prompt key")
    run_parser.add_argument("--tag", help="按标签过滤用例")
    run_parser.add_argument("--model", help="覆盖模型选择")
    run_parser.add_argument("--compare-baseline", action="store_true")
    run_parser.add_argument("--save-baseline", action="store_true")
    run_parser.add_argument("--fail-on-regression", action="store_true")
    run_parser.add_argument("--output", help="输出报告路径")
    run_parser.add_argument("--concurrency", type=int, default=5)
    
    # diff 子命令
    diff_parser = subparsers.add_parser("diff", help="对比两次运行")
    diff_parser.add_argument("run_a", help="第一次运行 ID")
    diff_parser.add_argument("run_b", help="第二次运行 ID")
    
    args = parser.parse_args()
    
    if args.command == "run":
        asyncio.run(run_eval(args))
    elif args.command == "diff":
        diff_runs(args)

async def run_eval(args):
    """执行评估的主流程"""
    engine = PromptEngine()
    runner = EvalRunner()
    baseline_mgr = BaselineManager()
    
    # 加载 prompt 和 dataset
    prompt_key = args.prompt or detect_changed_prompts()
    template = engine.load(*prompt_key.split("/"))
    dataset = EvalDataset.from_jsonl(f"prompts/{prompt_key}/eval/dataset.jsonl")
    
    # 配置 scorer
    scorers = [
        ExactMatchScorer(weight=2.0),
        ContainsScorer(weight=1.5),
        NotContainsScorer(weight=1.0),
        JsonSchemaScorer(weight=1.0),
    ]
    
    # 执行
    config = EvalRunConfig(
        dataset=dataset,
        prompt_template=template,
        model=args.model or "claude-sonnet",
        scorers=scorers,
        concurrency=args.concurrency,
        tags_filter=args.tag,
    )
    
    result = await runner.run(config)
    
    # Baseline 对比
    if args.compare_baseline:
        report = baseline_mgr.compare(prompt_key, result)
        print(report.message)
        
        if args.fail_on_regression and not report.passed:
            exit(1)  # CI 失败
    
    # 保存 baseline
    if args.save_baseline:
        baseline_mgr.save_baseline(prompt_key, result)
    
    # 输出报告
    if args.output:
        save_report(result, args.output)

if __name__ == "__main__":
    main()
```

### 4.8 评估维度设计指南

```
┌──────────────────────────────────────────────────────────────┐
│                    评估维度选择指南                             │
├──────────────┬───────────────────────────────────────────────┤
│  任务类型     │  推荐评估维度                                  │
├──────────────┼───────────────────────────────────────────────┤
│  分类/意图    │  accuracy, precision, recall, F1              │
│  信息提取    │  exact_match, partial_match, schema_valid      │
│  摘要/生成   │  LLM-judge(准确性,完整性,流畅性), ROUGE        │
│  对话/客服   │  intent_correct, tool_use_correct,            │
│             │  response_helpful(LLM-judge), no_hallucination │
│  代码生成    │  syntax_valid, test_pass, efficiency          │
│  RAG问答     │  faithfulness(是否基于context),                │
│             │  answer_relevance, context_precision           │
└──────────────┴───────────────────────────────────────────────┘
```

---

## 5. 三者联动：闭环工作流

### 5.1 完整工作流

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Prompt 变更闭环                                   │
│                                                                     │
│  ① 发现问题（线上 bad case / 用户反馈 / 新需求）                       │
│       │                                                             │
│       ▼                                                             │
│  ② 编写/修改 Prompt（本地开发）                                       │
│       │                                                             │
│       ├──── 如果是新 case ────▶ 同时补充到 eval dataset               │
│       │                                                             │
│       ▼                                                             │
│  ③ 本地跑 Eval（快速验证）                                            │
│       │                                                             │
│       ├── 分数下降 ──▶ 继续迭代 prompt（回到②）                        │
│       │                                                             │
│       ├── 分数持平/上升 ──▶ 提交 PR                                   │
│       │                                                             │
│       ▼                                                             │
│  ④ CI 自动跑 Eval + Baseline 对比                                    │
│       │                                                             │
│       ├── 通过 ──▶ Code Review ──▶ Merge                            │
│       │                                                             │
│       ├── 失败 ──▶ 标记 PR，要求修复                                  │
│       │                                                             │
│       ▼                                                             │
│  ⑤ 上线后监控                                                        │
│       │                                                             │
│       ├── 模型路由器根据 eval 历史数据调整路由权重                       │
│       │                                                             │
│       └── 收集新的 bad case ──▶ 回到①                                │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### 5.2 实际开发示例

**场景：优化客服意图分类的 prompt**

```bash
# Step 1: 发现问题 —— 线上日志中 "退换货" 分类准确率低
# 从线上日志导出 bad cases
python -m eval.cli import-cases --source logs --filter "intent=refund AND score<0.5" --output new_cases.jsonl

# Step 2: 补充到 eval dataset
cat new_cases.jsonl >> prompts/customer_service/intent_classification/eval/dataset.jsonl

# Step 3: 用当前 prompt 跑一次，确认 baseline
python -m eval.cli run --prompt customer_service/intent_classification --save-baseline
# 输出: Overall Score: 82.3%  |  refund tag: 67.5%

# Step 4: 修改 prompt（比如增加 few-shot 示例、调整指令措辞）
vim prompts/customer_service/intent_classification/v3.yaml

# Step 5: 重新跑 eval，对比 baseline
python -m eval.cli run --prompt customer_service/intent_classification --compare-baseline
# 输出:
# ✅ PASSED Baseline Comparison
#   Score change: +5.2%
#   📈 Improvements: 8 cases
#   📉 Regressions: 1 case
#     [chat_003] 1.00 → 0.80  ← 需要关注

# Step 6: 分析回退的 case，微调 prompt
python -m eval.cli inspect --case-id chat_003 --run-id run_latest
# 查看该 case 的详细输入输出

# Step 7: 确认满意后提 PR
git add prompts/customer_service/intent_classification/
git commit -m "prompt(intent): v3.0.0 - 优化退换货分类准确率

eval score: 82.3% → 87.5% (+5.2%)
refund subset: 67.5% → 89.0% (+21.5%)
regression: chat_003 (0.80, acceptable)"
git push && gh pr create
```

### 5.3 Eval 结果反馈给路由器

```python
# router/adaptive_router.py

class AdaptiveRouter(ModelRouter):
    """自适应路由器 —— 根据 eval 历史动态调整"""
    
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.model_performance: dict[str, dict[str, float]] = {}
    
    def update_from_eval(self, eval_result: EvalRunResult, model: str, task_type: str):
        """根据 eval 结果更新模型在特定任务上的表现记录"""
        key = f"{model}:{task_type}"
        self.model_performance[key] = {
            "score": eval_result.overall_score,
            "latency": eval_result.avg_latency_ms,
            "cost": eval_result.total_cost,
            "updated_at": datetime.now().isoformat(),
        }
    
    def route(self, ctx: RoutingContext) -> RoutingDecision:
        """在基础路由上叠加性能数据"""
        base_decision = super().route(ctx)
        
        # 检查该模型在该任务类型上的历史表现
        key = f"{base_decision.model}:{ctx.task_type.value}"
        perf = self.model_performance.get(key)
        
        if perf and perf["score"] < 0.7:
            # 该模型在此任务上表现不佳，升级到更强模型
            upgraded = self._upgrade_model(base_decision.model)
            return RoutingDecision(
                model=upgraded,
                reason=f"Auto-upgraded: {base_decision.model} scored {perf['score']:.2f} on {ctx.task_type.value}",
                fallback_model=base_decision.model,
            )
        
        return base_decision
```

---

## 6. 项目目录结构建议

```
agent-project/
├── prompts/                        # Prompt 模板（Git 管理）
│   ├── customer_service/
│   │   ├── intent_classification/
│   │   │   ├── v1.yaml
│   │   │   ├── v2.yaml           # production
│   │   │   └── eval/
│   │   │       ├── dataset.jsonl
│   │   │       └── config.yaml
│   │   └── order_handling/
│   │       └── ...
│   └── _shared/
│       ├── safety.yaml
│       └── output_formats.yaml
│
├── eval/                           # Eval 系统
│   ├── __init__.py
│   ├── cli.py                     # CLI 入口
│   ├── runner.py                  # 执行器
│   ├── scorers.py                 # 评分函数
│   ├── dataset.py                 # 数据集管理
│   ├── baseline.py                # 基准线管理
│   ├── baselines/                 # 保存的 baseline 文件
│   │   └── customer_service__intent.json
│   └── reports/                   # 历史报告
│       └── run_1234567.json
│
├── router/                         # 模型路由
│   ├── __init__.py
│   ├── model_router.py           # 核心路由器
│   ├── fallback.py               # 降级策略
│   ├── cost_tracker.py           # 成本追踪
│   └── registry.py               # 模型注册表
│
├── prompt_engine/                  # Prompt 引擎
│   ├── __init__.py
│   ├── engine.py                  # 模板加载与渲染
│   └── validators.py             # 变量校验
│
├── agent/                          # Agent 核心逻辑
│   ├── __init__.py
│   ├── loop.py                   # Agent 循环
│   ├── tools/                    # 工具定义
│   └── memory/                   # 记忆管理
│
├── observability/                  # 可观测性
│   ├── tracing.py                # 调用追踪
│   ├── metrics.py                # 指标采集
│   └── dashboards/               # Grafana 配置
│
├── tests/                          # 传统单元测试
│   ├── test_router.py
│   ├── test_prompt_engine.py
│   └── test_scorers.py
│
├── .github/
│   └── workflows/
│       └── prompt-eval.yml        # CI Eval
│
├── requirements.txt
├── requirements-eval.txt
└── README.md
```

---

## 7. 快速开始 Checklist

### Phase 1：最小可用（1-2 周）

- [ ] 搭建 `prompts/` 目录结构，用 YAML 管理第一个 prompt
- [ ] 实现 `PromptEngine` 基础版（加载 + 渲染，不用 Jinja 可以先用 f-string）
- [ ] 手写 10-20 个 eval case（JSONL）
- [ ] 实现 `ExactMatchScorer` + `ContainsScorer`
- [ ] 实现 `EvalRunner` 基础版（串行即可）
- [ ] 本地能跑 `python -m eval.cli run` 并看到分数

### Phase 2：工程化（2-4 周）

- [ ] 实现 `BaselineManager`（保存/对比）
- [ ] 配置 CI，prompt 变更自动触发 eval
- [ ] 实现 `ModelRouter`（规则路由版）
- [ ] 增加 `LLMJudgeScorer`
- [ ] 接入 2-3 个模型（主力 + 经济 + 强推理）
- [ ] 增加 `CostTracker`

### Phase 3：成熟运营（持续）

- [ ] 从线上日志自动导入 bad case 到 eval dataset
- [ ] 实现 `AdaptiveRouter`（根据 eval 数据自动调整路由）
- [ ] 搭建可观测性 dashboard（LangFuse / 自建）
- [ ] A/B Test 框架（灰度发布 prompt）
- [ ] 每周 eval 质量报告

---

## 附录 A：推荐技术栈

| 层 | 推荐方案 | 备选 |
|----|---------|------|
| Prompt 模板 | YAML + Jinja2 | Handlebars, 自定义 DSL |
| Eval 框架 | 自建（本文方案） | Promptfoo, Braintrust |
| LLM SDK | Anthropic SDK + OpenAI SDK | LiteLLM（统一接口） |
| 向量数据库 | Qdrant / Milvus | Pinecone, Weaviate |
| 可观测性 | LangFuse | Helicone, Phoenix |
| CI/CD | GitHub Actions | GitLab CI |
| 成本监控 | 自建 + Grafana | Helicone |

## 附录 B：常见陷阱

| 陷阱 | 后果 | 避免方法 |
|------|------|---------|
| Eval 用例太少（<10个） | 无法代表真实分布 | 最少 50 个，覆盖 edge case |
| 只看平均分 | 掩盖局部回退 | 必须看逐 case 对比 |
| Eval 和线上数据分布不同 | 线上实际效果不符合预期 | 定期从线上采样补充 eval set |
| 没设 token budget | 单次对话消耗 $10+ | 硬限制 max_tokens + budget |
| Prompt 没 review 就上线 | prompt injection / 幻觉 | 和代码一样 PR review |
| 只用 LLM-as-Judge | Judge 本身可能犯错 | 规则 + LLM judge 结合 |
| 路由器太复杂 | 维护成本高于节省的成本 | 从简单规则开始，数据驱动升级 |

---

> **总结**：把 Agent 开发当作工程来做 —— Prompt 是代码（要 review），Eval 是测试（要 CI），Router 是架构（要监控）。三者形成闭环，让你每一次改动都有数据支撑，每一分钱都花在刀刃上。
