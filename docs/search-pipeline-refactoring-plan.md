# 搜车 Agent 分阶段重构落地方案

## 1. 文档状态

- 状态：开发实现已落地，等待完整外部契约回归和上线灰度
- 目标版本：搜车 Pipeline V3
- 实施方式：当前内存会话版本直接迁移；生产持久化版本仍需按阶段灰度
- 当前约束：Handler 串行执行，不引入并行调度
- 本文范围：顶层路由、车辆诉求提取、车辆实体归一、Guide 菜单映射、搜车决策、Pending、分页、Session 状态和验收方案
- 本文不包含：供应商搜索、订单/保险/支付等新领域

本文用于把当前已经工作的 Router、RentalContext、SearchCar 和 Pending 代码，迁移为职责清晰、可以逐阶段上线的条件化串行 Pipeline。

最重要的边界是：

```text
LLM 决定“用户表达了什么”
确定性归一层决定“用户指的是哪个标准值或车辆实体”
FilterCompiler 决定“当前 Guide 能否以及如何执行”
SearchCarHandler 只负责“按已经编译好的计划调用 Guide”
```

LLM、前端和普通 Domain Handler 都不能生成或提交 Guide 的 `filterCode`、`sortCode`、`groupCode`、`context_id`。

### 1.1 本次实现状态

已经落地：

- Router 拆为 `modify_rental_context`、`update_vehicle_requirements`、`request_vehicle_search`、`general_reply`；
- 诉求提取与 Guide 搜索拆成独立 Handler；
- 固定 Requirement Facet、operation/operator 和严格 JSON 合同；
- 服务端 Requirement ID、车辆实体别名归一和品牌/车系/车型层级消解；
- 仅基于无筛选 Baseline 菜单编译 FilterPlan，调用方不能注入 provider code/context；
- 14/15 分钟 Baseline 安全缓存、Filter 响应隔离、SearchSnapshot 和 next/previous/refresh；
- SearchPolicy 自动搜索、首次无诉求有限询问和 Pending 局部阻断；
- hard 冲突/不可执行不静默降级，soft 排序只使用真实返回字段；
- 只读 GeneralReply 兜底显式通用文本、未分配文本和 Domain mismatch 文本。

尚未宣称完成：

- Guide 同组/跨组多 code 的正式 AND/OR、Context 续期和枚举契约仍需服务方确认；
- 当前未实现生产 Feature Flag、shadow compare、Dashboard、持久化 Session CAS 和多实例灰度；
- 默认车辆目录只是首版种子数据，正式上线前必须接入权威、可版本化的数据源；
- 完整真实 LLM/Guide/Maps 回归需要在可访问开发服务的环境通过。

## 2. 重构前背景和问题

### 2.1 重构前 Router Action 粒度错误

当前 Router 的主要 Action 是：

```text
modify_rental_context
search_car
general_reply
```

其中 `search_car` 同时表示：

- 新增、替换、删除车辆诉求；
- 用户明确要求立即搜索；
- 在已有条件下刷新搜索；
- 潜在的结果翻页。

这些动作的状态影响、前置条件和执行成本不同，不能继续共用一个 Action。

### 2.2 重构前 SearchCarHandler 混合多个职责

当前 `internal/domain/searchcar/handler.go` 同时承担：

- 调用 LLM 提取车辆诉求；
- 合并和保存历史诉求；
- 判断 Pending 是否阻断搜索；
- 校验租车地点和时间；
- 请求无筛选菜单；
- 构建菜单索引；
- 将诉求映射为 `filterCode`；
- 校验调用方携带的 code；
- 调用 Guide；
- 本地排序；
- 保存菜单和搜索结果。

这导致：

- 诉求更新无法脱离搜索独立执行；
- Router 只能把“修改诉求”和“执行搜索”路由到同一 Handler；
- 映射失败、接口失败和语义失败混为一类；
- 任何一处修改都影响整个搜车路径；
- 无法清楚说明哪些诉求真正参与了筛选。

### 2.3 重构前 Requirement Facet 不完整

目标首批 Facet 是：

```text
seat_num
vehicle_type
price_preference
car_age
comfort_preference
energy_type
transmission
brand
vehicle_series
vehicle_model
custom
```

重构前实现使用 `seat_count`、`fuel_type`、`price` 等名称，缺少 `car_age`、`comfort_preference` 和明确的 `vehicle_series`。

`vehicle_series` 必须保留，否则无法稳定表达和处理：

```text
vehicle_model > vehicle_series > brand
```

这里的具体业务定义必须由车辆目录统一。例如：

```text
brand          特斯拉
vehicle_series Model Y 车系
vehicle_model  Guide/车辆目录中可独立识别的具体车型或具体款
```

产品若仍希望将用户口语中的“Model Y”落为 `vehicle_model`，可以在车辆目录中将它定义为当前可执行的最具体实体，但实体本身仍需携带父品牌和可选父车系关系，不能只保存字符串。

### 2.4 重构前菜单匹配没有使用 Facet

当前逻辑主要按照规范化后的 `Value` 在所有菜单项名称中搜索，没有先限定 Requirement Facet 对应的菜单组。

因此即使 LLM 提取正确，也可能出现：

- 同名菜单项跨组误匹配；
- 类型和值不一致；
- 无法区分品牌、车型、能源、座位等能力；
- 诉求 Key 与菜单名称不一致时直接 unresolved；
- 多个 code 的同组/跨组语义不明确。

目标必须改为：

```text
Requirement.Facet
  → FacetRegistry
  → 对应 Guide 菜单组
  → 组内标准值/别名匹配
  → Operator 能力校验
  → MenuFilter
```

### 2.5 重构前车型名称和包含关系没有真正实现

当前少量别名规则不足以处理：

```text
Tesla / 特斯拉
ModelY / Model Y / model-y
BYD / 比亚迪
小米SU7 / 小米 SU 7 / SU7
宝马3系 / BMW 3 Series
```

目标需要版本化车辆实体目录，包含：

- 标准名称；
- 类型：品牌、车系、车型；
- 别名；
- 父实体；
- 品牌实体；
- 可选 Guide 菜单别名；
- 数据版本。

在实体关系确认后，FilterCompiler 才能执行：

```text
车型 > 车系 > 品牌
```

的父级 code 剔除。

### 2.6 当前负向和比较语义容易丢失

`operation` 表示如何修改历史 Requirement：

```text
add
replace
remove
```

`operator` 表示如何比较：

```text
eq
not_eq
gt
gte
lt
lte
in
not_in
contains
```

两者必须独立保存。

例如：

```text
“不要燃油车”
operation = add
operator = not_eq
```

不能使用 `operation=exclude` 后在 Session 合并时丢掉否定方向。

### 2.7 Baseline 菜单和分页状态不完整

目标要求：

- 菜单只能来自完全无筛选的 Guide 请求；
- 菜单、`context_id`、租赁条件指纹和接收时间必须来自同一次响应；
- 名义 TTL 为 15 分钟，业务安全复用时间建议为 14 分钟；
- 筛选响应返回的菜单不能覆盖 Baseline 菜单；
- “换一批/还有别的吗”必须延续同一个有效搜索快照；
- 条件变化时旧分页失效，从第一页重新搜索。

当前 Session 尚未完整表达这些状态。

## 3. 设计目标

### 3.1 功能目标

1. 顶层 Router 可以区分租车条件修改、车辆诉求修改、搜索请求和通用回复；搜索领域内部再区分首次、翻页、返回和刷新。
2. 车辆诉求提取不依赖 Guide 菜单，不生成任何 provider code。
3. LLM 输出固定 Schema，所有字段、枚举和跨字段约束可被严格校验。
4. 车辆品牌、车系、车型在映射菜单前完成名称归一和实体关系确认。
5. Requirement 到 `filterCode` 的映射必须经过 FacetRegistry 和 Baseline 菜单。
6. 任何无法映射、无法验证或存在歧义的诉求都不能静默丢失。
7. Pending 仅用于少量阻断型、有限选项、可回答的歧义。
8. SearchPolicy 根据最终 Session 状态确定是否搜索，不让 Router 推断自动搜索。
9. 搜索执行只接受内部 FilterPlan。
10. 分页继续上一次 SearchSnapshot，不重新提取诉求。
11. 最终回复明确区分已筛选、已验证、已排序、仅建议和无法验证的诉求。

### 3.2 非目标

- 不通过 LLM 猜测车型事实、舒适性、后备箱容量等车辆属性。
- 不实现供应商筛选。
- 不假设 Guide 多个 code 的 AND/OR 规则。
- 不在首轮重构中引入 Handler 并行执行。
- 不把用户画像或 TripContext 自动转换为车辆筛选条件。
- 不因实现困难而把所有未知车辆诉求归入 `custom`。

## 4. 目标架构

```text
HTTP / SSE
  ↓
WebChat Service
  ↓
RequestGuard
  ↓
PendingResolver
  ↓
TopLevelRouter
  ↓
Conditional Serial Orchestrator
  ├─ RentalContextHandler
  ├─ VehicleRequirementHandler
  ├─ VehicleEntityNormalizer
  ├─ SearchPolicy
  ├─ BaselineMenuProvider
  ├─ FilterCompiler
  ├─ SearchCarHandler
  └─ GeneralReplyHandler
  ↓
ResponseComposer
  ↓
SessionCommit
```

这是 Router 驱动的条件化串行 Pipeline：

- 阶段顺序固定；
- 每个阶段可以执行、跳过、等待或阻断依赖阶段；
- Handler 不直接调用下一个 Handler；
- Orchestrator 统一调度；
- 一轮最多一个 Active Blocking Pending；
- 产生 Pending 不等于立即结束整条 Pipeline。

## 5. 顶层 Action 设计

目标 Action：

```go
type ActionType string

const (
    ActionModifyRentalContext      ActionType = "modify_rental_context"
    ActionUpdateVehicleRequirements ActionType = "update_vehicle_requirements"
    ActionRequestVehicleSearch     ActionType = "request_vehicle_search"
    ActionGeneralReply             ActionType = "general_reply"
)
```

### 5.1 Action 边界

`modify_rental_context`

- 新增、修改、取消取还车地点；
- 新增、修改、取消取还车时间；
- 回答地点/时间类型的 Active Pending。

`update_vehicle_requirements`

- 新增、替换、删除车辆条件；
- 例如品牌、车型、座位、能源、预算、车龄、舒适性偏好。

`request_vehicle_search`

- 明确要求现在搜索；
- “直接搜”“帮我找一下”“都行，开始搜”“没有要求，搜吧”。
- 延续、返回或刷新当前搜索结果；
- “换一批”“还有别的吗”“下一页”“上一批”“刷新一下”仍属于这个 Action；
- 具体 `search_now/next_batch/previous_batch/refresh` 由搜索领域解析。

`general_reply`

- 纯知识问答、能力咨询、闲聊；
- 当前没有对应领域 Handler 的任务；
- 未分配文本的统一兜底。

### 5.2 Router 输出

```json
{
  "candidates": [
    {
      "action": "update_vehicle_requirements",
      "evidence_text": "想要七座SUV",
      "confidence": 0.99
    },
    {
      "action": "request_vehicle_search",
      "evidence_text": "直接搜",
      "confidence": 0.99
    }
  ],
  "unassigned_text": ""
}
```

Router 只做召回，不修改 Session。Domain Handler 必须再次校验 `domain_matched`，避免 Router 误判直接改变业务状态。

### 5.3 混合意图

用户输入：

```text
后天下午虹桥取，想要七座SUV，直接搜
```

Router 返回三个 Action，Orchestrator 按固定顺序执行：

```text
RentalContext
  → VehicleRequirement
  → SearchPolicy
  → FilterCompiler
  → SearchCar
```

## 6. Pipeline 调度契约

### 6.1 统一 Stage 结果

```go
type StageOutcome string

const (
    StageCompleted StageOutcome = "completed"
    StageSkipped   StageOutcome = "skipped"
    StageWaiting   StageOutcome = "waiting"
    StageRejected  StageOutcome = "rejected"
    StageFailed    StageOutcome = "failed"
)

type StageResult struct {
    Outcome StageOutcome

    StatePatch   *SessionPatch
    Pending      *PendingCandidate
    Events       []DomainEvent
    MessageHints []MessageHint

    BlockedStages []StageName
    ReasonCode    string
}
```

业务歧义和能力不足不得使用普通 error 表达：

- `waiting`：需要用户处理阻断型 Pending；
- `rejected`：业务条件冲突或不可执行；
- `failed`：LLM、Guide、Maps、存储等系统错误。

### 6.2 阶段执行表

| 阶段 | 执行条件 | 跳过条件 | 可阻断阶段 |
|---|---|---|---|
| RequestGuard | 每轮 | 无 | 全部 |
| PendingResolver | 有 Active Pending | 无 Active Pending | 由 Pending 类型决定 |
| Router | 存在未消费文本 | 文本被 Pending 完整消费 | 无 |
| RentalContextHandler | 有租车条件 Action 或租车 Pending 命令 | 无相关 Action | SearchPolicy 之后 |
| VehicleRequirementHandler | 有诉求修改 Action | 只有明确搜索或分页 | 仅依赖该诉求的搜索 |
| VehicleEntityNormalizer | 新增/变化的品牌、车系、车型尚未归一 | 无车辆实体变化 | FilterCompiler |
| SearchPolicy | 有状态变化、明确搜索、分页或延期搜索 | 纯通用回复 | Baseline 之后 |
| BaselineMenuProvider | 决定搜索且租车条件完整 | 不搜索或存在阻断 Pending | FilterCompiler |
| FilterCompiler | 决定搜索且 Baseline 有效 | 不搜索 | SearchCar |
| SearchCarHandler | FilterPlan 可执行，或存在有效 SearchSnapshot 的搜索操作 | 计划被阻断且没有可返回的历史批次 | 无 |
| GeneralReplyHandler | 有 general/unassigned | 纯领域操作 | 无 |
| ResponseComposer | 每轮 | 无 | 无 |
| SessionCommit | 有状态或消息变化 | 真正无变化 | 无 |

### 6.3 调度优先级

```text
Pending 消费
  → 租车条件修改
  → 车辆诉求修改
  → 车辆实体归一
  → Pending 候选协调
  → 搜索/分页决策
  → 运行时能力编译
  → Guide 搜索
  → 通用回复
  → 统一回复和提交
```

同一轮既有条件变化又有“下一批”时：

```text
条件变化优先
旧 SearchSnapshot 失效
按新条件从第一页搜索
```

## 7. VehicleRequirementHandler

### 7.1 职责

只负责：

- 构造 LLM 提取输入；
- 严格解析提取结果；
- 执行字段级确定性归一；
- 标记需要 VehicleEntityNormalizer 处理的车辆实体；
- 合并历史 Requirement；
- 检测同 Facet 更新和显式冲突；
- 产生 Requirement StatePatch；
- 产生必要的 PendingCandidate 或能力状态。

不负责：

- 获取 Guide 菜单；
- 生成 `filterCode`；
- 判断是否应该搜索；
- 调用 Guide；
- 保存搜索结果。

### 7.2 LLM 输入

```json
{
  "source_text": "本轮车辆领域原文",
  "current_requirements": [
    {
      "facet": "brand",
      "raw_value": "丰田",
      "canonical_value": "丰田",
      "operator": "eq",
      "importance": "hard",
      "status": "active"
    }
  ],
  "recent_domain_history": [
    "上一轮车辆诉求相关原文"
  ]
}
```

不得提供：

```text
Guide filterCode
sortCode
groupCode
context_id
POI ID
供应商 code
完整菜单 code
```

菜单不属于语义提取阶段。菜单变化不应该导致相同用户原文被提取为不同 JSON Key。

### 7.3 LLM 输出

```json
{
  "requirements": [
    {
      "facet": "vehicle_model",
      "raw_text": "特斯拉 Model Y",
      "raw_value": "Model Y",
      "operation": "replace",
      "operator": "eq",
      "importance": "hard",
      "confidence": 0.99,
      "entity_context": {
        "brand_hint": "特斯拉",
        "series_hint": ""
      }
    }
  ],
  "domain_matched": true
}
```

RequirementExtractor 不输出搜索指令。纯“直接搜/都行”由 Router 产生
`request_vehicle_search`，不调用 RequirementExtractor；“品牌不限，直接搜”
由 Router 同时产生 `update_vehicle_requirements` 和 `request_vehicle_search`，
RequirementExtractor 只负责删除品牌 Requirement。

### 7.4 固定 Facet

`seat_num`

- 座位数量；
- “七座”通常为 `eq 7`；
- “至少七座”为 `gte 7`。

`vehicle_type`

- SUV、MPV、轿车、跑车等车辆类别；
- 不能放品牌、车系、车型。

`price_preference`

- 日均价、总价、价格上限、价格优先；
- 金额、币种、计价周期应结构化。

`car_age`

- 明确车龄、年份或“尽量新”的偏好；
- 没有真实车龄字段时不能参与筛选或排序。

`comfort_preference`

- 明确的乘坐、座椅、静谧、悬架或长途舒适偏好；
- 不能自动映射为 SUV、MPV；
- 当前无可靠车辆字段时为 advisory/unverifiable。

`energy_type`

- 燃油、纯电、插混、混动等；
- 必须依赖正式枚举和菜单能力。

`transmission`

- 自动挡、手动挡等；
- 必须依赖正式枚举和菜单能力。

`brand`

- 品牌实体。

`vehicle_series`

- 车系实体。

`vehicle_model`

- 当前车辆目录定义的最具体车型实体。

`custom`

- 确实属于车辆选择条件，但标准 Facet 无法无损表达的诉求；
- 不能作为未知文本垃圾桶；
- 不能自动变为 RankFactor。

### 7.5 Operation 和 Operator

```go
type RequirementOperation string

const (
    OperationAdd     RequirementOperation = "add"
    OperationReplace RequirementOperation = "replace"
    OperationRemove  RequirementOperation = "remove"
)

type ComparisonOperator string

const (
    OperatorEQ       ComparisonOperator = "eq"
    OperatorNotEQ    ComparisonOperator = "not_eq"
    OperatorGT       ComparisonOperator = "gt"
    OperatorGTE      ComparisonOperator = "gte"
    OperatorLT       ComparisonOperator = "lt"
    OperatorLTE      ComparisonOperator = "lte"
    OperatorIN       ComparisonOperator = "in"
    OperatorNotIN    ComparisonOperator = "not_in"
    OperatorContains ComparisonOperator = "contains"
)
```

示例：

| 用户原文 | Operation | Operator |
|---|---|---|
| 改成小米 | replace | eq |
| 也看看丰田 | add | eq |
| 品牌不限 | remove | eq |
| 不要燃油车 | add | not_eq |
| 至少七座 | replace | gte |
| 每天不超过300 | replace | lte |

### 7.6 Importance

```text
hard
soft
```

- Importance 表示用户强弱，不表示模型置信度；
- LLM 提取置信度不随轮次衰减；
- 用户明确硬诉求不能自动降级为软排序；
- 系统从 TripContext 推断的内容不能直接成为 soft Requirement。

### 7.7 Requirement State

LLM 不生成 ID。服务端合并成功后生成 ID。

```go
type RequirementState struct {
    ID string

    Facet    RequirementFacet
    RawText  string
    RawValue RequirementValue

    CanonicalValue *CanonicalRequirementValue
    Operator       ComparisonOperator
    Importance     RequirementImportance

    Resolution RequirementResolution
    Status     RequirementStatus

    CreatedAt       time.Time
    UpdatedAt       time.Time
    LastMentionedAt time.Time
    SupersededBy    string
}
```

状态：

```text
active
ambiguous
unverifiable
unsupported
superseded
removed
```

## 8. 字段级确定性归一

车辆实体之外的常见字段优先由确定性代码归一：

```text
七座 → seat_num=7, operator=eq
至少七座 → seat_num=7, operator=gte
自动波 → transmission=自动挡
越野车 → vehicle_type=SUV
每天300以内 → daily_price<=300 CNY
```

归一化输出必须区分：

```text
raw_value：用户原始表达
canonical_value：服务端确认后的标准值
```

LLM 可以输出受控枚举建议，但最终 CanonicalValue 由服务端验证器产生。

闭集字段使用版本化词典：

```go
type AliasDictionary struct {
    Version string
    Facets  map[RequirementFacet]map[string]CanonicalRequirementValue
}
```

所有去重集合使用 `map[T]struct{}`。

## 9. VehicleEntityNormalizer

### 9.1 车辆实体目录

```go
type VehicleEntity struct {
    ID            string
    Type          VehicleEntityType
    CanonicalName string
    Aliases       []string

    ParentID string
    BrandID  string

    CatalogVersion string
}
```

实体类型：

```text
brand
series
model
```

目录来源优先级：

```text
权威车辆基础库
  → 业务维护的版本化别名配置
  → Guide Baseline 菜单名称
  → 确定性格式规则
```

LLM 只能协助召回候选，不能确认实体 ID 或 provider code。

### 9.2 解析结果

```go
type EntityResolutionStatus string

const (
    EntityExact     EntityResolutionStatus = "exact"
    EntityAlias     EntityResolutionStatus = "alias"
    EntityAmbiguous EntityResolutionStatus = "ambiguous"
    EntityNotFound  EntityResolutionStatus = "not_found"
)

type EntityResolution struct {
    Status EntityResolutionStatus

    EntityID      string
    EntityType    VehicleEntityType
    CanonicalName string
    ParentIDs     []string

    Candidates []VehicleEntityCandidate
    Evidence   []ResolutionEvidence
}
```

### 9.3 “特斯拉 Model Y”处理

提取阶段：

```text
facet=vehicle_model
raw_value=Model Y
brand_hint=特斯拉
```

归一阶段：

```text
Model Y → canonical entity
brand_hint 特斯拉 → 验证父品牌
```

FilterCompiler 不生成独立的特斯拉品牌约束，除非用户明确表达的是独立品牌条件。

### 9.4 层级剔除规则

```text
vehicle_model > vehicle_series > brand
```

仅在以下条件全部满足时剔除父级：

1. 子级和父级属于同一实体路径；
2. 子级 Requirement 已经可靠归一；
3. 子级能够被当前 FilterPlan 执行或验证；
4. 删除父级不会丢失用户独立表达的其他约束；
5. Guide code 组合语义已经确认。

示例：

```text
特斯拉 Model Y
```

结果：

```text
保留 Model Y
品牌“特斯拉”只作实体上下文，不生成品牌 code
```

示例：

```text
特斯拉的 SUV 都可以
```

结果：

```text
保留 brand=特斯拉
保留 vehicle_type=SUV
```

因为它们属于两个独立 Facet。

### 9.5 不允许静默降级

如果精确车型无法映射，而品牌可以映射：

- hard 车型：不能偷偷改成品牌搜索；
- soft 车型：可以执行其他条件，但该车型标为未应用；
- 只有用户确认“按这个品牌扩大范围也可以”后才能生成替代 Requirement。

## 10. Pending 收敛方案

### 10.1 Pending 定位

Pending 仅表示：

```text
必须绑定当前交互上下文，
用户下一轮回答后才能安全继续的阻断型问题。
```

系统提出问题不等于必须创建 Pending。

### 10.2 保留的 Blocking Pending

```text
select_location
clarify_time
select_vehicle_entity
resolve_hard_conflict
```

`select_vehicle_entity` 仅在以下条件全部满足时使用：

1. 用户表达的是硬条件；
2. 有两个或多个真实候选；
3. 候选会产生不同搜索结果；
4. 上下文无法消歧；
5. 不确认就不能可靠执行；
6. 可以提供有限选项。

唯一标准名、唯一别名、品牌上下文唯一命中都不创建 Pending。

完全没有候选时返回 `not_found`，不创建无选项 Pending。

### 10.3 非 Blocking Prompt

以下不创建 Active Pending：

- 首次询问车辆偏好；
- 软诉求无法映射；
- 车型名称完全找不到；
- 舒适性或行李容量无法验证；
- 建议用户放宽条件；
- 建议向门店确认；
- 普通能力说明。

这些作为 `MessageHint` 或 RequirementResolution 保存。

### 10.4 首次无诉求询问

使用 SearchGoal 状态，不使用 Blocking Pending：

```go
type SearchGoalState struct {
    Status             SearchGoalStatus
    PreferenceAskCount int
    NoPreference       bool
    LastAskedAt        time.Time
}
```

用户回答“都行/直接搜”时，Router 产生 `request_vehicle_search`。SearchPolicy
根据该 Action、证据文本和当前无有效车辆诉求的状态直接搜索，并在明确表达
“不限/都行”时更新 `NoPreference`；RequirementExtractor 不参与纯搜索指令。

### 10.5 Pending 优先级

```text
地点/时间必要条件
  > 硬车型实体歧义
  > 硬条件冲突
  > 非阻断提示
```

一轮最多激活一个 Blocking Pending。

其他 PendingCandidate 不冻结成队列问题，只保存重新评估事件。当前 Pending 解决后，必须基于最新 Session 重新计算是否仍需询问。

## 11. RequirementCapabilityResolver

### 11.1 能力状态

```go
type RequirementCapability string

const (
    CapabilityFilterable   RequirementCapability = "filterable"
    CapabilityVerifiable   RequirementCapability = "verifiable"
    CapabilityRankable     RequirementCapability = "rankable"
    CapabilityAdvisory     RequirementCapability = "advisory"
    CapabilityAmbiguous    RequirementCapability = "ambiguous"
    CapabilityUnverifiable RequirementCapability = "unverifiable"
    CapabilityUnsupported  RequirementCapability = "unsupported"
)
```

含义：

- `filterable`：可以生成 Guide 菜单筛选；
- `verifiable`：可以通过真实报价/车型字段二次验证；
- `rankable`：可以使用可靠字段对候选排序；
- `advisory`：只影响提示、确认建议；
- `ambiguous`：候选歧义；
- `unverifiable`：当前没有足够数据；
- `unsupported`：产品明确不支持。

“无法验证”不能描述为“无法满足”。

### 11.2 Capability 解析顺序

```text
确定性字段规则
  → 受控别名词典
  → 车辆实体目录
  → Guide 报价字段能力
  → 无筛选 Baseline 菜单
  → 有限候选消歧
  → advisory / ambiguous / unverifiable / unsupported
```

如果需要受限 LLM 协助候选选择：

- 只提供候选序号和展示名称；
- LLM 只能返回候选序号；
- 真实实体 ID 和 code 仍由服务端根据序号取得；
- 低置信度不得自动选中。

## 12. FacetRegistry 和菜单匹配

### 12.1 FacetRegistry

```go
type FacetDefinition struct {
    Facet RequirementFacet

    MenuGroupMatchers []MenuGroupMatcher
    SupportedOps      []ComparisonOperator
    ResolverType      ResolverType

    QuoteField string
    RankField  string
}
```

用途：

- 把稳定语义 Facet 映射到可能变化的 Guide 菜单组；
- 限定 Operator 能力；
- 确定是否可以使用报价字段验证；
- 防止跨菜单组名称误匹配。

FacetRegistry 是服务端版本化配置，不放入 LLM Prompt。

### 12.2 菜单索引

```go
type MenuIndex struct {
    GroupsByFacet map[RequirementFacet][]MenuGroupRef
    ItemsByFacet  map[RequirementFacet]map[string][]MenuItemRef
    ItemsByCode   map[string]MenuItemRef
}
```

构建过程：

```text
Guide MenuGroup
  → FacetRegistry 识别菜单组
  → 保存组内标准名、别名和 code
  → 只允许 Facet 内匹配
```

### 12.3 映射结果

```go
type MenuMappingStatus string

const (
    MappingExact       MenuMappingStatus = "exact"
    MappingAlias       MenuMappingStatus = "alias"
    MappingAmbiguous   MenuMappingStatus = "ambiguous"
    MappingUnsupported MenuMappingStatus = "unsupported"
    MappingNotFound    MenuMappingStatus = "not_found"
)

type RequirementResolution struct {
    RequirementID string
    Status        MenuMappingStatus
    Capability    RequirementCapability

    MenuCandidates []MenuItemRef
    QuotePredicate *QuotePredicate
    RankFactor     *RankFactor

    ReasonCode string
    Evidence   []ResolutionEvidence
}
```

### 12.4 Operator 能力

菜单有某个值不代表所有 Operator 都可执行。

例如菜单只有：

```text
7座
```

它可以支持：

```text
seat_num eq 7
```

但不一定支持：

```text
seat_num gte 7
seat_num not_eq 7
```

如果 Quote 中有真实 Seats 字段，可以将范围操作编译成 QuoteFilter；否则必须标记 unverifiable/unsupported，不能错误转换。

### 12.5 Guide AND/OR 契约

在 Guide 契约明确前，不假设：

- 同一 Group 多个 code 是 AND 还是 OR；
- 不同 Group 多个 code 是 AND 还是 OR；
- GroupCode 对请求结果的精确影响；
- SortCode 和 FilterCode 是否互斥。

FacetRegistry 上线前必须通过 Guide 文档或真实集成测试确认这些规则。

## 13. FilterCompiler

### 13.1 输入和输出

输入：

```go
type CompileInput struct {
    Requirements []RequirementState
    Baseline     *GuideBaselineCache
    EntityCatalogVersion string
    FacetRegistryVersion string
}
```

输出：

```go
type FilterPlan struct {
    MenuFilters  []MenuFilter
    QuoteFilters []QuoteFilter

    ServerSort   *ServerSort
    LocalRanking []RankFactor

    Verifications []Verification
    Advisory      []RequirementResolution
    Unverifiable  []RequirementResolution
    Ambiguous     []RequirementResolution

    PlanHash string
}
```

### 13.2 编译步骤

```text
验证 Requirement 状态
  → 合并同 Facet 范围
  → 构建车辆实体关系图
  → 检测父子、互斥、正负和 OR 冲突
  → 解析每个 Facet 的运行时能力
  → 删除会扩大语义的父级 code
  → 生成最小 MenuFilter
  → 生成 QuoteFilter/Verification
  → 为有真实字段的 soft 诉求生成排序
  → 记录未应用诉求
  → 计算稳定 PlanHash
```

### 13.3 FilterPlan 内部所有权

目标 `SearchCarInput` 不再接收：

```text
FilterCodes
SortCode
GroupCode
ContextID
```

这些只能由：

```text
FilterCompiler
BaselineMenuProvider
SearchSnapshotManager
```

产生。

前端只能发送自然语言、分页/交互标识和请求幂等信息。

### 13.4 父子 code 最小化

同一实体路径上：

```text
model code 可执行 → 删除 series/brand code
series code 可执行且 model 不可执行 → 不得自动降级，除非用户同意
brand code 可执行且 model/series 不可执行 → 不得自动降级，除非用户同意
```

如果具体模型通过 QuoteFilter 可验证，品牌 code 是否还能保留必须根据 Guide AND/OR 规则判断；默认不使用会扩大范围的父级 code。

## 14. RankFactors 是否保留

保留，但严格限制。

RankFactor 只用于：

```text
用户明确表达的 soft 偏好
+
当前系统存在真实可比较字段
```

首批候选：

- 价格；
- 座位数；
- 品牌；
- 已确认枚举的能源类型；
- 已确认枚举的变速箱类型。

不能生成 RankFactor 的示例：

- 舒适一点；
- 安静一点；
- 后备箱放两个 28 寸箱；
- 底盘软一点；
- 适合老人。

这些在缺少可靠数据时是 advisory/unverifiable。

### 14.1 服务端排序优先

```text
Guide SortCode
  > 完整候选集的本地排序
  > 当前已获取集合的本地排序
  > advisory/unverifiable
```

如果只获取第一页，本地排序只能声明作用于当前 fetched set，不能声称是全量最优。

```go
type RankingScope string

const (
    RankingServerGlobal RankingScope = "server_global"
    RankingFetchedSet   RankingScope = "fetched_set"
)
```

## 15. Guide Baseline 菜单缓存

### 15.1 缓存结构

```go
type GuideBaselineCache struct {
    RentalFingerprint string

    ContextID string
    Menu      []MenuGroupSnapshot
    BaseQuotes []VehicleQuoteSnapshot

    FirstReceivedAt  time.Time
    ServiceExpiresAt time.Time
    SafeExpiresAt    time.Time

    Source   MenuSource
    Complete bool
}
```

菜单、ContextID、租赁指纹和时间必须来自同一次响应，并整体替换。

### 15.2 Baseline 请求

必须满足：

```text
ContextID = ""
FilterCodes = nil/empty
SortCode = ""
GroupCode = ""
Page = 1
```

不得携带用户诉求。

### 15.3 缓存有效性

```text
ServiceExpiresAt = FirstReceivedAt + 15min
SafeExpiresAt    = FirstReceivedAt + 14min
```

复用条件：

```text
now < SafeExpiresAt
RentalFingerprint 一致
ContextID 非空
Menu 完整
```

失效条件：

- 地点变化；
- 取车时间变化；
- 还车时间变化；
- 租赁指纹变化；
- 安全 TTL 到期；
- ContextID 或菜单缺失；
- Guide 明确返回上下文失效。

### 15.4 筛选响应隔离

筛选响应中的 `menu_group` 不覆盖 Baseline.Menu。

```go
type FilteredSearchSnapshot struct {
    RentalFingerprint string
    BaseContextID      string
    ResponseContextID  string
    FilterPlanHash     string
    AppliedCodes       []string
    Quotes             []VehicleQuoteSnapshot
    ReceivedAt         time.Time
}
```

## 16. SearchPolicy

### 16.1 输入

```go
type SearchPolicyInput struct {
    ExplicitSearchRequested bool
    SearchRequestEvidence   string
    RequestedOperation      SearchOperation

    RentalContextChanged bool
    RequirementsChanged  bool
    HadPreviousSearch    bool

    RentalContextComplete bool
    HasActiveRequirements bool
    ActivePending         *PendingInteraction

    PreferenceAskCount int
}
```

### 16.2 输出

```go
type SearchDecision string

const (
    SearchNowFresh      SearchDecision = "search_now_fresh"
    SearchContinue      SearchDecision = "search_continue"
    SearchAskPreference SearchDecision = "ask_preference"
    SearchWaitPending   SearchDecision = "wait_pending"
    SearchSkip          SearchDecision = "skip"
)
```

### 16.3 决策顺序

```text
存在阻断搜索的 Pending
  → wait_pending

租车条件不完整
  → skip

本轮租车条件或车辆诉求变化
  → 旧分页失效
  → 有明确诉求则 fresh search
  → 之前搜过则 fresh search

纯分页动作且 SearchSnapshot 有效
  → continue

用户明确直接搜/都行
  → fresh search

首次搜索、条件完整、有明确车辆诉求
  → fresh search

首次搜索、条件完整、无诉求、询问次数未达到上限
  → ask_preference

首次搜索、条件完整、无诉求、已经询问1～2次
  → fresh search

其他
  → skip
```

Router 不决定“条件变化后自动搜索”。SearchPolicy 必须在 RentalContext 和 Requirement StatePatch 应用后运行。

## 17. SearchCarHandler

目标职责：

- 接收可执行 FilterPlan；
- 使用有效 Baseline/SearchSnapshot 中的 ContextID；
- 组装 Guide 请求；
- 调用 Guide；
- 执行 QuoteFilter 和 Verification；
- 执行合法的本地排序；
- 返回事实结果；
- 不生成自然语言；
- 不重新提取诉求；
- 不重新全局匹配菜单。

目标接口：

```go
type SearchCarInput struct {
    Operation    SearchOperation
    EvidenceText string
    PageSize     int
}
```

FilterPlan、Baseline 和 `context_id` 仍由内部确定性组件产生或注入，不允许调用方直接提交任意值。

## 18. 分页和“换一批”

### 18.1 搜索领域内部操作

```text
request_vehicle_search
  → search_now | next_batch | previous_batch | refresh
```

导航操作：

```text
next_batch
previous_batch
refresh
restart
```

示例：

| 用户话术 | 操作 |
|---|---|
| 换一批 | next_batch |
| 还有别的吗 | next_batch |
| 下一页 | next_batch |
| 上一批 | previous_batch |
| 刷新一下 | refresh |
| 重新搜一下 | restart |
| 换个车型 | update_vehicle_requirements |

### 18.2 SearchSnapshot

```go
type ActiveSearchSnapshot struct {
    SearchID string

    RentalFingerprint  string
    RequirementVersion int64
    FilterPlanHash     string

    BaselineContextID     string
    ContinuationContextID string

    FilterPlan FilterPlan

    CurrentPage int
    PageSize    int
    NextPage    int

    SeenQuoteIDs     []string
    SeenVehicleCodes []string
    Batches          []SearchResultBatch

    Status    SearchSnapshotStatus
    CreatedAt time.Time
    ExpiresAt time.Time
}
```

### 18.3 翻页规则

纯“换一批”：

- 不调用 RequirementExtractor；
- 不重新生成 FilterPlan；
- 校验 RentalFingerprint、RequirementVersion、PlanHash 和上下文有效期；
- 使用相同租赁条件、筛选、排序和 PageSize；
- 只推进 Page/ContinuationContext；
- 去除已展示结果；
- 保存新 Batch。

条件变化：

- 旧快照标记 `superseded`；
- 重新取得或校验 Baseline；
- 重新编译 FilterPlan；
- 从第一页搜索。

### 18.4 去重

展示单位为车型：

```text
VehicleCode
```

展示单位为报价：

```text
ReferenceID
```

可同时保存两者。`SupplierCode` 可以作为报价身份的一部分保留，但不能恢复供应商筛选能力。

如果一页大部分结果重复，可以继续向后读取补齐，但单轮最多追加读取固定页数，例如 3 页，避免无限请求。

### 18.5 是否还有更多

优先使用 Guide 的正式字段：

```text
has_more
total
next_page
next_cursor
```

如果 Guide 当前没有：

- 空页可以标记 exhausted；
- 少于 PageSize 是否代表结束必须由 Guide 契约确认；
- 不能自行假设。

### 18.6 ContextID 契约

必须确认翻页使用：

- BaselineContextID；
- 第一次筛选响应 ContextID；
- 还是上一页响应返回的 ContinuationContextID。

代码和文档中应使用不同字段名表达，禁止继续用单一 `ContextID` 混合多种生命周期。

## 19. 无法映射和无法验证的处理

RequirementState 保留用户表达，FilterPlan 只保存实际可执行内容。

例如：

```text
必须七座
最好安静
后备箱必须放两个28寸箱
```

可能得到：

```text
七座                    filterable
安静                    unverifiable/advisory
两个28寸行李箱          unverifiable
```

最终回复必须说明：

```text
已筛选：七座
未验证：车内静谧性、两个28寸行李箱容量
```

### 19.1 hard 无法验证

- 默认不声称结果满足；
- 如果这是唯一/核心硬条件，优先说明能力边界并询问是否接受替代；
- 用户明确“先搜出来我自己确认”后，可以按其他条件搜索；
- 搜索结果状态必须是 partial/capability_limit，不得标为完全满足。

### 19.2 soft 无法验证

- 可以继续执行其他条件；
- 不生成虚假的 RankFactor；
- 在回复中说明未应用；
- 可以提出非阻断替代建议。

### 19.3 custom 生命周期

- 当前 SearchGoal 内保留；
- 被用户替换或删除时 superseded/removed；
- SearchGoal 重置后不自动带入新任务；
- 高频 custom 用于分析新的 Facet/数据能力，不进入长期用户画像。

## 20. ResponseComposer

所有 Domain Handler 返回结构化事实，不各自生成最终回复。

输出事实分类：

```go
type AppliedRequirementSummary struct {
    Filtered     []RequirementSummary
    Verified     []RequirementSummary
    Ranked       []RequirementSummary
    Advisory     []RequirementSummary
    Unverifiable []RequirementSummary
}
```

回复规则：

1. 不把 advisory/unverifiable 写成已满足；
2. fetched-set 本地排序不描述为全量最优；
3. 模型/品牌归一采用可撤销解释，例如“已按特斯拉 Model Y 理解”；
4. Pending 问题最多一个；
5. 有搜索结果时先返回结果，再简要说明未应用软诉求；
6. hard 能力不足必须明显说明；
7. 不暴露内部 code、context ID、实体 ID。

## 21. SSE 进度事件

同步 Pipeline 可以通过 SSE 返回非业务状态：

```text
accepted
understanding
resolving_requirements
searching
composing
result
done
```

进度事件只表示处理阶段，不泄露内部推理，也不作为业务提交状态。

跳过阶段不需要发送事件。例如纯翻页可以直接：

```text
accepted
searching
result
done
```

## 22. SessionDraft、提交和版本冲突

### 22.1 TurnDraft

一轮内先在 Draft 上应用：

- Pending 消费；
- 租车条件更新；
- Requirement 更新；
- SearchGoal 更新；
- Baseline/SearchSnapshot 更新。

```go
type TurnDraft struct {
    BaseVersion int64
    State       AgentSession
    Events      []DomainEvent
}
```

Confirmed Requirement 在后续搜索被 Pending 阻断时仍应保存，避免混合输入丢失诉求。

### 22.2 提交时机

建议一次逻辑回合统一提交，但要区分业务变化和外部搜索失败：

- 已确认的地点、时间和车辆诉求不能仅因 Guide 临时失败而丢失；
- 搜索失败不写入成功 SearchSnapshot；
- 可以提交状态变更和 `search_failed` 事件；
- 最终回复提示稍后重试。

### 22.3 版本冲突

Session 保存发现版本号不一致：

1. 重新读取最新 Session；
2. 使用原始输入和 RequestID 检查是否已完成；
3. 对纯确定性 StatePatch 做一次重新校验/重放；
4. 涉及外部结果或 Pending 选项已变化时，不直接覆盖；
5. 单 Session 当前已有串行锁时仍保留版本检查，防止多实例并发；
6. 重试次数达到上限后返回可重试冲突，不覆盖新状态。

## 23. 错误和回退

### 23.1 Router 错误

- Router 是召回层；
- Domain Handler 返回 `domain_matched=false` 时视为 StageSkipped；
- 未处理原文交给 GeneralReply；
- 不能因为 Router 误判直接修改 Session 或调用 Guide。

### 23.2 LLM Schema 错误

- 严格 JSON 解码；
- 拒绝未知字段；
- 校验所有必填字段、枚举、空值和跨字段约束；
- 可以进行一次受控重试；
- 仍失败则保留原文并生成可理解的失败回复，不执行搜索。

### 23.3 Guide 错误

- 不丢失已经确认的 Session 状态；
- 不写成功搜索快照；
- Baseline 请求失败和筛选请求失败分别统计；
- Context 失效可重新创建 Baseline 并重试一次；
- 不能无限重试。

### 23.4 Fallback

GeneralReply 可以结合历史对话回复，但不能：

- 修改车辆诉求；
- 生成 provider code；
- 声称执行了搜索；
- 绕过 Pending 或 SearchPolicy。

## 24. 推荐包布局

兼容当前仓库，建议逐步形成：

```text
internal/router/
    types.go
    router.go
    contract.go

internal/orchestrator/
    orchestrator.go
    stage.go
    pipeline_context.go

internal/domain/vehiclerequirement/
    types.go
    extractor.go
    contract.go
    handler.go
    merger.go
    normalizer.go

internal/vehiclecatalog/
    interface.go
    entity.go
    resolver.go
    aliases.go

internal/searchpolicy/
    policy.go
    types.go

internal/searchplan/
    facet_registry.go
    menu_index.go
    capability.go
    compiler.go
    reducer.go
    rank.go

internal/domain/searchcar/
    handler.go
    request.go
    result.go

internal/searchsnapshot/
    baseline.go
    active_search.go
    navigation.go

internal/session/
    model.go
    requirement.go
    search_goal.go
    pending.go

internal/response/
    composer.go
```

迁移初期不必一次移动全部文件，可以先在 `internal/domain/searchcar` 内创建清晰接口，再逐步搬迁，降低大范围包重命名风险。

### 24.1 当前文件到目标职责

| 当前文件 | 当前主要问题 | 目标职责/迁移位置 |
|---|---|---|
| `internal/router/*` | 已完成 | 新 Action、严格合同和分页证据路由 |
| `internal/webchat/service.go` | 已完成 | 构造多 Action TurnRequest 和 GeneralReply 上下文 |
| `internal/orchestrator/orchestrator.go` | 已完成首版 | 条件化串行调度；统一 Stage DTO 可在持久化改造时再抽取 |
| `internal/domain/vehiclerequirement/*` | 已完成首版 | 独立提取、严格合同、归一和合并 |
| `internal/vehiclecatalog/*` | 已完成种子版 | Resolver 接口、别名、父子关系；待接权威数据 |
| `internal/searchplan/*` | 已完成首版 | Facet-aware 编译、能力判断、冲突和父级消解 |
| `internal/domain/searchcar/*` | 已完成首版 | Baseline、计划执行、可信结果和分页 |
| `internal/session/search_state.go` | 已完成 | SearchGoal、Baseline、ActiveSearchSnapshot 分区 |
| `internal/session/pending.go` | 已完成首版 | 单 Active Blocking Pending 和 Deferred revalidation |
| `internal/domain/generalreply/*` | 已完成首版 | 只读历史感知兜底 |
| `internal/webchat/format.go` | 已完成首版 | 结构化领域事实统一组合；后续可独立成 response 包 |

### 24.2 阶段和代码落点

| 阶段 | 首要修改文件/包 | 首要新增文件/包 |
|---|---|---|
| 0 | `api/guide/dto.go`、Guide 真实集成测试、设计文档 | Guide 契约记录和回归对话数据 |
| 1 | `internal/router/*`、`internal/webchat/service.go`、`internal/orchestrator/orchestrator.go` | `stage.go`、`pipeline_context.go`、SearchPolicy 接口 |
| 2 | `internal/domain/searchcar/extractor.go`、`extract_contract.go`、`types.go`、Session | `domain/vehiclerequirement/*`、Requirement 迁移适配 |
| 3 | Requirement Handler | `internal/vehiclecatalog/*` |
| 4 | `internal/domain/searchcar/handler.go`、Session 缓存字段 | `internal/searchplan/*`、`internal/searchsnapshot/baseline.go` |
| 5 | Orchestrator、Pending、Session SearchGoal | `internal/searchpolicy/*`、Pending 协调器 |
| 6 | `internal/domain/searchcar/handler.go`、`webchat/format.go` | Search 执行 DTO、`internal/response/*` |
| 7 | Router、SearchPolicy、Session | `internal/searchsnapshot/active_search.go`、`navigation.go` |
| 8 | 所有兼容入口 | 指标、灰度配置和运维文档 |

## 25. 分阶段实施计划

### 阶段 0：契约确认和基线冻结

#### 目标

在改代码前确认 Guide 行为，冻结当前回归样本和状态模型。

#### 工作项

1. 确认 Guide 菜单：
   - GroupCode/ItemCode 稳定性；
   - 同组多个 code 的 AND/OR；
   - 跨组多个 code 的 AND/OR；
   - SortCode/GroupCode 语义；
   - Filter 后菜单是否为裁剪菜单。
2. 确认 ContextID：
   - Baseline Context；
   - 筛选响应 Context；
   - 翻页 Continuation Context；
   - 15 分钟 TTL 起点。
3. 确认分页：
   - `has_more/total/next_page` 是否存在；
   - 少于 PageSize 是否表示结束。
4. 确认车辆字段枚举：
   - energy/fuel；
   - transmission；
   - 车龄和车型字段是否存在。
5. 保存代表性真实菜单和搜索响应的脱敏结构快照，用于契约测试；外部服务测试仍使用真实 client。
6. 建立回归对话集。

#### 验收

- Guide 未确认项有明确负责人和结论；
- 当前线上/开发环境行为有可复现记录；
- 不再用代码猜测 AND/OR、TTL 或枚举。

#### 回滚

无运行时代码变更。

### 阶段 1：拆 Action 和条件化串行骨架

#### 目标

先解决“需求提取和搜车 Handler 合并”的结构问题，不改变当前实际搜车结果。

#### 工作项

1. Router 增加：
   - `update_vehicle_requirements`；
   - `request_vehicle_search`。
2. Router Prompt 和严格 Contract 增加新 Action。
3. 增加 PipelineContext、StageOutcome。
4. 从 SearchCarHandler 抽出 VehicleRequirementHandler 接口。
5. 增加 SearchPolicy，但第一版可通过兼容配置复现当前搜索触发行为。
6. SearchCarHandler 保留旧编译逻辑，输入暂时通过内部适配器产生。
7. webchat 只为 Router 选中的 Action 构造输入。
8. 保持执行顺序：

```text
RentalContext → VehicleRequirement → SearchPolicy → SearchCar
```

#### 当前仓库落地策略

- 当前仓库使用内存 Session，没有需要长期读取的旧持久化记录，因此直接删除旧 `search_car` Action 和可注入 code/context 的 `SearchCarCommand`；
- 生产若存在旧持久化 Session，仍应在接入层增加一次性双读适配和 Feature Flag，不能把兼容字段重新暴露给前端或 LLM。

#### 验收

- 只修改车辆诉求时可以更新 Session，而不必调用 Guide；
- “直接搜”可以不调用 RequirementExtractor；
- 纯地点时间输入不调用车辆提取；
- 混合输入按 RentalContext → Requirement 串行执行；
- Router 误判被 Domain `domain_matched` 防御；
- 所有现有搜索回归结果保持一致。

#### 回滚

关闭新 Pipeline Feature Flag，恢复旧 Action 适配。

### 阶段 2：Requirement Schema 和状态迁移

#### 目标

固定 LLM 输出和 Session Requirement 模型，彻底分离 operation/operator。

#### 工作项

1. 新增目标 Facet；
2. `seat_count → seat_num`；
3. `fuel_type → energy_type`；
4. `price → price_preference`；
5. 增加 `car_age`、`comfort_preference`、`vehicle_series`、`custom`；
6. Prompt 明确完整 JSON Schema 和示例；
7. 严格 Decoder 校验未知/缺失字段；
8. LLM 不输出 ID；
9. 服务端生成 Requirement ID；
10. Session 保存 Operator、CanonicalValue、Resolution；
11. 生产存在旧持久化 Session 时编写旧模型读取适配；当前内存 Session 无迁移数据；
12. 同 Facet 默认替换，“也看看”才 add；
13. 删除 `operation=exclude`，迁移为 `operator=not_eq/not_in`。

#### 验收

- Prompt 中所有 Facet、字段和枚举固定；
- “特斯拉 Model Y”不会生成 filterCode；
- “不要燃油车”否定方向经过 Session 合并后仍存在；
- 未识别车辆诉求进入受控 custom 或明确不匹配，不丢失；
- 新旧 Session 可在灰度期双读；
- 真实 LLM 合同测试覆盖单域、混合域、删除、替换、否定和无偏好。

#### 回滚

- 保留旧 Requirement 读取适配；
- 写入可双写新旧摘要，回滚后旧逻辑仍可读取。

### 阶段 3：车辆实体归一

#### 目标

建立品牌、车系、车型的权威名称统一和父子关系，不依赖 Prompt 猜标准名。

#### 工作项

1. 定义 VehicleCatalog 接口；
2. 接入权威实体数据或首版版本化配置；
3. 建立 aliases、ParentID、BrandID；
4. 实现 Exact/Alias/Ambiguous/NotFound；
5. 支持 brand_hint/series_hint；
6. 添加车型实体解析证据；
7. 按收敛策略实现极少数 select_vehicle_entity Pending；
8. 唯一高可信文字修正使用可撤销提示；
9. 完全找不到车型不创建无选项 Pending；
10. 建立别名命中、歧义和未命中指标。

#### 验收

- Tesla/特斯拉、ModelY/Model Y 等代表性别名唯一归一；
- 车型实体包含可验证父品牌；
- 软车型歧义不阻断其他搜索；
- hard 多候选且无法消歧时才创建 Pending；
- 没有目录数据时明确 not_found，不自动降级品牌。

#### 回滚

- Feature Flag 关闭实体归一，只保留语义 Requirement；
- 不回滚已经保存的 RawText/RawValue。

### 阶段 4：Baseline 缓存、FacetRegistry 和 FilterCompiler

#### 目标

替换当前全局名称匹配，建立唯一合法的 Requirement → filterCode 编译路径。

#### 工作项

1. 实现 GuideBaselineCache；
2. 只用完全无筛选请求更新 Baseline；
3. 加入租赁指纹和 14/15 分钟 TTL；
4. 筛选响应与 Baseline 隔离；
5. 建立版本化 FacetRegistry；
6. 按 Facet 构建 MenuIndex；
7. 实现 Operator 能力校验；
8. 实现 RequirementCapabilityResolver；
9. 实现父子实体 reducer；
10. 生成 FilterPlan 和 PlanHash；
11. 对 old resolver 和 new compiler 进行 shadow compare；
12. 移除从前端/LLM/公开命令传入 code 的路径；
13. 最终删除 `SearchCarCommand.FilterCodes/SortCode/GroupCode/ContextID`。

#### Shadow 模式

灰度初期：

- 旧 resolver 继续驱动真实请求；
- 新 FilterCompiler 只计算不执行；
- 记录两者的 Applied/Unresolved/code 差异；
- 不在日志中记录敏感信息；
- 差异达到门槛后切换。

#### 验收

- 每个 code 都能追溯到 Requirement、FacetRegistry 和 Baseline 菜单项；
- 不再进行跨 Facet 的全局名称匹配；
- “特斯拉 Model Y”不会因父品牌 code 扩大结果；
- hard 无法映射不会降级软排序；
- filtered response 菜单不会覆盖 Baseline；
- Baseline 过期或租赁条件变化会重新获取；
- 外部调用入口无法注入任意 code/context。

#### 回滚

- Feature Flag 切回旧 resolver；
- Baseline 新缓存是旁路状态，不覆盖旧请求必要字段；
- 保留编译差异日志用于修复。

### 阶段 5：SearchPolicy 和 Pending 收敛

#### 目标

实现确定性的自动搜索规则，减少不必要 Pending。

#### 工作项

1. 实现 SearchGoal.PreferenceAskCount；
2. 首次无诉求询问不再创建 Blocking Pending；
3. 实现 explicit/no_preference/search-after-change 规则；
4. 条件变化后根据 previous search 自动重搜；
5. Pending 增加明确 BlockedStages；
6. 一轮只激活一个 Blocking Pending；
7. Pending 候选在激活前基于最新 Session 重算；
8. soft/unverifiable 使用 MessageHint；
9. hard capability limit 和用户授权近似替代分开处理。

#### 验收

- 地点 Pending 不导致同轮七座/SUV 诉求丢失；
- “都行/直接搜”不依赖 Active Pending 也能搜索；
- 唯一车型别名不产生 Pending；
- soft 车型歧义不阻断其他硬条件；
- hard 有限多候选时才出现车型 Pending；
- 条件修改且之前搜过会自动重搜；
- 第一次无诉求只询问限定次数。

#### 回滚

- SearchPolicy 可切回仅显式搜索；
- Pending 类型保留向后读取兼容。

### 阶段 6：SearchCarHandler 纯化和可信结果

#### 目标

让 SearchCarHandler 只执行 FilterPlan，并清楚报告实际能力。

#### 工作项

1. 新 SearchCarInput 只接受 FilterPlan 和分页控制；
2. Context 由 Baseline/SearchSnapshotManager 注入；
3. 实现 QuoteFilter/Verification；
4. RankFactor 仅使用真实字段；
5. 优先服务端 Sort；
6. 本地排序记录 RankingScope；
7. 结果按 filtered/verified/ranked/advisory/unverifiable 分类；
8. ResponseComposer 使用结构化事实统一回复；
9. 删除 Handler 内的 LLM 提取和菜单全局匹配。

#### 验收

- Handler 不依赖 RequirementExtractor；
- Handler 不接受任意 provider code；
- soft 排序都能指出真实 DataField；
- 无字段的舒适性/行李能力不会进入 RankFactor；
- 返回文案不把 unverifiable 描述为满足；
- 只排当前页时明确 fetched-set 范围。

#### 回滚

- 保留兼容适配器，将旧命令转换为临时 FilterPlan；
- Feature Flag 回到阶段 4 的执行路径。

### 阶段 7：分页和结果导航

#### 目标

实现“换一批/还有别的吗/上一批/刷新”，不重新提取诉求。

#### 工作项

1. 在 `request_vehicle_search` 内实现 `SearchOperation`；
2. 实现 ActiveSearchSnapshot；
3. 保存 FilterPlanHash、RequirementVersion、租赁指纹；
4. 保存 Baseline/Continuation Context；
5. 保存 Batches 和 Seen IDs；
6. 实现 next/previous/refresh/restart；
7. 条件变化自动 supersede 旧分页；
8. 限制补齐一批时的最大向后读取页数；
9. Context 过期时重新 Baseline，并明确结果已刷新；
10. 没有历史搜索时不错误翻页。

#### 验收

- 纯“换一批”不调用 RequirementExtractor；
- 下一批使用相同 FilterPlan；
- 上一批直接使用缓存结果；
- 修改条件加“换一批”时从新条件第一页开始；
- 已展示结果按产品展示单位去重；
- 无更多结果时不自动放宽硬条件；
- Context 生命周期符合 Guide 正式契约。

#### 回滚

- 关闭 next/previous/refresh 操作解析，回退为普通 `search_now`；
- 前端保留明确分页按钮时可临时回退普通 Page 请求，但不允许前端提交 FilterCode。

### 阶段 8：观测、灰度和清理

#### 目标

完成灰度验证、删除旧路径和固化运维能力。

#### 工作项

1. Dashboard 和告警；
2. 新旧编译结果差异分析；
3. 按流量逐步切换新 Pipeline；
4. 清理旧 Action 和兼容 DTO；
5. 清理旧全局菜单匹配；
6. 清理旧菜单缓存字段；
7. 更新主技术设计、README 和运行手册；
8. 建立车型别名运营更新流程。

#### 验收

- 新 Pipeline 全量后无旧 code 注入入口；
- unmappable/unverifiable/ambiguous 比率稳定；
- Pending 触发率显著低于旧设计；
- 搜索成功率、空结果率、重复结果率无异常；
- 用户诉求未应用时都有可追溯原因；
- 旧兼容代码删除前至少完成一个稳定观察周期。

#### 回滚

- 灰度期间保留阶段级 Feature Flag；
- 清理旧代码前保留完整版本发布回滚能力。

## 26. 测试方案

### 26.1 Router

- 单一租车条件；
- 单一车辆诉求；
- 明确搜索；
- 结果翻页；
- 多意图；
- 纯知识问题；
- Pending 回答加新诉求；
- “看点别的”在有/无搜索历史下的差异。

### 26.2 Requirement Contract

- 全部 Facet；
- 缺字段；
- 未知字段；
- 非法枚举；
- operation/operator 组合；
- hard/soft；
- custom 边界；
- 不提取地点时间；
- 不输出 ID/code/context。

### 26.3 Requirement Merge

- 同 Facet 默认替换；
- “也看看”并集；
- remove 整个 Facet；
- remove 单个值；
- not_eq 持久化；
- 范围交集；
- 正负冲突；
- superseded 历史。

### 26.4 Vehicle Entity

- 标准名；
- 唯一别名；
- 品牌上下文消歧；
- 多候选 hard；
- 多候选 soft；
- not_found；
- 模型/车系/品牌父子关系；
- 不允许自动父级降级。

### 26.5 FilterCompiler

- Facet 内匹配；
- 跨 Facet 同名不误匹配；
- Operator 支持/不支持；
- 模型 > 车系 > 品牌；
- 独立 brand + vehicle_type 不剔除；
- hard unresolved；
- soft rankable；
- soft unverifiable；
- PlanHash 稳定；
- Baseline 过期；
- Filtered menu 隔离。

### 26.6 SearchPolicy

- 首次有诉求；
- 首次无诉求第 1/2 次；
- no_preference；
- 已搜过后修改时间地点；
- 已搜过后修改车辆条件；
- Blocking Pending；
- 非阻断提示；
- 条件变化和分页同轮。

### 26.7 Pagination

- next batch；
- previous batch；
- refresh；
- expired context；
- no active search；
- duplicate results；
- empty page；
- requirement version changed；
- rental fingerprint changed；
- Guide Continuation Context 契约。

### 26.8 真实集成测试

遵守仓库规则：

- Domain/API 集成测试使用 `conf/dev.yaml` 真实 client；
- 不使用 fake client、mock 外部响应或 `httptest` 模拟 Guide/Maps；
- 关键 Guide 契约通过真实环境验证；
- LLM Prompt 使用真实 LLM 合同测试；
- 确定性内部编译器可以使用本地结构化菜单夹具测试纯函数，但不得把它冒充外部 API 集成测试。

## 27. 可观测性

每轮至少记录：

```text
trace_id
request_id
session_id
session_version
router_actions
stage_outcomes
stage_skip_reasons
requirement_facets
entity_resolution_status
entity_catalog_version
facet_registry_version
baseline_cache_hit
baseline_age_ms
filter_plan_hash
filter_count
verification_count
ranking_scope
unverifiable_count
pending_type
search_decision
search_page
deduplicated_count
guide_duration_ms
```

禁止记录：

- Bearer token；
- 不必要的完整个人信息；
- 内部思维过程；
- 未脱敏的敏感位置详情。

建议指标：

```text
router_domain_mismatch_rate
requirement_schema_error_rate
entity_alias_hit_rate
entity_ambiguous_rate
entity_not_found_rate
filter_mapping_success_rate
hard_unverifiable_rate
soft_unverifiable_rate
pending_trigger_rate
vehicle_pending_trigger_rate
baseline_cache_hit_rate
search_success_rate
search_empty_rate
pagination_duplicate_rate
new_old_plan_diff_rate
```

## 28. 上线门槛

新 Pipeline 全量前必须满足：

1. Guide AND/OR、Context 和分页契约已确认；
2. Router 新 Action 真实 LLM 测试通过；
3. Requirement 新 Schema 真实 LLM 测试通过；
4. 车辆目录覆盖核心品牌和高频车型；
5. FilterCompiler shadow diff 达到约定门槛；
6. hard 条件没有静默降级；
7. 任何 filterCode 都可追溯；
8. Baseline 不被筛选响应覆盖；
9. Pending 只出现在阻断型场景；
10. 分页不重新提取诉求；
11. 完整 `go test ./...` 和 `go vet ./...` 通过；
12. 真实 Guide、Maps、LLM 集成测试通过；
13. Feature Flag 和阶段回滚路径已演练。

## 29. 需要 Guide/产品确认的问题

### Guide

1. 同组多个 FilterCode 的 AND/OR；
2. 跨组多个 FilterCode 的 AND/OR；
3. GroupCode 的实际用途；
4. SortCode 支持项和全局排序范围；
5. ContextID 在 Baseline、筛选和分页中的生命周期；
6. 15 分钟 TTL 的准确起点和续期规则；
7. 筛选响应 MenuGroup 是否为裁剪菜单；
8. 是否提供 total/has_more/next cursor；
9. FuelType、TransmissionType 的正式枚举；
10. 是否提供车龄、车型层级、行李能力和舒适性字段。

### 产品

1. “Model Y”在业务 Schema 中定义为 vehicle_series 还是 vehicle_model；
2. “七座”默认是 `eq 7` 还是 `gte 7`；
3. hard 无法验证时默认阻断，还是允许返回带明显提示的部分结果；
4. 同 Facet 多值是否需要一次请求 OR；
5. “换一批”按车型去重还是按报价去重；
6. 首次无诉求最多询问 1 次还是 2 次；
7. 本地 fetched-set 排序是否对用户展示说明；
8. 车辆目录和别名由哪个系统/团队维护。

## 30. 推荐实施顺序总结

```text
阶段0  Guide契约确认
  ↓
阶段1  Router Action拆分 + 串行Pipeline骨架
  ↓
阶段2  Requirement Schema和Session状态
  ↓
阶段3  车辆实体归一
  ↓
阶段4  Baseline + FacetRegistry + FilterCompiler
  ↓
阶段5  SearchPolicy + Pending收敛
  ↓
阶段6  SearchCarHandler纯化 + 可信回复
  ↓
阶段7  SearchSnapshot + 分页导航
  ↓
阶段8  灰度、观测和旧路径清理
```

不能跳过阶段 0、2、3 直接实现 FilterCompiler。否则仍然会出现：

- LLM Key 和菜单字段偶然匹配；
- 车型名称不统一；
- 品牌、车系、车型 code 同时下发；
- 不可验证诉求被错误降级；
- 分页和条件更新使用不同搜索语义。

最终落地后的主流程是：

```text
PendingResolver
  → Router
  → RentalContextHandler
  → VehicleRequirementHandler
  → VehicleEntityNormalizer
  → SearchPolicy
  → BaselineMenuProvider
  → FilterCompiler
  → SearchCarHandler
  → ResponseComposer
  → SessionCommit
```

每个阶段按条件执行或跳过，所有实际 Guide 参数都由服务端确定性组件生成。
