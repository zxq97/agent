# 车型对比与租车规则功能技术方案

## 1. 文档目标

本文描述新增的两项能力：

1. 对当前搜索结果中的车辆报价进行结构化对比；
2. 查询租车证件、押金、取消、里程等规则。

方案遵循以下约束：

- 车型对比只能使用 Guide 已返回的真实字段；
- 不通过 LLM 补造车辆配置、价格、空间、安全性或供应商规则；
- 租车规则回答必须来自受控规则目录；
- 订单级规则没有权威数据时，必须明确要求用户查看订单页或咨询门店；
- Router 只识别 Action 和原文证据，不生成车辆 ID、报价 ID 或规则结论；
- 两项能力不修改搜车条件，不触发隐式 FilterCode。

---

## 2. 功能边界

### 2.1 车型对比支持范围

当前支持对比同一会话当前结果中的 2～4 个报价。

支持字段：

| 字段 | 来源 | 是否可推断 |
|---|---|---|
| 车辆名称 | Guide quote | 否 |
| 品牌 | Guide quote | 否 |
| 车型组 | Guide quote | 否 |
| 座位数 | Guide quote | 否 |
| 供应商 | Guide quote | 否 |
| 当前总价 | Guide quote | 否 |
| 日均扣减金额字段 | Guide quote | 否 |
| 能源类型编码 | Guide quote | 不翻译未知枚举 |
| 变速箱类型编码 | Guide quote | 不翻译未知枚举 |

当前不比较：

- 未返回的后备箱容积；
- 车身尺寸和轴距；
- ISOFIX 数量；
- 主被动安全配置；
- 真实油耗或电耗；
- 舒适性结论；
- “适合老人”“适合儿童”等探索性结论。

这些字段只有接入权威车型事实后才能进入对比。

### 2.2 租车规则支持范围

当前规则目录覆盖：

- 证件要求；
- 年龄与驾龄；
- 押金和预授权；
- 取消与改期；
- 里程限制；
- 燃油与充电；
- 超时与续租；
- 使用区域和异地还车；
- 附加驾驶人；
- 保障与责任。

当前规则目录提供的是通用核对指引，不是具体供应商或订单承诺。

以下问题不会生成确定答案：

- “取消一定扣多少钱”；
- “所有车统一押金多少”；
- “最低年龄一定是多少”；
- “一定可以跨境吗”；
- “事故一定赔多少”。

缺少订单级权威数据时，结果必须包含：

```text
verification_required=true
source=订单页、供应商和门店最终条款
```

---

## 3. 总体架构

```text
用户消息
  ↓
LLM Router
  ├─ compare_vehicles
  └─ query_rental_rules
  ↓
Deterministic Planner
  ↓
Orchestrator
  ├─ VehicleComparisonHandler
  │    ├─ Session.LastResults
  │    ├─ ActiveSearch.Batches
  │    ├─ Quote Selector
  │    └─ Deterministic Comparator
  │
  └─ RentalRulesHandler
       ├─ Versioned Rule Catalog
       ├─ Keyword/Category Matcher
       └─ Verification Boundary
  ↓
WebChat Response
  ├─ vehicle_comparison
  └─ rental_rules
```

### 3.1 新增模块

```text
internal/domain/vehiclecompare/
  types.go
  handler.go
  handler_test.go

internal/domain/rentalrules/
  types.go
  catalog.go
  handler.go
  handler_test.go
```

### 3.2 修改模块

```text
internal/router/
internal/planner/
internal/orchestrator/
internal/webchat/
cmd/http/
web/index.html
```

---

## 4. Router 设计

新增两个 Action：

```go
const (
    ActionCompareVehicles  ActionType = "compare_vehicles"
    ActionQueryRentalRules ActionType = "query_rental_rules"
)
```

### 4.1 compare_vehicles

适用输入：

```text
对比1和3
第一辆和第二辆哪个好
对比这几个报价
```

不适用：

```text
SUV和MPV的概念有什么区别
```

概念知识问题仍属于 `general_reply`。

### 4.2 query_rental_rules

适用输入：

```text
取消订单怎么收费
需要多少押金
驾龄有什么要求
能不能异地还车
```

规则问题不能路由到：

- `update_vehicle_requirements`；
- `request_vehicle_search`；
- `general_reply`。

### 4.3 Prompt 工程

Router Prompt 负责：

- 定义两个新 Action；
- 提供正反例；
- 约束 `evidence_text` 必须逐字来自用户原文；
- 禁止把车型概念解释误判为当前报价对比；
- 禁止把规则问题误判为车辆筛选。

Router 不负责：

- 解析报价 ReferenceID；
- 选择车辆；
- 计算价格差；
- 生成规则答案。

---

## 5. Planner 和执行顺序

确定性执行顺序：

```text
modify_rental_context
  → update_vehicle_requirements
  → execute_vehicle_search
  → compare_vehicles
  → query_rental_rules
  → general_reply
```

这样可以支持同一轮：

```text
按当前条件重新搜，然后对比1和2，再告诉我押金规则
```

搜车完成后，车型对比读取本轮刚写入 Session 的结果。

规则查询与搜车无数据依赖，但保持串行执行，避免回复顺序不稳定。

---

## 6. 车型对比处理流程

### 6.1 数据来源

车型对比不重新调用 Guide。

数据链路：

```text
SearchState.LastResults
  → ReferenceID / VehicleCode / SupplierCode
  → ActiveSearch.Batches
  → searchruntime.Quote
  → Comparison Option
```

查找优先级：

1. `ReferenceID` 精确匹配；
2. `VehicleCode + SupplierCode` 匹配；
3. 无法匹配则跳过该失效引用。

### 6.2 车辆选择

支持：

- 阿拉伯数字序号：`1、2、3、4`；
- 中文序号：`第一、第二、第三、第四`；
- 当前结果中的完整车辆名称；
- 当前恰好两个结果时，“对比一下”默认对比两个；
- 当前结果不超过四个且用户明确说“全部、这些、这几个”时对比全部。

约束：

- 少于两个：`no_search_result` 或 `needs_selection`；
- 多于四个：`needs_selection`；
- 不允许模型输出内部报价 ID；
- 不允许比较不在 `LastResults` 中的车辆。

### 6.3 高亮计算

当前只计算确定性高亮：

```text
最低总价选项
最多座位选项
总价差
```

计算规则：

- 缺少总价的选项不参与最低价和价格差计算；
- 缺少座位数的选项不参与最大座位数计算；
- 相同最低价或相同最大座位数允许多个并列选项；
- 不计算“综合最好”。

### 6.4 输出状态

| 状态 | 含义 |
|---|---|
| `success` | 已完成 2～4 个当前报价对比 |
| `needs_selection` | 当前候选较多或用户未明确选择 |
| `no_search_result` | 当前没有至少两个可对比报价 |

### 6.5 输出限制说明

每次成功对比都携带：

```text
只比较 Guide 当前报价返回的字段
未返回的空间、安全和配置字段不会被推断
价格只代表当前搜索上下文和当前报价
```

---

## 7. 租车规则处理流程

### 7.1 权威边界

当前系统没有：

- 供应商规则 API；
- 门店规则 API；
- 订单取消规则 API；
- 保障条款 API。

因此第一阶段使用版本化 `StaticCatalog`，只提供规则核对指引。

```text
Rule Catalog
  → Category Match
  → Guidance
  → Verification Required
```

### 7.2 规则结构

```go
type Rule struct {
    ID                   string
    Category             string
    Title                string
    Guidance             string
    Scope                string
    Source               string
    VerificationRequired bool
}
```

当前统一使用：

```text
scope=general_guidance
verification_required=true
source=订单页、供应商和门店最终条款
```

### 7.3 匹配方式

当前采用确定性关键词匹配。

示例：

```text
押金 / 预授权 / 免押
  → deposit

取消 / 退款 / 改期
  → cancellation

年龄 / 驾龄 / 新手
  → driver_qualification
```

用户询问“租车有哪些规则”时返回规则总览。

没有命中时：

```text
status=insufficient_knowledge
```

并明确说明不能编造答案。

### 7.4 后续权威数据接入

生产版本建议新增：

```go
type ProviderRuleClient interface {
    QueryRules(ctx context.Context, input *RuleQuery) (*RuleSnapshot, error)
}
```

推荐优先级：

```text
当前订单规则
  > 当前报价规则
  > 供应商+门店规则
  > 城市/国家通用规则
  > 本地通用核对指引
```

每条规则需要：

- `source_id`；
- `source_version`；
- `effective_from`；
- `effective_to`；
- `supplier_code`；
- `store_id`；
- `quote_reference_id`；
- `retrieved_at`。

失效规则不得继续回答。

---

## 8. LLM、Prompt、Context 与 Harness

### 8.1 使用 LLM 的位置

只在 Router 使用 LLM 判断：

```text
compare_vehicles
query_rental_rules
```

### 8.2 不使用 LLM 的位置

- 车辆序号解析；
- Session 报价解析；
- 对比字段生成；
- 最低价和座位高亮；
- 规则类别匹配；
- 规则正文生成；
- WebChat 结构化输出。

### 8.3 Context 工程

Router 可看到：

- `has_previous_search`；
- 最近对话；
- 当前租车条件；
- 当前车辆要求。

车型对比 Handler 可看到：

- `LastResults`；
- `ActiveSearch.Batches`。

规则 Handler 只看到：

- Router 逐字提供的规则问题证据；
- 版本化规则目录。

规则 Handler 不接收完整 Session，减少无关上下文和隐私暴露。

### 8.4 Harness 工程

Router 继续使用统一 LLM Harness：

- PromptVersion；
- SchemaVersion；
- ValidatorVersion；
- Flash 主模型；
- Pro 降级；
- Schema 错误自动重试；
- Token、延迟和调用记录。

车型对比与规则领域本身不新增 LLM 调用，因此不增加额外 Token 成本。

---

## 9. WebChat 协议

### 9.1 车型对比

```json
{
  "vehicle_comparison": {
    "status": "success",
    "options": [
      {
        "index": 1,
        "vehicle_name": "车辆名称",
        "brand_name": "品牌",
        "group_name": "车型组",
        "seats": 7,
        "supplier_name": "供应商",
        "total_amount": 500
      }
    ],
    "highlights": {
      "lowest_total_price_indexes": [1],
      "most_seats_indexes": [1],
      "total_price_spread": 50
    },
    "scope": "current_search_result",
    "limitations": []
  }
}
```

### 9.2 租车规则

```json
{
  "rental_rules": [
    {
      "id": "cancellation",
      "category": "cancellation",
      "title": "取消与改期",
      "guidance": "订单级通用核对指引",
      "scope": "general_guidance",
      "source": "订单页、供应商和门店最终条款",
      "verification_required": true
    }
  ]
}
```

前端使用 DOM `textContent` 渲染所有服务端文本，不使用 HTML 注入。

---

## 10. 错误与降级

### 10.1 车型对比

| 情况 | 处理 |
|---|---|
| 没有历史搜索 | 提示先搜车 |
| 少于两个有效报价 | 不生成对比 |
| 未指定车辆且候选过多 | 返回候选并要求选择 |
| Session 引用失效 | 跳过失效引用 |
| 字段缺失 | 显示未知，不推断 |

### 10.2 租车规则

| 情况 | 处理 |
|---|---|
| 未命中目录 | `insufficient_knowledge` |
| 缺少订单级规则 | 返回通用指引并要求核对 |
| 询问具体金额或门槛 | 不生成统一数字 |
| 规则目录版本为空 | 构造阶段使用默认版本 |

---

## 11. 测试策略

### 11.1 车型对比

- 根据序号选择两个报价；
- 中文序号选择；
- 名称选择；
- 当前结果恰好两个时默认全选；
- 多候选未指定时要求选择；
- ReferenceID 优先解析；
- 最低价并列；
- 座位数并列；
- 缺失价格不参与价差；
- 不在当前结果中的车辆不能进入对比。

### 11.2 租车规则

- 押金问题命中 `deposit`；
- 取消问题命中 `cancellation`；
- 规则总览返回全部类别；
- 未知问题不编造答案；
- 每条静态规则必须 `verification_required=true`。

### 11.3 链路测试

- Router 接受两个新 Action；
- Planner 将对比放在搜索之后；
- Orchestrator 调用正确 Handler；
- WebChat 返回结构化字段；
- 已完成请求重放时深拷贝新字段；
- 页面安全渲染对比和规则。

---

## 12. 当前限制与后续计划

当前限制：

1. 车型对比只覆盖当前搜索结果；
2. 不支持跨会话车型对比；
3. 不支持用户输入任意市场车型进行参数对比；
4. 车型事实仍以 Guide 当前返回字段为主；
5. 租车规则尚未接入供应商、门店和订单级权威接口；
6. 规则目录当前是代码内版本化静态目录。

后续建议：

1. 接入权威 VehicleFacts 数据；
2. 接入报价级和订单级规则快照；
3. 给规则记录增加有效期和来源 ID；
4. 支持收藏车辆后跨批次对比；
5. 增加规则回放和过期规则扫描；
6. 对 Router 新 Action 建立离线混淆矩阵；
7. 统计 `needs_selection`、规则未命中率和规则核验点击率。

---

## 13. 验收标准

车型对比：

- 只能选择当前结果车辆；
- 只能使用真实返回字段；
- 缺失字段不推断；
- 最多同时比较四个；
- 不输出“综合最好”等无依据结论。

租车规则：

- 规则问题不进入通用自由生成；
- 每条回答有范围和核验要求；
- 未知规则明确拒绝编造；
- 具体订单规则以权威订单数据为最终依据。

整体：

- 两个 Action 可与搜车、修改条件混合执行；
- 统一 Harness 只用于路由，不参与事实生成；
- WebChat API 和页面均能展示结构化结果；
- `go test ./...` 和 `go vet ./...` 通过。
