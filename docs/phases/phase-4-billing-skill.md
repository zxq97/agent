# Phase 4: 费用解读 Agent (Billing Explanation)

## 目标

实现 BillingAgent，解读订单中各项费用明细（租金、手续费、保险费、押金等），解释退款规则和退款明细。

## 前置条件

- Phase 1 基础骨架已完成
- tyche MCP 的 `rental_get_order_details`、`rental_get_reservation` 工具可用

## 交付物

| # | 交付物 | 说明 |
|---|--------|------|
| 1 | Billing Agent 完善 | System Prompt + 对话策略 |
| 2 | 费用相关 MCP Tool 精细封装 | 订单详情、退款规则 |
| 3 | 费用知识库 | 费用结构说明、退款政策 |
| 4 | CLI 交互测试 | 端到端费用解读体验 |

---

## Step 4.1: Billing Agent System Prompt

```
你是租车平台的费用顾问。你的职责是帮助用户理解订单中的各项费用。

工作流程：
1. 查询订单：根据用户提供的订单号或手机号，调用工具查询订单详情
2. 费用拆解：逐项解释租金、手续费、保险费、押金、增值服务费等
3. 退款解读：解释退款规则、退款金额计算方式
4. 费用对比：如有多个订单，对比费用差异

约束：
- 所有费用数据必须来自 MCP 工具查询，不得编造金额
- 退款规则以知识库为准，不得凭记忆回答
- 如果订单查询失败，如实告知原因
- 押金冻结/解冻金额以工具返回为准
```

## Step 4.2: 费用相关 MCP Tool 封装

### Tool: get_order_details
- 底层调用: `rental_get_order_details`
- 语义化参数: `order_id` 或 `reference_id`
- 输出格式化: 费用明细列表 + 退改政策

### Tool: get_reservation_status
- 底层调用: `rental_get_reservation`
- 语义化参数: `order_id`
- 输出格式化: 订单状态 + 支付状态 + 押金状态

## Step 4.3: 费用知识库

**目录**: `knowledge/billing/`

### knowledge/billing/fee_structure.json
```json
[
  {
    "fee_type": "rental_fee",
    "name": "车辆租金",
    "description": "按日计算的车辆使用费",
    "calculation": "日租金 × 租期天数",
    "refundable": true,
    "refund_rule": "提前还车按实际使用天数计算，多出部分退还"
  },
  {
    "fee_type": "service_fee",
    "name": "手续费",
    "description": "平台服务费，包含订单处理和客服支持",
    "calculation": "固定费用",
    "refundable": false,
    "refund_rule": "手续费不退还"
  },
  {
    "fee_type": "insurance_fee",
    "name": "保险费",
    "description": "车辆保险费用",
    "calculation": "按天计费，取决于保险方案",
    "refundable": true,
    "refund_rule": "未使用的保险天数可退保"
  },
  {
    "fee_type": "deposit",
    "name": "押金/预授权",
    "description": "车辆安全押金，还车后解冻",
    "calculation": "根据车型和保险方案确定",
    "refundable": true,
    "refund_rule": "还车后 7-15 个工作日自动解冻，如有违章扣除后退还剩余"
  }
]
```

### knowledge/billing/refund_policy.md
```markdown
## 退款政策

### 提前还车
- 租金：按实际使用天数计算，退还多余部分
- 保险费：未使用天数可退保
- 手续费：不退还

### 取消订单
- 取车前 48 小时以上：全额退款
- 取车前 24-48 小时：扣除 1 天租金
- 取车前 24 小时内：扣除 2 天租金
- 取车后：不支持取消

### 违章押金
- 还车后冻结 30 天作为违章观察期
- 如有违章，扣除违章金额后退还剩余
- 观察期结束后无违章，全额解冻
```

## Step 4.4: 端到端测试场景

1. "帮我查一下订单 ORD123456 的费用明细" → 调用 MCP 查询 + 逐项解释
2. "这笔押金什么时候退" → 解释押金解冻规则
3. "我想提前还车，能退多少钱" → 解释提前还车退款规则
4. "保险费能退吗" → 解释保险退保规则
5. "为什么收了手续费" → 解释手续费用途
