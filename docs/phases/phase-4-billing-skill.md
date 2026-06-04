# Phase 4: 费用解读 Skill (Billing Explanation)

## 目标

实现订单费用解读与答疑能力，帮助用户理解账单明细、退款规则和费用构成。

## 前置条件

- Phase 3 保险推荐 Skill 已完成
- 订单/支付业务 API 文档已获取（或 Mock 定义已确认）

## 交付物清单

| # | 交付物 | 说明 |
|---|--------|------|
| 1 | Billing Tool 定义 | 查询订单明细、退款记录、计算退款 |
| 2 | Billing Skill 实现 | System Prompt + Tool 注册 |
| 3 | 费用分类与解读逻辑 | 各项费用的标准解读模板 |
| 4 | Tool 与业务 API 对接 | 调用订单/支付 API |
| 5 | 集成测试 | 端到端费用解读对话测试 |

## 详细步骤

### Step 4.1: 定义 Billing Tools

**文件**: `internal/tool/billing/order_detail.go`

```go
// GetOrderDetail 查询订单费用明细
// 参数:
//   - order_id: 订单ID（必填）
// 返回: 订单费用明细（租金、手续费、保险费、押金、增值服务费等）
```

**文件**: `internal/tool/billing/refund_record.go`

```go
// GetRefundRecords 查询退款记录
// 参数:
//   - order_id: 订单ID（必填）
// 返回: 退款记录列表（退款项目、金额、状态、预计到账时间）
```

**文件**: `internal/tool/billing/refund_calc.go`

```go
// CalculateRefund 计算预期退款
// 参数:
//   - order_id: 订单ID（必填）
//   - scenario: 退款场景：early_return / cancellation / insurance_cancel（必填）
// 返回: 预期退款明细
```

**文件**: `internal/tool/billing/fee_rule.go`

```go
// GetFeeRule 查询费用规则
// 参数:
//   - fee_type: 费用类型：rental / deposit / service_fee / insurance（必填）
// 返回: 该费用的收取规则和退费规则
```

### Step 4.2: 费用数据模型

```
OrderFeeDetail {
    OrderID      string
    TotalAmount  string        // 订单总金额
    Fees         []FeeItem
}

FeeItem {
    Type         string        // rental / deposit / service_fee / insurance / addon
    Name         string        // 费用名称（展示用）
    Amount       string        // 金额
    Description  string        // 费用说明
    Refundable   bool          // 是否可退
    RefundRule   string        // 退费规则简述
}

RefundRecord {
    RefundID     string
    Type         string        // 退款类型
    Amount       string
    Status       string        // processing / completed / failed
    Reason       string
    ExpectedTime string        // 预计到账时间
}

RefundCalculation {
    Scenario     string        // 退款场景
    RefundAmount string        // 可退金额
    Deductions   []DeductionItem  // 扣款明细
    TotalRefund  string
}

DeductionItem {
    Name   string
    Amount string
    Reason string
}
```

### Step 4.3: Billing Skill System Prompt

**文件**: `internal/skill/billing/prompt.go`

```
你是租车平台的费用解读顾问。你的任务是帮助用户理解订单中的各项费用明细和退款规则。

## 你的工作方式
1. 根据订单号查询费用明细
2. 逐项解释各项费用的含义和计算方式
3. 说明哪些费用可退、哪些不可退
4. 如果用户对退款有疑问，查询退款记录或计算预期退款

## 解读原则
- 用通俗易懂的语言解释费用，避免专业术语
- 金额标注清晰，区分"已支付""待支付""可退还"
- 退款规则要讲清楚：什么情况可以退、退多少、多久到账
- 如果用户觉得费用不合理，客观解释计算依据
- 不隐瞒任何费用，透明展示所有明细

## 常见问题处理
- "为什么有手续费？" → 解释平台服务费包含的内容
- "押金什么时候退？" → 说明押金退还条件和时间
- "提前还车能退多少？" → 调用退款计算 Tool
- "保险费能退吗？" → 区分未取车和已取车的退保规则
- "这个增值服务费是什么？" → 查询具体增值服务项目

## 可用工具
- get_order_detail: 查询订单费用明细
- get_refund_records: 查询退款记录
- calculate_refund: 计算预期退款
- get_fee_rule: 查询费用规则
```

### Step 4.4: 费用解读模板

对每类费用，预定义解读模板，Tool 返回数据后填充模板：

```
【租金】¥560.00（¥140/天 × 4天）
  这是你租用丰田卡罗拉的租金，按日租金乘以租期天数计算。

【基础服务费】¥40.00
  包含车辆清洁费、24小时道路救援服务。

【尊享险】¥120.00（¥30/天 × 4天）
  你购买的尊享险保费，保障期间内的车损可零免赔理赔。

【押金】¥2,000.00（可退还）
  车辆押金，还车后无车损将在3-5个工作日内原路退还。
```

### Step 4.5: 集成测试场景

1. **费用总览**：提供订单号 → 展示所有费用明细和解读
2. **单项追问**：问"押金什么时候退" → 详细说明退还规则
3. **退款计算**：问"提前还车能退多少" → 调用计算 Tool
4. **退款记录**：问"我的退款到账了吗" → 查询退款状态
5. **费用质疑**：问"为什么收这个服务费" → 解释费用构成
6. **缺少订单号**：问"帮我看看费用" → 提示提供订单号
7. **保险退保**：问"保险费能退吗" → 区分情况说明

## 验收标准

- [ ] 提供订单号可获取费用明细解读
- [ ] 各项费用解释通俗易懂
- [ ] 退款计算结果准确
- [ ] 退款记录可查询
- [ ] 缺少订单号时主动询问
- [ ] 费用质疑时客观解释
