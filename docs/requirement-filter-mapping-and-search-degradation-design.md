# 用户车辆诉求到 Guide FilterCode 映射与询问机制改进方案

## 1. 文档目标

本文定义车辆诉求从用户原文进入 Session、归一化为标准语义、映射为 Guide `filtercode`、执行搜索、返回结果和询问用户的完整流程。

本版根据最新决策做以下收敛：

1. Requirement 只保留 `Operation=add/replace/remove`，删除通用比较 Operator；
2. “不要手动挡”等排除诉求使用独立 `negative` 字段表达；
3. 删除 CapabilityResolver；
4. 删除 QuoteFilter、LocalRank 和所有 Guide 之外的过滤、排序；
5. Guide 返回字段只用于展示和结果契约校验，不用于本地过滤或重排；
6. 中间处理只判断诉求能否映射为真实 Guide 参数；
7. 无法映射的诉求无论 hard/soft 都不阻断默认搜索，但必须逐条告知；
8. 品牌、车系、车型必须先经过 VehicleEntityCatalog 确认和归一；
9. 出行人数是上下文，不直接推导座位数；
10. “带老人和小孩、长途、高速安静”等诉求即使无法映射，也必须被回复层理解、回应并产生非阻断建议；
11. Blocking Pending 和普通非阻断询问使用不同数据结构和生命周期；
12. 保留用户明确表达的 OR，避免“奥迪或Model Y”被错误编译成 AND。

本文是落地方案，不在本任务中修改业务代码。

如果本文与以下旧文档中的诉求映射、Capability、QuoteFilter、RankFactor 或 Pending/普通询问规则冲突：

- `docs/search-pipeline-refactoring-plan.md`
- `docs/rental-guide-agent-technical-design.md`

以本文为准。旧文档中的其他总体架构内容不受影响。

---

## 2. 核心原则

### 2.1 只使用 Guide 的搜索能力

搜索请求只允许包含 Guide 认可的参数：

```text
FilterCodes
SortCode
GroupCode
ContextID
Page
PageSize
```

其中：

- `FilterCodes`、`SortCode`、`GroupCode` 只能由服务端根据 Guide 契约产生；
- `ContextID` 只能来自 Guide 响应；
- 前端和 LLM 都不能生成这些字段；
- 不使用 Guide 返回后的本地字段过滤候选；
- 不使用本地分数重新排列 Guide 返回顺序。

### 2.2 每条诉求必须有结果

每条激活诉求最终必须是：

```text
mapped
```

或者：

```text
unmapped
```

不允许：

- 静默丢失；
- 因为 hard 无法映射就阻止其他条件搜索；
- 把无法验证说成车辆不满足；
- 把未使用的诉求说成已经筛选。

### 2.3 用户语义与单次执行分开

```text
hard / soft
negative / positive
```

属于用户诉求。

```text
mapped / unmapped
applied / omitted / relaxed
```

属于系统执行结果。

自动省略或放宽不能修改用户原始 hard/soft，也不能删除 Requirement。

### 2.4 不从人物和场景推导车辆条件

```text
两人出行
带老人和小孩
出差
旅游
长途出行
高速开着安静一点
```

都可以保存为上下文或体验诉求，但不能自动变成：

```text
seat_num=2
seat_num=7
vehicle_type=SUV
vehicle_type=舒适型
```

系统可以在搜索后用普通问题询问用户是否需要这些明确筛选条件。

---

## 3. 总体架构

```text
用户输入
  │
  ▼
Top-level Router
  │
  ├─ modify_rental_context
  ├─ update_vehicle_requirements
  ├─ request_vehicle_search
  └─ general_reply
  │
  ▼
RequirementExtractor
  │ 只提取本轮增量、否定标记和逻辑分支
  ▼
RequirementContractValidator
  │ 固定字段、枚举、必填项和禁止推断
  ▼
RequirementNormalizer
  ├─ 普通菜单语义归一
  └─ VehicleEntityResolver车辆实体归一
      ├─ 本地权威VehicleEntityCatalog优先
      └─ 本地未命中时可选AgentHub长尾召回
  ▼
RequirementReducer
  │ add / replace / remove合并Session
  ▼
SearchPolicy
  │ 判断是否搜索，不生成FilterCode
  ▼
GuideBaselineProvider
  │ 获取无筛选菜单和context_id
  ▼
FilterCodeMapper
  │ 每条诉求输出mapped或unmapped
  ▼
GuidePlanBuilder
  │ 只组装Guide FilterCodes/SortCode/分支
  ▼
GuideSearchExecutor
  │ 调用Guide；必要时进行Guide内的有限放宽
  ▼
GuideResponseValidator
  │ 只做结构和契约校验，不本地过滤/排序
  ▼
UnifiedReplyComposer
  ├─ 告知已生效条件
  ├─ 告知未映射/已放宽条件
  ├─ 回应老人儿童、长途、安静等诉求
  └─ 生成普通非阻断问题或Blocking Pending
```

### 3.1 删除 CapabilityResolver

目标流程中不再区分：

```text
filterable
verifiable
rankable
advisory
```

也不再产生：

```text
QuoteFilter
RankFactor
LocalRank
```

原 CapabilityResolver 的职责收敛到：

```text
FilterCodeMapper
```

它只回答：

```text
能否映射成真实Guide参数？
```

不能映射时记录原因和告知策略。

---

## 4. 诉求提取协议

### 4.1 RequirementOperation

只保留一个操作枚举：

```go
type RequirementOperation string

const (
    RequirementAdd     RequirementOperation = "add"
    RequirementReplace RequirementOperation = "replace"
    RequirementRemove  RequirementOperation = "remove"
)
```

含义：

- `add`：在现有值之外增加一个可选诉求；
- `replace`：替换相同 Facet 的当前诉求；
- `remove`：删除相应诉求。

Operation 只描述如何修改 Session，不描述如何比较值。

### 4.2 删除通用比较 Operator

不再保存：

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

原因：

- 目标执行能力只来自 Guide 菜单；
- 最终是否可执行取决于是否能无损匹配真实菜单项；
- 通用数值比较容易造成错误转换；
- 返回数据不再用于本地过滤；
- `add/replace/remove` 与“等于/大于”是不同概念，混在 Requirement 中增加复杂度。

类似“至少7座”保留为完整语义值：

```text
raw_value = 至少7座
normalized_value = 7座及以上
```

FilterCodeMapper 只有在 Guide 菜单能够无损表达“7座及以上”时才映射；否则为 unmapped。

### 4.3 显式负向字段

```go
type VehicleRequirementDelta struct {
    Facet           RequirementFacet
    RawText         string
    RawValue        string
    Operation       RequirementOperation
    Negative        bool
    Importance      RequirementImportance
    Confidence      float64
    EntityContext   EntityContext
    BranchID        string
}
```

提取结果：

```go
type VehicleRequirementExtractResult struct {
    Common       []VehicleRequirementDelta
    Alternatives []RequirementBranch
    DomainMatched bool
}
```

例如：

```text
不要手动挡
```

提取为：

```json
{
  "facet": "transmission",
  "raw_text": "不要手动挡",
  "raw_value": "手动挡",
  "operation": "replace",
  "negative": true,
  "importance": "hard",
  "confidence": 0.99,
  "entity_context": {
    "brand_hint": "",
    "series_hint": ""
  }
}
```

下面两句话语义不同：

```text
不要手动挡
→ 添加/替换一条negative=true的激活诉求

去掉“不要手动挡”这个限制
→ operation=remove，删除对应负向诉求
```

不能把 `negative=true` 当成 `operation=remove`。

### 4.4 Requirement Facet

首批车辆筛选 Facet：

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

为了保存不应直接转成 FilterCode 的出行信息，增加：

```text
passenger_context
trip_scenario
experience_preference
```

含义：

| Facet | 示例 | 是否默认进入FilterCode映射 |
|---|---|---|
| `passenger_context` | 两人出行、带老人和小孩 | 否 |
| `trip_scenario` | 长途、出差、旅游 | 否 |
| `experience_preference` | 高速安静、坐着舒服、行李空间大 | 仅在真实Guide菜单可无损表达时 |

### 4.5 Importance

继续保留：

```go
type RequirementImportance string

const (
    ImportanceHard RequirementImportance = "hard"
    ImportanceSoft RequirementImportance = "soft"
)
```

它只用于：

- 用户告知强度；
- Guide 条件放宽顺序；
- 是否允许系统提出普通建议；
- exact-only 模式下是否阻断自动放宽。

它不用于判断系统有没有本地过滤或排序能力，因为这些能力已删除。

---

## 5. 出行人数与 SeatNum 的边界

### 5.1 禁止直接推断

```text
两人出行
```

必须提取为：

```text
passenger_context = 两人
```

不能提取为：

```text
seat_num = 2
```

原因：

- 出行人数不等于车辆标称座位数；
- 两人可能需要5座、SUV、跑车或其他车辆；
- 自动映射会错误缩小搜索范围。

同理：

```text
7个人出行
```

也不应直接变成：

```text
seat_num = 7
```

系统可以询问：

> 了解到这次有7人同行。是否需要我明确按7座或更大车型筛选？

这是普通非阻断问题，不是 Pending。

### 5.2 可以提取 SeatNum 的表达

只有用户明确描述车辆座位要求时才提取：

```text
要7座车
只看7座
至少要7座车型
不要5座车
```

分别保留用户完整语义：

```text
7座
7座
7座及以上
5座 + negative=true
```

### 5.3 混合表达

```text
两个人出行，想租一辆2座跑车
```

可以同时提取：

```text
passenger_context = 两人
seat_num = 2座
vehicle_type = 跑车
```

因为“2座跑车”是明确车辆要求，不是从人数推断。

---

## 6. 逻辑关系与 OR 分支

### 6.1 为什么必须保存 OR

```text
奥迪或Model Y
```

是：

```text
brand=奥迪
OR
vehicle_model=特斯拉Model Y
```

不能保存成没有关系的两条 Requirement，否则会被错误当成：

```text
奥迪 AND 特斯拉Model Y
```

### 6.2 简化表达模型

首期使用 DNF：

```go
type RequirementExpression struct {
    Common       []VehicleRequirementDelta
    Alternatives []RequirementBranch
}

type RequirementBranch struct {
    BranchID     string
    Requirements []VehicleRequirementDelta
}
```

语义：

```text
Common中的条件：所有分支都AND
一个Branch内部：AND
不同Branch之间：OR
```

### 6.3 示例

```text
奥迪或Model Y，都要7座
```

提取为：

```text
Common:
  seat_num=7座

Branch A:
  brand=奥迪

Branch B:
  vehicle_model=Model Y
```

归一化后：

```text
Branch A:
  filter/brand/奥迪
  filter/seat_num/7

Branch B:
  filter/vehicle_name/特斯拉Model Y
  filter/seat_num/7
```

由于 brand 和 vehicle_name 属于不同 Guide filter group，跨 Facet OR 需要两个 Guide 请求并集。

### 6.4 冲突只在同一个 AND 分支判断

```text
Branch A：奥迪
OR
Branch B：特斯拉Model Y
```

不冲突。

```text
同一个Branch：
  brand=奥迪
  vehicle_model=特斯拉Model Y
```

才是确定性实体冲突。

---

## 7. 提取 Prompt 约束

Prompt 必须明确：

1. 只输出本轮相对 Session 的增量；
2. 只允许固定 Facet；
3. 只允许 `add/replace/remove`；
4. 禁止输出比较 Operator；
5. 所有 Requirement 必须输出 `negative`；
6. 所有 Requirement 必须输出 `confidence` 和 `entity_context`，非车辆实体的品牌/车系提示为空字符串；
7. “不要、排除、不考虑”设置 `negative=true`；
8. “去掉某条件”使用 `operation=remove`；
9. 不能从出行人数推断座位数；
10. 不能从老人儿童、长途、旅游推断SUV、7座或舒适型；
11. 必须保留“或、都可以、二选一”的 OR 分支；
12. 品牌、车系、车型只输出原始名称和实体提示，不输出标准名称；
13. 不输出 ID、filtercode、sort code、group code、context ID；
14. “直接搜、换一批、还有别的吗”不产生车辆 Requirement；
15. 地点、时间、供应商不属于车辆 Requirement。

### 7.1 示例：不要手动挡

```json
{
  "common": [
    {
      "facet": "transmission",
      "raw_text": "不要手动挡",
      "raw_value": "手动挡",
      "operation": "replace",
      "negative": true,
      "importance": "hard",
      "confidence": 0.99,
      "entity_context": {
        "brand_hint": "",
        "series_hint": ""
      }
    }
  ],
  "alternatives": [],
  "domain_matched": true
}
```

### 7.2 示例：两人长途

```text
两个人长途自驾，高速安静一点
```

提取为：

```json
{
  "common": [
    {
      "facet": "passenger_context",
      "raw_text": "两个人",
      "raw_value": "两人",
      "operation": "replace",
      "negative": false,
      "importance": "soft",
      "confidence": 0.99,
      "entity_context": {
        "brand_hint": "",
        "series_hint": ""
      }
    },
    {
      "facet": "trip_scenario",
      "raw_text": "长途自驾",
      "raw_value": "长途",
      "operation": "replace",
      "negative": false,
      "importance": "soft",
      "confidence": 0.99,
      "entity_context": {
        "brand_hint": "",
        "series_hint": ""
      }
    },
    {
      "facet": "experience_preference",
      "raw_text": "高速安静一点",
      "raw_value": "高速静谧",
      "operation": "replace",
      "negative": false,
      "importance": "soft",
      "confidence": 0.99,
      "entity_context": {
        "brand_hint": "",
        "series_hint": ""
      }
    }
  ],
  "alternatives": [],
  "domain_matched": true
}
```

不能输出：

```text
seat_num=2
vehicle_type=舒适型
```

### 7.3 示例：奥迪或Model Y

```json
{
  "common": [],
  "alternatives": [
    {
      "branch_id": "vehicle-option-a",
      "requirements": [
        {
          "facet": "brand",
          "raw_text": "奥迪",
          "raw_value": "奥迪",
          "operation": "replace",
          "negative": false,
          "importance": "hard",
          "confidence": 0.99,
          "entity_context": {
            "brand_hint": "",
            "series_hint": ""
          }
        }
      ]
    },
    {
      "branch_id": "vehicle-option-b",
      "requirements": [
        {
          "facet": "vehicle_model",
          "raw_text": "Model Y",
          "raw_value": "Model Y",
          "operation": "replace",
          "negative": false,
          "importance": "hard",
          "confidence": 0.99,
          "entity_context": {
            "brand_hint": "",
            "series_hint": ""
          }
        }
      ]
    }
  ],
  "domain_matched": true
}
```

---

## 8. RequirementReducer

### 8.1 持久状态

```go
type RequirementState struct {
    ID string

    Facet           RequirementFacet
    RawText         string
    RawValue        string
    NormalizedValue string

    Negative   bool
    Importance RequirementImportance

    SemanticStatus RequirementSemanticStatus

    EntityID       string
    EntityType     string
    EntityBrandID  string
    EntityParentID string
    CatalogVersion string

    BranchID   string
    CreatedTurn int64
    UpdatedTurn int64
}
```

持久 `SemanticStatus` 只包含：

```text
active
removed
superseded
```

不保存：

```text
mapped
unmapped
applied
omitted
```

这些是单次编译和搜索状态。

### 8.2 Requirement ID

ID 由服务端生成：

```text
Facet
+ NormalizedValue
+ Negative
+ BranchID
```

LLM 不生成 ID。

必须包含 `Negative`，否则：

```text
要手动挡
不要手动挡
```

会得到相同身份。

### 8.3 Replace

```text
上一轮：丰田、7座
本轮：看看小米
```

Reducer：

```text
brand=丰田 → superseded
brand=小米 → active
seat_num=7座 → 保留
```

### 8.4 Add

```text
特斯拉也可以
```

在原品牌分支之外增加可选品牌，必须保留 OR 关系。

### 8.5 Remove

```text
品牌不限
```

删除当前品牌诉求。

```text
手动挡也可以了
```

删除或替换原 `negative=true, transmission=手动挡`。

### 8.6 Branch-aware 合并

Reducer 的 replace/remove 必须限定逻辑范围：

- Common Requirement 只替换 Common 中相同 Facet；
- Branch Requirement 只替换同一个 BranchID 中相同 Facet；
- 用户用一个新的完整 OR 表达式替换旧选择时，整体 supersede 旧 Alternatives；
- 不能因为 Branch A 替换品牌就删除 Branch B 的车型；
- `add` 必须明确加入现有 Branch，或新增一个 OR Branch，不能依靠数组顺序猜测。

---

## 9. VehicleEntityCatalog 与 AgentHub 长尾召回

### 9.1 职责

VehicleEntityCatalog 只负责车辆品牌、车系、车型：

- 别名归一；
- 标准名称；
- 标准实体 ID；
- 品牌—车系—车型父子关系；
- 品牌/车系提示消歧；
- 系列完整展开；
- 车型冲突判断；
- 版本管理。

它不负责：

- 搜车；
- 判断 hard/soft；
- 生成普通菜单 FilterCode；
- 判断当前库存；
- 推断SUV、座位、舒适性；
- 生成回复。

生产流程必须始终本地目录优先。AgentHub 不是第二套权威车型库，也不能覆盖本地目录已经得到的唯一结果；它只处理错别字、口语别名和长尾名称导致的本地未命中。

### 9.2 接口

```go
type VehicleEntityResolver interface {
    Resolve(context.Context, *VehicleResolveInput) VehicleResolveResult
}

type VehicleEntityCatalog interface {
    ResolveLocal(*VehicleResolveInput) VehicleResolveResult
    ExpandSeries(seriesID string) SeriesExpansionResult
    Version() string
}

type VehicleResolveStatus string

const (
    VehicleResolveExact     VehicleResolveStatus = "exact"
    VehicleResolveAlias     VehicleResolveStatus = "alias"
    VehicleResolveAmbiguous VehicleResolveStatus = "ambiguous"
    VehicleResolveNotFound  VehicleResolveStatus = "not_found"
)

type VehicleResolveSource string

const (
    VehicleResolveSourceLocal    VehicleResolveSource = "local_catalog"
    VehicleResolveSourceAgentHub VehicleResolveSource = "agenthub_recall"
)

type VehicleResolveResult struct {
    Status         VehicleResolveStatus
    Source         VehicleResolveSource
    Entity         *VehicleEntity
    Candidates     []VehicleEntity
    Reason         string
    CatalogVersion string
}
```

`Status` 描述匹配结果，`Source` 描述候选来源，两者不能混为一个枚举。例如 AgentHub 找到候选并经目录确认后，可以是 `status=exact, source=agenthub_recall`。

### 9.3 “Model Y”

输入：

```text
facet=vehicle_model
raw_value=Model Y
```

Catalog：

```text
canonical_name=特斯拉Model Y
entity_id=model:tesla:model-y
brand_id=brand:tesla
```

FilterCodeMapper 再生成：

```text
filter/vehicle_name/特斯拉Model Y
```

不能在 Catalog 确认前直接拼：

```text
filter/vehicle_name/Model Y
```

### 9.4 父级剔除

同一 AND 分支内：

```text
brand=特斯拉
vehicle_model=特斯拉Model Y
```

只保留：

```text
filter/vehicle_name/特斯拉Model Y
```

不能同时保留：

```text
filter/brand/特斯拉
```

避免搜索到其他特斯拉车型。

### 9.5 车系展开

Guide 支持 `vehicle_name`，不假设存在通用 `vehicle_series` code。

车系映射流程：

```text
vehicle_series
→ Catalog确认标准车系
→ 完整展开该车系的标准车型
→ 生成多个filter/vehicle_name/*
→ 同组OR
```

如果不能保证展开完整：

```text
unmapped_reason=series_expansion_incomplete
```

不能只取前5个车型后声称已经搜索整个车系。

### 9.6 数据来源

生产 Catalog 应来自：

- Guide/Tyche 权威车辆索引接口；
- 或定期同步的版本化车辆目录。

当前少量手写静态目录只能用于开发测试，不能继续扩展成生产白名单。

### 9.7 AgentHub 的定位

参考 Tyche AI 导购的车辆处理链路：

```text
LLM提取brand / vehicle_series / vehicle_model
→ 本地VehicleIndex按model > series > brand解析
→ 本地未命中
→ AgentHub车型向量库召回有限候选
→ 小模型只在候选内选择
→ 候选一致性校验
→ 生成vehicle_name或brand code
```

参考实现位置：

- `tyche/logic/agent/search/vehicle_resolver.go`：本地 `VehicleIndex` 解析和 `model > series > brand`；
- `tyche/logic/agent/vehicle_recall.go`：AgentHub 长尾召回、TopK、4秒预算、候选内精选和防幻觉校验；
- `tyche/logic/agent/capabilities.go`：只有本地未命中才进入召回；
- `tyche/library/agenthub/client.go`：`/v1/workflows/run` blocking 检索和车型库独立 key。

本方案采用同样的“本地优先、长尾兜底、候选约束、失败不阻断”原则，但增加一项安全限制：

> AgentHub 返回的是召回证据，不是可直接执行的车辆实体。候选必须重新通过权威 VehicleEntityCatalog 验证，才能交给 FilterCodeMapper。

AgentHub 主要解决：

- `modely`、`model why` 等非标准拼写；
- 品牌和车型混写、空格或中英文变体；
- 本地别名表未覆盖的常见口语；
- 车型目录中存在实体，但本地检索词没有命中的长尾表达。

AgentHub 不负责：

- 判断用户要不要搜车；
- 从“适合老人孩子”推断 SUV 或七座；
- 把类目词当成车辆实体；
- 生成或返回可直接信任的 FilterCode；
- 绕过目录创建一个不存在的品牌、车系或车型；
- 处理 Guide 库存和排序。

### 9.8 触发条件

只有同时满足以下条件才触发 AgentHub：

1. Facet 是正向的 `brand`、`vehicle_series` 或 `vehicle_model`；
2. 用户本轮或当前激活诉求中确实包含车辆名称；
3. 本地 Catalog 返回 `not_found`；
4. AgentHub 车型召回已配置且当前未被熔断；
5. 当前请求上下文仍有足够的剩余时间预算。

以下情况不触发：

- 本地已 exact/alias 命中；
- 本地返回 ambiguous，此时已有确定候选，应走普通询问或 exact-only Pending；
- 只有 SUV、七座、自动挡等非车辆实体诉求；
- 只有负向且 Guide 无法执行的车辆排除诉求；
- AgentHub 在同一个 CatalogVersion、同一个归一化 query 上已有短期空结果缓存。

### 9.9 完整处理流程

```text
RequirementExtractor
  │ 输出raw_value、facet、brand_hint、series_hint
  ▼
LocalCatalog.ResolveLocal
  ├─ exact/alias
  │    └─ 直接返回标准实体
  ├─ ambiguous
  │    └─ 保留候选；不调用AgentHub
  └─ not_found
       ▼
AgentHub.RetrieveVehicle
       │ 只返回有限候选资料
       ▼
CandidateParser
       │ 解析为candidate_id/type/brand/series/model
       │ 去重并限制TopK
       ▼
CandidateSelector
       ├─ 唯一高置信候选：确定性选择
       └─ 多候选：受约束LLM只返回candidate_id或no_match
       ▼
AuthoritativeCatalog.Revalidate
       ├─ 唯一实体：status=exact, source=agenthub_recall
       └─ 失败/歧义：status=not_found/ambiguous
       ▼
RequirementState保存raw_text、raw_value、解析结果和来源
       ▼
FilterCodeMapper
       ├─ 已确认实体：映射Guide brand/vehicle_name
       └─ 未确认：unmapped并明确告知用户
```

AgentHub 召回不能覆盖原始诉求。即使整个召回链路失败，Session 中仍保存用户的 `raw_text` 和 `raw_value`，后续目录版本升级或用户补充品牌/车系时可以重新解析。

### 9.10 候选协议和防幻觉

外部服务放在独立 API 包中：

```go
type Client interface {
    RetrieveVehicle(context.Context, *RetrieveVehicleRequest) (*RetrieveVehicleResponse, error)
}

type RetrieveVehicleRequest struct {
    Query string `json:"query"`
}

type RetrieveVehicleResponse struct {
    Content string `json:"content"`
}
```

如果 AgentHub 当前只能返回知识文本，`api/agenthub` 只负责协议调用和响应字段校验，候选文本解析放在 `internal/vehiclecatalog`。不得让 LLM 从整库自由生成车辆名。

对齐 Tyche 当前 workflow 时，Client 将 `Query` 组装为：

```json
{
  "inputs": {"input": "用户车辆名称查询"},
  "response_mode": "blocking",
  "user": "固定的服务调用方标识"
}
```

`entity_type`、`brand_hint`、`series_hint` 和 TopK 是内部解析与选择参数；除非 AgentHub workflow 明确扩展协议，否则不能擅自发送成 Provider 字段。

候选选择输出只允许：

```json
{
  "candidate_id": "candidate-2",
  "matched": true
}
```

约束：

- `candidate_id` 必须来自本次候选列表；
- `matched=false` 时 candidate_id 必须为空；
- 不允许模型自由输出品牌、车系、车型或 FilterCode；
- 候选数量建议不超过8个；
- 多候选选择是一个独立结构化 LLM Task，可以复用统一 LLM Harness；
- 即使 Harness 重试成功，结果仍必须经过 candidate_id 白名单和 Catalog 复验。

### 9.11 超时、缓存和降级

AgentHub 位于搜车热路径，只允许有界调用：

- 召回与候选选择共用独立子 Context；
- 总预算上限不超过4秒，并受父 Context 更短截止时间约束；
- 不做无限重试；
- 父 Context 取消时立即停止；
- 超时、调用错误、空结果、解析失败、选择失败和复验失败都不阻断其余条件搜索。

缓存与 Guide Baseline 缓存必须分开：

```text
key = normalized_query + entity_type + brand_hint + series_hint + catalog_version
```

- 正向命中可以按 CatalogVersion 缓存；
- 空结果只做更短的负缓存，避免长时间压住新车型；
- CatalogVersion 变化后旧解析缓存自然失效；
- AgentHub 缓存不使用 Guide context_id，也不受 Guide 菜单15分钟 TTL 代替；
- Guide Baseline 仍必须是无筛选请求产生的菜单。

降级结果不能静默：

| 内部结果 | 搜索行为 | 用户行为 |
|---|---|---|
| 本地命中 | 正常映射并搜索 | 告知已按标准车辆名筛选 |
| AgentHub召回且复验通过 | 正常映射并搜索 | 可自然使用标准名称，不暴露内部召回 |
| 本地歧义 | 去掉该条件，其他条件继续搜 | 普通询问候选；exact-only时才可Blocking Pending |
| AgentHub超时/失败 | 去掉该条件，其他条件继续搜 | 明确该车辆名称暂未准确识别 |
| 召回候选未通过目录复验 | 去掉该条件，其他条件继续搜 | 明确未按该名称筛选，禁止声称已满足 |
| 所有条件都未映射 | 按第14.4节执行 | 返回参考结果或遵守exact-only |

---

## 10. Guide Baseline

### 10.1 无筛选请求

Baseline 请求必须满足：

```text
ContextID=""
FilterCodes=nil
SortCode=""
GroupCode=""
```

返回并整体缓存：

```go
type GuideBaselineCache struct {
    RentalFingerprint string
    ContextID         string
    Menu              []GuideMenuGroup
    BaseQuotes        []GuideQuote

    FirstReceivedAt  time.Time
    ServiceExpiresAt time.Time
    SafeExpiresAt    time.Time
    Complete         bool
}
```

### 10.2 生命周期

- Guide 有效期15分钟；
- Agent 安全复用期建议14分钟；
- 时间、地点变化立即失效；
- 带用户筛选条件返回的菜单不能覆盖 Baseline；
- `context_id`、菜单、租赁指纹必须来自同一次响应；
- 第一页报价不代表完整库存。

### 10.3 DTO 补全

Agent Guide DTO 必须保留：

```go
type GroupItem struct {
    Name           string
    GroupCode      string
    IsSupportMulti bool
    IsNested       bool
    Items          []Item
    GroupItems     []GroupItem
    Desc           string
}
```

否则不能可靠判断：

- code 属于哪个 group；
- 当前组是否支持多选；
- 菜单是否嵌套；
- code 是否来自当前 Baseline。

### 10.4 刷新和重编译

以下任一不满足时刷新 Baseline：

```text
RentalFingerprint一致
Baseline完整
当前时间小于SafeExpiresAt
ContextID非空
Plan使用同一个ContextID
```

刷新后必须重新运行 FilterCodeMapper 和 GuidePlanBuilder，不能只更换 ContextID 后沿用旧 code。

---

## 11. FilterCodeMapper

### 11.1 唯一职责

```text
Requirement
+ VehicleEntityCatalog
+ Guide Baseline Menu
+ 确认过的Guide原生code契约
→ mapped或unmapped
```

FilterCodeMapper：

- 不调用 Guide 搜索；
- 不读取 Guide 返回车辆字段做过滤；
- 不生成本地排序；
- 不决定是否创建 Pending；
- 不修改 Session Requirement。

### 11.2 输出

```go
type MappingStatus string

const (
    MappingMapped   MappingStatus = "mapped"
    MappingUnmapped MappingStatus = "unmapped"
)

type ProviderParameterKind string

const (
    ProviderFilter ProviderParameterKind = "filter_code"
    ProviderSort   ProviderParameterKind = "sort_code"
)

type RequirementMappingResult struct {
    RequirementID string
    Status        MappingStatus

    Parameters []GuideParameter

    ReasonCode string
    Reason     string

    Evidence     []MappingEvidence
    UserNotice   string
    Suggestions  []QuestionSuggestion
}
```

对外核心状态只有：

```text
mapped
unmapped
```

内部保留 ReasonCode 供回复、监控和评测使用。

### 11.3 UnmappedReason

```go
type UnmappedReason string

const (
    UnmappedNoGuideFilter          UnmappedReason = "no_guide_filter"
    UnmappedMenuItemNotFound       UnmappedReason = "menu_item_not_found"
    UnmappedEntityNotFound         UnmappedReason = "vehicle_entity_not_found"
    UnmappedEntityAmbiguous        UnmappedReason = "vehicle_entity_ambiguous"
    UnmappedEntityRecallDisabled   UnmappedReason = "vehicle_entity_recall_disabled"
    UnmappedEntityRecallTimeout    UnmappedReason = "vehicle_entity_recall_timeout"
    UnmappedEntityRecallNoMatch    UnmappedReason = "vehicle_entity_recall_no_match"
    UnmappedEntityRecallUnverified UnmappedReason = "vehicle_entity_recall_unverified"
    UnmappedNegativeNotSupported   UnmappedReason = "negative_not_supported"
    UnmappedSeriesExpansionPartial UnmappedReason = "series_expansion_incomplete"
    UnmappedValueNotExact          UnmappedReason = "value_not_exact"
    UnmappedConflict               UnmappedReason = "requirement_conflict"
    UnmappedContextOnly            UnmappedReason = "context_only"
)
```

这些不是额外 Capability，只是 `unmapped` 的具体原因。

### 11.4 映射顺序

```text
1. 读取激活Requirement
2. 判断是否为上下文型Facet
3. 归一普通语义值
4. 品牌/车系/车型进入VehicleEntityResolver
   4.1 本地VehicleEntityCatalog优先解析
   4.2 本地not_found时可选AgentHub长尾召回
   4.3 AgentHub候选重新进入权威Catalog复验
5. 检查negative是否有Guide原生表达
6. 根据FacetRegistry限定可匹配Guide group
7. 在当前Baseline Menu内精确/别名匹配
8. 对确认过的品牌/车型契约生成code
9. 校验group、多选和context
10. 输出mapped或unmapped
```

### 11.5 FacetRegistry

FacetRegistry 仍然保留，但只配置 Guide 映射：

```go
type FacetDefinition struct {
    Facet RequirementFacet

    GuideGroups []string
    ResolverType ResolverType

    SupportsNegative bool
}
```

不再配置：

```text
QuoteField
RankField
ComparisonOperators
```

### 11.6 首批映射表

| Facet | Guide映射 |
|---|---|
| `seat_num` | 当前菜单 `filter/seat_num/*` 的无损语义匹配 |
| `vehicle_type` | `filter/vehcle_choice/*` |
| `price_preference` | 当前 `filter/price/*`、`filter/total_fee/*` 菜单项；明确“便宜优先”可使用Guide SortCode |
| `car_age` | 当前 `filter/car_age/*` |
| `energy_type` | 当前 `filter/fuel/*` |
| `transmission` | 当前 `filter/transmission/*` |
| `brand` | Catalog标准品牌 + `filter/brand/{canonical brand}` |
| `vehicle_model` | Catalog标准车型 + `filter/vehicle_name/{canonical vehicle}` |
| `vehicle_series` | Catalog完整展开 + 多个 `filter/vehicle_name/*` |
| `comfort_preference` | 仅在Guide菜单有无损选项时映射 |
| `experience_preference` | 仅在Guide菜单有无损选项时映射 |
| `passenger_context` | unmapped/context_only |
| `trip_scenario` | unmapped/context_only |
| `custom` | 只允许匹配当前真实Guide菜单候选 |

语义必须无损。例如：

```text
用户明确说“舒适型”
→ 可以匹配Guide真实“舒适型”菜单

用户说“坐着舒服”
→ 不能自动等价为“舒适型”
→ 没有其他真实Guide菜单时为unmapped
```

### 11.7 品牌与车型不是直接字符串拼接

错误：

```go
code := "filter/vehicle_name/" + requirement.RawValue
```

正确：

```text
RawValue
→ VehicleEntityCatalog
→ CanonicalName
→ 确认实体类型
→ Guide code builder
```

### 11.8 Negative 映射

Guide 当前主要是正向筛选。负向诉求不能自动转换成正向条件。

例如：

```text
不要手动挡
```

不能默认转换为：

```text
只看自动挡
```

除非 Guide 契约明确保证 transmission 只有手动/自动两种且未知值语义也确定。首期默认：

```text
MappingStatus=unmapped
ReasonCode=negative_not_supported
```

搜索其他已映射条件，并告知：

> 当前筛选菜单不能直接排除手动挡，我先按其他条件搜索。你如果可以明确只看自动挡，我可以继续按自动挡筛选。

这是普通非阻断问题。

### 11.9 可选的受限语义菜单匹配

长尾 `custom` 可以让 LLM 在当前 Guide 菜单候选中选择，但必须封闭：

```json
{
  "requirement": "需要宝宝座椅",
  "candidates": [
    {"candidate_id": 1, "name": "儿童安全座椅"},
    {"candidate_id": 2, "name": "雪地链"}
  ]
}
```

LLM 只能返回候选 ID 和置信度。服务端再根据候选 ID 取回真实 code 并校验。

LLM 不能：

- 生成 code；
- 生成菜单不存在的选项；
- 决定 Provider group 语义；
- 把上下文需求推成新的车辆条件。

这个步骤是可选的 Mapper 插件，不是 CapabilityResolver。

---

## 12. Mapping 与 Execution 分离

一条诉求映射成功，不代表每一次搜索都必须使用它。

例如 soft：

```text
最好SUV，其他也可以
```

可以映射：

```text
filter/vehcle_choice/suv
```

第一次 Guide 搜索可以先使用该 FilterCode；如果无结果，可以移除这个 soft code 再调用 Guide。

因此增加：

```go
type RequirementExecutionStatus string

const (
    RequirementApplied RequirementExecutionStatus = "applied"
    RequirementOmitted RequirementExecutionStatus = "omitted"
    RequirementRelaxed RequirementExecutionStatus = "relaxed"
)
```

组合示例：

```text
mapped + applied
mapped + relaxed
unmapped + omitted
```

不能出现：

```text
unmapped + applied
```

---

## 13. GuidePlanBuilder

### 13.1 输出只包含 Guide 参数

```go
type GuideSearchPlan struct {
    PlanID   string
    PlanType GuidePlanType

    FilterCodes []string
    SortCode    string
    GroupCode   string

    AppliedRequirementIDs []string
    OmittedRequirementIDs []string
    RelaxedRequirementIDs []string

    ContextID string
    PlanHash  string
}
```

不存在：

```text
QuoteFilters
RankFactors
LocalRanking
```

### 13.2 同组和跨组

Guide 已确认：

```text
同一filter group多个值：OR
不同filter group之间：AND
```

例如：

```text
特斯拉或奥迪
```

可以一个请求：

```text
filter/brand/特斯拉
filter/brand/奥迪
```

```text
特斯拉 + 7座
```

一个请求：

```text
filter/brand/特斯拉
filter/seat_num/7
```

### 13.3 跨 Facet OR

```text
奥迪或Model Y
```

不能一个请求传：

```text
filter/brand/奥迪
filter/vehicle_name/特斯拉Model Y
```

因为 Guide 会按 AND 处理。

必须生成两个 Plan：

```text
Plan A: filter/brand/奥迪
Plan B: filter/vehicle_name/特斯拉Model Y
```

分别调用 Guide，按稳定分支顺序合并并去重。

合并只允许：

- 去掉同一个报价的重复项；
- 使用稳定 round-robin 防止一个分支完全占满页面；
- 保留各分支内部 Guide 返回顺序。

不能使用本地车辆字段对两个分支重新评分。

### 13.4 空分支保护

如果一个 OR 分支唯一的车辆诉求 unmapped：

```text
Branch A: 奥迪 → mapped
Branch B: 一个无法识别的车型 → unmapped
```

不能把 Branch B 编译成无 FilterCode 的全量搜索后与 Branch A 合并，这会无限扩大用户语义。

处理：

- 跳过空 Branch B；
- 执行 Branch A；
- 明确告知另一个车型未识别、没有参与搜索。

如果所有分支都空：

- 在默认非 exact-only 模式可以进行一次无筛选 Guide 搜索；
- 结果分类为参考结果；
- 明确所有诉求均未映射；
- 不说车辆符合条件。

---

## 14. 无法映射的统一处理

### 14.1 默认规则

无论 hard/soft，只要 unmapped：

```text
Requirement继续保存在Session
→ 不加入FilterCodes/SortCode
→ 搜索其他mapped条件
→ SearchResult记录omitted
→ Reply逐条告知
```

### 14.2 hard unmapped

用户：

```text
必须7座SUV，后备箱放两个28寸箱
```

映射：

```text
7座              → mapped
SUV              → mapped
两个28寸行李箱    → unmapped/no_guide_filter
```

Guide 请求：

```text
filter/seat_num/7
filter/vehcle_choice/suv
```

回复：

> 我已经按7座、SUV筛选。当前Guide菜单还不能直接筛选“后备箱放两个28寸箱”，所以这个条件没有参与本次搜索，下面车辆也不保证满足行李空间要求。

不能阻断搜索。

### 14.3 soft/context unmapped

用户：

```text
带老人和小孩，准备长途出行，高速开着安静一点
```

这些诉求可以全部 unmapped，但不能只回复：

```text
无法筛选
```

回复至少包含：

1. 明确复述场景；
2. 说明当前菜单无法直接筛选的项目；
3. 表达已经考虑到舒适、空间、上下车便利等关注点，但不声称已筛选；
4. 给出普通非阻断问题。

示例：

> 了解到这次会带老人和小孩，而且是长途出行，乘坐舒适、上下车方便和高速体验确实很重要。当前Guide菜单还不能直接筛选“高速安静”和“老人儿童友好”，我先展示当前可租车辆供你参考。你希望我进一步按SUV、7座还是舒适型这类明确条件筛选吗？

注意：

```text
“乘坐舒适、上下车方便值得关注”
```

是回应用户场景。

```text
“已经按舒适和上下车方便筛选”
```

是错误声明。

### 14.4 全部 unmapped

默认模式：

- 可以执行一次无筛选 Guide 搜索；
- 结果标题使用“当前可租车辆参考”；
- 所有 unmapped 诉求逐条说明；
- 附带一个普通问题帮助用户转换为 Guide 支持的明确条件。

exact-only 模式：

- 不展示声称相关的车辆；
- 告知当前无法使用这些诉求精确搜索；
- 询问用户是否愿意选择Guide支持的明确条件。

---

## 15. Guide 内的严格搜索与放宽

### 15.1 不使用本地降级能力

所有搜索尝试都必须再次调用 Guide。

允许：

```text
移除某个FilterCode后重新调用Guide
使用Guide返回的SortCode
扩大VehicleEntityCatalog映射后重新调用Guide
```

禁止：

```text
在返回报价上本地过滤
本地价格排序
本地品牌加分
本地座位匹配排序
扫描三页后自行判断完整库存
```

### 15.2 初始计划

初始 Guide Plan 使用：

- 所有 mapped hard 条件；
- 所有明确希望优先满足且 mapped 的 soft 条件；
- Guide 菜单提供的排序参数；
- 当前有效 Baseline ContextID。

### 15.3 无结果放宽顺序

只有 Guide 请求返回空结果时才进入库存放宽。

建议：

```text
1. 移除mapped soft FilterCode
2. 对有确定父子关系的车型做Guide内拓宽
   model → series完整展开 → brand
3. 按配置移除一个mapped hard FilterCode，生成明确备选结果
```

每一步仍是新的 Guide 请求。

默认限制：

```text
MaxRelaxAttempts=2
MaxRelaxedDimensionsPerAttempt=1
```

### 15.4 hard 放宽

hard 可以为了提供备选结果而暂时移除，但：

- Session 中仍是 hard；
- RequirementVersion 不变化；
- SearchSnapshot 记录 `mapped + relaxed`；
- 回复必须说明具体放宽了什么；
- 结果只能称为备选。

例如：

> 完全满足“Model Y、7座”的车辆当前没有库存。我保留了7座条件，暂时把具体车型放宽到特斯拉品牌，下面是当前备选；这些车辆不满足你原来的具体车型要求。

### 15.5 exact-only

用户明确说：

```text
不符合就不要推荐
必须全部满足
没有就算了
```

则：

- unmapped hard 不自动转换；
- Guide 严格结果为空时不移除 hard FilterCode；
- 可以询问是否愿意放宽，但不先执行放宽；
- 需要用户决定后才能执行 materially different 的搜索时，可以创建 Blocking Pending。

---

## 16. GuideResponseValidator

### 16.1 允许做什么

Guide 返回字段只用于：

- 检查必填结构是否完整；
- 检查 Provider 返回的品牌、车型等字段是否明显违反已下发 code；
- 验证 `context_id` 是否存在；
- 检查分页和引用 ID；
- 填充车辆卡片和回复事实；
- 记录 Provider 契约异常。

### 16.2 禁止做什么

不能：

- 删除不符合本地规则的车辆；
- 根据座位、品牌、车型再次过滤；
- 根据价格、品牌、车型重新排序；
- 用有限分页结果判断市场完整无车；
- 把校验逻辑当成 FilterCodeMapper 的 fallback。

### 16.3 校验失败

如果返回结果明显违反下发的 Guide code：

1. 记录 `provider_validation_mismatch`；
2. 检查 Baseline/Context 是否过期；
3. 必要时刷新 Baseline、重编译并重试一次；
4. 仍失败时不声称相应条件已被可靠执行；
5. 不逐条本地删除车辆；
6. 返回 Provider 异常提示或参考结果，由产品策略决定。

---

## 17. SearchResult

### 17.1 结构

```go
type RequirementExecutionResult struct {
    RequirementID string
    RawText       string
    Importance    string
    Negative      bool

    MappingStatus  MappingStatus
    ExecutionStatus RequirementExecutionStatus

    Parameters []GuideParameter
    ReasonCode string
    Reason     string
}

type SearchCarResult struct {
    Status SearchResultStatus

    Vehicles []guide.VehRate

    Requirements []RequirementExecutionResult
    Questions    []NonBlockingQuestion

    InteractionID string
    Message       string
}
```

### 17.2 ResultStatus

```text
success
partial
alternatives
no_results
needs_context
waiting_user
provider_error
```

判定：

- 所有 hard `mapped+applied`：`success`；
- 至少一个 hard `unmapped+omitted`：`partial`；
- 至少一个 hard `mapped+relaxed`：`alternatives`；
- Guide 所有合法计划都为空：`no_results`；
- Blocking Pending：`waiting_user`。

soft/context unmapped 不一定把状态改成 partial，但必须出现在回复和 Questions 中。

---

## 18. 统一回复与“情绪价值”

### 18.1 回复输入

ReplyComposer 接收：

```go
type ReplyContext struct {
    Applied  []RequirementExecutionResult
    Omitted  []RequirementExecutionResult
    Relaxed  []RequirementExecutionResult

    PassengerContext []RequirementState
    TripScenarios    []RequirementState
    ExperienceNeeds  []RequirementState

    Vehicles  []guide.VehRate
    Questions []NonBlockingQuestion
}
```

### 18.2 回复顺序

```text
1. 回应用户场景和核心关注
2. 说明哪些Guide条件已生效
3. 逐条说明哪些诉求未映射
4. 说明哪些条件因无结果被放宽
5. 展示车辆结果
6. 给出普通非阻断问题
```

### 18.3 情绪价值的边界

需要：

- 复述并理解用户场景；
- 说明为什么这些关注点重要；
- 给出可操作的下一步；
- 使用自然语言而不是只报技术状态。

不能：

- 编造车辆满足未筛选诉求；
- 根据常识自动添加FilterCode；
- 把画像事实写成已确认车辆条件；
- 用“我已经帮你考虑好了”掩盖实际没有筛选。

### 18.4 可使用 LLM 生成最终话术

统一回复可以使用 LLM 润色，但输入必须是结构化事实：

```text
applied
omitted
relaxed
passenger_context
trip_scenario
experience_preference
allowed_questions
```

Prompt 必须要求：

- 每条 hard omitted/relaxed 必须出现；
- 不得说 omitted 已满足；
- 不得增加新的车辆事实；
- 不得改变 Questions 的语义；
- 车辆信息只能来自 Guide；
- 生成失败时回退确定性模板。

---

## 19. Blocking Pending 与普通询问

### 19.1 判断标准

```text
没有用户回答就无法安全继续某个具体动作
→ Blocking Pending

没有用户回答也能安全返回当前结果
→ 普通非阻断询问
```

### 19.2 Blocking Pending

典型场景：

- 全国地图存在多个同名地点；
- 缺少必须的取还车时间；
- exact-only 下核心车型存在多个候选；
- exact-only 下必须由用户决定是否放宽；
- 同一个 AND 分支存在无法安全自动处理的强冲突。

特征：

- 保存 `PendingStore.Active`；
- 有 `BlockingActions`；
- 必要时有 `DeferredAction`；
- 返回 `waiting_user`；
- 下一轮优先由 PendingResolver 处理；
- 只阻止依赖该问题的动作，不阻止其他领域更新。

### 19.3 普通非阻断询问

典型场景：

- 两人出行，询问是否有明确座位需求；
- 带老人小孩，询问是否需要SUV、7座或舒适型；
- 不要手动挡无法映射，询问是否可以明确只看自动挡；
- 高速安静无法映射，询问是否需要舒适型等 Guide 支持项；
- 车型歧义但仍可安全展示其他已映射结果；
- 自动返回备选后询问是否保留放宽。

特征：

- 先正常执行搜索；
- 可以和车辆结果一起返回；
- 不创建 `PendingStore.Active`；
- 不创建 DeferredAction；
- 不阻止下一轮任何 Handler；
- 用户不回答没有副作用；
- 用户回答时走正常 Router 和 RequirementExtractor。

### 19.4 数据结构

```go
type NonBlockingQuestion struct {
    ID       string
    Question string
    Options  []QuestionOption

    SourceRequirementIDs []string
    CreatedAt            time.Time
    ExpireAt             time.Time
}

type QuestionOption struct {
    Label    string
    UserText string
}
```

普通问题的选项优先从当前 Guide Baseline Menu 生成，确保用户点击后能够映射。无法确认当前菜单支持的概念可以出现在开放式问题中，但不能做成承诺可筛选的固定按钮。

QuestionOption 不携带：

```text
FilterCode
ContextID
ProviderEntityID
```

点击“需要7座”时，前端提交：

```text
user_text=需要7座
```

下一轮正常提取为：

```text
seat_num=7座
negative=false
```

### 19.5 “第一个”等回答

为了支持：

```text
第一个
第二个
都可以
```

可以短暂保存最近一次 NonBlockingQuestion，但它仍不是 Pending：

- 只用于解析选项编号；
- 匹配失败直接回到正常 Router；
- 不返回 waiting_user；
- 不阻止搜索；
- 新的明确用户条件使旧 Question 失效。

### 19.6 Pending 生命周期

Blocking Pending：

```text
active
→ resolved / cancelled / superseded / expired / suspended
```

下一轮顺序：

```text
Expire
→ 取消检测
→ 选项/自然语言匹配
→ 替代信息检测
→ 提取剩余文本
→ 更新最新Session
→ 重新运行SearchPolicy和Mapper
```

不能直接重放创建 Pending 时冻结的旧 FilterCode 计划。

### 19.7 一轮多个问题

- 同时只能有一个 Active Blocking Pending；
- 其他 Blocking 候选只保存重新评估描述；
- 普通问题不进入 Pending 队列；
- 一次回复普通问题建议最多1个主问题、3个选项；
- Active Pending 解决后基于最新 Session 重新判断后续问题是否仍存在。

---

## 20. 典型场景

### 20.1 两人出行

用户：

```text
我们两个人出去玩
```

提取：

```text
passenger_context=两人
```

映射：

```text
unmapped/context_only
```

处理：

- 不生成 `filter/seat_num/2`；
- 租赁条件完整时可展示当前车辆；
- 普通询问：

> 了解到是两人出行，我不会直接把车辆限制成2座。你对SUV、轿车、跑车或座位数有明确偏好吗？

### 20.2 不要手动挡

提取：

```text
transmission=手动挡
negative=true
hard
```

首期映射：

```text
unmapped/negative_not_supported
```

处理：

- 其他 mapped 条件继续搜索；
- 明确手动挡尚未排除；
- 普通询问：

> 当前Guide筛选不能直接表达“排除手动挡”。如果你的意思是只看自动挡，我可以按自动挡继续筛选。

不能擅自添加自动挡。

### 20.3 Model Y

用户：

```text
看一下model y
```

处理：

```text
vehicle_model=Model Y
→ Catalog=特斯拉Model Y
→ filter/vehicle_name/特斯拉Model Y
→ mapped+applied
```

不携带：

```text
filter/brand/特斯拉
```

### 20.4 奥迪或Model Y

处理：

```text
Branch A:
  filter/brand/奥迪

Branch B:
  filter/vehicle_name/特斯拉Model Y
```

分别调用 Guide 并集。

不能：

- 判为奥迪和特斯拉冲突；
- 在一个请求中按跨组 AND 下发；
- 把奥迪展开成不完整车型列表。

### 20.5 老人儿童、长途、安静

用户：

```text
带老人和小孩，准备跑长途，高速安静一点
```

提取：

```text
passenger_context=老人和小孩
trip_scenario=长途
experience_preference=高速静谧
```

如果无 Guide 精确菜单：

```text
全部unmapped
```

回复：

> 这次有老人和小孩同行，又是长途驾驶，乘坐舒适和高速体验确实值得重点关注。当前Guide菜单不能直接筛选“高速安静”或“老人儿童友好”，我不会把它们假装成已满足条件。你希望我进一步按SUV、7座还是舒适型筛选吗？

Question 是普通问题，不是 Pending。

### 20.6 混合可映射和不可映射

用户：

```text
要特斯拉Model Y，带孩子长途，高速安静一点
```

映射：

```text
Model Y    → mapped+applied
带孩子     → unmapped+omitted
长途       → unmapped+omitted
高速安静   → unmapped+omitted
```

Guide 按 Model Y 搜索。

回复明确：

- 已按 Model Y；
- 没有直接筛选儿童适配和高速静谧；
- 可以询问是否还需要儿童座椅等 Guide 当前真实菜单项。

---

## 21. SearchSnapshot 与分页

### 21.1 单 Plan

```go
type ActiveSearchSnapshot struct {
    SearchID string

    RentalFingerprint  string
    RequirementVersion int64

    BaselineContextID     string
    ContinuationContextID string

    SelectedPlan GuideSearchPlan
    Attempts     []GuideSearchAttempt

    CurrentPage int
    NextPage    int
    PageSize    int

    Status    SearchSnapshotStatus
    CreatedAt time.Time
    ExpiresAt time.Time
}
```

### 21.2 OR 多分支

```go
type BranchSearchSnapshot struct {
    BranchID              string
    Plan                  GuideSearchPlan
    ContinuationContextID string
    NextPage              int
    Exhausted             bool
}
```

用户说“换一批”：

- 没有修改时间、地点和诉求时复用同一批 Plan；
- 多分支稳定轮询；
- 不重新让 LLM解释OR；
- 不改变 mapped/unmapped 结果；
- 不重新选择另一个放宽策略。

### 21.3 条件变化

以下任一发生：

```text
RentalFingerprint变化
RequirementVersion变化
Baseline过期
Catalog版本变化且影响车辆实体
```

旧 Snapshot 失效，重新从 Baseline 和 Mapping 开始。

自动放宽不修改 RequirementVersion。

---

## 22. 可观测性

### 22.1 事件

```text
requirement_extracted
requirement_normalized
vehicle_entity_resolved
vehicle_entity_recall_started
vehicle_entity_recall_finished
vehicle_entity_recall_rejected
filtercode_mapping_finished
guide_plan_built
guide_search_started
guide_search_finished
guide_plan_relaxed
provider_response_validated
non_blocking_question_created
blocking_pending_created
reply_composed
```

### 22.2 字段

```text
trace_id
session_id
search_id
requirement_id
requirement_version
facet
negative
importance
mapping_status
reason_code
execution_status
filter_group
filter_code_count
plan_hash
context_id
attempt_type
result_count
duration_ms
vehicle_resolve_source
catalog_version
recall_candidate_count
recall_outcome
```

### 22.3 指标

提取：

- Facet 准确率；
- Operation 准确率；
- Negative 准确率；
- passenger_context 被误提为 seat_num 的比例；
- OR 关系准确率；
- 历史诉求误回传率。

映射：

- mapped/unmapped 比例；
- 各 Facet mapped 比例；
- entity_not_found/ambiguous 比例；
- 本地 Catalog 命中率；
- AgentHub 触发率、召回率、复验通过率；
- AgentHub timeout/error/empty/unverified 比例；
- AgentHub 召回后产生虚假 FilterCode 的比例，目标0；
- AgentHub 对最终 mapped 率的净提升；
- AgentHub 增加的 P95/P99 延迟和单轮调用成本；
- negative_not_supported 比例；
- menu_item_not_found 比例；
- series expansion incomplete 比例。

搜索：

- mapped hard 搜索成功率；
- hard unmapped 但成功返回其他结果的比例；
- Guide 内放宽率；
- exact-only 无结果率；
- OR 多分支结果和分页稳定性；
- provider validation mismatch 比例。

回复：

- hard unmapped 告知覆盖率，目标100%；
- relaxed hard 告知覆盖率，目标100%；
- 错误声称“已满足未映射条件”的比例，目标0；
- 非阻断问题点击/回答率；
- 用户重申未生效诉求的比例。

---

## 23. 代码改造范围

### 23.1 `internal/domain/vehiclerequirement`

- 删除 `Operator` 类型和字段；
- Contract 不再要求 operator；
- 增加 `Negative bool`；
- 增加 passenger_context/trip_scenario/experience_preference；
- 增加 Common + Alternatives 提取结构；
- Prompt 增加人数禁止推断和 OR 规则；
- 更新 reducer 的 ID、replace/remove 逻辑；
- 更新 current requirement projection。

### 23.2 `internal/vehiclecatalog`

- 用权威目录替换少量手写生产目录；
- 标准车型名使用 Guide 真实名称；
- 支持品牌/车系/车型父子关系；
- 支持完整车系展开；
- 增加 context-aware `VehicleEntityResolver`；
- 增加本地优先、AgentHub未命中召回、候选解析和Catalog复验；
- 候选选择只接受本次候选ID；
- 记录 ResolveSource、CatalogVersion 和内部失败原因；
- AgentHub失败时保留原始Requirement并返回unmapped；
- 未命中或歧义不生成 code；
- 目录版本变化触发相关 Requirement 重新归一。

### 23.3 `api/agenthub`

- 按仓库 API 包规范提供 `dto.go`、`interface.go`、`client.go`；
- 只封装车型检索 workflow，不混入业务选择和 FilterCode 生成；
- 配置 endpoint、车型库凭证和 timeout；
- 校验 workflow 状态和 content；
- Provider ID、知识库 ID 和候选资料只能来自真实响应或服务配置；
- 下层错误原样返回，不包装；
- 远程集成测试使用 `conf/dev.yaml` 的真实客户端。

### 23.4 `api/guide`

- 补齐 Menu GroupItem 字段；
- 增加嵌套菜单遍历；
- 增加品牌、车型、多品牌 OR、跨组 AND 集成测试；
- 确认所有使用到的 Guide SortCode；
- 保持 Baseline 无筛选和 context_id 契约。

### 23.5 `internal/searchplan`

- 删除 Capability 枚举；
- 删除 QuoteFilter；
- 删除 RankFactor；
- 删除返回字段本地过滤和排序；
- 删除通用 Operator 处理；
- 增加 FilterCodeMapper；
- 增加 mapped/unmapped + reason；
- 支持 brand/vehicle_name；
- 支持父级剔除；
- 支持同组 OR 和跨 Facet OR 分支；
- Plan 只保存 Guide 参数。

### 23.6 `internal/domain/searchcar`

- unmapped hard 不再返回 capability_limit；
- 使用所有 mapped 条件调用 Guide；
- Guide 内有限放宽；
- 记录 applied/omitted/relaxed；
- 增加 OR 多分支执行和稳定合并；
- ResponseValidator 只校验，不过滤或排序；
- 分页固定复用 SelectedPlan/Branches。

### 23.7 `internal/session`

- RequirementState 增加 Negative、BranchID；
- RequirementState 增加 VehicleResolveSource、CatalogVersion 和 ResolutionReason；
- 移除 Operator；
- MappingResult 不覆盖持久 SemanticStatus；
- ActiveSearchSnapshot 保存 Guide Plans 和 Attempts；
- 增加短期 NonBlockingQuestion 状态；
- NonBlockingQuestion 不进入 PendingStore。

### 23.8 `internal/webchat`

- 区分 Blocking Pending 和普通 Questions；
- SearchResult 返回 mapped/unmapped 明细；
- 回复逐条说明 omitted/relaxed；
- 场景诉求进入 ReplyContext；
- 前端 QuestionOption 只提交自然语言；
- 不向前端开放 FilterCode/ContextID 的写入口。

---

## 24. 分阶段落地

### 阶段0：删除 Operator

工作：

- 新提取协议；
- `Negative` 字段；
- Session 数据迁移；
- Prompt 和 Contract 测试；
- 人数禁止推断测试。

兼容：

- 读取旧 Session 时把 `not_eq/not_in` 转成 `negative=true`；
- 其他旧比较信息必须结合 RawText 重新提取或标为 unmapped，不能丢掉“至少/以内”等语义后按等值映射；
- 新写入不再保存 Operator。

验收：

- 不要手动挡与删除条件可区分；
- 两人出行不产生 seat_num；
- 旧 Session 可平滑读取。

### 阶段1：VehicleEntityCatalog 和车辆 code

工作：

- 权威车辆目录；
- 品牌、车系、车型归一；
- 完整车系展开；
- model > series > brand；
- 接入 AgentHub 车型库长尾召回；
- AgentHub 候选ID约束选择和 Catalog 复验；
- 独立超时、缓存、开关和失败降级；
- Guide 品牌/车型集成测试。

验收：

- Model Y 生成 `filter/vehicle_name/特斯拉Model Y`；
- 不携带冗余品牌 code；
- 名称歧义和未命中不生成虚假 code；
- 本地命中不调用 AgentHub；
- AgentHub 超时/错误不阻断其他条件搜索；
- 未通过 Catalog 复验的候选不能生成 code。

### 阶段2：FilterCodeMapper

工作：

- mapped/unmapped；
- reason codes；
- Guide MenuIndex；
- FacetRegistry 只保留 Guide 映射；
- 删除 CapabilityResolver shadow 输出；
- 新旧结果 shadow 对比。

验收：

- 每条激活 Requirement 恰好一个 MappingResult；
- 无诉求静默丢失；
- hard unmapped 不阻断其他 mapped 条件。

### 阶段3：删除本地过滤排序

工作：

- 删除 QuoteFilters；
- 删除 RankFactors；
- 删除多页本地验证过滤；
- SearchPlan 只保留 Guide 参数；
- ResponseValidator 只做契约校验。

验收：

- 生产结果顺序来自 Guide；
- 不再通过返回品牌/车型/座位删除车辆；
- 不再使用 fetched_set 排序或过滤。

### 阶段4：OR 和 Guide 内放宽

工作：

- Common/Alternatives；
- 同组 OR；
- 跨 Facet OR 多 Plan；
- 稳定合并和多分支分页；
- mapped soft/hard 的 Guide 内放宽。

验收：

- 奥迪或Model Y 不冲突；
- `(奥迪 OR Model Y) AND 7座` 正确拆分；
- hard 放宽有明确告知；
- 不产生空分支全量扩大。

### 阶段5：回复和询问

工作：

- ReplyContext；
- 情绪价值 Prompt 和确定性兜底；
- hard unmapped 强制披露；
- NonBlockingQuestion；
- Pending/Question UI 区分。

验收：

- 老人儿童、长途、安静被明确回应；
- 不声称已经按这些条件筛选；
- 普通问题不创建 Active Pending；
- Blocking Pending 仍按原生命周期工作。

### 阶段6：灰度与评测

工作：

- 离线回放；
- shadow Mapping；
- 人数、negative、OR、车辆别名专项集；
- 小流量灰度；
- 指标监控和 bad case 扩展。

验收：

- hard unmapped 披露100%；
- seat_num 人数误推断显著下降；
- 虚假 FilterCode 为0；
- 非阻断问题不误阻塞搜索；
- Guide 搜索成功率和用户点击不显著下降。

---

## 25. 核心测试用例

### 25.1 Operation 与 Negative

| 输入 | Operation | Negative | 结果 |
|---|---|---:|---|
| 要自动挡 | replace | false | 尝试映射自动挡 |
| 不要手动挡 | replace | true | 不等价于自动挡 |
| 手动挡也不要 | add | true | 增加负向诉求 |
| 去掉手动挡限制 | remove | 根据目标匹配 | 删除对应诉求 |

### 25.2 人数

| 输入 | 期望 |
|---|---|
| 两人出行 | passenger_context=两人，不产生seat_num |
| 7个人出行 | passenger_context=7人，普通询问是否需要7座 |
| 两人，要2座跑车 | passenger_context=两人、seat_num=2座、vehicle_type=跑车 |
| 带老人小孩 | passenger_context，不推断SUV/7座 |

### 25.3 车辆实体

| 输入 | 期望 |
|---|---|
| 特斯拉 | `filter/brand/特斯拉` |
| modely | Catalog归一后 `filter/vehicle_name/特斯拉Model Y` |
| 特斯拉Model Y | 只保留vehicle_name code |
| 宝马3系 | 完整展开或unmapped，不静默截断 |
| 未知车型 | unmapped/entity_not_found |
| 本地未收录别名但AgentHub召回Model Y | Catalog复验后映射vehicle_name |
| AgentHub返回候选外ID | 拒绝并unmapped，不生成code |
| AgentHub候选在Catalog不存在 | unmapped/entity_recall_unverified |
| AgentHub超时 | 其他条件继续搜并明确未按该车型筛选 |

### 25.4 OR

| 输入 | 期望 |
|---|---|
| 特斯拉或奥迪 | 同组两个brand code |
| 奥迪A6L或宝马325Li | 同组两个vehicle_name code |
| 奥迪或Model Y | 两个Guide Plan并集 |
| 奥迪或Model Y，都要7座 | 两个Plan分别带seat_num |
| 奥迪且Model Y | 同分支实体冲突 |

### 25.5 Unmapped

| 输入 | 期望 |
|---|---|
| 7座SUV，放两个28寸箱 | 搜7座SUV，行李诉求逐条告知 |
| 不要手动挡 | 不本地过滤，询问是否只看自动挡 |
| 高速安静 | 不映射时回应场景并普通询问 |
| 所有条件均unmapped | 默认展示参考结果；exact-only不自动展示匹配结果 |

### 25.6 Pending 与普通问题

| 场景 | 期望 |
|---|---|
| 南京路有多个城市 | Blocking Pending |
| 两人出行，问车型偏好 | NonBlockingQuestion |
| 老人小孩，问SUV/7座 | NonBlockingQuestion |
| exact-only车型歧义 | Blocking Pending |
| strict无结果且允许备选 | 直接Guide放宽+普通问题 |
| strict无结果且exact-only | Pending询问是否放宽 |

### 25.7 禁止本地处理

测试必须断言：

- FilterPlan 无 QuoteFilters；
- FilterPlan 无 RankFactors；
- Guide 返回顺序未被本地重排；
- 返回车辆不被品牌、车型、座位本地删除；
- 有限分页不产生完整库存结论；
- ResponseValidator 失败不触发本地 fallback 筛选。

---

## 26. 验收标准

实现完成后必须满足：

1. Requirement 不再包含通用比较 Operator；
2. `Operation` 只负责 add/replace/remove；
3. 显式排除使用 `negative`；
4. “不要X”与“删除X条件”可稳定区分；
5. 两人/多人出行不直接产生 seat_num；
6. 老人儿童、长途、安静等诉求不自动推断车辆条件；
7. 每条激活诉求都有 mapped/unmapped 结果；
8. hard unmapped 默认不阻断其他条件搜索；
9. hard unmapped 和 hard relaxed 告知覆盖率100%；
10. 品牌、车系、车型必须经过 VehicleEntityCatalog；
11. Model Y 使用标准 `filter/vehicle_name/特斯拉Model Y`；
12. 具体车型存在时不携带扩大范围的父品牌 code；
13. 同组 OR、跨组 AND 正确；
14. 跨 Facet OR 使用多 Guide Plan，不错误判冲突；
15. 只有 Guide FilterCode/SortCode 参与搜索；
16. 不存在本地 QuoteFilter、RankFactor 或结果重排；
17. Guide 返回字段只用于展示和契约校验；
18. 普通非阻断问题不创建 Pending、DeferredAction 或 waiting_user；
19. Blocking Pending 只阻止依赖动作；
20. 回复明确回应上下文诉求，但不虚假声称已筛选；
21. Baseline 菜单来自无筛选请求并绑定 context_id 和TTL；
22. 下一批复用同一 SelectedPlan/Branches；
23. 自动放宽不修改持久 Requirement；
24. 所有关键映射和询问决策可观测、可回放；
25. 本地 Catalog 命中时不调用 AgentHub；
26. AgentHub 只在本地 not_found 后有界触发；
27. AgentHub 候选必须经过候选ID白名单和权威 Catalog 复验；
28. AgentHub 失败、超时或未验证候选不会丢失原始诉求，也不会阻断其余条件搜索；
29. AgentHub 结果不能直接携带或生成受信任的 Guide FilterCode。

---

## 27. 最终边界

最终流程保持简单且可解释：

```text
LLM提取用户说了什么
→ Operation更新Session
→ Negative记录明确排除
→ VehicleEntityResolver先查本地Catalog
→ 本地未命中时可选AgentHub长尾召回
→ AgentHub候选回到权威Catalog复验
→ FilterCodeMapper判断mapped/unmapped
→ GuidePlanBuilder只生成Guide参数
→ Guide执行搜索
→ 返回字段只做展示和校验
→ Reply明确说明已生效、未映射和已放宽
→ 能继续搜索的问题使用普通询问
→ 不能安全继续的问题才使用Blocking Pending
```

最重要的产品原则是：

> 不能映射，不等于不理解用户；可以继续搜索，不等于已经满足用户。系统既要把能力边界说清楚，也要让用户明确感受到其出行场景和真实关注点被认真考虑。
