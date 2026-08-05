# 车辆诉求提取与搜车流程 V2

> 文档状态：目标方案
>
> 日期：2026-08-04
>
> 范围：车辆诉求增量提取、Session 合并、Guide 菜单映射、本地严格过滤、候选排序、OR 分支、未生效说明和搜索快照
>
> 本文描述目标方案，不表示相关能力已经全部实现

## 0. 2026-08-04 实施进度

本轮已落地：

- 提取协议拆分 `operation` 与业务 `relation`，提取器不再接收执行状态；
- 人数不得推断座位数的确定性校验；
- 品牌、车系、车型 `any_of` 的单 Requirement 表达与本地 OR 严格校验；
- Hard 本地否定三态过滤，unknown 不通过；
- Hard/Soft unresolved 强制 Disclosure；
- 严格无结果不再自动选择条件放宽；
- 本地过滤/排序结果不足时继续收集候选；
- 0～1 归一化 VerifiedRank；
- 过期分页不再静默 fresh；
- 本地租期和 CityID 错误不再归类为 Guide 外部故障。

仍需外部契约或后续版本完成：

- Guide 同组多 code 的 OR 语义和正式分支分页协议；当前 OR 在已扫描候选中本地执行；
- Guide `fuel_type`、`transmission_type` 正式枚举值。代码只在配置提供版本化映射后启用，未配置时保持 unresolved；
- 可配置的 Candidate Collector 耗时、报价数预算以及动态车型目录；
- 取还车双地点模型和 Guide Context TTL 正式字段。

Guide 枚举配置只接受经过服务契约确认的值，不应凭样例猜测：

~~~yaml
guide:
  vehicle_enums:
    version: guide-enums-v1
    fuel_types:
      汽油: [由 Guide 契约确认的整数]
      纯电动: [由 Guide 契约确认的整数]
    transmission_types:
      手动: [由 Guide 契约确认的整数]
      自动: [由 Guide 契约确认的整数]
~~~

## 1. 文档结论

目标流程采用以下边界：

~~~text
LLM 提取器
  只回答“用户本轮表达了什么、如何修改已有诉求”

确定性归一与 Reducer
  负责类型、值、单位、实体和 Session 合并

Search Planner
  负责“当前 Guide 菜单和车辆事实能否执行这些诉求”

Search Executor
  负责 Guide 搜索、本地严格过滤、候选收集和排序

Result Contract
  负责逐条说明哪些诉求已筛选、已验证、已排序或未生效
~~~

最重要的设计决策：

1. 提取阶段不读取 Guide 菜单，不判断诉求是否可筛选或可排序，不生成 FilterCode、Provider ID、Capability 或车辆事实。
2. 提取阶段保留 add/replace/remove，同时保留精确、至少、至多、范围、排除、任选等用户语义；这些是用户含义，不是 Guide 执行参数。
3. 搜车前必须先取得当前租期、地点对应的无条件 Guide baseline，FilterCode 只能来自该菜单或经过当前菜单验证的车型 Provider Binding。
4. 能无损映射的诉求优先使用 Guide FilterCode；只能宽泛表达的诉求使用 Remote Prefilter + Local Verifier。
5. 否定诉求优先使用真实报价字段做本地严格过滤，不能用排序代替排除。
6. Soft 偏好可以使用确定性 LocalRank；开放场景只有在存在足够可靠事实时才能做探索性排序。
7. 无法筛选、无法验证、无法排序的诉求不阻断其他条件搜索，但必须逐条告知，本次结果不能宣称满足。
8. 可执行 Hard 条件严格搜索无结果时不自动放宽；必须由用户确认后生成替代搜索计划。
9. 品牌、车系、车型支持同组 OR；不同条件组默认 AND；排除项使用 NOT。
10. 本地过滤或排序存在时必须收集足够候选，不能只对 Guide 第一页做结论。

## 2. 与现有文档的关系

本文是本轮讨论后的收敛方案。

如果以下文档与本文在 Operator、Negative、LocalFilter、LocalRank、Hard 自动放宽或 OR 处理上冲突，以本文为准：

- requirement-filter-mapping-and-search-degradation-design.md
- vehicle-search-technical-design-v2.md
- exploratory-requirement-scoring-design.md
- search-pipeline-refactoring-plan.md

其中仍然有效并继续复用的原则包括：

- LLM 和前端不能生成 FilterCode、ContextID、车辆实体 ID；
- Guide baseline 菜单是运行时筛选能力的唯一事实源；
- 车辆实体必须经过权威目录和 Provider Binding；
- Session、RequirementVersion、PlanHash 和 SearchSnapshot 必须保持一致；
- 所有未生效或降级行为必须对用户透明。

## 3. 总体流程

~~~mermaid
flowchart TD
    U["用户本轮输入"] --> R["Router"]
    R --> E["Requirement Extractor：只提取增量语义"]
    E --> V["Contract Validator：结构和禁止推断"]
    V --> N["Normalizer / Entity Resolver"]
    N --> RD["Requirement Reducer：合并 Session"]
    RD --> P["Search Policy：是否搜索"]
    P -->|不搜索| S["保存诉求并回复"]
    P -->|搜索| B["Guide Baseline：无条件菜单、Context、基础报价"]
    B --> C["Search Plan Compiler：逐条生成 Resolution"]
    C --> G["Guide Branch Plan：FilterCode、SortCode、OR 分支"]
    G --> X["Guide Search / Candidate Collector"]
    X --> LF["Local Strict Filter：正向校验和否定过滤"]
    LF --> F["Vehicle Facts"]
    F --> LR["Verified Rank + Exploratory Rank"]
    LR --> RC["Result Contract：逐诉求执行结果和 Disclosure"]
    RC --> O["用户结果、说明和下一步选项"]
~~~

执行顺序不可交换：

~~~text
严格远程筛选
→ 本地严格过滤
→ 车辆事实组装
→ 确定性偏好排序
→ 探索性场景排序
→ 用户说明
~~~

排序不能让违反 Hard 条件的车辆重新进入结果。

## 4. 第一步：提取本轮增量诉求

### 4.1 提取器负责什么

提取器只负责：

- 找到本轮车辆诉求的原文证据；
- 判断是新增、替换还是删除；
- 识别业务语义类型和结构化值；
- 保留精确、范围、排除、任选等逻辑；
- 判断用户明确表达的 hard 或 soft；
- 保留开放语义标签；
- 为品牌、车系、车型提供文本 Hint。

提取器不负责：

- 当前 Guide 有没有对应菜单；
- 是否能生成 FilterCode；
- 是否能本地过滤或排序；
- 是否需要放宽；
- 车辆是否满足；
- 生成 Requirement ID、车型库 ID、Provider ID、FilterCode 或 ContextID。

### 4.2 为什么不能只保留 add/replace/remove

add/replace/remove 只说明如何修改 Session，不能完整表达诉求。

~~~text
“7座”
“至少7座”
“最多7座”
“不要7座”
~~~

它们都可能是 add 或 replace，但搜索语义不同。因此应将 Session 变更和用户约束拆开：

~~~go
type RequirementMutation string

const (
    MutationAdd     RequirementMutation = "add"
    MutationReplace RequirementMutation = "replace"
    MutationRemove  RequirementMutation = "remove"
)

type ConstraintRelation string

const (
    RelationExact   ConstraintRelation = "exact"
    RelationAtLeast ConstraintRelation = "at_least"
    RelationAtMost  ConstraintRelation = "at_most"
    RelationRange   ConstraintRelation = "range"
    RelationExclude ConstraintRelation = "exclude"
    RelationAnyOf   ConstraintRelation = "any_of"
)
~~~

ConstraintRelation 属于用户语义层，不等同于 Guide 的比较 Operator。

### 4.3 推荐提取协议

~~~go
type RequirementDelta struct {
    RawText       string
    Mutation      RequirementMutation
    Facet         string
    SemanticLabel string
    Category      string
    Importance    string
    Constraint    SemanticConstraint
    EntityContext EntityContext
    Confidence    float64
}

type SemanticConstraint struct {
    Relation     ConstraintRelation
    Alternatives []ConstraintAlternative
}

type ConstraintAlternative struct {
    Facet string
    Value SemanticValue
}

type SemanticValue struct {
    Kind  string
    Text  string
    Number *float64
    Min   *float64
    Max   *float64
    Unit  string
}
~~~

要求：

- LLM 不输出 ID；
- LLM 不输出执行模式；
- LLM 不输出 FilterCode；
- Alternatives 表示用户明确说出的一个值或 OR 备选，不补充未表达事实；
- 同 Facet OR 和跨实体层级 OR 都可以表达，例如“宝马或奔驰”以及“奥迪或 Model Y”；
- Remove 可以通过 Facet、SemanticLabel 和明确值定位目标，最终目标 ID 由服务端解析。

### 4.4 提取示例

| 用户表达 | Mutation | Relation | 语义值 | Importance |
|---|---|---|---|---|
| “要7座” | replace | exact | 7 seat | hard |
| “至少7座” | replace | at_least | 7 seat | hard |
| “不要燃油车” | add | exclude | 汽油 | hard |
| “最好纯电” | replace | exact | 纯电 | soft |
| “宝马或奔驰” | replace | any_of | 宝马、奔驰 | hard |
| “也看看Model Y” | add | exact | Model Y | hard |
| “品牌不限” | remove | exact | 空，删除整个 brand facet | hard |
| “去掉不要燃油车的限制” | remove | exclude | 汽油 | hard |
| “适合老人” | add | exact | semantic_label=elderly_friendly | soft |
| “必须放下两个28寸箱子” | add | at_least | 2 × 28 inch luggage | hard |

### 4.5 提取阶段禁止推断

必须通过 Prompt、Validator 和 Eval 防止：

~~~text
两人出行 → seat_num=2
带老人小孩 → SUV / 7座 / 舒适型
行李多 → 大后备箱已满足
长途 → 某个车型一定合适
Model Y → 同时产生冗余特斯拉品牌条件
直接搜 / 换一批 → Vehicle Requirement
~~~

人物数量、行李场景和使用场景可以保存为开放语义，但不能自动转换为标准筛选条件。

## 5. 第二步：确定性归一和车型实体解析

Normalizer 负责：

1. 校验 Facet、Category、Relation、Value Kind 和 Unit 的组合；
2. 归一“七座/7座”“电车/纯电动”等无歧义表达；
3. 将价格区分为总价和日均价；
4. 将品牌、车系、车型交给 VehicleEntityResolver；
5. 生成服务端 Requirement ID；
6. 保留 RawText，不覆盖用户原始语义。

车辆实体解析顺序：

~~~text
本地权威目录 exact/alias
→ 若唯一命中，生成 Canonical Entity
→ 若本地 not_found，可调用 AgentHub 召回
→ AgentHub 候选必须回到本地目录复验
→ 多个已复验候选才允许受限 LLM 选择
→ 无法唯一确认则 ambiguous/unresolved
~~~

AgentHub 和 LLM 都不能直接产生受信任的 Provider Binding。

## 6. 第三步：合并 Requirement Session

### 6.1 Add

- 新增一条不存在的诉求；
- 相同语义和值已存在时幂等；
- “宝马或奔驰”保存为一个 any_of Requirement，不拆成两个互相冲突的 Hard Requirement。
- “奥迪或 Model Y”保存为包含 brand 和 vehicle_model 两个 Alternative 的 any_of Requirement。

### 6.2 Replace

- 替换同一语义槽位的旧诉求；
- 标准 Facet 按 Facet 替换；
- 品牌、车系、车型同时处理父子关系；
- 开放语义按 Category + SemanticLabel 替换，不能只按 RawText 完全相等；
- “不要安静了，改成空间大”必须删除旧的安静诉求。

### 6.3 Remove

- 支持删除整个 Facet；
- 支持删除特定正向或负向值；
- 支持通过同义表达删除开放诉求；
- Remove 不创建一条新的“负向诉求”。

### 6.4 状态变化

Requirement 真正变化后：

- RequirementVersion +1；
- 旧 SearchSnapshot 失效；
- 清理旧的 Resolution 和 Disclosure；
- 保留用户原始 hard/soft；
- 重新计算 SearchGoal；
- SearchPolicy 决定是否立即搜索。

## 7. 第四步：是否搜索

本方案不强制修改当前 SearchPolicy，但要求职责清晰：

- Router 只识别是否存在明确搜索动作；
- Requirement Handler 只返回 Changed；
- SearchPolicy 根据明确搜索、是否已有搜索、租车条件变化和 Requirement 变化决定 fresh/next/previous/refresh；
- 条件变化后的自动搜索必须是显式产品策略和 Feature Flag；
- SearchPolicy 不生成 FilterCode、不判断 Capability。

建议默认规则：

| 场景 | 决策 |
|---|---|
| 首次只补充车辆诉求，未说搜索 | 保存诉求，不立即搜 |
| 用户明确“直接搜” | fresh search |
| 已有结果后修改条件 | 可自动 fresh，受 Feature Flag 控制 |
| “换一批/下一批”且快照有效 | continuation |
| 快照过期 | 明确提示后 fresh，不静默重搜 |
| Active Pending 阻塞地点/时间 | 等待 Pending |

## 8. 第五步：获取无条件 Guide Baseline

输入：

- 取车地点；
- 还车地点；
- 取车时间；
- 还车时间；
- page/page_size；
- 空 FilterCodes、SortCode、ContextID。

输出：

- ContextID；
- 完整菜单；
- 基础报价；
- 接收时间和服务有效期。

规则：

- FilterCode 只能从本次 baseline 菜单产生；
- filtered response 的菜单不能覆盖 baseline；
- ContextID TTL 应来自 Guide 契约或响应，不能长期依赖本地猜测；
- baseline 绑定 RentalFingerprint；
- 取还车地点需要分开建模；业务暂不支持异地还车时必须明确拒绝。

## 9. 第六步：逐条编译 Requirement Resolution

每条有效 Requirement 必须得到一个 Resolution，不能静默丢失。

~~~go
type RequirementResolution struct {
    RequirementID string
    RawText       string
    Importance    string
    Status        string
    Executions    []Execution
    ReasonCode    string
    Reason        string
    MustDisclose  bool
}

type ExecutionMode string

const (
    ExecutionRemoteFilter      ExecutionMode = "remote_filter"
    ExecutionRemotePrefilter   ExecutionMode = "remote_prefilter"
    ExecutionLocalVerifier     ExecutionMode = "local_verifier"
    ExecutionLocalFilter       ExecutionMode = "local_filter"
    ExecutionRemoteSort        ExecutionMode = "remote_sort"
    ExecutionVerifiedRank      ExecutionMode = "verified_rank"
    ExecutionExploratoryRank   ExecutionMode = "exploratory_rank"
)
~~~

### 9.1 决策顺序

~~~text
1. Guide 菜单能否无损表达
   → RemoteFilter

2. Guide 菜单能否安全扩大召回
   且真实报价字段能精确验证
   → RemotePrefilter + LocalVerifier

3. 没有合适菜单，但真实报价字段能严格判断
   → LocalFilter

4. 是 Soft 偏好且存在可靠可比较字段
   → RemoteSort 或 VerifiedRank

5. 是开放场景且存在版本化评分模型和足够事实
   → ExploratoryRank

6. 以上都不能
   → unresolved，搜索忽略，但必须说明
~~~

一条 Requirement 可以产生多个 Execution。

### 9.2 典型映射

| 诉求 | Resolution |
|---|---|
| 7座，菜单有精确7座 | RemoteFilter，必要时 LocalVerifier |
| 至少9座，菜单只有8座以上 | RemotePrefilter(8+) + LocalVerifier(seats>=9) |
| 总价≤450，菜单只有≤500 | RemotePrefilter(≤500) + LocalVerifier(total≤450) |
| 不要燃油车 | LocalFilter(fuel_type NOT IN fuel) |
| 不要手动挡 | LocalFilter(transmission NOT IN manual) |
| 优先便宜 | RemoteSort(total_price_asc)，否则 VerifiedRank |
| 最好7座 | VerifiedRank，按座位接近度 |
| 优先特斯拉/Model Y | VerifiedRank，必须先完成实体归一 |
| 适合老人 | 有可靠模型时 ExploratoryRank，否则 unresolved |
| 后备箱放两个28寸箱子 | 当前无后备箱事实，unresolved |

## 10. 第七步：否定诉求本地严格过滤

否定条件不能用负分替代排除。

首批支持：

- energy_type exclude/not_in；
- transmission exclude/not_in；
- brand exclude/not_in；
- vehicle_series exclude/not_in；
- vehicle_model exclude/not_in；
- vehicle_type exclude/not_in，前提是 Guide 车型组枚举已经确认。

Verifier 必须返回三态：

~~~text
match：明确满足
mismatch：明确违反
unknown：字段缺失或枚举不可信
~~~

Hard 条件：

- match：保留；
- mismatch：剔除；
- unknown：不能当作满足，应剔除并记录 unknown。

Soft 条件不使用严格否定过滤；如果用户表达“最好不要”，应进入偏好排序或 unresolved，而不是删除所有未知候选。

所有枚举必须先建立 Provider Code → Canonical Value 的版本化映射，不能直接猜 Guide 数字含义。

## 11. 第八步：品牌、车系、车型 OR

### 11.1 布尔规则

~~~text
同一个 any_of Requirement 内：OR
不同 Requirement 之间：AND
exclude：NOT
~~~

any_of 的 Alternative 可以来自同一 Facet，也可以是品牌、车系、车型之间的跨层级选择；每个 Alternative 在实体归一后独立生成远程分支。

例如：

~~~text
(宝马 OR 奔驰)
AND 7座
AND 纯电
AND NOT Model 3
~~~

跨层级示例：

~~~text
(奥迪品牌 OR 特斯拉 Model Y)
AND 自动挡
~~~

### 11.2 Guide 支持同组 OR

如果 Guide 契约明确同 Facet 多 code 是 OR：

- 在一个 Branch 中发送多个已验证 code；
- Plan 记录 ProviderBooleanSemantics 版本；
- 返回后仍可做实体一致性校验。

### 11.3 Guide 不支持或契约未知

构造分支查询：

~~~text
Branch A：宝马 AND 7座 AND 纯电
Branch B：奔驰 AND 7座 AND 纯电
~~~

然后：

1. 分支分别调用 Guide；
2. 使用 ReferenceID 优先、稳定报价键兜底去重；
3. 合并候选；
4. 统一执行 NOT、本地严格过滤和排序；
5. 保存每个 Branch 的 Context 和 continuation；
6. 分页按各分支游标继续拉取。

### 11.4 车型父子去重

- 具体车型已经蕴含同品牌时，删除冗余父品牌过滤；
- 车系可展开为权威目录内、具有 Guide Binding 的车型集合；
- 不同品牌的车型和品牌组合才是冲突；
- “宝马或奔驰”不是冲突。

## 12. 第九步：Guide 搜索与候选收集

### 12.1 只包含远程可执行条件

Guide 请求只包含：

- RemoteFilters；
- RemoteSort；
- GroupCode；
- 当前 Branch 的 ContextID；
- Page/PageSize。

unresolved 和 LocalRank 不进入 Guide 参数。

### 12.2 Candidate Collector

存在 LocalFilter、LocalVerifier 或 LocalRank 时，不能收到第一页就结束。

Collector 使用可配置预算：

~~~go
type CandidateCollectionBudget struct {
    TargetQualifiedCount int
    RankPoolSize         int
    MaxProviderPages     int
    MaxRawQuotes         int
    MaxDuration          time.Duration
}
~~~

停止条件：

- 严格过滤后已达到 TargetQualifiedCount，并达到排序所需 RankPoolSize；
- Provider 明确 exhausted；
- 达到页数、报价数或耗时预算；
- 请求被取消。

首版可从“目标展示数的 2～3 倍候选池”开始，通过 Eval 再调整，不能把固定 3 页当作业务真理。

### 12.3 没有远程 FilterCode

- 可以使用 baseline quotes 作为第一页；
- 若需要本地过滤或排序，通过 baseline Context 继续拉取；
- 不重新请求相同的无条件第一页；
- 结果必须说明哪些诉求未参与远程筛选。

## 13. 第十步：本地严格过滤

执行对象仅限真实字段和权威实体事实：

- seats；
- total_amount、daily_amount；
- fuel_type；
- transmission_type；
- brand_name；
- vehicle_name、group_name；
- Catalog Provider Binding。

流程：

~~~text
Guide Candidates
→ 正向 LocalVerifier
→ 否定 LocalFilter
→ Strict Candidate Set
~~~

约束：

- LocalFilter 只能减少车辆，不能新增车辆；
- unknown 不能被伪装成 match；
- 每条 Requirement 分别记录 match/mismatch/unknown 数量；
- 如果因字段缺失导致全部剔除，返回 capability_limit，而不是 no_inventory；
- 本地过滤只能声称“当前已获取候选中的验证结果”，除非已经确认 Provider exhausted。

## 14. 第十一步：加权排序

### 14.1 VerifiedRank

适合有明确真实字段的 Soft 偏好：

- 总价更低；
- 日均价更低；
- 座位数接近目标；
- 品牌优先；
- 车系/车型优先；
- 能源类型优先；
- 自动挡优先；
- SUV/MPV 优先；
- 车龄更新，前提是有可靠车龄字段。

所有单项分数归一到 0～1：

~~~text
FinalVerifiedScore =
    Σ(requirement_weight × factor_score)
    / Σ有效 requirement_weight
~~~

禁止继续使用不同量纲直接相加，例如“价格减实际金额、品牌命中加100”。

### 14.2 ExploratoryRank

适用于多个事实共同表达、不能严格证明的场景：

- 适合老人；
- 家庭出行；
- 长途；
- 空间大；
- 行李多；
- 上下车方便。

要求：

- 使用版本化 ScoreDefinition；
- 每个因子输出 available/match/mismatch/unknown；
- 计算 coverage 和 confidence；
- 低于 Coverage 阈值时不改变排序；
- 回复只能说“根据有限事实优先展示”，不能说“满足”。

### 14.3 不允许排序的诉求

缺少可靠事实时不能打分：

- 后备箱放特定数量和尺寸的箱子；
- 儿童座椅安装数量；
- 高速安静/NVH；
- 安全性；
- 操控感；
- 充电方便；
- 冬季性能；
- 座椅舒适性。

这些诉求进入 unresolved。

### 14.4 Hard 开放诉求

例如“必须适合老人”：

- 如果没有严格验证能力，不得视为满足；
- 可以在其他严格条件后的候选中做 ExploratoryRank；
- Result Status 必须为 partial；
- 必须在结果前说明“只能探索排序，不代表满足必须条件”；
- 用户可以选择补充可执行条件，例如低车身、自动挡或具体座位数。

## 15. 第十二步：无法执行诉求和用户说明

所有未生效诉求都生成 Disclosure，Hard 和 Soft 都不能静默丢失。

| 状态 | 用户说明 |
|---|---|
| remote_filtered | 已按该条件筛选 |
| locally_verified | 已在返回车辆中严格验证 |
| locally_excluded | 已在返回车辆中排除 |
| verified_ranked | 已按该偏好排序 |
| exploratory_ranked | 仅根据有限事实探索排序，不代表满足 |
| unresolved_hard | 未参与筛选或排序，结果不代表满足 |
| unresolved_soft | 本次未能应用该偏好 |
| provider_unknown | 当前车辆字段不足，无法验证 |

### 15.1 后备箱示例

用户：

~~~text
必须7座，后备箱要放下两个28寸箱子，预算1000以内。
~~~

目标回复：

~~~text
已按“7座”和“总价1000元以内”筛选。
“后备箱能放下两个28寸箱子”由于当前车辆数据没有可靠的后备箱尺寸，
本次未参与筛选或排序，下面车辆不代表满足该条件，建议下单前向门店确认。
~~~

### 15.2 Soft 未生效示例

~~~text
已按纯电条件筛选。
“高速更安静”缺少可比较的车辆数据，本次没有参与排序。
~~~

### 15.3 回复顺序

1. 已严格生效条件；
2. 本地排除/复验条件；
3. 排序偏好和排序范围；
4. 未生效 Hard；
5. 未生效 Soft；
6. 候选数量和是否已扫描完；
7. 下一步调整选项。

## 16. 第十三步：严格无结果

必须区分：

### 16.1 Provider 无库存

Guide 已 exhausted，远程严格条件下没有车辆：

~~~text
当前严格条件下没有可用车辆。
~~~

### 16.2 本地过滤后无结果

Guide 有候选，但都违反本地 Hard 或字段 unknown：

~~~text
Guide 返回了候选，但当前已获取车辆中没有能够验证全部硬条件的结果。
~~~

### 16.3 不自动放宽

系统列出可调整项，由用户确认：

~~~text
可以尝试：
1. 将总价上限从500调整到700；
2. 将7座改为6座以上；
3. 保持条件，继续扩大候选扫描范围。
~~~

只有用户明确同意后才产生新的 Requirement Delta 或 AlternativePlan。不能在后台移除 Hard 条件后直接展示替代车辆。

## 17. 第十四步：分页和搜索快照

SearchSnapshot 至少包含：

- RentalFingerprint；
- RequirementVersion；
- PlanHash；
- Guide MenuFingerprint；
- VehicleCatalogVersion；
- ScoreDefinitionVersion；
- Provider Boolean Semantics Version；
- OR Branch Contexts；
- LocalFilter/Rank 版本；
- SeenQuoteIDs；
- 已缓存结果批次；
- ExpiresAt。

操作规则：

| 操作 | 行为 |
|---|---|
| 下一批 | 优先返回已缓存的下一批，否则继续各 Branch |
| 上一批 | 只返回缓存；当前第一批时明确提示 |
| 刷新 | 使用相同 Session 语义重新取得 baseline |
| 快照过期 | 明确提示并 fresh，不静默重复第一页 |
| Requirement 改变 | 旧快照 superseded，fresh |

分页期间不重新调用 Requirement Extractor。

## 18. 场景处理矩阵

| 场景 | 提取 | 执行 | 用户说明 |
|---|---|---|---|
| “7座纯电” | 两条 exact hard | 两个 RemoteFilter | 已严格筛选 |
| “至少9座” | at_least 9 | RemotePrefilter + LocalVerifier | 已在候选中验证≥9 |
| “不要燃油车” | exclude 汽油 hard | LocalFilter | 已排除明确燃油车辆；unknown 不通过 |
| “最好纯电” | exact 纯电 soft | VerifiedRank | 已按纯电偏好优先排序 |
| “宝马或奔驰” | any_of 实体组 | Guide OR 或分支查询 | 已按两个品牌任选搜索 |
| “奥迪或Model Y” | any_of 跨层级实体组 | 两个 Guide 分支查询后合并 | 已按品牌/车型任选搜索 |
| “宝马且7座” | 两条 Requirement | 跨组 AND | 已同时应用 |
| “必须适合老人” | open hard | 可探索排序但不验证 | partial，明确不代表满足 |
| “适合老人即可” | open soft | 有覆盖率才探索排序 | 说明评分事实和作用域 |
| “两个28寸箱子” | open luggage hard | unresolved | 未参与搜索，建议门店确认 |
| “两个人出行” | trip context | 不转座位数 | 保存上下文，不筛2座 |
| “Model Y” | vehicle entity | Catalog→Binding→Filter | 已按确认车型筛选 |
| 未知车型 | vehicle entity | AgentHub recall→Catalog revalidate | 未确认则 unresolved |
| “品牌不限” | remove brand | Reducer 删除品牌 | 后续计划不含品牌 |
| “去掉不要燃油限制” | remove exclude fuel | 删除负向条件 | 后续允许燃油车辆 |
| 快照过期后“下一批” | search operation | 提示并 fresh | 不静默重复第一页 |
| 取车时间已过去 | 非车辆诉求 | domain validation error | 提示修改时间，不报 Guide 故障 |

## 19. 失败和降级边界

| 失败 | 行为 |
|---|---|
| Requirement LLM 失败 | 不修改车辆诉求；保留其他领域成功结果 |
| Guide baseline 失败 | 搜索失败，可重试；不构造菜单和 Context |
| FilterCode 不在菜单 | Plan 编译失败或该诉求 unresolved；绝不发送 |
| Guide filtered search 失败 | 保留 Session；标记 provider failure |
| Local Filter 异常 | 不把未知当满足；返回 capability_limit |
| Rank 失败 | 保留严格候选的 Provider 顺序；说明偏好未排序 |
| Vehicle Catalog 失败 | 品牌/车型诉求 unresolved；不能用 LLM 名称生成 code |
| Disclosure 缺失 | 结果不得返回，视为安全合同失败 |

## 20. 目标代码职责

| 目标模块 | 职责 |
|---|---|
| internal/domain/vehiclerequirement | Delta 提取协议、语义 Validator、Normalizer、Reducer |
| internal/vehiclecatalog | 品牌/车系/车型权威实体和 Provider Binding |
| internal/searchplan | Requirement Resolution、Filter Mapper、OR Branch Planner、PlanHash |
| internal/vehiclefacts | Provider-neutral 车辆事实与 unknown 状态 |
| internal/localfilter | 正向/否定三态严格过滤 |
| internal/localrank | 0～1 VerifiedRank、ExploratoryRank、Coverage、Evidence |
| internal/domain/searchcar | Baseline、Guide 执行、Candidate Collector、分页 |
| internal/webchat | 结构化 Resolution 和 Disclosure 的确定性输出 |

SearchCar 不重新理解用户语言；Extractor 不读取 Guide 菜单。

## 21. 分阶段实施

### P0：正确性主链

1. 拆分 Mutation 与 SemanticConstraint；
2. 补充人物数量不得映射座位数的 Validator/Eval；
3. 引入统一 RequirementResolution；
4. 实现 energy/transmission/brand/model 的本地否定过滤；
5. unknown 在 Hard 下不通过；
6. 所有 unresolved Hard/Soft 强制 Disclosure；
7. 删除 Hard 自动放宽；
8. 修正本地校验错误与 Provider 错误分类。

### P1：召回和排序质量

1. any_of Requirement 与 OR Branch Planner；
2. Candidate Collector 按合格数和预算收集；
3. RemotePrefilter + LocalVerifier 统一模型；
4. VerifiedRank 分数归一化；
5. Soft energy/transmission/vehicle_type 排序；
6. 修复开放语义 replace/remove；
7. 分页过期和第一页边界状态。

### P2：开放场景能力

1. VehicleFacts Schema；
2. 权威车型目录和事实补全；
3. 版本化 Scenario ScoreDefinition；
4. Coverage/Confidence/Evidence；
5. 老人、家庭、空间、长途、行李场景重新评测；
6. 无可靠事实的场景保持 unresolved。

### P3：运行治理

1. Guide OR、Context TTL、枚举契约正式确认；
2. Eval 数据集和 SearchPlan 回放；
3. Filter/Rank/Disclosure 线上指标；
4. Feature Flag、Shadow Compare 和 Canary；
5. 动态、可版本化车型目录。

## 22. 核心测试与 Eval

### 22.1 提取

- add/replace/remove 与 relation 独立；
- “不要燃油车”不是 remove；
- “去掉不要燃油限制”是 remove；
- “宝马或奔驰”是单个 any_of；
- “两人出行”不生成 seat_num；
- 搜索控制不生成 Requirement；
- 历史诉求不作为本轮增量输出。

### 22.2 计划

- FilterCode 只来自当前菜单；
- 精确映射、宽预筛和 unmapped 正确区分；
- 一条 Requirement 可同时产生 RemotePrefilter 和 LocalVerifier；
- 同组 OR、跨组 AND、NOT 正确；
- 车型过滤移除冗余父品牌；
- unknown 不作为 Hard match。

### 22.3 执行

- 本地否定过滤不会放过明确排除车辆；
- LocalFilter 只减少候选；
- LocalRank 只改变顺序；
- Rank 失败不影响严格结果；
- Collector 能补足目标结果数或明确达到预算；
- 快照过期不静默 fresh；
- Hard 无结果不自动放宽。

### 22.4 回复

- 每条 Requirement 都有 Resolution；
- Hard unresolved disclosure coverage=100%；
- Soft unresolved disclosure coverage=100%；
- ExploratoryRank 不使用“满足”；
- 未扫描完不能声称全量无结果；
- 后备箱两个28寸箱子场景明确说明无法验证。

## 23. 验收标准

1. Extractor 不包含 Guide 菜单、Capability、FilterCode 或 Provider ID。
2. add/replace/remove 与 exact/at_least/at_most/range/exclude/any_of 独立表达。
3. 所有激活 Requirement 在每次搜索中都有唯一 Resolution。
4. FilterCode 100% 来自当前 baseline 菜单或经过菜单验证的 Binding。
5. 支持首批否定诉求本地三态过滤，Hard unknown 不通过。
6. 支持品牌/车系/车型 OR，且不会编译成意外 AND。
7. RemotePrefilter 不会被误报为精确满足。
8. VerifiedRank 使用归一化分数；ExploratoryRank 输出 Coverage 和 Evidence。
9. 本地过滤和排序基于有界但足够的候选池，不再命中1辆即停止。
10. Hard 条件不会未经用户同意自动放宽。
11. 所有未生效 Hard/Soft 都在车辆结果前说明。
12. 分页、缓存和 PlanHash 绑定 Requirement、菜单、目录、评分和 OR 分支版本。

## 24. 最终流程摘要

~~~text
用户原文
→ 提取本轮 Requirement Delta
→ 校验和确定性归一
→ 合并 Session
→ SearchPolicy 决定是否搜索
→ 获取无条件 Guide baseline
→ 每条诉求生成 Requirement Resolution
→ 组装远程 FilterCode / SortCode / OR Branch
→ Guide 搜索并收集足够候选
→ 本地正向校验和否定过滤
→ 组装车辆事实
→ VerifiedRank / ExploratoryRank
→ 输出严格生效、排序、未生效和作用域说明
→ 保存 SearchSnapshot
~~~

该方案的判断标准不是“是否成功返回车辆”，而是：

~~~text
是否正确理解了用户；
是否只执行了系统能够证明的条件；
是否没有让排序替代严格约束；
是否把所有未生效诉求完整告诉了用户。
~~~
