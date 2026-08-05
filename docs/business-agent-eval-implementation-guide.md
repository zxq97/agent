# 租车业务 Agent Eval 落地补充：范围、优先级与目的

> 文档状态：实施补充
>
> 日期：2026-08-04
>
> 配套文档：[LLM Eval 技术方案](llm-eval-technical-design.md)、[当前架构与处理流程](current-architecture-and-processing-flow.md)

## 1. 结论：本项目应如何做业务 Agent Eval

本项目不适合只给最终回复打一个“好不好”的分数。它是一个由受约束 LLM、确定性状态机和 Maps/Guide/AgentHub 等外部服务组成的租车 Agent；错误可能改变 Session、触发搜索、选错车辆实体，或对用户假称已经完成操作。

因此，Eval 应采用以下闭环：

```text
真实/构造业务 Case
  → 单 Task 语义评测
  → 确定性组件与多轮场景回归
  → Stable 与 Candidate 对比、发布门禁
  → Canary 与线上失败样本回流
  → 数据集和规则持续更新
```

核心原则：

1. **按业务后果排优先级。** 能改变路由、持久状态、搜索执行权限和用户承诺的输出优先评测。
2. **确定性规则优先。** ID 白名单、证据原文、时间、状态变更、禁止字段等必须用程序断言；LLM Judge 仅补充体验判断。
3. **比较 Candidate 与 Stable。** 单看 Candidate 的绝对分数无法发现退化；Prompt、模型、Fallback、温度或契约变化都要在同一输入集比较。
4. **离线 Eval 与生产隔离。** 首版不调用 Maps、Guide、AgentHub 的真实业务接口，不修改真实 Session，也不依赖随机的线上状态。
5. **一个 Badcase 不是一个结论。** 每个线上问题都要转为可复现 Case，并同时在相邻反例、同义改写和持出集上检查是否引入新问题。

## 2. 结合当前项目：评测对象和风险排序

当前链路为：

```text
Router
→ RentalContext / VehicleRequirement
→ Session Reducer + SearchPolicy
→ VehicleCatalog / CapabilityResolver / SearchPlan
→ Guide 搜索
→ Disclosure / GeneralReply
```

已有 `internal/llmharness` 可以提供稳定 Task ID、Prompt/Schema/Validator 版本、模型、尝试次数、Fallback、耗时和 Token 用量；现有单元测试已经覆盖 Harness、Reducer、SearchPolicy、SearchSnapshot 等确定性行为。缺少的是版本化 Case、任务专属 Evaluator、可比较的运行记录和发布报告。

| 风险等级 | 当前模块/Task | 为什么优先 | 典型业务后果 |
|---|---|---|---|
| 最高 | `router.route` | 决定本轮执行哪些领域动作及是否马上搜索 | 漏掉地点修改；误触发搜索；把规则问答当闲聊；混合意图丢失 |
| 最高 | `vehicle_requirement.extract` | 输出会写入 Session 并改变筛选、排序和后续对话 | 把“两人出行”误当 2 座；复述历史条件；把“直接搜”变成车辆条件 |
| 高 | `rental_context.extract` | 输出会改变取还地点/时间并决定搜索是否可执行 | 相对时间算错、取还方向颠倒、地点串入虚构 ID |
| 高 | `vehicle_entity.select_candidate`、`capability.match` | 决定实体或能力能否进入执行计划 | 同名异地/相似车型选错；候选外 ID；把 `relevant` 误升级为可执行 `exact` |
| 高 | 跨组件 SearchPolicy / Disclosure | 单点输出合法也可能在组合后造成错误承诺 | 条件变更后没重搜；硬条件无法满足却不提示；Pending 回答吞掉新条件 |
| 中 | `general_reply.generate` | 自由文本不改状态，但会影响用户对执行结果的判断 | 虚构报价、库存或已搜/已改状态；泄露内部信息 |
| 中 | Maps、Guide、AgentHub 契约 | 外部返回变化会令正确语义落到错误执行 | 解析字段缺失、菜单变化、超时后伪造实体 |

## 3. 优先级路线图

### P0：先建立可阻断的离线回归基线

**范围**：L0 合约测试保持全绿；建立 Router、车辆诉求、租车条件三类 L1 单 Task Eval，并输出 Stable 基线报告。

| 项目 | 要做什么 | 目的 | 能解决或避免的问题 | 完成标准 |
|---|---|---|---|---|
| P0.1 版本化 Case 与标注规范 | 为每个 Case 固定 `case_id`、`task_id`、输入、期望、禁止项、标签、`dataset_version`；相对时间写入 `fixed_now` 与 `timezone` | 使错误可重放、可审计、可比较 | 同一时间表达每次跑出不同答案；Bug 修复后无法确认是否回归 | JSONL Core 集、标注说明和 Case 审核人可追溯 |
| P0.2 Router 确定性 Evaluator | 比较 Action 集、每项 Evidence 是否为原文连续子串、重复 Action、禁止串域、搜索误触发 | 防止错误动作进入编排 | 混合意图丢失；误搜；规则问题被错答；历史状态被当本轮动作 | Evidence 违规和重复/Schema 违规均为 0；按混合/Pending/规则问答切片出报告 |
| P0.3 车辆诉求 Evaluator | 比较本轮增量、标准/开放语义、operation、operator、value/unit、hard/soft 和原文证据 | 防止错误状态沉淀 | 历史诉求回写；禁止推断；FilterCode/ID 幻觉；搜索控制串入筛选 | History Echo、Forbidden Inference、服务端 ID/Code 幻觉均为 0 |
| P0.4 租车条件 Evaluator | 比较地点文本、取还字段、时间状态与固定时间下的 RFC3339 值 | 使搜索前置条件可靠 | 相对时间错误、取还颠倒、模糊时间被擅自具体化、地点虚构 ID | Provider ID/经纬度/城市 ID 幻觉为 0；相对和模糊时间单独统计 |
| P0.5 Stable/Candidate Runner 与报告 | 同一 Case 分别运行当前稳定配置和候选配置；记录版本、模型策略、Attempt、Fallback、延迟、Token 和逐 Case 差异 | 把“感觉更好”变成可发布判断 | 修复一个 Prompt Case 却损伤其他场景；Fallback 成本或失败率被忽略 | 同时输出机器可读 JSON 与人工可读 Markdown；可定位每个新增失败 |
| P0.6 零容忍门禁 | 任一关键违规直接失败，不允许以平均分抵消 | 优先保证不会越权或误导 | 高总分掩盖一次错误搜索、错误 ID、虚假执行声明 | CI/手动发布脚本返回非零并列出违规 Case |

P0 的最小数据集不以“Case 总数”作为目标。每条关键规则至少应有正例、反例和邻近反例：例如“品牌不限，直接搜”“换一批”“两人出行”“带老人小孩”“第一个，每天预算 300”“取消订单怎么收费”“SUV 和 MPV 有什么区别”。

### P1：覆盖错误被确定性流程放大的路径

**范围**：L2 组件组合和高风险 L3 多轮场景；补上实体/能力决策与回复安全。

| 项目 | 要做什么 | 目的 | 能解决或避免的问题 | 完成标准 |
|---|---|---|---|---|
| P1.1 车辆实体与能力 Eval | 用受控候选集测试 `vehicle_entity.select_candidate` 与 `capability.match`；覆盖候选顺序扰动、相似车型、低证据拒绝、`exact/relevant/no_match` | 防止不可靠语义获得执行权 | 选中候选外 ID；车型歧义误选；Relevant 被当 Exact 执行 | 候选外 ID、多候选、Relevant→Exact 升级错误均为 0 |
| P1.2 组件组合 Eval | 输入 Provider 无关快照和结构化结果，验证 Catalog→Resolver→Compiler→Disclosure | 在不调用真实外部服务的前提下验证业务后果 | Hard 未映射仍假装满足；条件没有进入计划；降级说明丢失 | 每个 Hard Unmapped/Relaxed/本地校验剔除场景都有预期结果和文案断言 |
| P1.3 多轮场景 Eval | 固定初始 Session、时钟和外部快照，运行 Router→Orchestrator→Reducer→Policy | 验证跨轮状态正确性 | Pending 回答吞掉新条件；换一批重置条件；改时间不重搜；“都行”不结束偏好询问 | 覆盖关键 12 条主链场景；断言最终 Session、SearchDecision 和用户可见说明 |
| P1.4 通用回复安全集 | 对 `general_reply.generate` 做禁止声称、数据编造和敏感信息泄露断言；体验质量另行抽检 | 防止文本越权承诺 | 虚构库存、报价或已执行搜索/状态修改 | 安全违规为 0；体验项只作为非阻断参考，或经过人工复核 |
| P1.5 外部服务契约回归 | 延续项目“真实远程客户端”测试方式，低频运行 Maps/Guide/AgentHub 关键读接口契约测试；不纳入每次 L1 离线跑 | 发现外部字段/菜单/语义变化 | 服务返回变化导致编译、地点解析或实体复验悄然失效 | 有独立环境、超时与失败告警；仅在可信环境运行 |

P1 重点场景：地点时间与车辆条件混合、Pending 回答加新条件、`品牌不限，直接搜`、`换一批`、Model Y 消除冗余品牌、未映射车型但其他条件继续搜索、修改租车条件后自动重搜、硬条件降级披露、`都行`直接搜索。

### P2：让线上问题形成长期资产

**范围**：L4 观测、样本回流、持出集和漂移检测。

| 项目 | 要做什么 | 目的 | 能解决或避免的问题 | 完成标准 |
|---|---|---|---|---|
| P2.1 脱敏 CallRecord | 持久化必要元数据和采样后的输入/输出哈希或脱敏内容；区分成功、校验失败、Repair、Fallback | 让线上异常可复盘而非依赖日志猜测 | 模型未报错但语义漂移；无法重建某次失败条件 | 可按 Task/版本/失败类型检索，保留策略与权限明确 |
| P2.2 失败样本回流 | 将用户纠正、低置信、校验失败、Fallback、未映射和人工投诉变成“待审核 Case” | 持续覆盖真实长尾 | 数据集只含 Prompt 示例，无法代表用户语言 | 有去重、脱敏、双人审核、标注冲突仲裁和入库记录 |
| P2.3 Holdout 与 Challenge 集 | 将部分边界、对抗和新样本限制查看；按版本只增不改 | 防止针对公开集调 Prompt | Core 提升但真实泛化下降 | 发布前必须跑 Holdout；结果只向有权限人员展示 |
| P2.4 定时漂移检查 | 定期用固定 Stable Profile 重跑 Core/Challenge，比较质量、Repair、Fallback、延迟、成本 | 发现同名模型或供应商行为变化 | 没有代码改动却出现行为退化 | 预警含受影响 Task/Slice/Case，达到阈值可暂停推广 |
| P2.5 Canary 与业务指标 | 小流量比较 Stable/Candidate；观测纠正率、Pending 后撤、搜索后立即改条件、Unsupported 比例、延迟和成本 | 验证离线结果在真实流量中成立 | 离线集偏差、生产流量结构变化 | 可快速回滚；线上指标不替代离线零容忍门禁 |

### P3：体验优化与规模化治理

**范围**：通用回复的 Pairwise Judge、人工质检台、报表/趋势面板、数据和发布治理自动化。

这一阶段的目的不是取代 P0–P2 的确定性门禁，而是提高自由文本体验、标注效率和跨版本决策效率。优先做盲评（随机交换 A/B）和人工复核，再考虑自动化 Prompt 优化；不要让 LLM Judge 单独决定安全或执行类发布。

## 4. 数据集、Evaluator 与指标的最小设计

### 4.1 数据集分层

| 数据集 | 用途 | 当前优先级 |
|---|---|---|
| Core | 高频主链和每条关键规则的正反例 | P0 |
| Regression | 已修复的线上/测试 Badcase | P0，持续增加 |
| Challenge | 歧义、混合、同义改写、最小差异和对抗表达 | P1 |
| Holdout | 防止测试集过拟合的受限集合 | P2 |
| Shadow | 脱敏线上样本，仅分析、经审核后入库 | P2 |

建议 Case 结构：

```json
{
  "case_id": "router-mixed-pending-001",
  "task_id": "router.route",
  "input": {},
  "expected": {},
  "forbidden": [],
  "tags": ["mixed_intent", "pending"],
  "fixed_now": "2026-08-04T10:00:00+08:00",
  "timezone": "Asia/Shanghai",
  "dataset_version": "router/core/1"
}
```

相对时间、Session、候选列表、Guide 菜单和外部响应都必须固定为 Case 输入的一部分；否则无法比较两次运行。

### 4.2 Evaluator 顺序

```text
确定性断言 → 归一化语义比较 → 人工审核 → LLM Judge（仅体验补充）
```

- **确定性断言**：Action 集、Evidence 子串、白名单 ID、禁止字段、时间、状态变化、SearchDecision、Disclosure。
- **归一化语义比较**：例如“7座”与“七座”归一为同一 `value=7, unit=seat`；Requirement 数组不依赖输出顺序。
- **人工审核**：新 Task、重要边界、Judge 分歧、Holdout 和线上真实失败。
- **LLM Judge**：只用于通用回复的相关性、简洁度、自然度等主观指标；固定 Judge 版本并做盲评与抽样人工复核。

### 4.3 必须报告的指标

| 维度 | 指标 | 说明 |
|---|---|---|
| 正确性 | Task 专属准确率、Precision/Recall/F1、Exact Match | Router 重点看 Action 集和混合意图；提取任务分字段统计 |
| 安全 | Critical Violation Count | 必须为 0，不能被平均分抵消 |
| 鲁棒性 | 各标签 Slice 指标、候选顺序稳定性 | 防止总分掩盖 Pending、相对时间、负向条件等退化 |
| 工程可用性 | Schema/Validator Pass、Repair/Fallback/Final Failure | 识别“可用但不稳定”的模型版本 |
| 性能成本 | P50/P95/P99、Token、平均 Attempt、单 Case 成本 | 防止质量微升却延迟/成本失控 |
| 差异解释 | Stable/Candidate 的改善、退化、持平 Case | 方便人工快速确认是否该发布 |

## 5. 发布门禁与运行节奏

### 5.1 何时必须跑

| 变更 | 最少需要运行 |
|---|---|
| Prompt、示例、模型、温度、Fallback、重试策略变化 | 对应 Task 的 Core + Regression；候选有明显变化时加 Challenge/Holdout |
| Schema、Decoder、Validator、Reducer、SearchPolicy、Compiler 变化 | L0 全量 + 相关 L1/L2/L3；检查 Evaluator 是否需要升版本 |
| Maps、Guide、AgentHub 契约变化 | 相关 L2/L3 + 独立远程契约测试 |
| 模型供应商版本/行为变化 | 定时 Stable 重跑 + 异常时 Canary |
| 线上 Badcase 修复 | 原始 Case 加入 Regression，并添加至少一个相邻反例 |

### 5.2 不可发布的条件

以下任一项出现即阻断：证据不是用户原文；编造 Provider ID、FilterCode 或 `context_id`；候选外 ID；把 `relevant` 当可执行 `exact`；历史诉求被写成当前增量；新增禁止推断；通用回复假称已执行；必需的硬条件降级说明遗漏；离线评测修改真实 Session 或访问业务写接口。

其余情况采用相对门禁：Candidate 不得降低 Stable 的关键指标和关键切片，不得超过任务的 P95/Token/成本预算，也不得异常放大 Repair、Fallback 或最终失败。样本较小时同时查看差异 Case 清单和置信区间，不能只看百分比。

## 6. 推荐的首个迭代交付物

建议按以下顺序交付，而非一次性建设完整平台：

1. `cmd/llmeval`（或等价独立 Runner）：加载 JSONL、固定配置、运行单 Task、产出 JSON/Markdown 报告。
2. `eval/datasets/`：先放 Router、VehicleRequirement、RentalContext 的 Core 与 Regression 数据。
3. `eval/evaluators/`：先实现三个任务的确定性 Evaluator 和零容忍规则。
4. Stable Profile：固化当前 Prompt/Model/Policy 版本，跑出第一份基线。
5. Candidate Compare：接入发布检查；每个差异显示输入、期望、Stable、Candidate、违规和成本指标。
6. P1 的组件组合与关键多轮场景；随后才做线上 CallRecord、Canary 和体验 Judge。

这样做能最快回答四个最关键的问题：**模型是否理解对了、是否会写错状态或触发错误执行、改动是否真的优于当前版本、上线后出现新问题能否回流并复现。**

## 7. 暂不建议优先投入的事项

- 为所有确定性模块再包一层 LLM Judge；现有 Go 单元测试和断言更准确、更便宜。
- 每次 CI 都访问真实 Maps/Guide/AgentHub；会让评测受网络、数据和限流波动影响。
- 全量永久保存生产对话；先做最小化、脱敏、有保留期限的采样。
- 自动生成 Prompt 后自动发布；在数据集和 Judge 尚未成熟时会放大偏差。
- 追求单一“总分”；它会掩盖一次高风险的错误搜索或虚假承诺。
- 穷举全部多轮组合；先覆盖会改变状态、搜索或用户理解的高风险主链。
