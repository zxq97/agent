# Phase 3: 保险推荐 Skill (Insurance Recommendation)

## 目标

实现保险推荐与介绍能力，用户选中车辆报价后可获得保险方案推荐，了解各保险产品的保障范围和理赔规则。

## 前置条件

- Phase 2 车辆推荐 Skill 已完成
- 保险业务 API 文档已获取（或 Mock 定义已确认）

## 交付物清单

| # | 交付物 | 说明 |
|---|--------|------|
| 1 | Insurance Tool 定义 | 查询保险方案、计算保费、获取保障详情 |
| 2 | Insurance Skill 实现 | System Prompt + Tool 注册 |
| 3 | 跨 Skill 上下文传递 | 从车辆推荐自然过渡到保险推荐 |
| 4 | Tool 与业务 API 对接 | 调用保险 API 获取数据 |
| 5 | 集成测试 | 端到端保险推荐对话测试 |

## 详细步骤

### Step 3.1: 定义 Insurance Tools

**文件**: `internal/tool/insurance/plans.go`

```go
// GetInsurancePlans 查询保险方案
// 参数:
//   - quote_id: 报价ID（必填，从车辆推荐上下文获取）
// 返回: 可选保险方案列表（基础险/尊享险/补充险）
```

**文件**: `internal/tool/insurance/premium.go`

```go
// CalculatePremium 计算保费
// 参数:
//   - quote_id: 报价ID（必填）
//   - plan_ids: 保险方案ID列表（必填）
//   - rental_days: 租期天数（必填）
// 返回: 各方案保费明细
```

**文件**: `internal/tool/insurance/coverage.go`

```go
// GetCoverageDetail 获取保障详情
// 参数:
//   - plan_id: 保险方案ID（必填）
// 返回: 保障范围、免赔额、理赔流程说明
```

### Step 3.2: 保险数据模型

```
InsurancePlan {
    PlanID          string   // 方案ID
    Name            string   // 基础险 / 尊享险 / 补充险（玻璃/轮胎）
    DailyPrice      float64  // 每日保费
    Coverage        []string // 保障范围列表
    Deductible      string   // 免赔额描述
    ClaimProcess    string   // 理赔流程简述
    Recommended     bool     // 是否推荐
    RecommendReason string   // 推荐理由
}
```

### Step 3.3: Insurance Skill System Prompt

**文件**: `internal/skill/insurance/prompt.go`

```
你是租车平台的保险顾问。你的任务是在用户选好车辆后，帮助用户选择最合适的保险方案。

## 你的工作方式
1. 了解用户已选的车辆和报价
2. 展示可选的保险方案和保费
3. 解释各方案的保障范围和差异
4. 根据用户需求给出推荐

## 推荐原则
- 默认推荐尊享险（覆盖面广，适合大多数用户）
- 短途/熟悉路线用户，可以推荐基础险 + 补充险
- 长途自驾/不熟悉路线，强烈推荐尊享险
- 始终说明不买保险的风险（全额承担车损）

## 交互规范
- 先展示方案概览，再展开细节
- 用表格对比不同方案的关键差异
- 解释免赔额时用具体数字举例
- 主动说明"最坏情况"下的费用差异
- 不要过度推销，让用户自主决策

## 可用工具
- get_insurance_plans: 查询保险方案
- calculate_premium: 计算保费
- get_coverage_detail: 获取保障详情
```

### Step 3.4: 跨 Skill 上下文传递

用户从车辆推荐自然过渡到保险咨询：

```
用户: "我想在北京租一辆SUV"
Agent: [调用 search_vehicles] "为您推荐以下车型..."
用户: "第二款汉兰达不错"
Agent: [记录 vehicle_id + quote_id] "汉兰达是个好选择！需要为您推荐保险方案吗？"
用户: "有什么保险"
Agent: [调用 get_insurance_plans] "以下是可选的保险方案..."
```

关键实现：
- Session 的 Slots 中保存 `vehicle_id` 和 `quote_id`
- 意图检测识别到"保险"关键词时，自动从 Slots 获取上下文
- 如果 Slots 中缺少必要信息，主动询问

### Step 3.5: Tool 与业务 API 对接

Mock 数据需要覆盖：
- 基础险方案（低保费、高免赔、有限保障）
- 尊享险方案（中保费、零免赔、全面保障）
- 补充险方案（玻璃/轮胎单独损坏）
- 不同车型对应的保费差异

### Step 3.6: 集成测试场景

1. **基本推荐**：选车后问保险 → 展示方案
2. **保费计算**：问具体保费 → 调用计算 Tool
3. **保障详情**：问尊享险保什么 → 展示保障范围
4. **方案对比**：基础险和尊享险区别 → 对比表格
5. **推荐建议**：长途自驾 → 推荐尊享险并说明理由
6. **缺少上下文**：直接问保险但没选车 → 提示先选车
7. **跨 Skill 追问**：保险讨论后问"换个车型呢" → 切回车辆 Skill

## 验收标准

- [ ] 选车后可获取保险推荐
- [ ] 保险数据来自 Tool 调用
- [ ] 跨 Skill 上下文正确传递
- [ ] 保费计算准确
- [ ] 保障范围解释清晰
- [ ] 缺少上下文时主动询问
