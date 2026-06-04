# Phase 5: 履约支持 Skill (Fulfillment Support)

## 目标

实现履约过程中的规则解答与问题支持，帮助用户理解取还车流程、违章处理、续租换车等规则。

## 前置条件

- Phase 4 费用解读 Skill 已完成
- 履约业务 API 文档已获取（或 Mock 定义已确认）

## 交付物清单

| # | 交付物 | 说明 |
|---|--------|------|
| 1 | Fulfillment Tool 定义 | 取还车规则、违章规则、续租规则等 |
| 2 | Fulfillment Skill 实现 | System Prompt + Tool 注册 |
| 3 | 规则结构化表达 | 将复杂的业务规则转为 LLM 可理解的结构 |
| 4 | 紧急情况指引 | 事故/故障处理流程 |
| 5 | Tool 与业务 API 对接 | 调用履约 API |
| 6 | 集成测试 | 端到端履约支持对话测试 |

## 详细步骤

### Step 5.1: 定义 Fulfillment Tools

**文件**: `internal/tool/fulfillment/pickup_rule.go`

```go
// GetPickupRules 查询取车规则
// 参数:
//   - order_id: 订单ID（可选，有则返回该订单的具体取车信息）
//   - city: 城市（可选，无订单时查询通用规则）
// 返回: 取车时间、地点、证件要求、验车流程
```

**文件**: `internal/tool/fulfillment/return_rule.go`

```go
// GetReturnRules 查询还车规则
// 参数:
//   - order_id: 订单ID（可选）
// 返回: 还车时间/地点要求、油量要求、超时计费规则、异地还车规则
```

**文件**: `internal/tool/fulfillment/violation_rule.go`

```go
// GetViolationRules 查询违章处理规则
// 参数:
//   - order_id: 订单ID（可选）
// 返回: 违章查询方式、处理流程、费用承担、押金扣除规则
```

**文件**: `internal/tool/fulfillment/extension_rule.go`

```go
// GetExtensionRules 查询续租规则
// 参数:
//   - order_id: 订单ID（可选）
// 返回: 续租申请方式、费用计算、最短/最长续租期限
```

**文件**: `internal/tool/fulfillment/accident_guide.go`

```go
// GetAccidentGuide 获取事故/故障处理指引
// 参数:
//   - scenario: 场景类型：accident / breakdown / tire_damage（必填）
// 返回: 紧急处理步骤、联系电话、理赔指引
```

### Step 5.2: 履约规则数据模型

```
PickupInfo {
    OrderID       string
    PickupTime    string
    PickupLocation string
    RequiredDocs  []string    // 身份证、驾驶证、信用卡等
    PickupProcess []Step      // 取车流程步骤
    Notes         []string    // 注意事项
}

ReturnInfo {
    OrderID        string
    ReturnTime     string
    ReturnLocation string
    FuelRequirement string     // 满油取还 / 按实际计费
    OvertimeRule   string     // 超时计费规则
    DifferentLocationRule string // 异地还车规则和费用
}

ViolationInfo {
    QueryMethod   string     // 违章查询方式
    ProcessFlow   []Step     // 处理流程
    FeeResponsibility string // 费用承担说明
    DepositDeduction string // 押金扣除规则
    TimeLimit      string    // 处理时限
}

ExtensionInfo {
    ApplyMethod   string     // 续租申请方式
    FeeCalcMethod string     // 费用计算方式
    MinDays       int        // 最短续租天数
    MaxDays       int        // 最长续租天数
    Notice        string     // 注意事项
}

AccidentGuide {
    Scenario      string
    EmergencySteps []Step    // 紧急处理步骤
    Hotline       string     // 24小时救援电话
    ClaimSteps    []Step     // 理赔步骤
    InsuranceTip  string     // 保险相关提示
}
```

### Step 5.3: Fulfillment Skill System Prompt

**文件**: `internal/skill/fulfillment/prompt.go`

```
你是租车平台的履约支持顾问。你的任务是帮助用户理解租车过程中的各种规则，并解答履约中遇到的问题。

## 你的工作方式
1. 理解用户当前遇到的问题或想了解的规则
2. 查询具体的规则信息或处理流程
3. 用清晰易懂的方式解释给用户
4. 如果涉及紧急情况，优先给出关键步骤和联系电话

## 解答原则
- 规则解释要准确，不要模糊表述
- 流程说明要分步骤，1-2-3 清晰列出
- 紧急情况（事故、故障）优先给出救援电话
- 费用相关规则要给出具体数字和计算方式
- 区分"必须"和"建议"的不同语气
- 如果用户的问题涉及多个规则，主动补充关联信息

## 常见问题处理
- "取车需要带什么" → 列出必需证件和可选物品
- "还车可以异地吗" → 说明异地还车规则和额外费用
- "违章了怎么处理" → 详细说明违章处理全流程
- "想多租几天怎么操作" → 说明续租申请和费用
- "出了事故怎么办" → 紧急步骤 + 救援电话 + 理赔指引
- "车坏了怎么办" → 故障报告流程 + 替换车辆安排

## 可用工具
- get_pickup_rules: 查询取车规则
- get_return_rules: 查询还车规则
- get_violation_rules: 查询违章处理规则
- get_extension_rules: 查询续租规则
- get_accident_guide: 获取事故/故障处理指引
```

### Step 5.4: 紧急情况优先策略

当检测到用户处于紧急情况时（事故、故障），改变响应策略：

1. **跳过寒暄**，直接给出关键步骤
2. **突出救援电话**，放在回复最前面
3. **简化步骤**，只列最关键的 3-5 步
4. **主动追问**确认人员安全和车辆状态

### Step 5.5: 集成测试场景

1. **取车规则**："取车需要带什么" → 列出证件和流程
2. **还车规则**："还车超时怎么算" → 说明超时计费规则
3. **异地还车**："可以在上海还车吗" → 异地还车规则和费用
4. **违章处理**："违章了怎么处理" → 完整处理流程
5. **续租**："想多租两天" → 续租方式和费用
6. **事故处理**："出了事故怎么办" → 紧急步骤 + 救援电话
7. **故障报告**："车启动不了" → 故障报告流程
8. **复合问题**："异地还车 + 违章" → 补充关联信息

## 验收标准

- [ ] 取还车规则查询和解释正确
- [ ] 违章处理流程说明完整
- [ ] 续租规则和费用解释清晰
- [ ] 紧急情况优先给出救援电话和关键步骤
- [ ] 异地还车规则和额外费用说明准确
- [ ] 复合问题能补充关联信息
