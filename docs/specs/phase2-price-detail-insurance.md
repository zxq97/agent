# Phase 2: 价格明细 + 保险推荐 (CLI)

> **目标:** 在 P1 单 ReAct agent 基础上,新增 `get_price_detail / list_insurances` 两个 tool,让 agent 能"讲清价格"和"给出保险组合建议"。仍在 CLI 验证。

---

## 1. Context

P1 完成后,agent 可以"列出有哪些车 + 多少钱"。但用户的下一句话往往是:
- "这个价怎么算出来的?" → 需要 **价格明细**
- "我要不要加全险?" → 需要 **保险推荐**

P2 解决这两类追问,**不动 agent 架构**(仍是单 ReAct agent),只加 tool + 扩 prompt + 引入用户画像槽位。

---

## 2. 验收标准

### 2.1 必过 demo
```
用户: (沿用 P1 对话,已选定大众朗逸)
       这个 276 块怎么算出来的?
agent: [调用 get_price_detail(quote_id=...)]
       这笔费用拆解如下:
        - 日租金 138 元 × 2 天 = 276 元
        - 基础保险已含(¥30/天的盗抢险 + 第三者险)
        - 当前无优惠
       夜间还车有 30 元服务费(您选的是晚 8 点,在白天范围内,免)
       最终价格以下单时为准。

用户: 我开车 3 年,需要加保险吗?
agent: [调用 list_insurances(quote_id=...)]
       为您选了 3 款常见保险:
        1. 车损无忧 ¥38/天:涵盖车损 / 自燃,免赔额 0
        2. 第三者升额 ¥18/天:三者从 30 万升至 100 万
        3. 不计免赔 ¥15/天:小磕碰也免赔
       推荐组合:1+3(共 ¥53/天),原因:您驾龄 3 年属于较安全段,
       但市区出行刮蹭概率仍在,组合可覆盖 95% 常见场景且性价比最高。
       (保障范围以保险合同条款为准)
```

### 2.2 检查清单
- [ ] `get_price_detail` 输入只需 `quote_id`,输出含费用项数组(name/amount/note)
- [ ] `list_insurances` 输出 `保险卡片[]`,每张含 名称 / 价格(¥/天) / 保障范围摘要 / 免赔额 / 推荐标记
- [ ] agent 在用户描述驾龄/出行场景时,**自动更新槽位**(`DriverAge / Usage / Passenger`)
- [ ] 保险推荐**必须**基于 `list_insurances` 真实返回,不允许 LLM 自由发挥保障范围
- [ ] 答复末尾的"以下单时为准 / 以合同条款为准"必须出现(prompt 强约束)

---

## 3. 分步实现

### Step 1 — `get_price_detail` tool
**文件:** `internal/tools/get_price_detail.go`

**入参:**
```go
type GetPriceDetailInput struct {
    QuoteID  string `json:"quote_id"  jsonschema:"description=报价 ID,来自 list_quotes 返回的 quote_id"`
    Source   int    `json:"source"    jsonschema:"description=渠道,默认 0"`
}
```

**出参:**
```go
type PriceDetailLine struct {
    Name     string  `json:"name"`              // "日租金" / "夜间还车费" / "基础保险"
    Quantity string  `json:"quantity,omitempty"` // "138 元 × 2 天"
    Amount   float64 `json:"amount"`            // 元
    Note     string  `json:"note,omitempty"`
}
type GetPriceDetailOutput struct {
    QuoteID     string             `json:"quote_id"`
    VehicleName string             `json:"vehicle_name"`
    Lines       []PriceDetailLine  `json:"lines"`
    TotalYuan   float64            `json:"total_yuan"`
    Note        string             `json:"note,omitempty"`
}
```

**后端接口:** `POST /ota/rental/sapi/inner/price/detail`(body: `{source, reference_id}`)
- `reference_id` = 入参 `quote_id`
- 真实响应解析待后端联调,先用 `mockPriceDetail` 兜底

### Step 2 — `list_insurances` tool
**文件:** `internal/tools/list_insurances.go`

**入参:**
```go
type ListInsurancesInput struct {
    QuoteID string `json:"quote_id"  jsonschema:"description=报价 ID 或订单 ID(可选,留空走通用列表)"`
}
```

**出参:**
```go
type InsuranceCard struct {
    Code         string  `json:"code"`
    Name         string  `json:"name"`                  // "车损无忧"
    PriceYuanDay float64 `json:"price_yuan_day"`        // ¥/天
    PriceTotal   float64 `json:"price_total,omitempty"` // 全程总价
    Coverage     []string `json:"coverage"`             // 保障范围条目
    Deductible   string  `json:"deductible,omitempty"`  // "免赔额 0"
    Highlight    string  `json:"highlight,omitempty"`   // 一句话亮点,展示用
}
type ListInsurancesOutput struct {
    Insurances []InsuranceCard `json:"insurances"`
    Note       string          `json:"note,omitempty"` // 固定:"保障范围以保险合同条款为准"
}
```

**后端接口:** `POST /ota/rental/sapi/inner/insurance/list`(对应 `GetAddInsurancePriceList`)
- 入参先用最小集试调,真实 req 结构待对齐

### Step 3 — 扩 prompt
**文件:** `internal/prompt/shopping_system.go`(扩写)

新增段落:
```
用户选定某条报价后:
- 如果用户问"价格怎么算" / "为什么这个价" → 调 get_price_detail
- 如果用户问"要不要保险" / "保险有哪些" / "推荐什么险" → 调 list_insurances
- 保险推荐组合逻辑:
  * 询问驾龄(若未知)
  * 驾龄 < 2 年:推荐全险组合
  * 驾龄 2-5 年:推荐"车损 + 不计免赔"
  * 驾龄 > 5 年:可选最简组合,提示"按需选择"
  * 任何推荐**必须**说"保障范围以保险合同条款为准"
```

### Step 4 — 更新 `tools.All`
**文件:** `internal/tools/common.go`

把新 2 个 tool 加进去:
```go
func All(d *Deps) ([]tool.BaseTool, error) {
    q, _ := NewListQuotesTool(d)
    s, _ := NewListStoresTool(d)
    v, _ := NewListVehiclesTool(d)
    pd, _ := NewGetPriceDetailTool(d)   // P2 新增
    ins, _ := NewListInsurancesTool(d)  // P2 新增
    return []tool.BaseTool{q, s, v, pd, ins}, nil
}
```

### Step 5 — 槽位推导
**文件:** `internal/orchestration/state.go` 或新增 `internal/orchestration/slot_extract.go`

引入辅助函数(可选):
- 从用户自然语言抽取 `DriverAge / Passenger / Usage` 写回 `QuoteSlot`
- P2 可以"先用 LLM 自动用 prompt 引导"路径,槽位提取放后端是 P3 supervisor 才需要

### Step 6 — 验收
新增 demo 脚本 `scripts/demo_p2.sh`,跑通 2.1 demo。

---

## 4. 文件清单(P2 增量)

```
internal/tools/get_price_detail.go         # 新增
internal/tools/get_price_detail_mock.go    # 新增
internal/tools/list_insurances.go          # 新增
internal/tools/list_insurances_mock.go     # 新增
internal/tools/common.go                   # 修改(加 2 个 tool)
internal/prompt/shopping_system.go         # 修改(扩 prompt)
docs/specs/phase2-price-detail-insurance.md  # 本文档
```

---

## 5. 关键决策

| # | 决策 | 理由 |
|---|---|---|
| P2-D1 | 保险话术由 prompt 强约束 + tool 真实数据兜底 | 合规风险高,不允许 LLM 自由发挥 |
| P2-D2 | 不在 agent 内显式 phase 切换 | 单 ReAct agent 由 LLM 自主决策何时调哪个 tool;P3 才引入 supervisor |
| P2-D3 | 槽位推导先用 prompt 引导,不上独立抽取层 | 简单 case;复杂 case 留到 P3 supervisor |

---

## 6. 已识别 TODO(P2 内必清)

- [ ] `get_price_detail` mock 与真实响应映射
- [ ] `list_insurances` mock 与真实响应映射
- [ ] prompt 中"以合同条款为准"的强制注入(可在 tool 输出层固定加 note 字段)
- [ ] 缺一个评估集:5 条"价格明细 + 保险推荐"问题
