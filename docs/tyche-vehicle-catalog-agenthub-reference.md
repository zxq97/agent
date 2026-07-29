# Tyche AI 导购车型库与 AgentHub 参考说明

> 文档状态：参考实现调研
>
> 调研日期：2026-07-29
>
> 参考代码：`/Users/didi/work/tyche`
>
> 适用项目：当前租车 Agent

## 1. 结论

Tyche 在车辆名称处理上采用的是“两级解析”：

```text
LLM提取车辆语义
→ 本地车型库确定性解析
→ 本地未命中时，AgentHub长尾召回
→ 小模型从召回候选中选择
→ 合并车型FilterCode
→ 搜车
```

两者的职责不同：

| 能力 | 主要职责 | 是否权威 | 是否应直接生成FilterCode |
|---|---|---:|---:|
| 本地车型库 | 标准车型数据、名称归一、品牌/车系/车型匹配、父子层级、静态规格 | 是，依赖本地车型数据 | Tyche当前会生成；本项目建议交给独立Mapper |
| AgentHub | 在本地未命中时召回错别字、口语名、长尾名称对应的候选 | 否，只是召回证据 | 否 |

这套设计的核心价值不是“用另一个大模型识别车型”，而是：

1. 高频请求走本地确定性路径，稳定、低延迟、低成本；
2. AgentHub只补长尾覆盖率，不改变本地权威数据；
3. 品牌、车系、车型使用专门链路，不混入通用菜单选码；
4. 同一套车型能力还能复用于OCR归一、价格保护和车辆对比。

当前项目可以参考这种分层，但不应直接照搬 Tyche 的“召回名称后直接拼 FilterCode”。更安全的目标链路应增加“权威目录复验”和“Guide菜单能力复验”。

---

## 2. Tyche 中的整体位置

Tyche V4 AI 导购的相关主链如下：

```text
用户输入
  │
  ▼
NeedExtractor
  ├─ brand
  └─ vehicle_model
      （共享Need结构和解析器也支持vehicle_series）
  │
  ▼
FilterCodeStage
  ├─ 普通诉求结合菜单生成FilterCode
  └─ 主动排除filter/brand，车辆实体交给专门链路
  │
  ▼
SearchPipeline
  │
  ▼
searchVehicleCapability
  ├─ ResolveVehicleFromNeeds
  │    ├─ vehicle_model
  │    ├─ vehicle_series
  │    └─ brand
  │
  ├─ 本地命中：MergeVehicleFilters
  │
  └─ 本地未命中且VehicleRecall=true
       └─ recallVehicleLongTail
            ├─ AgentHub.RetrieveVehicle
            ├─ 解析Top 8候选
            ├─ Lite模型选择一个候选
            ├─ 候选白名单校验
            └─ 生成车辆FilterCode
  │
  ▼
QuoteSearcher.Search
```

V4 的 `SearchStage` 显式设置 `VehicleRecall: true`。因此 AgentHub 车型召回是可选兜底能力，不是所有搜车请求的固定依赖。

---

## 3. 车型库负责什么

### 3.1 数据来源和刷新

Tyche 的车型库不是一份只写在 Prompt 中的白名单，而是基于车辆静态数据缓存构建。

基础车型数据包含：

- 标准车型名；
- 品牌名；
- 公共车型ID；
- 能源类型；
- 变速箱；
- 年款；
- 座位数；
- 车辆分类；
- 图片、车型包等其他静态信息。

`VehicleIndex` 是在基础车型缓存之上构建的轻量名称索引。它会在以下时机重建：

- 服务启动、车型缓存初始化后；
- 车型数据增量刷新后；
- 车型缓存全量重建后。

因此它不是永久静态字典，解析口径可以跟随车型数据更新。

### 3.2 四类索引

Tyche 的 `VehicleIndex` 包含四类数据：

| 索引 | Key | Value | 用途 |
|---|---|---|---|
| `NormalizedNameMap` | 归一化后的完整车型名 | 车型条目列表 | 精确匹配“特斯拉Model Y” |
| `BrandVehiclesMap` | 标准品牌名 | 品牌下的标准车型名列表 | 确认品牌存在、生成品牌条件 |
| `SeriesKeywordMap` | 去掉品牌前缀后的归一化名称 | 标准车型名列表 | 匹配“Model Y”“锋兰达”等 |
| `BrandNames` | 标准品牌集合 | — | 品牌精确确认 |

索引构建时按标准车型名去重，避免同一车型在多个数据来源中重复进入搜索条件。

### 3.3 表面文本归一

Tyche 使用统一的 `Normalize` 处理：

- 英文字母小写化；
- 删除空白；
- 删除标点和符号；
- 转换少量租车场景高频繁体字。

例如：

```text
Model Y
MODEL-Y
model_y
```

会得到接近的归一化结果。

这一步只解决字符串写法差异，不解决：

- 业务别名；
- 错别字；
- 模糊语义；
- 同名实体歧义；
- 车型是否受 Guide 支持。

品牌别名由单独的品牌别名表完成，例如把品牌的英文名、旧称或业务别称归一到标准品牌名。

### 3.4 车辆实体解析

`ResolveVehicleFromNeeds` 只处理以下三类诉求：

```text
vehicle_model
vehicle_series
brand
```

其他诉求，例如 `SUV`、`7座`、`自动挡`，不进入车型库。

解析顺序固定为：

```text
vehicle_model > vehicle_series > brand
```

这个顺序不依赖 LLM 返回数组的先后顺序。

#### vehicle_model

依次尝试：

1. 使用完整名称精确查找；
2. 如果 LLM 把品牌和车型拆开，尝试 `brand + model`；
3. 使用去品牌后的车系关键词精确查找。

例如：

```text
brand=丰田
vehicle_model=锋兰达
```

可以通过拼接恢复为标准名称“丰田锋兰达”。

#### vehicle_series

先清除“系列”“车系”“车型”等后缀，再对 `SeriesKeywordMap` 做前缀匹配，并展开为具体标准车型名。

Tyche 当前最多保留5个展开结果。这是性能保护，不等于完整表达整个车系。

#### brand

先通过品牌别名表归一，再在标准品牌集合中精确确认。

### 3.5 具体车型优先，避免父级放宽

如果用户同时说：

```text
brand=特斯拉
vehicle_model=Model Y
```

本地解析一旦命中具体车型便立即返回，只生成车型条件，不再追加品牌条件。

目标效果是：

```text
filter/vehicle_name/特斯拉Model Y
```

而不是同时携带：

```text
filter/brand/特斯拉
```

这体现了“车型 > 车系 > 品牌”的特异性规则，避免父级条件意外扩大用户的具体车型诉求。

### 3.6 负向诉求

Tyche 会跳过 `negative=true` 的车辆实体诉求，防止把：

```text
不要蔚来
```

错误转换为正向的：

```text
filter/brand/蔚来
```

但“跳过”不代表负向车辆条件已经被执行。是否支持排除品牌或车型，仍应由 Guide 当前菜单能力决定。

### 3.7 车型库不负责什么

车型库不负责：

- 从完整用户句子中提取全部车辆诉求；
- 判断是否应该立即搜车；
- 判断取还车条件是否齐全；
- 处理SUV、座位、价格、舒适性等普通筛选维度；
- 判断当前库存；
- 生成最终用户回复；
- 把“带老人小孩”推断成SUV或7座；
- 解决通用菜单项与FilterCode的匹配。

它处理的是“这个车辆名称究竟对应哪个标准实体”，不是“整句用户需求应该如何执行”。

另外，Tyche V4 抽取 Prompt 当前会把“几个人/带老人小孩”归入 `seat_num`。这属于上游诉求抽取策略，不是车型库能力，也与当前项目已经确定的规则不同。当前项目仍应把出行人数和同行画像保留为上下文，只有用户明确要求“7座车”等车辆座位条件时才生成 `seat_num`。

---

## 4. 车型库在哪里使用

### 4.1 AI 导购搜车

这是最核心的使用点：

1. 上游产生 `brand/vehicle_series/vehicle_model` 语义诉求；V4 抽取 Prompt 明确描述了 `brand` 和 `vehicle_model`，共享 Need 结构、解析器及价格保护等链路同时支持 `vehicle_series`；
2. 普通菜单选码结束后，车辆实体走专门解析；
3. 本地车型库命中后，把车辆条件合并到搜索请求；
4. 本地未命中时才考虑 AgentHub。

Tyche 的通用 `FilterCodeStage` 主动排除了 `filter/brand`，说明车辆名称不是普通菜单语义选码问题。

### 4.2 价格保护

竞品页面OCR识别出品牌、车系或车型后，价格保护流程复用 `ResolveVehicleFromNeeds`：

- 把OCR名称归一为平台标准车型；
- 保存匹配到的标准车型列表；
- 生成车型过滤条件；
- 再结合座位、挡位、牌照、能源等条件搜索。

收益是OCR名称与AI导购使用同一套标准，不需要再维护第二套车型别名规则。

### 4.3 OCR品牌/车系校验

OCR结果与平台车型比较时，先用车型库做品牌和车系归一，再检查是否命中标准车型；只有本地解析未覆盖时，才回退到归一化字符串包含判断。

### 4.4 车辆对比

车辆对比场景通过车型静态数据补充：

- 能源；
- 座位数；
- 变速箱；
- 年款；
- 车辆分类。

价格仍来自实时报价卡片，而不是车型库。这一边界很重要：

```text
车型库提供相对稳定的静态规格
报价服务提供当前时间地点下的价格与库存
```

---

## 5. AgentHub 负责什么

### 5.1 AgentHub 客户端能力

Tyche 的 AgentHub 客户端提供三个入口：

| 方法 | 用途 | 返回 |
|---|---|---|
| `StreamChat` | AgentHub流式对话 | 文本流 |
| `Retrieve` | 租车规则知识检索 | `data.outputs.content`文本 |
| `RetrieveVehicle` | 车型向量库检索 | `data.outputs.content`文本 |

规则库和车型库使用同一个 workflow endpoint，但通过不同凭证路由到不同知识库。

车型处理使用的是 `RetrieveVehicle`，它只返回检索文本，不返回可信的车辆实体ID或Guide FilterCode。

### 5.2 触发条件

车型 AgentHub 召回只在同时满足以下条件时触发：

1. 当前存在活跃、非负向的品牌/车系/车型诉求；
2. 本地 `VehicleIndex` 没有命中；
3. 当前链路启用了 `VehicleRecall`；
4. AgentHub客户端和LLM客户端均已注入；
5. 车型召回配置可用。

以下诉求不会触发车型召回：

```text
7座SUV
自动挡
便宜点
带老人和小孩
```

### 5.3 长尾召回流程

Tyche 的完整长尾流程是：

```text
车辆类活跃诉求
→ 拼接检索query
→ AgentHub车型向量库检索
→ 返回知识文本
→ 按“知识项N”切块
→ 抽取品牌和车型名
→ 品牌+车型去重
→ 最多保留8个候选
→ Lite模型选择一个候选
→ 对模型输出做候选白名单复验
→ 生成车辆FilterCode
```

AgentHub 主要补充：

- 本地别名表未收录的叫法；
- 错别字或口语表达；
- 中英文混写；
- 品牌和车型混写；
- 长尾车型名称。

### 5.4 小模型只做候选选择

AgentHub 返回的是文本候选，Tyche 再使用一个轻量模型选择最符合用户描述的候选。

模型输出结构为：

```json
{
  "brand": "候选中的品牌",
  "series": "候选中的车型名"
}
```

Prompt 要求模型只能原样复制候选。代码还会再次检查：

```text
模型输出的brand + series
必须与AgentHub候选中的某一项完全一致
```

如果模型产生候选外名称，结果会被丢弃。

这个设计把 LLM 的权力限制为“候选选择”，而不是“自由生成车型”。

### 5.5 超时与失败降级

车型召回处于搜车热路径，因此 Tyche 为“AgentHub检索 + Lite模型选择”设置了4秒总预算。

以下任一情况发生时，召回直接返回空结果：

- AgentHub未配置；
- AgentHub请求失败；
- AgentHub返回空文本；
- 候选解析失败；
- LLM调用失败；
- LLM输出不是合法JSON；
- 模型返回候选外实体；
- 整体调用超时。

这些失败不会阻断搜车，系统会继续使用其他已确认条件搜索。

Tyche 当前偏向“可用性优先”，但没有在这个步骤内保存完整的未映射原因，也未保证最终回复明确告知用户“车型条件没有参与搜索”。当前项目需要补上这项用户可感知能力。

---

## 6. AgentHub 在规则问答中的另一种使用方式

AgentHub 不只用于车型。

在 V4 规则问答中：

```text
interpret_rules
→ AgentHub.Retrieve检索规则资料
→ Tyche自己的LLM基于资料生成回复
→ 流式返回
```

这里明确拆开了：

- AgentHub负责找资料；
- 业务侧LLM负责按产品口径组织回复。

如果检索失败、资料为空或生成失败，系统使用固定兜底文案，不允许模型凭自身知识补充租车规则。

这个模式带来的参考价值是：知识检索平台不应同时垄断业务判断、结构化输出和最终话术。业务系统仍需掌握验证规则与回复口径。

---

## 7. 两者如何配合

### 7.1 正常命中

用户说：

```text
想看特斯拉Model Y
```

理想流程：

```text
LLM:
  brand=特斯拉
  vehicle_model=Model Y

本地车型库:
  标准实体=特斯拉Model Y

特异性处理:
  删除冗余brand父级

执行:
  仅按特斯拉Model Y搜索
```

AgentHub不参与。

### 7.2 品牌与车型被拆开

用户说：

```text
想要丰田的锋兰达
```

LLM可能提取为：

```text
brand=丰田
vehicle_model=锋兰达
```

本地解析器会尝试 `丰田 + 锋兰达`，恢复到标准完整名称。

### 7.3 长尾或错别字

用户输入未被本地索引识别时：

```text
本地not_found
→ AgentHub召回有限候选
→ Lite模型只从候选中选一个
→ 候选复验通过才继续
```

如果仍然无法确认，该车型条件应被标记为未映射，其他条件继续搜。

### 7.4 非车辆名称诉求

用户说：

```text
7座SUV，高速开着安静一点
```

处理应是：

- `7座` 和 `SUV` 进入普通 Guide 菜单映射；
- “高速安静”作为不可直接映射的体验诉求保存，用于告知和回复话术；
- 不调用车型库；
- 不调用 AgentHub；
- 不根据“安静”自行猜测具体品牌或车型。

---

## 8. Tyche 带来的收益

### 8.1 准确性

- 具体车型优先于车系和品牌；
- 品牌与车型拆分后仍能组合确认；
- 标准名称统一，避免同一车型多种写法产生不同条件；
- 模型不能自由生成候选外车型。

### 8.2 覆盖率

- 本地车型库覆盖常见标准名称和别名；
- AgentHub补充长尾、错别字和口语表达；
- 本地目录升级后无需频繁修改抽取Prompt。

### 8.3 延迟和成本

- 常见车型不调用 AgentHub 和额外 LLM；
- 无车辆实体诉求时零额外调用；
- 候选数和总耗时都有上限；
- AgentHub异常不会拖垮主搜索。

### 8.4 一致性

同一车型解析能力可用于：

- AI导购；
- OCR车型归一；
- 价格保护；
- 车辆对比；
- 后续车型详情和推荐说明。

这样可以避免每个领域分别维护“Model Y怎么写、丰田别名是什么、车系如何展开”。

### 8.5 可演进性

职责分层后可以独立迭代：

- 更新车型数据，不必修改LLM Prompt；
- 扩充别名，不必修改搜索策略；
- 调整AgentHub召回，不影响本地命中；
- 更换候选选择模型，不改变权威目录；
- Guide FilterCode变化时，只调整映射层。

---

## 9. Tyche 当前实现的边界和风险

### 9.1 AgentHub候选直接拼FilterCode

Tyche 在候选白名单验证通过后，会直接拼：

```text
filter/vehicle_name/{品牌+车型}
```

或：

```text
filter/brand/{品牌}
```

这里的“白名单”只证明模型没有超出 AgentHub 的召回候选，不证明：

- 候选存在于当前权威车型目录；
- 名称与 Guide 的标准名称完全一致；
- 当前 Guide Baseline 菜单支持该 FilterCode；
- FilterCode在当前 `context_id` 下仍有效。

这是当前项目不应照搬的地方。

### 9.2 车系展开最多5个

Tyche 对车系结果最多保留5个车型。这样可以控制请求大小，但如果一个车系实际有更多车型，就不能宣称已经完整执行整个车系诉求。

本项目应：

- 完整展开后再执行；或者
- 明确标记 `series_expansion_incomplete`，把该诉求作为未完整映射处理。

### 9.3 缺少完整解析状态

Tyche 的主要返回状态是：

```text
exact / series / brand / recall
```

当前项目还需要区分：

```text
exact
alias
ambiguous
not_found
agenthub_timeout
agenthub_empty
agenthub_candidate_unverified
guide_code_missing
series_expansion_incomplete
```

只有这样才能决定是否询问、是否继续搜索、如何告知用户，并支持后续评估。

### 9.4 AgentHub文本协议较脆弱

Tyche 通过“知识项N”“品牌：”“车型名：”等文本格式解析候选。如果知识库输出模板变化，解析可能静默失效。

更稳定的方式是让 AgentHub workflow 返回结构化候选：

```json
{
  "candidates": [
    {
      "candidate_id": "opaque-id",
      "brand": "特斯拉",
      "vehicle_name": "特斯拉Model Y",
      "score": 0.92
    }
  ]
}
```

即使暂时只能返回文本，也应在 API 边界做严格字段校验，并记录解析失败原因。

### 9.5 可用性降级缺少用户告知

“失败后不阻断搜索”是正确方向，但不能静默丢掉用户的强诉求。

例如用户要求一个无法识别的具体车型，系统仍可以按其他条件搜，但回复必须说明：

> 你提到的车型名称我暂时没能准确对应到当前可筛选车型，这批结果没有按该车型限定；其他条件已经保留。

---

## 10. 当前项目已有基础

当前项目已经有：

### 10.1 `internal/vehiclecatalog`

当前 `StaticCatalog` 支持：

- `brand/series/model`实体类型；
- 标准名称；
- 别名；
- 父级ID；
- 品牌ID；
- `exact/alias/ambiguous/not_found`解析状态；
- Catalog版本。

但默认目录目前只是少量硬编码种子，不能作为生产车型全量数据。

### 10.2 车辆诉求Handler

`vehiclerequirement.Handler.normalize` 已经在车辆诉求写入 Session 前调用 Catalog，并保存：

- 原始文本和原始值；
- 标准值；
- EntityID；
- EntityType；
- BrandID；
- ParentID；
- ResolutionStatus；
- ResolutionReason；
- CatalogVersion。

这是接入组合式车型解析器的合适位置。

### 10.3 Session

`SearchRequirementStateItem` 已经能同时保存：

- 用户原始诉求；
- 语义Facet；
- 标准实体；
- 解析结果；
- 目录版本。

因此即使车型暂时没识别，也不需要删除原始诉求。后续目录升级、AgentHub恢复或用户补充品牌后，可以重新解析。

### 10.4 SearchPlan Compiler

当前 Compiler 已经使用：

- `EntityBrandID`；
- `EntityParentID`；
- `EntityID`；

识别品牌、车系和车型之间的冲突与冗余。这可以承接“车型 > 车系 > 品牌”的特异性处理。

不过当前运行时代码仍保留本地报价过滤和排序能力，与已确定的“只使用Guide FilterCode执行”目标不一致。引入 Tyche 参考方案时应按目标设计改造，不能把现有 Compiler 行为当最终结论。

---

## 11. 建议在当前项目中的目标流程

建议采用比 Tyche 更严格的链路：

```text
1. RequirementExtractor
   只输出语义诉求和原始证据
   brand / vehicle_series / vehicle_model

2. Local VehicleEntityCatalog
   名称归一、别名、父子关系、精确/歧义/未命中

3. 仅local not_found时调用AgentHub
   召回有限候选，不返回可信FilterCode

4. Candidate Selector
   LLM只返回candidate_id
   代码校验candidate_id必须来自候选集合

5. Authoritative Catalog Revalidation
   用候选重新查询权威车型目录
   得到标准EntityID、名称和父子关系

6. Specificity Reducer
   vehicle_model > vehicle_series > brand
   删除同一AND分支中的冗余父级

7. Guide FilterCode Mapper
   使用当前未筛选Baseline菜单和context_id
   验证标准实体是否存在可执行FilterCode

8. Search
   只发送Guide确认支持的FilterCode

9. Reply
   明确告知哪些诉求已应用、哪些未能应用
```

### 11.1 推荐接口边界

```go
type VehicleEntityResolver interface {
    Resolve(context.Context, *VehicleResolveInput) (*VehicleResolveResult, error)
}

type VehicleEntityCatalog interface {
    ResolveLocal(*VehicleResolveInput) VehicleResolveResult
    GetByID(string) (VehicleEntity, bool)
    ExpandSeries(string) SeriesExpansionResult
    Version() string
}

type VehicleRecallClient interface {
    RetrieveVehicle(context.Context, *VehicleRecallRequest) (*VehicleRecallResponse, error)
}
```

当前 `vehiclecatalog.Resolver.Resolve` 没有 `context.Context`，不适合在实现内部直接调用 AgentHub 或控制超时。建议：

- 本地 Catalog 保持无I/O、确定性接口；
- 在外层增加带 `context.Context` 的组合式 `VehicleEntityResolver`；
- 外层负责本地优先、AgentHub兜底、超时和复验。

### 11.2 AgentHub输出约束

候选选择模型只允许输出：

```json
{
  "candidate_id": "candidate-3"
}
```

不允许输出：

- FilterCode；
- Provider实体ID；
- 自由生成的品牌名；
- 自由生成的车型名。

选择完成后仍必须通过 Catalog 复验。

### 11.3 未映射时的处理

如果车辆名称最终无法映射：

1. 原始诉求继续保存在 Session；
2. 不生成虚假 FilterCode；
3. 其他可执行条件继续搜索；
4. 搜索结果标记为部分满足；
5. 最终回复明确说明该车辆名称没有参与筛选；
6. 可以提出非阻断问题，但不默认创建 Pending。

只有当用户明确要求“必须就是这款、无法确认就不要搜”时，才需要考虑阻断式澄清。

---

## 12. 推荐接入位置

| 位置 | 建议改造 | 目的 |
|---|---|---|
| `internal/vehiclecatalog` | 接入真实车型数据、别名、父子关系、版本和系列展开 | 建立权威本地目录 |
| `internal/domain/vehiclerequirement` | 注入组合式 `VehicleEntityResolver` | 诉求入Session前完成名称确认 |
| `api/agenthub` | 新增纯传输客户端，只负责车型检索workflow | 隔离外部服务协议 |
| `internal/vehiclecatalog/recall` | 候选解析、缓存、TopK、超时、复验 | 保持业务逻辑不进入API层 |
| `internal/llmharness` | 增加 `vehicle_entity.select_candidate`任务 | 统一候选选择的解码和白名单校验 |
| `internal/searchplan` | 按特异性消除父级，并映射Guide支持的车辆码 | 防止品牌条件放宽具体车型 |
| `internal/session` | 补充解析来源和未映射原因 | 支持告知、回放和重新解析 |

建议新增字段：

```text
EntityResolutionSource:
  local_catalog
  agenthub_recall

ResolutionReason:
  entity_not_found
  entity_ambiguous
  agenthub_timeout
  agenthub_empty
  agenthub_candidate_unverified
  guide_code_missing
  series_expansion_incomplete
```

---

## 13. 可观测性和评估指标

不能只看“AgentHub命中了多少”，还要证明最终结果更准确。

建议记录：

### 本地车型库

- 本地精确命中率；
- 别名命中率；
- 歧义率；
- 未命中率；
- Catalog版本；
- 车型/车系/品牌命中分布；
- 父级剔除次数；
- 系列不完整展开次数。

### AgentHub

- AgentHub触发率；
- 召回为空比例；
- 候选解析成功率；
- 候选选择成功率；
- 候选白名单拒绝率；
- Catalog复验通过率；
- Guide FilterCode最终映射率；
- 超时率和错误率；
- P50/P95/P99额外延迟；
- 单轮Token和调用成本。

### 最终业务结果

- 车辆名称诉求最终映射率；
- 虚假FilterCode比例，目标为0；
- 车型条件被静默丢弃比例，目标为0；
- 用户澄清率；
- 用户纠正车型的比例；
- 搜索无结果率；
- 加入AgentHub后的净映射提升；
- 本地已命中却误调用AgentHub的比例，目标为0。

评估时要单独维护：

- 高频标准车型集；
- 英文/空格/符号变体集；
- 品牌+车型拆分集；
- 错别字和口语长尾集；
- 同名歧义集；
- 品牌/车系/车型父子冲突集；
- AgentHub无结果和超时集；
- 候选外幻觉攻击集。

---

## 14. 分阶段落地建议

### 阶段一：先完善本地车型库

- 接入生产车型数据；
- 建立标准实体ID；
- 完善别名与父子关系；
- 实现车型、车系、品牌固定优先级；
- 保存 CatalogVersion 和解析原因；
- 完成 Guide 车辆 FilterCode 映射与复验。

这一阶段完成后，常见车辆名称应不依赖 AgentHub。

### 阶段二：接入 AgentHub 长尾召回

- 新增 `api/agenthub`；
- 仅本地 `not_found` 时触发；
- 设置独立超时、TopK、熔断和短期缓存；
- 使用候选ID约束的小模型选择；
- 回到权威 Catalog 复验；
- 失败时保留原始诉求并继续搜索。

### 阶段三：评估和迭代

- 建立离线Badcase集；
- 比较“仅本地”与“本地+AgentHub”；
- 分析净映射提升和误映射；
- 通过灰度开关控制AgentHub；
- 只有离线、回放和线上指标同时达标后再扩大流量。

---

## 15. 源码位置索引

### Tyche

| 文件 | 关键位置 | 作用 |
|---|---|---|
| `common/util/normalize.go` | `Normalize` | 名称表面归一 |
| `common/handlers/static/vehicle.go` | 车型缓存初始化、增量更新、全量重建 | 车型基础数据与索引刷新 |
| `common/handlers/static/vehicle_index.go` | `VehicleIndex`、`buildVehicleIndex` | 轻量车型索引 |
| `logic/data/veh.go` | `VehBrandNameMap`、`CanonicalBrand` | 品牌别名归一 |
| `logic/agent/v4_extract.go` | `extractBodyTemplate` | 提取brand和vehicle_model等语义 |
| `logic/agent/v4_stage_filtercode.go` | `filterGroupBlacklist` | 品牌不进入通用菜单选码 |
| `logic/agent/search/vehicle_resolver.go` | `ResolveVehicleFromNeeds` | 本地车型确定性解析 |
| `logic/agent/capabilities.go` | `searchVehicleCapability.Run` | 本地优先、AgentHub兜底、合并搜索条件 |
| `logic/agent/v4_stage_search.go` | `VehicleRecall: true` | V4开启车型长尾召回 |
| `logic/agent/vehicle_recall.go` | `recallVehicleLongTail` | AgentHub召回、候选解析和精选 |
| `library/agenthub/client.go` | `RetrieveVehicle`、`Retrieve` | AgentHub传输客户端 |
| `logic/agent/v4_stage_rules.go` | `RulesStage` | 规则知识检索与业务侧生成 |
| `logic/agent/price_protection.go` | `buildPriceProtectFilters` | 价格保护车型标准化 |
| `logic/guibipei/step_ocr_normalize.go` | `matchBrandSeries` | OCR品牌和车系归一 |
| `logic/agent/vehicle_compare.go` | `lookupVehSpecFromCard` | 车辆对比静态规格 |

### 当前项目

| 文件 | 作用 |
|---|---|
| `internal/vehiclecatalog/catalog.go` | 当前静态车型目录与解析状态 |
| `internal/domain/vehiclerequirement/handler.go` | 车辆诉求归一和Session写入入口 |
| `internal/session/search_state.go` | 车辆实体解析状态持久化 |
| `internal/searchplan/compiler.go` | 实体冲突、父子冗余和执行计划 |
| `docs/requirement-filter-mapping-and-search-degradation-design.md` | 车型映射、Guide-only执行和降级的目标方案 |

---

## 16. 最终建议

当前项目应参考 Tyche 的四个原则：

1. 车辆名称使用独立的实体解析链，不与普通菜单选码混在一起；
2. 本地权威车型库优先，AgentHub只处理本地未命中的长尾；
3. LLM只在有限候选中选择，不能自由生成实体或FilterCode；
4. 车型库能力要跨搜车、OCR、价格保护、对比等场景复用。

同时应比 Tyche 当前实现多做两层验证：

```text
AgentHub候选
→ 权威VehicleEntityCatalog复验
→ 当前Guide Baseline FilterCode复验
```

只有两层都通过，车辆名称才能成为可执行筛选条件；否则保留原始诉求、继续按其他条件搜索，并明确告知用户该车型条件未被应用。
