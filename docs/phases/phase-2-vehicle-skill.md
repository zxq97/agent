# Phase 2: 车辆推荐 Skill (Vehicle Recommendation)

## 目标

实现车辆推荐与介绍能力，用户可通过自然语言获取车型推荐、了解车型详情、对比不同车型。

## 前置条件

- Phase 1 基础骨架已完成并可运行
- 车辆业务 API 文档已获取（或 Mock 定义已确认）

## 交付物清单

| # | 交付物 | 说明 |
|---|--------|------|
| 1 | Vehicle Tool 定义 | 搜索车型、获取详情、对比车型的 Tool |
| 2 | Vehicle Skill 实现 | System Prompt + Tool 注册 |
| 3 | 意图检测示例 | vehicle 相关意图的示例对话 |
| 4 | Tool 与业务 API 对接 | 调用车辆 API 获取数据 |
| 5 | 集成测试 | 端到端车辆推荐对话测试 |

## 详细步骤

### Step 2.1: 定义 Vehicle Tools

**文件**: `internal/tool/vehicle/search.go`

```go
// SearchVehicles 搜索车型
// 参数:
//   - city: 城市名称（必填）
//   - start_date: 取车日期（可选）
//   - end_date: 还车日期（可选）
//   - passenger_count: 乘客数（可选）
//   - budget_max: 最大日租金（可选）
//   - car_type: 车型偏好：经济型/舒适型/商务型/SUV/MPV（可选）
//   - fuel_type: 油电类型：燃油/混动/纯电（可选）
// 返回: 车型列表（精简字段）
```

**文件**: `internal/tool/vehicle/detail.go`

```go
// GetVehicleDetail 获取车型详情
// 参数:
//   - vehicle_id: 车型ID（必填）
// 返回: 车型详细信息（含配置、特色标签、用户评价摘要）
```

**文件**: `internal/tool/vehicle/compare.go`

```go
// CompareVehicles 对比车型
// 参数:
//   - vehicle_ids: 车型ID列表，2-4个（必填）
// 返回: 对比表格数据
```

### Step 2.2: Tool 数据精简

业务 API 可能返回大量字段，Tool 层需要精简为 LLM 需要的关键信息：

```
原始数据（业务 API 返回）：
{
  "vehicle_id": "V001",
  "brand": "丰田",
  "model": "卡罗拉",
  "year": 2024,
  "displacement": "1.2T",
  "seats": 5,
  "transmission": "自动",
  "fuel_type": "燃油",
  "luggage_capacity": 3,
  "daily_price": 189,
  "deposit": 2000,
  "features": ["蓝牙", "倒车影像", "定速巡航"],
  "tags": ["经济实惠", "省油之选"],
  "rating": 4.5,
  "review_count": 1234,
  "images": ["..."],
  // ... 更多字段
}

精简后（给 LLM 的 Tool Result）：
{
  "vehicle_id": "V001",
  "name": "丰田卡罗拉 2024款",
  "type": "舒适型",
  "seats": 5,
  "luggage": "3个行李箱",
  "fuel": "燃油 1.2T",
  "daily_price": "¥189/天",
  "features": "蓝牙、倒车影像、定速巡航",
  "tags": "经济实惠、省油之选",
  "rating": "4.5分 (1234条评价)"
}
```

### Step 2.3: Vehicle Skill System Prompt

**文件**: `internal/skill/vehicle/prompt.go`

```
你是租车平台的车辆推荐顾问。你的任务是帮助用户找到最合适的租车方案。

## 你的工作方式
1. 了解用户需求：出行城市、日期、人数、预算、偏好
2. 根据需求推荐 2-3 款最合适的车型
3. 介绍推荐车型的核心亮点和适用场景
4. 如果用户想了解更多，可以查询详情或进行对比

## 推荐原则
- 优先匹配用户的出行场景（商务选舒适/商务型，家庭选SUV/MPV，经济选经济型）
- 考虑人数和行李需求，座位数和后备箱空间很重要
- 在预算范围内推荐性价比最高的选择
- 主动提示用户可能忽略的因素（如长途选SUV更舒适）

## 交互规范
- 每次推荐不超过 3 款车型，避免信息过载
- 先给结论，再展开说明
- 使用简洁的对比表格展示关键差异
- 如果信息不足，主动询问 1-2 个关键问题
- 不要编造车型信息，所有数据来自 Tool 查询

## 可用工具
- search_vehicles: 搜索车型
- get_vehicle_detail: 获取车型详情
- compare_vehicles: 对比车型
```

### Step 2.4: 意图检测示例

```go
var Examples = []skill.Example{
    {Query: "我想租一辆车", Intent: "vehicle_recommend", Skill: "vehicle"},
    {Query: "北京有什么车可以租", Intent: "vehicle_recommend", Skill: "vehicle"},
    {Query: "5个人出行租什么车好", Intent: "vehicle_recommend", Skill: "vehicle"},
    {Query: "帮我推荐一款省油的车", Intent: "vehicle_recommend", Skill: "vehicle"},
    {Query: "卡罗拉和轩逸哪个好", Intent: "vehicle_compare", Skill: "vehicle"},
    {Query: "这款车怎么样", Intent: "vehicle_detail", Skill: "vehicle"},
    {Query: "商务车有哪些", Intent: "vehicle_recommend", Skill: "vehicle"},
    {Query: "预算200一天能租什么车", Intent: "vehicle_recommend", Skill: "vehicle"},
}
```

### Step 2.5: Tool 与业务 API 对接

**文件**: `internal/tool/vehicle/client.go`

```go
// VehicleClient 车辆业务 API 客户端接口
type VehicleClient interface {
    Search(ctx context.Context, params *SearchParams) (*SearchResult, error)
    GetDetail(ctx context.Context, vehicleID string) (*VehicleDetail, error)
}

// 实现两个版本：
// 1. MockClient — 开发和测试用，返回预设数据
// 2. HTTPClient — 生产用，调用真实业务 API
```

Mock 数据需要覆盖：
- 不同城市（北京/上海/广州）
- 不同车型类别（经济/舒适/商务/SUV/MPV）
- 不同价格区间
- 零结果场景

### Step 2.6: 集成测试场景

1. **基本推荐**：用户说"我想在北京租车" → 返回推荐车型
2. **带条件推荐**：用户说"5个人出行，预算200一天" → 返回匹配车型
3. **详情查询**：用户说"卡罗拉怎么样" → 返回车型详情
4. **车型对比**：用户说"卡罗拉和朗逸对比" → 返回对比表格
5. **追问**：推荐后用户问"第二款有自动挡吗" → 保持上下文回答
6. **零结果**：搜索条件无匹配 → 给出建议调整条件
7. **信息不足**：用户说"推荐个车" → 主动询问城市和日期

## 验收标准

- [ ] 用户可通过对话获取车辆推荐
- [ ] 推荐结果来自 Tool 调用（非 LLM 编造）
- [ ] 多轮对话中上下文正确保持
- [ ] 对比和详情查询功能正常
- [ ] 信息不足时主动追问
- [ ] 零结果时给出友好提示
