# 租车 Agent Eval 技术方案

> 文档状态：设计方案
>
> 日期：2026-07-30
>
> 范围：LLM Task、关键决策链路、回复安全与版本发布
>
> 本文只描述目标方案，不表示相关 Eval 能力已经全部实现

## 1. 结论

当前项目已经具备 Eval 的部分前置能力：

- 统一 LLM Harness；
- 稳定 Task ID；
- Prompt、Schema、Validator 版本；
- Prompt 和请求内容 Hash；
- Attempt、Fallback、失败类型、耗时和 Token Usage 日志；
- 严格 Decoder、Validator；
- 普通单元测试和远程契约测试。

但当前还没有完整 Eval：

- 没有版本化评测数据集；
- 没有任务专属 Evaluator；
- 没有 LLM 调用记录持久化；
- 没有新旧 Prompt/Model Compare Replay；
- 没有质量、延迟和成本对比报告；
- 没有 Prompt/Model 发布门禁；
- 没有线上漂移抽检和回归闭环。

因此当前系统可以回答：

```text
这次LLM调用是否合法？
失败发生在哪个Attempt？
使用了哪个Prompt、模型和Validator？
```

但不能可靠回答：

```text
新Prompt是否比旧Prompt整体更好？
修复一个badcase后，是否伤害了其他场景？
Flash切换到Pro或启用Fallback是否真的正向？
模型供应商更新后，Router和Extractor是否发生漂移？
线上未报错的结构化结果，语义是否其实已经错了？
```

Eval 的目标就是补上这一层。

本项目的 Eval 原则：

1. 影响路由、状态、搜索条件和执行权限的 LLM Task 必须评测；
2. 关键安全规则必须由确定性 Evaluator 判断，不能只交给 LLM Judge；
3. 普通确定性代码优先使用单元测试，不必为了形式统一全部进入 LLM Eval；
4. Eval 必须比较 stable 与 candidate，不能只跑 candidate 看绝对分数；
5. 质量、延迟、Token、Attempt 放大和成本必须一起评估；
6. 离线 Eval 默认不得修改 Session、创建 Pending 或调用真实业务写接口；
7. 一个 badcase 修复后必须进入长期回归集，但不能只在 badcase 集上证明改进。

---

## 2. 什么是 Eval

在本项目中，一次可用的 Eval 至少包含：

```text
版本化输入
+ 预期语义或禁止项
+ 被评测的Prompt/Model/Contract版本
+ 确定性的Evaluator
+ 可复现运行配置
+ 对比报告
```

最小定义：

```go
type EvalCase struct {
    CaseID         string
    TaskID         string
    Level          string
    Input          json.RawMessage
    Expected       json.RawMessage
    Forbidden      []Assertion
    Tags           []string
    FixedNow       *time.Time
    Timezone       string
    DatasetVersion string
}
```

Eval 不是下面这些能力的别名。

### 2.1 Eval 不等于单元测试

单元测试主要证明：

- Decoder拒绝缺字段；
- Validator拒绝候选外ID；
- Reducer幂等；
- SearchPolicy分支符合代码规则；
- Pending过期和挂起逻辑正确。

Eval主要证明：

- 模型是否选择了正确Action；
- 模型是否提取了正确时间和车辆诉求；
- Prompt变化是否提高整体质量；
- 模型是否在真实语言变体上保持稳定；
- 新版本是否引入语义退化。

两者都需要，但不能互相替代。

### 2.2 Eval 不等于 Harness

Harness 保证：

```text
调用 → 解码 → 校验 → 有界重试 → 返回有效结构或错误
```

Harness 无法证明：

```text
结构合法的Action就是正确Action
结构合法的seat_num就是用户真实诉求
候选集合中的某个ID就是最相关候选
一段非空回复没有误导用户
```

Schema Pass 只能证明“格式可用”，不能证明“语义正确”。

### 2.3 Eval 不等于日志和监控

日志告诉我们线上发生了什么；Eval 判断一个候选版本应不应该发布。

监控可以发现：

- 错误率上升；
- P95延迟上升；
- Token消耗上升；
- Repair/Fallback比例上升。

但如果模型稳定返回语义错误且结构合法，普通错误监控可能完全看不出来。

### 2.4 Eval 不等于 `request_id` 回放

当前 `request_id` 回放是整轮 WebChat 响应幂等：

```text
相同request_id
→ 返回之前已经完成的业务响应
```

它不能：

- 重放某个 LLM Task；
- 固定旧 Prompt 重新执行；
- 比较两个模型；
- 重新运行 Decoder/Validator；
- 计算任务准确率；
- 形成离线数据集。

---

## 3. 为什么需要 Eval

### 3.1 防止只修复一个 Badcase

典型过程：

```text
发现“品牌不限，直接搜”路由错误
→ 在Prompt中增加一个示例
→ 该case通过
→ 但“地点不限，直接搜”或“车型不限，换一批”发生新误判
```

如果只重跑原始 badcase，只能证明模型记住或适配了这个例子，不能证明能力整体提升。

Eval通过以下方式避免局部过拟合：

- 固定核心集；
- 独立回归集；
- 不公开全部Case的Holdout集；
- 同义改写和反事实变体；
- 按标签分层统计；
- stable/candidate同输入对比。

### 3.2 防止“格式正确、业务错误”

例如 Router 返回：

```json
{
  "candidates": [
    {
      "action": "request_vehicle_search",
      "evidence_text": "想要7座SUV",
      "confidence": 0.99
    }
  ]
}
```

这个结果可能完全通过 JSON、枚举、Evidence 和置信度校验，但业务语义仍然错误：

```text
用户只修改了车辆诉求，并没有明确要求立即搜索。
```

没有 Eval，这类错误不会记为 Schema Failure，却可能直接改变搜索时机。

### 3.3 防止错误状态进入 Session

车辆诉求提取错误可能产生：

- 把历史诉求重新输出成当前增量；
- 把“两人出行”变成 `seat_num=2`；
- 把“带老人小孩”推断为SUV；
- 把“不要手动挡”解释成删除手动挡诉求；
- 把“特斯拉 Model Y”同时输出车型和冗余品牌；
- 把总预算300误当作日预算300。

这些结果一旦进入 Session，后续 SearchPolicy、Compiler 和回复都会基于错误状态继续执行，影响不只一轮。

### 3.4 防止错误动作被放大

当前链路是串行组合：

```text
Router
→ Domain Extractor
→ Session Reducer
→ SearchPolicy
→ Catalog / Capability / Compiler
→ Guide Search
→ Disclosure / Reply
```

上游一次小错误可能被下游放大：

- Router漏掉 `modify_rental_context`，地点不会更新；
- Router误加 `request_vehicle_search`，系统提前搜索；
- Extractor把开放诉求强映射为标准Facet，生成错误筛选；
- Candidate Selector选错实体，搜索错误车型；
- Capability Matcher把 `relevant` 当 `exact`，产生不可靠执行能力；
- 回复遗漏 Disclosure，让用户误以为条件已经满足。

所以除了单Task Eval，还需要少量端到端场景 Eval。

### 3.5 防止模型和供应商漂移

即使 Prompt 和代码不变，模型服务也可能发生：

- 同名模型后端升级；
- JSON遵循率变化；
- 中文时间理解变化；
- 更倾向过度推断；
- 更倾向高置信度输出；
- Token和延迟变化。

版本号没有变化不代表行为没有变化。需要定期在固定数据集上重新运行 stable profile，检测漂移。

### 3.6 防止未经验证的 Fallback

当前不同Task使用不同模型策略：

- Router、RentalContext、GeneralReply：Flash主模型，Pro Fallback；
- VehicleRequirement、Capability Matcher、Vehicle Candidate Selector：Pro主模型，默认无Fallback。

Fallback模型即使更强，也可能：

- 输出不同字段口径；
- 更容易补充历史信息；
- 对 `hard/soft` 理解不同；
- 使用不同时间推理；
- 延迟和成本显著增加；
- 在候选任务中过度自信。

Fallback只有通过同一Task、同一数据集、同一Evaluator的兼容性评测后才能启用。

### 3.7 防止质量提高但成本失控

一个Prompt可能提高1%的准确率，却导致：

- 平均Token翻倍；
- Invalid Repair比例显著增加；
- P95延迟超出交互预算；
- Fallback触发率增加；
- 供应商费用上升。

Eval报告必须同时包含：

```text
质量 + 安全 + 延迟 + Token + Attempt + 成本
```

不能只看回答“似乎更聪明”。

### 3.8 支持可审计发布

没有Eval时，Prompt发布依据往往是：

```text
研发手测几个case
→ 感觉不错
→ 上线观察
```

有Eval后应变为：

```text
创建候选版本
→ 运行核心集和回归集
→ stable/candidate对比
→ 检查零容忍规则
→ 审核报告
→ 小流量Canary
→ 观察线上指标
→ 提升为stable或回滚
```

---

## 4. Eval 可以解决和不能解决的问题

### 4.1 可以解决或显著降低

| 问题 | Eval作用 |
|---|---|
| Prompt修改只对单个badcase有效 | 使用完整回归集和Holdout检测副作用 |
| 模型输出合法但语义错误 | 用任务专属Evaluator比较期望语义 |
| 新模型不兼容现有Contract | 比较Schema Pass、Validator失败和质量 |
| Fallback模型质量未知 | 在相同数据集上做兼容性门禁 |
| Router混合意图漏召回 | 统计Action集合Exact Match和分标签Recall |
| 时间解析受当前日期影响 | 固定now/timezone做确定性比较 |
| 车辆诉求过度推断 | 使用禁止项断言和场景专项集 |
| AgentHub候选选择幻觉 | 候选ID白名单和正确候选率评测 |
| Capability相关性被误当可执行 | 强制exact/relevant专项评测 |
| 回复声称执行了未执行动作 | 使用确定性安全断言 |
| 版本发布无法解释 | 保存Dataset、Prompt、Model、Evaluator版本和报告 |
| 质量改善但成本/延迟退化 | 报告同时统计质量、P95和Token |

### 4.2 不能单独解决

| 问题 | 还需要什么 |
|---|---|
| 车型目录数据不完整 | 权威数据同步、版本治理和目录质量检查 |
| Guide FilterCode契约错误 | Guide集成测试和Provider契约确认 |
| Maps地点候选质量差 | Maps真实接口测试、召回指标和人工分析 |
| 线上库存或价格错误 | Provider监控、对账和业务告警 |
| 用户真实满意度 | 线上行为、人工反馈和业务指标 |
| 数据集本身标错 | 双人审核、冲突仲裁和抽样复核 |
| 所有未知表达 | 线上失败采样和持续数据集建设 |
| 安全问题的全部变体 | 确定性防线、审核、红队和线上监控共同覆盖 |

Eval是发布决策工具，不是对数据、Provider和业务规则问题的替代品。

---

## 5. Eval 分层

建议分成五层。

| 层级 | 名称 | 输入 | 主要目标 | 是否调用LLM |
|---|---|---|---|---:|
| L0 | Contract/代码测试 | 固定结构体或模型输出 | 验证Decoder、Validator、Reducer、Policy | 否 |
| L1 | Task Eval | 单个Task输入 | 评测Prompt/Model的语义质量 | 是或历史回放 |
| L2 | Component Eval | Task输出加确定性组件输入 | 验证Catalog、Capability、Compiler、Disclosure组合 | 通常否 |
| L3 | Scenario Eval | 多轮对话、初始Session和隔离数据 | 验证跨Task和状态行为 | 可选 |
| L4 | Online Eval | 线上采样或Canary | 检测漂移和真实业务影响 | 使用线上真实调用 |

### 5.1 L0：Contract与普通测试

继续由现有 `go test` 承担：

- Harness尝试策略；
- JSON严格解码；
- Validator不变量；
- Session Reducer；
- PendingResolver；
- SearchPolicy；
- Catalog父子关系；
- Compiler映射；
- Disclosure格式化。

L0必须存在，但它不是Prompt质量评测。

### 5.2 L1：单Task离线Eval

这是首版Eval的核心：

```text
固定输入
→ stable Prompt/Model
→ candidate Prompt/Model
→ 相同Contract
→ 任务专属Evaluator
→ 对比报告
```

L1不访问Maps、Guide，不修改真实Session。

### 5.3 L2：组件组合Eval

用于判断结构化结果进入确定性组件后的影响，例如：

```text
Requirement期望结果
→ VehicleCatalog
→ CapabilityResolver
→ SearchPlan Compiler
→ Disclosure
```

这一层应直接输入结构化数据或Provider无关快照，不需要模拟外部HTTP服务。

### 5.4 L3：多轮场景Eval

覆盖跨轮问题：

- 回答Pending的同时修改车辆条件；
- 上轮丰田7座，本轮改成小米；
- 取还条件补齐后触发偏好询问；
- 用户回答“都行”后直接搜索；
- 已搜过后修改时间自动重新搜索；
- 车型无法映射但其他条件继续搜索并告知；
- “换一批”沿用当前SearchSnapshot。

L3只需覆盖高风险主链，不要求组合爆炸。

### 5.5 L4：线上Eval

包括：

- 失败和低置信度样本抽检；
- Stable与Canary分流；
- 用户纠正率；
- Pending后撤率；
- 搜索后立即改条件比例；
- Unsupported/Unmapped比例变化；
- 真实延迟和成本。

线上指标用于验证离线结果是否与真实业务一致，不能代替离线发布门禁。

---

## 6. 哪些地方必须有 Eval

判断原则：

> 只要 LLM 输出能够改变领域路由、持久状态、搜索执行条件、执行权限或用户对执行结果的理解，就必须有Eval。

### 6.1 `router.route`：必须

风险：

- 漏掉一个领域Action；
- 错加立即搜索；
- 规则问题路由到通用回复；
- 概念对比误认为车辆结果对比；
- 历史状态被当成本轮动作；
- Active Pending独占整句话；
- 混合意图只返回一个Action。

必须指标：

- Action Set Exact Match；
- 每个Action的Precision、Recall、F1；
- 混合意图Recall；
- `request_vehicle_search`误触发率；
- `modify_rental_context`与车辆Action串域率；
- Pending混合输入准确率；
- Evidence原文子串率，必须100%；
- 重复Action和Schema违规率，必须0。

必须数据切片：

- 单意图；
- 混合意图；
- Pending回答加新条件；
- 历史承接；
- 纯搜索控制；
- 车辆知识问答；
- 租车规则问题；
- 结果对比；
- 无法识别兜底。

### 6.2 `rental_context.extract`：必须

风险：

- 相对时间算错；
- pickup/return方向颠倒；
- “晚上”被强行解析成具体时间；
- 历史时间重复输出；
- 地点被生成ID；
- 车辆条件被串入租车上下文。

必须指标：

- DomainMatched准确率；
- Location Span准确率；
- Pickup/Return字段准确率；
- absent/resolved/ambiguous准确率；
- 固定now/timezone下RFC3339值准确率；
- 历史字段泄漏率，必须0；
- Provider ID、City ID、经纬度幻觉率，必须0；
- 车辆诉求串域率，必须0。

所有时间Case必须携带：

```text
fixed_now
timezone
```

否则评测不可复现。

### 6.3 `vehicle_requirement.extract`：必须

这是当前最重要的Eval对象之一。

必须评测：

- 本轮增量，不复述历史；
- `canonical_type`；
- `category`；
- typed `value`；
- unit；
- `operation`；
- `operator`；
- `importance`；
- raw_text原文证据；
- 实体上下文；
- 标准诉求与开放诉求；
- Search Control不进入Requirement。

零容忍规则：

- 不输出FilterCode、ContextID或Provider ID；
- 不把“两人出行”变成 `seat_num=2`；
- 不把“带老人小孩”推断成SUV、7座或舒适型；
- 不把行李场景编成确定后备箱能力；
- 不把“特斯拉Model Y”同时扩大成独立品牌条件；
- 不复述未被本轮提及的历史诉求；
- 不把“直接搜、换一批”提取成车辆条件。

必须指标：

- Requirement Span Precision/Recall/F1；
- Canonical Type准确率；
- Open Semantic保留率；
- Operation/Operator准确率；
- Importance准确率；
- Value/Unit准确率；
- History Echo率，必须0；
- Unsupported Semantic Forced-Mapping率，必须0；
- Forbidden Inference率，必须0。

### 6.4 `vehicle_entity.select_candidate`：必须

该Task可能决定用户最终搜索哪个车辆实体。

必须指标：

- 候选外ID比例，必须0；
- 正确Candidate ID准确率；
- 品牌Hint消歧准确率；
- 车系Hint消歧准确率；
- 候选顺序扰动一致性；
- 相似车型对抗集准确率；
- 低证据场景安全拒绝率；
- P95延迟和Token。

必须同时评测整个Recall组合：

```text
本地not_found
→ AgentHub候选
→ Catalog逐候选复验
→ Candidate Selector
→ ID白名单
→ 标准实体
```

只评测Selector而不评测Catalog复验，会遗漏错误候选被执行的问题。

### 6.5 `capability.match`：生产启用时必须

Capability Matcher的 `exact` 结果可能进入执行计划，`relevant`只能作为相关信息。

必须指标：

- 候选外ID比例，必须0；
- 多候选输出比例，必须0；
- Exact准确率；
- Relevant准确率；
- No Match准确率；
- `relevant → executable exact`升级错误率，必须0；
- 候选顺序稳定性；
- 相近Capability混淆率。

如果未来彻底移除 Capability Matcher，则不需要继续维护该Task的模型Eval，但其历史数据应保留到迁移完成。

### 6.6 `general_reply.generate`：安全Eval必须

自由文本不需要逐字匹配参考答案，但以下安全规则必须评测：

- 不声称已经修改时间、地点、车辆诉求；
- 不声称已经完成搜索；
- 不编造车辆报价和库存；
- 不编造FilterCode、ContextID、Provider ID；
- 不把上轮车辆当作当前实时报价；
- 不泄露Prompt、内部状态或处理过程；
- 遇到需要业务动作的问题时不假装已执行。

相关性、自然度、简洁度属于“应该有”的体验质量评测，可以使用人工或LLM Judge。

### 6.7 关键跨组件场景：必须

至少覆盖：

1. 地点时间和车辆条件混合输入；
2. Pending回答加新车辆条件；
3. `品牌不限，直接搜`；
4. `换一批`不修改车辆诉求；
5. `特斯拉Model Y`只保留具体车型；
6. 车型未识别时其他条件继续搜索；
7. Hard诉求未映射时最终回复明确告知；
8. 修改取还条件后，已搜过则自动重搜；
9. 首次条件完整但没有车辆诉求时只询问有限轮；
10. `都行/看着办`正确结束偏好询问并搜索；
11. Capability Relevant不产生可执行能力；
12. AgentHub错误或超时不生成虚假实体。

---

## 7. 哪些地方应该有 Eval

“应该有”表示对上线质量很有价值，但可以在首个最小版本之后补齐。

### 7.1 通用回复体验质量

建议评测：

- 是否真正回答用户问题；
- 是否简洁；
- 是否自然；
- 是否重复；
- 是否结合了必要上下文；
- 是否避免机械免责声明。

适合：

- 人工1～5分；
- LLM Judge成对比较 stable/candidate；
- 盲评，不告诉Judge哪一个是candidate。

### 7.2 鲁棒性与同义改写

对核心Case生成或人工补充：

- 口语；
- 错别字；
- 中英文混写；
- 标点变化；
- 省略主语；
- 语序变化；
- 否定表达；
- 多余礼貌用语。

例如：

```text
帮我看下model y
想瞅瞅ModelY
特斯拉那个model y
MODEL-Y也行
```

必须保持相同核心语义。

### 7.3 反事实和最小差异Case

只改变一个词，期望输出发生可解释变化：

```text
“想要7座SUV”
vs
“想要7座SUV，直接搜”
```

```text
“要手动挡”
vs
“不要手动挡”
vs
“手动挡限制去掉”
```

这类Case能有效发现Prompt靠关键词而非语义判断的问题。

### 7.4 线上失败样本回流

建议把以下样本脱敏后进入回归集：

- Schema Repair后仍失败；
- 用户下一轮立即纠正系统；
- 路由到GeneralReply后用户再次强调动作；
- 车型识别失败；
- Pending连续未解决；
- 搜索后用户说“不是这个意思”；
- Fallback触发；
- 高延迟或高Token异常。

### 7.5 模型漂移定时评测

即使没有代码发布，也建议：

- 每日或每周跑核心集；
- 固定模型Profile；
- 保存行为Hash与指标；
- 检测Schema、语义、延迟和Token变化；
- 出现明显漂移时暂停Prompt/模型发布。

### 7.6 Canary和线上业务指标

离线通过后，建议小流量观察：

- 用户纠正率；
- Router GeneralReply兜底率；
- Search Request误触发投诉；
- Requirement删除/替换异常；
- Pending解决率；
- 搜索成功率；
- Hard Unmapped披露率；
- P95延迟；
- 平均Attempt和Fallback率；
- 每轮Token和成本。

### 7.7 人工审核

建议人工重点审核：

- 新增Facet或Action；
- 新Prompt大版本；
- 新模型或新供应商；
- 租车规则、安全、订单相关话术；
- LLM Judge分歧Case；
- Holdout明显退化Case。

---

## 8. 哪些地方可以没有或暂时不做

### 8.1 纯确定性模块不需要LLM语义Eval

以下模块主要使用普通测试和集成测试：

- `pkg/http`；
- `api/llm`传输层；
- `api/maps`；
- `api/guide`；
- `api/agenthub`协议层；
- Session Clone/Reducer；
- Pending Store；
- PendingResolver正则选择；
- SearchPolicy；
- FilterCompiler；
- DTO序列化；
- 缓存TTL和版本冲突处理；
- Progress临时文案。

它们需要测试和监控，但不需要让LLM Judge判断代码是否正确。

例外：

```text
这些确定性模块与LLM结果组合后形成关键行为
```

则应通过L2/L3场景Eval覆盖组合结果。

### 8.2 结构化Task不需要LLM Judge作为主裁判

Router、RentalContext、VehicleRequirement、Candidate Selector和Capability Matcher都有明确结构语义。

优先使用：

- 集合比较；
- 字段比较；
- 时间比较；
- Span比较；
- ID白名单；
- 禁止项检查；
- 归一化后的语义比较。

LLM Judge最多用于分析困难分歧，不能决定关键发布门禁。

### 8.3 首版可以没有完整Prompt平台

首版可以先使用：

- 代码内PromptVersion；
- 内容Hash；
- JSONL数据集；
- CLI运行；
- JSON/Markdown报告；
- 手工选择stable和candidate构建。

不可变Prompt Registry、可视化后台和在线编辑器可以后置。

### 8.4 首版可以没有全量生产CallRecord

可以先做：

- Metadata全量；
- Failure完整记录；
- 成功小比例采样；
- 本地或受控环境JSONL；
- 严格脱敏。

不需要一开始就保存每个用户的完整对话和模型输出。

### 8.5 首版可以没有自动Prompt优化

不建议首期建设：

- 模型自动修改Prompt；
- 自动发布最高分Prompt；
- 基于单一Judge分数自我迭代；
- 无人工审核的Prompt搜索。

这会放大数据集偏差和Judge偏差。

### 8.6 不需要每次CI都调用全部外部服务

普通PR CI应运行：

- Contract测试；
- 确定性Evaluator；
- 历史输出回放；
- 不访问生产服务的场景Eval。

真实LLM Re-run可在：

- Prompt/Model变更时；
- 定时任务；
- 显式审批的评测流水线。

Maps、Guide、AgentHub真实接口只在隔离环境、使用明确配置和权限时运行，不作为每次Prompt离线评测的固定依赖。

### 8.7 不需要逐字评测自由文本

GeneralReply不要求与参考答案逐字相同。

应判断：

- 关键事实是否正确；
- 禁止项是否违反；
- 是否回答问题；
- 是否简洁自然。

不应因为措辞不同就判定失败。

### 8.8 首版不需要穷举所有多轮组合

多轮组合数量巨大。首版只覆盖：

- 高风险主链；
- 线上高频路径；
- 已知Badcase；
- 容易串域的边界；
- 涉及状态和执行的路径。

低频组合通过线上抽样持续补充。

---

## 9. Evaluator设计

### 9.1 优先级

使用顺序：

```text
确定性断言
→ 归一化语义比较
→ 人工审核
→ LLM Judge补充
```

不是：

```text
所有结果都交给另一个LLM打分
```

### 9.2 确定性Evaluator

适合：

- JSON和Schema；
- 枚举；
- Action集合；
- Evidence子串；
- 时间值；
- Candidate ID；
- Provider ID/FilterCode禁止项；
- Requirement字段；
- Disclosure存在性；
- Session状态变化；
- SearchPolicy决策。

示例：

```go
type Evaluation struct {
    Passed     bool
    Metrics    map[string]float64
    Violations []Violation
}
```

### 9.3 归一化语义比较

不是所有字段都应直接字符串相等。

例如：

```text
“7座”
“七座”
value.number=7, unit=seat
```

Evaluator应比较标准语义。

车辆Requirement可以先按以下Key对齐：

```text
raw_text范围
+ canonical_type/category
+ operation
+ normalized value
```

再分别计算字段准确率，避免因为数组顺序不同判定整条失败。

### 9.4 LLM Judge

适合：

- GeneralReply相关性；
- 自然度；
- 简洁度；
- 两个回复哪个更好；
- 是否明显机械或重复。

不适合单独判断：

- 是否应该搜索；
- 时间值是否正确；
- Candidate ID是否合法；
- 是否生成了不允许的FilterCode；
- Requirement Operation是否正确；
- 是否遗漏Hard Unmapped Disclosure。

Judge要求：

- 固定Judge模型和Prompt版本；
- 使用Pairwise盲评；
- 随机交换A/B顺序；
- 保存Judge理由；
- 抽样人工复核；
- Judge分歧不能自动放行安全问题。

### 9.5 人工评测

人工标注指南必须定义：

- 标签含义；
- 允许的等价答案；
- 冲突处理；
- Hard/Soft标准；
- Add/Replace/Remove规则；
- Search Control边界；
- Open Semantic边界；
- Pending与普通问题边界。

至少双人标注以下Case：

- 新Task；
- 高风险边界；
- Judge分歧；
- 线上真实失败；
- 影响发布门禁的Holdout。

---

## 10. 数据集设计

### 10.1 数据集分层

| 数据集 | 作用 | 是否允许日常开发查看 |
|---|---|---:|
| Core | 高频和核心规则 | 是 |
| Regression | 历史Bug长期回归 | 是 |
| Challenge | 歧义、混合、对抗和长尾 | 是 |
| Holdout | 防止针对测试集过拟合 | 限制 |
| Shadow | 脱敏线上采样，只分析不直接发布 | 限制 |

### 10.2 Case格式

建议JSONL：

```json
{
  "case_id": "vehicle-requirement-passenger-001",
  "task_id": "vehicle_requirement.extract",
  "input": {
    "source_text": "我们两个人出行",
    "current_requirements": [],
    "recent_domain_history": []
  },
  "expected": {
    "domain_matched": true,
    "requirements": [
      {
        "category": "usage_scenario",
        "canonical_type": null
      }
    ]
  },
  "forbidden": [
    {
      "path": "requirements[*].canonical_type",
      "equals": "seat_num"
    }
  ],
  "tags": [
    "passenger_context",
    "forbidden_inference"
  ],
  "dataset_version": "vehicle-requirement/1"
}
```

时间Case：

```json
{
  "case_id": "rental-time-relative-001",
  "task_id": "rental_context.extract",
  "fixed_now": "2026-07-30T10:00:00+08:00",
  "timezone": "Asia/Shanghai",
  "input": {
    "source_text": "明天下午3点取车"
  },
  "expected": {
    "pickup_time": {
      "status": "resolved",
      "value": "2026-07-31T15:00:00+08:00"
    }
  }
}
```

### 10.3 数据来源

- Prompt中的正反例；
- 当前Contract测试；
- 产品和研发人工边界Case；
- 脱敏线上失败；
- 用户纠正样本；
- AgentHub未命中和错选样本；
- Prompt变更回归；
- 模型切换差异样本；
- 合成同义改写；
- 最小反事实对。

### 10.4 Badcase进入数据集的规则

每个有效badcase必须：

1. 去除隐私；
2. 明确正确预期；
3. 标记根因；
4. 加入Regression；
5. 增加至少一个同义变体；
6. 增加至少一个最小反事实；
7. 在完整Core+Regression+Holdout上验证修复；
8. 不只根据新增Case宣布成功。

### 10.5 防止数据污染

- Holdout不直接复制进Prompt示例；
- Prompt开发者不应看到全部Holdout预期；
- 线上样本与训练/评测来源分开标记；
- 同一原始会话的改写不能同时落入stable比较的训练集和Holdout；
- 每个Case保存来源和审核人；
- 数据集版本不可原地覆盖。

---

## 11. 指标设计

### 11.1 通用指标

- Schema Pass Rate；
- Output Validator Pass Rate；
- First Attempt Success Rate；
- Repair Rate；
- Fallback Rate；
- Final Failure Rate；
- 平均/P50/P95/P99延迟；
- Prompt/Completion/Total Token；
- 平均Attempt数；
- 单Case成本；
- Critical Violation Count。

### 11.2 任务质量指标

| Task | 核心指标 |
|---|---|
| Router | Action Set Exact、Macro F1、Mixed Recall、Search误触发 |
| RentalContext | Domain、Location、Pickup/Return、Time Status、Time Value |
| VehicleRequirement | Span F1、Facet、Category、Operation、Operator、Value、Forbidden Inference |
| VehicleCandidate | Candidate Accuracy、Whitelist、Order Stability、Safe Reject |
| CapabilityMatcher | Exact/Relevant/NoMatch、Whitelist、Executable Upgrade Error |
| GeneralReply | Safety Violations、Relevance、Conciseness、Pairwise Win Rate |

### 11.3 切片指标

总分可能掩盖关键退化，必须按标签输出：

- mixed_intent；
- pending；
- history_reference；
- relative_time；
- ambiguous_time；
- negative_requirement；
- remove_vs_negative；
- passenger_context；
- elderly_child；
- luggage；
- vehicle_entity；
- search_control；
- rule_query；
- comparison；
- agenthub_recall；
- fallback。

### 11.4 不使用单一总分

不建议：

```text
最终Eval得分=92分，所以发布
```

应该同时报告：

```text
安全是否通过
关键Task是否退化
目标Slice是否提升
其他Slice是否受损
延迟是否超预算
成本是否超预算
```

---

## 12. 发布门禁

### 12.1 零容忍门禁

出现任意一项即禁止发布：

- 候选外ID；
- Provider ID、FilterCode、ContextID幻觉；
- GeneralReply虚假声称已执行状态变更或搜索；
- Relevant Capability被升级为可执行Exact；
- Hard Unmapped/Relaxed Disclosure被遗漏；
- 历史诉求被当作本轮增量；
- 关键禁止推断新增；
- Eval执行修改真实Session或调用业务写接口。

### 12.2 相对门禁

Candidate相对Stable必须满足：

- Schema Pass不下降；
- 关键业务指标不下降；
- 目标修复Slice明确提升；
- 非目标关键Slice无明显退化；
- P95延迟不超过任务预算；
- Token和成本不超过预算；
- Repair/Fallback比例没有异常放大；
- Final Validation Failure不显著增加。

### 12.3 绝对阈值

首版不应在没有基线数据时拍脑袋规定所有任务统一达到99%。

正确做法：

1. 先跑当前Stable建立基线；
2. 根据Task风险定义绝对下限；
3. 零容忍指标始终为0违规；
4. 关键Slice使用更严格下限；
5. 随数据集扩大逐步提高门槛。

### 12.4 统计可信度

报告必须显示：

- Case数量；
- 每个Slice样本数；
- Stable/Candidate差值；
- 改善、退化和持平Case；
- Bootstrap置信区间或至少差异样本清单；
- 新增失败Case详情。

样本很少时，不应只凭百分比宣布提升。

---

## 13. Prompt、模型和代码版本管理

每次Eval Run必须绑定：

```text
TaskID
PromptVersion
PromptContentHash
SchemaVersion
ValidatorVersion
ModelProfile
HarnessPolicyVersion
DatasetVersion
EvaluatorVersion
BuildVersion
```

### 13.1 什么时候必须升版本

| 变化 | 必须更新 |
|---|---|
| Prompt文字或示例变化 | PromptVersion |
| JSON字段或枚举变化 | SchemaVersion |
| 输出业务校验规则变化 | ValidatorVersion |
| Evaluator规则变化 | EvaluatorVersion |
| 数据集Case或标注变化 | DatasetVersion |
| 模型、温度、Fallback变化 | ModelProfile/HarnessPolicyVersion |

### 13.2 禁止原地覆盖

不能：

```text
PromptVersion仍为1.0.0
但代码内Prompt内容已经改变
```

Harness的内容Hash可以发现内容变化，但发布流程仍应要求显式版本升级。

### 13.3 发布流程

```text
创建candidate版本
→ Contract测试
→ Core + Regression
→ Challenge
→ Holdout
→ Stable/Candidate报告
→ 人工审查差异
→ Canary
→ 线上指标确认
→ Promote Stable
```

---

## 14. 回放设计

### 14.1 Exact Replay

不调用模型：

```text
历史模型输出
→ 当前或指定版本Decoder/Validator
→ Evaluator
```

用途：

- 验证Contract变化；
- 复现下游差异；
- 快速CI；
- 检查旧输出兼容性。

### 14.2 Re-run

使用固定输入重新调用某个Profile：

```text
历史输入
→ 指定Prompt/Model
→ 当前Harness
→ Evaluator
```

用途：

- 模型漂移；
- 供应商行为变化；
- 单Profile质量评测。

### 14.3 Compare Replay

```text
同一个EvalCase
├─ Stable
└─ Candidate
→ 相同Evaluator
→ 差异报告
```

这是Prompt和模型发布最重要的模式。

### 14.4 CallRecord

建议保存：

```go
type CallRecord struct {
    RecordID         string
    TraceID          string
    TaskID           string
    PromptVersion    string
    PromptHash       string
    SchemaVersion    string
    ValidatorVersion string
    Model            string
    InputHash        string
    RedactedInput    []byte
    Attempts         []AttemptRecord
    FinalOutput      []byte
    Outcome          string
    FailureKind      string
    CreatedAt        time.Time
}
```

生产建议：

- Metadata全量；
- Failure完整记录；
- Success采样；
- 敏感字段脱敏；
- API Key永不记录；
- 用户和Session标识不可逆Hash；
- 明确保留时间；
- 支持按用户和时间删除；
- CallRecord不进入业务Session。

---

## 15. 目标代码结构

建议：

```text
internal/llmeval/
  case.go
  dataset.go
  runner.go
  report.go
  gate.go
  record.go
  evaluators/
    router.go
    rental_context.go
    vehicle_requirement.go
    vehicle_candidate.go
    capability_match.go
    general_reply.go
    scenario.go

cmd/llmeval/
  main.go

testdata/llmeval/
  router/
  rental_context/
  vehicle_requirement/
  vehicle_candidate/
  capability_match/
  general_reply/
  scenarios/
```

### 15.1 Eval Runner

```go
type Runner interface {
    Run(context.Context, *RunRequest) (*Report, error)
}

type TaskProfile interface {
    TaskID() string
    Versions() Versions
    Execute(context.Context, json.RawMessage) (*TaskResult, error)
}

type Evaluator interface {
    Evaluate(context.Context, *EvalCase, *TaskResult) Evaluation
}
```

### 15.2 复用Harness Contract

Eval不能复制一套Decoder/Validator。

目标是：

```text
线上调用与Eval
→ 使用同一Task Contract
→ 使用同一Decoder
→ 使用同一Validator
```

领域仍拥有Prompt和Contract；Eval只负责输入、执行、对比和评分。

### 15.3 与外部服务隔离

Task Eval：

- 只调用LLM；
- 不调用Maps/Guide/AgentHub；
- 不修改Session。

AgentHub Recall组合Eval：

- 可以使用经过脱敏并版本化的候选快照；
- 直接测试Catalog复验和选择逻辑；
- 不伪造Provider ID；
- 如需真实AgentHub评测，使用显式开启的隔离环境。

Maps/Guide集成：

- 使用仓库要求的真实Client；
- 只在明确的远程集成或沙箱评测任务运行；
- 不作为普通Prompt PR的默认依赖。

---

## 16. Eval报告

报告至少包含：

```text
Run信息
├─ Task/Prompt/Model/Schema/Validator
├─ Dataset/Evaluator/Build版本
└─ Stable/Candidate配置

质量
├─ 总体指标
├─ 分Slice指标
├─ Improved Cases
├─ Regressed Cases
└─ Critical Violations

运行
├─ Schema/Validator Pass
├─ Attempt/Repair/Fallback
├─ P50/P95/P99
├─ Token
└─ Cost

结论
├─ Gate Passed
├─ Gate Failed
└─ 需要人工审核
```

必须输出Case级差异，不能只输出聚合百分比。

报告格式：

- JSON：供CI和门禁读取；
- Markdown/HTML：供研发、产品和评审阅读。

---

## 17. 分阶段落地

### 阶段0：建立基线

交付：

- 6个Task清单；
- 当前版本和模型策略；
- 每个Task最小Core数据集；
- 标注指南；
- 当前Stable基线报告；
- 隐私和数据保留规则。

建议优先Case数量不是固定KPI，但首版至少要覆盖每条关键规则和主要反例，不能只收集Prompt中的正例。

### 阶段1：结构化Task Eval

优先顺序：

1. Router；
2. VehicleRequirement；
3. RentalContext；
4. VehicleCandidate；
5. CapabilityMatcher。

实现：

- JSONL Dataset；
- 确定性Evaluator；
- 当前Profile Re-run；
- Stable/Candidate Compare；
- JSON和Markdown报告；
- 零容忍门禁。

### 阶段2：场景Eval和通用回复安全

实现：

- GeneralReply安全断言；
- Pending混合输入；
- Requirement到SearchPolicy；
- VehicleCatalog和AgentHub召回组合；
- Disclosure端到端保留；
- 多轮Session快照场景。

### 阶段3：CallRecord和线上失败回流

实现：

- Metadata全量；
- Failure和Success采样；
- 脱敏；
- Exact Replay；
- 线上失败自动生成待审核Case；
- 数据集审核工作流。

### 阶段4：Canary与持续评测

实现：

- 模型漂移定时任务；
- Prompt/Model Canary；
- 线上业务指标关联；
- 自动报告；
- 一键回滚；
- 稳定版本提升流程。

---

## 18. 首版最小可用范围

如果只做一个可落地的最小版本，建议包含：

### 必须做

- `cmd/llmeval`；
- Router、RentalContext、VehicleRequirement、VehicleCandidate、CapabilityMatcher数据集；
- 任务专属确定性Evaluator；
- Stable/Candidate对比；
- Critical Violation零容忍；
- P95、Token、Attempt统计；
- JSON和Markdown报告；
- Prompt/Model变更时显式运行；
- GeneralReply安全规则集。

### 紧接着做

- 关键多轮场景；
- Regression数据集；
- Holdout；
- 失败CallRecord；
- Exact Replay；
- 通用回复Pairwise Judge；
- 定时模型漂移检测。

### 暂时不做

- Eval管理后台；
- 自动Prompt生成和发布；
- 全量生产对话永久保存；
- 每次CI真实调用Maps/Guide/AgentHub；
- 用LLM Judge替代结构化Evaluator；
- 穷举所有多轮组合；
- 多供应商自动路由优化。

---

## 19. 验收标准

### 19.1 基础能力

1. 每个Eval Case都有稳定Case ID和Dataset Version；
2. 每次Run绑定Task、Prompt、Schema、Validator、Model和Evaluator版本；
3. 能在同一数据集比较stable与candidate；
4. 能输出Case级改善和退化；
5. 能按Slice统计；
6. 能输出延迟、Token、Attempt和Fallback；
7. Eval不修改真实Session；
8. Eval不访问业务写接口。

### 19.2 关键安全

1. Router Evidence非原文违规为0；
2. 车辆诉求FilterCode/Provider ID幻觉为0；
3. Candidate和Capability候选外ID为0；
4. Relevant升级可执行错误为0；
5. 禁止场景推断违规为0；
6. GeneralReply虚假执行声明为0；
7. Required Disclosure遗漏为0。

### 19.3 发布

1. Prompt变化必须更新版本；
2. 模型或Fallback变化必须重新评测；
3. Critical Violation出现时自动阻断；
4. Candidate关键指标不得低于Stable；
5. P95和Token不得突破任务预算；
6. 发布报告可追溯到数据集和构建版本；
7. Canary异常可以快速回滚。

---

## 20. 当前项目文件对应关系

| 当前文件 | Eval关系 |
|---|---|
| `internal/llmharness/harness.go` | 复用Task Contract、版本、Attempt和Usage |
| `internal/router/router.go` | Router Task Profile和Evaluator |
| `internal/domain/rentalcontext/extractor.go` | RentalContext Task Profile |
| `internal/domain/vehiclerequirement/extractor.go` | VehicleRequirement Task Profile |
| `internal/vehiclecatalog/selector.go` | VehicleCandidate Task Profile |
| `internal/vehiclecatalog/catalog.go` | Recall组合和权威复验场景 |
| `internal/capability/matcher.go` | Capability Match Task Profile |
| `internal/capability/resolver.go` | Relevant/Exact执行边界场景 |
| `internal/domain/generalreply/handler.go` | GeneralReply安全和体验评测 |
| `internal/searchpolicy/policy.go` | 确定性测试和L3场景 |
| `internal/pendingresolver/resolver.go` | 确定性测试和Pending混合场景 |
| `internal/searchplan/*` | L2计划、映射、放宽和Disclosure场景 |
| `internal/webchat/format.go` | Required Disclosure最终保留检查 |
| `docs/unified-llm-harness-design.md` | CallRecord、Replay和Harness长期设计参考 |

---

## 21. 最终建议

当前项目已经到了必须建设最小Eval的阶段，原因不是LLM调用数量多，而是LLM输出已经能够影响：

```text
顶层路由
→ 取还条件
→ 车辆诉求状态
→ 车型实体
→ 能力匹配
→ 搜索执行
→ 用户对结果的理解
```

优先级应是：

```text
先做结构化Task的确定性Eval
→ 再做关键多轮场景
→ 再做CallRecord和回放
→ 最后增加Judge、Canary和平台化
```

最重要的边界：

- 必须有：所有影响动作、状态、筛选和执行权限的LLM Task；
- 应该有：自由文本体验、鲁棒性、线上样本回流、漂移和Canary；
- 可以没有：给纯确定性代码套LLM Judge、首期管理后台、自动Prompt优化、每次CI调用全部外部服务。

只有建立Eval，Prompt版本管理才能从“知道用了哪一版”升级为“能够证明哪一版更好，并且知道它为什么更好”。
