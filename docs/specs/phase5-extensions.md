# Phase 5: 扩展能力(决策辅助 / 资质闭环 / 售后 / 比价异议)

> **目标:** 在 P4 服务化基础上,扩出 4 类扩展能力,把 agent 从"会推荐"升级到"全程辅助"。

---

## 1. Context

P1-P4 完成的是**核心闭环**(导购 → 价格 → 保险 → 条款 → 服务化)。P5 是**功能纵深**:
- **决策辅助**:门店选择 / 取还时间优化 / 行程总费估算
- **资质闭环**:驾照预检 + 订单前问答 + 跳转下单 deeplink
- **售后**:常见 FAQ + 流程规则解读
- **比价异议**:贴竞品报价 / 质疑价格 → 拆解定价构成

---

## 2. 验收标准(分能力)

### 2.1 决策辅助
```
用户: 香山周边能取还的店?
agent: [list_stores(city_id=1, keyword="香山")] [check_stock?]
       这附近营业中且本周末可还的店:
       1. 海淀香山公园站  6:00-23:00  距您 1.2km
       2. ...

用户: 我晚两小时取车能省多少?
agent: [基于已有报价规则 + get_price_detail]
       原方案 9:00 取 → 19:00 还,日租 138 × 2 = 276
       改 11:00 取 → 19:00 还,系统计为 1 天 + 8 小时 = 1.5 天 = 207 元
       省 69 元。
       (前提是不超 24h 跨自然日规则)

用户: 北京到天津 2 天大概多少钱?
agent: 给出拆项估算(车 + 油 + ETC + 停车 + 保险),并显式标"非精确报价,实际以下单为准"
```

### 2.2 资质闭环
```
用户: 我驾龄 1 年能租 SUV 吗?
agent: 驾龄要求按车型不同:
       - 经济型小车:驾龄 ≥ 1 年(您可租)
       - SUV/MPV:多数要求驾龄 ≥ 2 年
       - 豪华车:驾龄 ≥ 3 年
       为您筛选驾龄 1 年可租的 SUV...
       [list_quotes(driver_age=1)]

用户: 就要这辆朗逸了,怎么下单?
agent: [build_order_deeplink(quote_id=...)]
       为您准备好下单链接 → didi://rental/confirm?xxx
       请在 App 内确认信息并完成支付。
       (agent 不替您下单)
```

### 2.3 售后 FAQ
```
用户: 我想提前还车,怎么操作?
agent: [search_knowledge(query="提前还车")]
       提前还车规则:
       - 提前 ≥ 24h:全额退已支付日租金
       - 提前 < 24h:已发生天数按日租,剩余按 50% 退
       [来源: knowledge/fulfillment/early_return.md]
       具体操作:打开 App > 我的订单 > 该订单 > 提前还车
       (agent 不替您发起申请)
```

### 2.4 比价异议
```
用户: 别家朗逸只要 99 一天,你们 138 为啥这么贵?
agent: [get_price_detail(quote_id=...)]
       我们的 138 / 天 包含:
       - 日租金 108
       - 基础保险 30(含盗抢险 + 第三者险)
       如不含基础保险对比,我们是 108,与您说的 99 元差 9 元,
       多出的部分主要在 ...(机场服务费 / 油箱等级 / 取车便利度)
       是否需要为您看看 不含保险 的同级车?
       ([约束] 不评价对方价格 / 不贬低对手)
```

---

## 3. 分步实现

### Step 1 — 决策辅助
**新增 tool:**
- `find_nearby_stores`:门店 + 地理位置联合查询(`internal/tools/find_nearby_stores.go`)
  - 入参:`pickup_lng / pickup_lat / radius_km / vehicle_filter`
  - 后端可能需要扩 `agent_mcp/store/nearby` 接口(P4 扩 BFF 时一并加)
- `time_optimize`:取还车时间优化建议(`internal/tools/time_optimize.go`)
  - 入参:`pickup_time / return_time / vehicle_id`
  - 实现:本地按报价规则计算几种 alternative,返回对比表
- `estimate_trip_cost`:行程总费估算(`internal/tools/estimate_trip_cost.go`)
  - 入参:`from / to / days / vehicle_class`
  - 实现:简单乘法 + 经验值,**强制 note:非精确报价**

### Step 2 — 资质闭环
**新增 tool:**
- `check_qualification`:驾照/资质预检(`internal/tools/check_qualification.go`)
  - 入参:`driver_age / id_card_age / vehicle_class`
  - 实现:本地规则表(可后端化为配置)
- `build_order_deeplink`:下单 deeplink 生成(`internal/tools/build_order_deeplink.go`)
  - 入参:`quote_id / store_code / pickup_time / return_time / insurance_codes`
  - 出参:`url / qrcode_url(可选)`
  - 实现:URL 拼接,**严禁后端写库**

**Prompt 调整:**
- ShoppingAgent 在用户明确"就这个"后,主动调 `build_order_deeplink` 并引导跳 App

### Step 3 — 售后 FAQ
- 复用 P3 KnowledgeAgent + `search_knowledge`
- 在 `knowledge/fulfillment/` 增加:`modify_order.md / change_vehicle.md / extend_rental.md / early_return.md`
- **AftersalesAgent** 是 KnowledgeAgent 的子图,prompt 强制:
  - 任何"操作"指令 → 引导用户在 App / 客服执行,agent 只解释规则

### Step 4 — 比价异议
- 新增 `ComparePriceAgent`
- toolset:`get_price_detail`(必),`list_quotes`(可选,推荐同级车)
- Prompt 红线:
  - **不评价**竞品价格真伪
  - **不贬低**对手
  - 只拆解自家定价构成,提供"不含保险" / "降配置"等可比选项

### Step 5 — Supervisor 路由扩展
- 加 `Phase` 枚举:`PhaseDecision / PhaseQualification / PhaseAftersales / PhaseCompare`
- LLM 意图分类 prompt 扩到 4 个新意图

---

## 4. 文件清单(P5 增量)

```
internal/tools/find_nearby_stores.go       # 新增
internal/tools/time_optimize.go            # 新增
internal/tools/estimate_trip_cost.go       # 新增
internal/tools/check_qualification.go      # 新增
internal/tools/build_order_deeplink.go     # 新增
internal/agent/aftersales.go               # 新增
internal/agent/compare_price.go            # 新增
internal/agent/supervisor.go               # 修改(扩路由)
internal/prompt/{aftersales,compare_price}_system.go  # 新增
knowledge/fulfillment/{modify_order,change_vehicle,extend_rental,early_return}.md  # 新增
docs/specs/phase5-extensions.md            # 本文档
```

---

## 5. 关键决策

| # | 决策 | 理由 |
|---|---|---|
| P5-D1 | `build_order_deeplink` 只生成 URL,不写库 | 沿用"agent 不闭环下单"原则,链接由前端消化 |
| P5-D2 | `estimate_trip_cost` 显式标"非精确" | 避免误导用户 |
| P5-D3 | 比价场景**不贬低对手** | 合规;prompt 红线 + 评估集专项 case |
| P5-D4 | 资质规则放本地规则表(可配置化) | 频繁变更但变更范围小,无需后端接口 |
| P5-D5 | 售后 FAQ 复用 KnowledgeAgent | 不重复造轮子;只扩内容不扩 agent |

---

## 6. 已识别 TODO(P5 内必清)

- [ ] 业务方提供驾龄/车型对应表
- [ ] 下单 deeplink 协议与 App 同学对齐(url scheme / 参数)
- [ ] 评估集扩 30 → 50,新增 4 类扩展场景各 5 条
- [ ] "非精确报价"的免责措辞由法务 review
