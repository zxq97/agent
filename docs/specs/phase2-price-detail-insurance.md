# Phase 2: 价格明细 + 保险推荐

> **目标:** 让 agent 在用户选定某辆车后能"讲清价格"和"给出保险组合建议"。
> **方案:** 价格明细和保险列表都来自 `rental_get_order_details`，无需新增工具。

---

## 1. Context

### 数据来源

`rental_get_order_details` 一次调用返回两块数据：

```
price_detail:
  daily_price      日均价(元)
  total            总价(元)
  charges[]        费用明细项 {name, amount}
  promotions[]     优惠项 {title, deduction_amount}
  best_coupon      最佳优惠券

guarantee_list     保险列表
  level            1=基础(必选) 2=优享(非必选) 3=尊享(非必选)
  title            保险名称
  required         是否必选
  amount           总价(分)
  day_amount       日价(分)
  detail[]         保障详情条目
  broken           车损 {from, to}
  third            三者 {from, to}
  depreciation     折旧
  outage           停运费
```

P2 的工作是改进 prompt，让 LLM 正确解读这两块数据，无需新增工具。

---

## 2. 验收标准

### 2.1 必过 demo
```
用户: (P1 对话中已选定某辆车，reference_id 在 history 里)
      这辆车的价格怎么算的？

agent: [调用 rental_get_order_details(reference_id=..., context_id=..., supplier=...)]
       价格明细：
       - 日租金 138 元 × 2 天 = 276 元
       - 基础保障费 30 元（已含）
       - 优惠：无
       合计 276 元，以下单时为准。

用户: 我开车 3 年，要不要加保险？

agent: (无需再次调用工具，直接从刚才的 guarantee_list 解读)
       上面已经返回了保险选项，根据您驾龄 3 年：

       必选（已含在价格里）：
       - 基础保障 ¥30/天：车损 1500 元以上部分 + 三者 1500 元以上

       可选升级：
       - 优享保障 ¥38/天：车损免赔额降至 0，三者提升至 100 万
       - 尊享保障 ¥55/天：全面覆盖，含停运费

       推荐：驾龄 3 年可选"优享保障"，刮蹭赔付无压力，性价比合理。
       保障范围以保险合同条款为准。
```

### 2.2 检查清单
- [ ] 用户问价格明细 → agent 调 `rental_get_order_details`，解读 `price_detail.charges[]` 和 `promotions[]`
- [ ] 用户问保险 → **优先复用上一次 `get_order_details` 的返回值**（不重复调用），从 `guarantee_list` 解读
- [ ] 若 `guarantee_list` 为空或上下文没有，**且用户明确要看保险** → 重新调 `rental_get_order_details`
- [ ] 保险推荐**必须**基于 `guarantee_list` 真实返回，不允许 LLM 自由发挥保障范围
- [ ] 推荐逻辑依据驾龄：< 2 年全险，2-5 年优享，> 5 年按需
- [ ] 答复结尾**必须**出现"以下单时为准"和"保障范围以保险合同条款为准"

---

## 3. 分步实现

### Step 1 — 扩写 system prompt（核心工作）

**文件:** [internal/prompt/shopping_system.go](../../internal/prompt/shopping_system.go)

在"严格的调用顺序"一节的第 3 步之后补充：

```
**第 3 步 — 价格明细 + 保险（用户选定某辆车后）:**
调用 rental_get_order_details，一次调用同时拿到：
  - price_detail.charges[]    → 费用明细
  - price_detail.promotions[] → 优惠
  - guarantee_list[]          → 保险列表（level/title/day_amount/detail）

解读规则：
价格明细:
  - 逐条解读 charges[]，把 amount(分) 转成元显示
  - 有 promotions[] 时说明优惠项
  - 结尾必须说"以下单时为准"

保险推荐（guarantee_list 解读）:
  - level=1 required=true：必选基础保障，已含在总价
  - level=2/3 required=false：可选升级保障，按驾龄推荐
  - 推荐逻辑：
      * 驾龄 < 2 年 → 推荐最高等级（尊享）
      * 驾龄 2-5 年 → 推荐中档（优享）
      * 驾龄 > 5 年 → 按需，说明各档差异让用户自选
  - 重要：保障范围**只能**从 detail[] 字段里读，不允许自行发挥
  - 结尾必须说"保障范围以保险合同条款为准"
  - 若已调用过 get_order_details 且 history 里有 guarantee_list，直接解读，不要重复调用
```

### Step 2 — tyche tool 返回数据确认

`rental_get_order_details` 的 guarantee_list 字段由 tyche 负责返回。运行一次验证实际返回字段是否包含 `title / day_amount / detail`：

```bash
go run ./cmd/cli
# 选一辆车后问"价格明细"
# 看 logs/agent.log 里 [tyche] resp 的 guarantee_list 字段
```

若 `guarantee_list` 为空或 `title` 缺失，需和 tyche 同学确认接口是否按 `GuaranteeDetail` 结构返回了完整字段。

### Step 3 — 金额单位处理（在 prompt 里约定）

tyche 返回的金额可能是"分"（int），也可能是"元"（float，如 `daily_price`）。在 prompt 里明确：
- `price_detail.daily_price` / `price_detail.total` → 已是**元**，直接用
- `price_detail.charges[].amount` → 需确认单位（看实际返回值）
- `guarantee_list[].day_amount` → **分**，÷100 得元；`amount` → 也是分

在 prompt 里加一行："guarantee_list 中 day_amount 和 amount 单位是分，展示时除以 100 转换为元。"

### Step 4 — 用户画像槽位（prompt 引导，无需代码）

P2 不新增槽位抽取代码，用 prompt 引导：

```
用户画像收集（在对话中自然问）：
- 若用户未说驾龄 → 问"您的驾龄大概多少年？"（影响保险推荐）
- 若用户未说用途 → 可选问"是市区通勤还是长途出行？"（影响是否推荐停运险）
```

### Step 5 — 验收

跑通 2.1 demo。重点验证：
1. 一次 `get_order_details` 同时输出价格 + 保险
2. 用户先问价格、再问保险时，**不重复调用**工具（从 history 里的 tool message 读）
3. 保险等级和名称完全来自 `guarantee_list.title`，没有幻造

---

## 4. 文件清单（P2 增量）

```
internal/prompt/shopping_system.go    # 修改（扩写价格明细 + 保险解读规则）
docs/specs/phase2-price-detail-insurance.md  # 本文档（已重写）
```

**无新增 tool 文件。**

---

## 5. 关键决策

| # | 决策 | 理由 |
|---|---|---|
| P2-D1 | 复用 `rental_get_order_details`，零新增工具 | 一次调用同时返回价格明细 + 保险列表 |
| P2-D2 | 若 history 已有 `get_order_details` 结果，直接解读不重复调用 | 节省 token + 时延 |
| P2-D4 | 保险话术 100% 基于 `guarantee_list.detail[]`，禁止自由发挥 | 合规风险；prompt 强约束 |
| P2-D5 | 驾龄推荐逻辑写在 prompt，不做代码判断 | 简单规则；P3 supervisor 引入用户画像后再做槽位 |

---

## 6. 已识别 TODO（P2 内必清）

- [ ] 跑一次 `get_order_details` 确认 `guarantee_list[].title` / `day_amount` / `detail[]` 有真实数据
- [ ] 确认 `charges[].amount` 单位（分还是元），在 prompt 里写清楚
- [ ] 若 `guarantee_list` 在 tyche 实际返回中为空，需要 tyche 同学补充或找替代字段
- [ ] 沉淀 5 条评估 case：2 条"价格明细"+ 3 条"保险推荐"（含不同驾龄场景）
