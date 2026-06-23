# Phase 5: 扩展能力(决策辅助 / 资质闭环 / 售后 / 比价异议)

> **目标:** 在 P4 服务化基础上,扩出 4 类扩展能力,把 agent 从"会推荐"升级到"全程辅助"。

---

## 1. Context

P1-P4 完成的是**核心闭环**(导购 → 价格 → 保险 → 服务化)。P5 是**功能纵深**,本次落地:
- **决策辅助**:取还时间优化 / 行程总费估算(本地估算 tool)
- **资质预检**:驾龄 → 可租车型(本地规则 tool)
- **比价异议**:质疑价格 → 拆解我方定价构成(复用 get_order_details + prompt 红线)
- **售后查询**:订单状态查询(复用 rental_get_reservation)

**本次未做**(见第 3 节):下单 deeplink(缺 App 协议)、售后流程规则解读(缺知识库)。

---

## 2. 验收标准(分能力)

### 2.1 决策辅助
```
用户: 我晚两小时取车能省多少?
agent: [optimize_pickup_time(pickup_time, return_time, car_daily_yuan=报价真实日租)]
       原方案:计费 2 天 = 276 元
       晚 2 小时取车:计费 1.5 天 = 207 元,省 69 元
       (估算,实际计费以下单为准)

用户: 北京到天津 2 天大概多少钱?
agent: [estimate_trip_cost(days=2, car_daily_yuan=报价真实日租, one_way_km=120)]
       拆项估算(车 + 油 + 高速 + 停车):...
       ⚠️ 粗略估算,非精确报价,实际以下单及行程消费为准
```

### 2.2 资质预检
```
用户: 我驾龄 1 年能租 SUV 吗?
agent: [check_qualification(driver_age_years=1)]
       按车型档次的驾龄门槛:
       - 经济型:≥ 1 年(您可租 ✅)
       - SUV / MPV:≥ 2 年(您暂不可租)
       - 豪华型:≥ 3 年
       (门槛为参考值,最终以下单页面/门店要求为准)
       想看驾龄 1 年可租的经济型车吗?我帮您查报价。
```

### 2.3 售后查询
```
用户: 我那个订单到哪一步了?
agent: [rental_get_reservation(order_id=...)]
       订单状态:待取车,车型 大众朗逸,门店 首都机场 T3 店,金额 276 元

用户: 我想提前还车怎么操作?
agent: 提前还车的具体规则和操作请在 App「我的订单」中查看,
       或联系人工客服办理。(当前不解读规则,等知识库上线后补)
```

### 2.4 比价异议
```
用户: 别家朗逸只要 99 一天,你们为啥贵?
agent: [rental_get_order_details(reference_id=...)] 拆解我方定价:
       - 日租金 108
       - 基础保障 30
       如只看不含保障部分是 108。
       要不要我帮您看看去掉可选服务后的同级车价格?
       ([约束] 不评价对方价格真伪 / 不贬低对手 / 只讲自己的构成)
```

---

## 3. 实际实现范围

P5 **不新增 MCP,不新增子 agent**。扩展能力通过两种方式实现:
- **本地 InvokableTool**(纯计算/规则,在 agent 进程内,不查后端)—— 3 个
- **复用现有 tyche tool + prompt 引导** —— 比价、售后

| 能力 | 实现方式 | tool |
|---|---|---|
| 资质预检 | 本地规则表 | `check_qualification`(新增) |
| 取还时间优化 | 本地计费估算 | `optimize_pickup_time`(新增) |
| 行程总费估算 | 本地经验公式 | `estimate_trip_cost`(新增) |
| 比价异议 | 复用 + prompt 红线 | `rental_get_order_details` |
| 售后查询 | 复用 | `rental_get_reservation` |
| 门店选择 | 复用(已在 P1) | `rental_search_locations` + `rental_resolve_poi` |

**本次未做(待后续):**
- 下单 deeplink(`build_order_deeplink`)—— 等 App 同学提供 url scheme
- 售后流程规则解读 —— 需要知识库(P3 跳过),当前引导转人工
- 独立 AftersalesAgent / ComparePriceAgent —— 当前都归入 ShoppingAgent,prompt 区分

### Step 1 — 三个本地 tool
**文件:** `internal/tools/local_*.go`

- `check_qualification`([local_qualification.go](../../internal/tools/local_qualification.go))
  - 入参:`driver_age_years` + 可选 `vehicle_class`
  - 出参:各档车型 `{vehicle_class, min_age_years, eligible}`
  - ⚠️ 规则表是占位经验值,真实门槛由业务方提供后替换
- `estimate_trip_cost`([local_trip_cost.go](../../internal/tools/local_trip_cost.go))
  - 入参:`days, car_daily_yuan`(用真实报价 daily_price)+ 可选 `one_way_km/fuel_type/...`
  - 出参:车/油(电)/高速/停车/保险 拆项 + 总额,note 强制标"非精确"
- `optimize_pickup_time`([local_time_optimize.go](../../internal/tools/local_time_optimize.go))
  - 入参:`pickup_time, return_time, car_daily_yuan`
  - 出参:原方案 + 几个备选时间方案的计费天数 + 车费对比

### Step 2 — 注册到 ShoppingAgent
- [internal/tools/common.go](../../internal/tools/common.go):`All()` 末尾追加 `localTools()`(不走 tyche,不受白名单约束)
- [internal/agent/adk.go](../../internal/agent/adk.go):三个本地 tool 加进 ShoppingAgent 的 toolset 白名单

### Step 3 — Prompt 扩展
- [internal/prompt/shopping_system.go](../../internal/prompt/shopping_system.go):
  - 工具列表加第 7/8/9 条(本地 tool)
  - 新增"扩展能力(P5)"段:资质 / 时间优化 / 行程估算 / 比价异议 / 售后查询的触发条件和话术红线
  - 比价红线:不评价竞品真伪、不贬低对手、只拆解自家构成

### Step 4 — 单元测试
- [internal/tools/local_tools_test.go](../../internal/tools/local_tools_test.go):6 个 case 覆盖三个本地 tool 的正常/异常路径

---

## 4. 文件清单(P5 增量)

```
internal/tools/local_qualification.go      # 新增
internal/tools/local_trip_cost.go          # 新增
internal/tools/local_time_optimize.go      # 新增
internal/tools/local_tools_test.go         # 新增
internal/tools/common.go                   # 修改(localTools 注册)
internal/agent/adk.go                       # 修改(ShoppingAgent 白名单加 3 个本地 tool)
internal/prompt/shopping_system.go         # 修改(工具列表 + 扩展能力段)
docs/specs/phase5-extensions.md            # 本文档
```

**不新增 MCP、不改 tyche、不改 saas-api。**

---

## 5. 关键决策

| # | 决策 | 理由 |
|---|---|---|
| P5-D1 | 扩展能力用本地 tool,不新增 MCP | 资质/估算/时间优化都是纯计算,无后端数据源 |
| P5-D2 | 不拆独立 AftersalesAgent / ComparePriceAgent | 当前归入 ShoppingAgent,prompt 区分即可;真复杂了再拆 |
| P5-D3 | `estimate_trip_cost` / `optimize_pickup_time` 显式标"非精确" | 经验公式估算,避免误导用户 |
| P5-D4 | 比价场景不贬低对手 | 合规;prompt 红线 |
| P5-D5 | 资质规则放本地规则表(占位) | 变更范围小,无需后端接口;真实值业务方提供 |
| P5-D6 | 下单 deeplink / 售后规则解读本次不做 | 前者缺 App 协议,后者缺知识库;不阻塞其他能力 |

---

## 6. 已识别 TODO

- [ ] 业务方提供真实"驾龄 → 可租车型"规则表,替换 `qualificationRules` 占位值
- [ ] 行程估算经验系数(油价/能耗/过路/停车)由业务/运营校准
- [ ] 下单 deeplink:等 App 同学给 url scheme 后补 `build_order_deeplink`
- [ ] 售后流程规则:依赖知识库,等 P3 知识库补回后做
- [ ] 评估集补 P5 场景 case(资质 / 估算 / 时间优化 / 比价 各 3 条)
