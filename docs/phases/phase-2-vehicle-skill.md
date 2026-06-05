# Phase 2: 车辆推荐 Agent (Vehicle Recommendation)

## 目标

完善 VehicleAgent，使其能根据用户的出行场景、预算、人数等条件推荐合适的车型，并介绍车辆参数和对比不同车型。

## 前置条件

- Phase 1 基础骨架已完成
- tyche MCP 可访问，`rental_search_locations`、`rental_resolve_poi`、`rental_search_quotes` 工具可用

## 交付物

| # | 交付物 | 说明 |
|---|--------|------|
| 1 | Vehicle Agent 完善 | System Prompt + 对话策略 |
| 2 | 车辆 MCP Tool 精细封装 | 搜索地点、搜索报价、对比车型 |
| 3 | 车型知识库 | 车型参数、标签、常见问题 |
| 4 | 多轮推荐流程 | 支持追问、条件调整、对比 |
| 5 | CLI 交互测试 | 端到端车辆推荐体验 |

---

## Step 2.1: Vehicle Agent System Prompt

```
你是租车平台的车辆推荐顾问。你的职责是帮助用户找到最合适的车型。

工作流程：
1. 了解需求：询问用户的出行场景、人数、预算、取还车城市和时间
2. 搜索报价：调用 rental_search_locations 搜索取车点，调用 rental_search_quotes 搜索可用车型
3. 推荐车型：根据用户需求和搜索结果，推荐 2-3 款最合适的车型
4. 对比说明：如用户需要对比，详细说明各车型的优劣势

约束：
- 车型和价格必须来自 rental_search_quotes 工具，不得编造
- 如果搜索结果为空，如实告知并建议调整条件
- 车型参数（座位数、油电类型等）以工具返回为准
- 常见问题可参考知识库，但具体车辆信息必须工具查询
```

## Step 2.2: 车辆 MCP Tool 封装

在 Phase 1 的基础 MCP Client 上，为 VehicleAgent 专门封装以下 Tool：

### Tool: search_pickup_locations
- 底层调用: `rental_search_locations`
- 语义化参数: `city`, `keyword`
- 输出格式化: 地点名称 + 地点ID

### Tool: search_vehicle_quotes
- 底层调用: `rental_search_quotes`
- 语义化参数: `pickup_location_id`, `dropoff_location_id`, `pickup_time`, `dropoff_time`
- 输出格式化: 车型名 + 座位数 + 日租金 + 总价 + 车型标签

### Tool: resolve_location
- 底层调用: `rental_resolve_poi`
- 语义化参数: `location_id`
- 输出格式化: 门店名 + 地址 + 营业时间

## Step 2.3: 车型知识库

**目录**: `knowledge/vehicle/`

### knowledge/vehicle/car_types.json
```json
[
  {
    "category": "经济型",
    "description": "适合日常通勤和短途出行",
    "typical_models": ["朗逸", "轩逸", "卡罗拉"],
    "seat_count": 5,
    "luggage": "2个行李箱",
    "tags": ["省油", "性价比高"]
  },
  {
    "category": "商务型",
    "description": "适合商务接待和正式场合",
    "typical_models": ["帕萨特", "凯美瑞", "奥迪A4L"],
    "seat_count": 5,
    "luggage": "3个行李箱",
    "tags": ["舒适", "体面"]
  }
  // ...
]
```

### knowledge/vehicle/faq.md
```markdown
## 常见问题

### Q: 电动车和燃油车怎么选？
A: 短途城市出行推荐电动车，成本低；长途或跨城推荐燃油车，续航有保障。

### Q: 可以指定具体车型吗？
A: 下单时选择车型组，实际取车时可能同级替代。如需指定，请联系门店确认。
```

## Step 2.4: 多轮推荐流程

VehicleAgent 需要支持以下多轮对话模式：

1. **信息收集轮**：用户说"我想租车" → Agent 追问城市、时间、人数
2. **搜索推荐轮**：收集完信息后调用 MCP 搜索，推荐 2-3 款
3. **追问轮**：用户问"有更便宜的吗" → 基于之前搜索条件调整
4. **对比轮**：用户问"A和B哪个好" → 逐项对比参数和价格

实现方式：通过 eino Agent 的多轮对话能力 + Session 上下文管理。

## Step 2.5: 端到端测试场景

1. "我想在北京租一辆5座车，周末用" → 推荐车型
2. "有SUV吗" → 调整筛选
3. "这辆朗逸和卡罗拉哪个更适合自驾游" → 对比
4. "在望京取车" → 搜索取车点
5. "这辆车一天多少钱" → 报价查询
