# Phase 3: 保险推荐 Agent (Insurance Recommendation)

## 目标

实现 InsuranceAgent，在用户选中车辆后推荐适配的保险方案，解释各保险产品的保障范围、免赔额和理赔流程。

## 前置条件

- Phase 1 基础骨架已完成
- Phase 2 VehicleAgent 可正常推荐车型（保险推荐依赖车辆上下文）

## 交付物

| # | 交付物 | 说明 |
|---|--------|------|
| 1 | Insurance Agent 完善 | System Prompt + 对话策略 |
| 2 | 保险知识库 | 保险产品、保障范围、理赔流程 |
| 3 | 与 Vehicle Agent 上下文联动 | 选车后自动进入保险推荐 |
| 4 | CLI 交互测试 | 端到端保险推荐体验 |

---

## Step 2.1: Insurance Agent System Prompt

```
你是租车平台的保险顾问。你的职责是帮助用户选择最合适的保险方案。

工作流程：
1. 了解车辆信息：从上下文中获取用户已选的车型和报价
2. 介绍保险方案：说明可选的保险产品及其保障范围
3. 风险评估：根据用户行程（长途/短途、城市/高速）给出保险建议
4. 计算保费：基于车型和行程计算各保险方案的总费用

约束：
- 保险产品信息必须来自知识库，不得编造保障范围和免赔额
- 保险推荐应客观中立，不得强制搭售
- 如工具可查保费，以工具返回为准；否则标注为"参考价"
- 未在保障范围内的情况应明确告知
```

## Step 2.2: 保险知识库

**目录**: `knowledge/insurance/`

### knowledge/insurance/products.json
```json
[
  {
    "id": "basic_insurance",
    "name": "基础保障险",
    "description": "覆盖车辆碰撞和第三方责任",
    "coverage": ["车辆碰撞损失", "第三方人身伤害", "第三方财产损失"],
    "exclusions": ["轮胎单独损坏", "玻璃单独破碎", "水淹车"],
    "deductible": "1500元",
    "claim_process": "报案 → 门店定损 → 保险公司理赔"
  },
  {
    "id": "premium_insurance",
    "name": "尊享保障险",
    "description": "零免赔额，全面保障",
    "coverage": ["车辆碰撞损失", "第三方人身伤害", "第三方财产损失", "轮胎损坏", "玻璃破碎", "水淹车", "盗抢"],
    "exclusions": ["酒驾/毒驾", "无证驾驶", "故意损坏"],
    "deductible": "0元",
    "claim_process": "报案 → 门店确认 → 全额赔付"
  },
  {
    "id": "supplementary_tire",
    "name": "轮胎损失险",
    "description": "基础险的补充，覆盖轮胎单独损坏",
    "coverage": ["轮胎扎钉", "轮胎爆裂", "轮毂刮擦"],
    "exclusions": ["故意损坏轮胎"],
    "deductible": "0元",
    "claim_process": "报案 → 门店确认 → 更换或赔付"
  }
]
```

### knowledge/insurance/faq.md
```markdown
## 常见问题

### Q: 基础险和尊享险怎么选？
A: 短途城市出行基础险足够；长途自驾、路况复杂建议尊享险，零免赔更安心。

### Q: 出了事故怎么理赔？
A: 第一时间拨打平台客服电话报案，保留现场照片，然后到门店定损理赔。

### Q: 不买保险可以吗？
A: 可以，但发生事故需全额承担车辆维修费用，建议至少购买基础保障险。
```

## Step 2.3: 与 Vehicle Agent 上下文联动

保险推荐通常发生在选车之后，需要：

1. **Orchestrator 上下文传递**：当 VehicleAgent 返回车型推荐结果后，Orchestrator 将车型信息作为上下文传给 InsuranceAgent
2. **Session 状态标记**：在 Session 中记录"已选车型"，InsuranceAgent 可读取
3. **主动推荐触发**：当用户确认选车后，Orchestrator 可主动建议"是否需要了解保险方案"

## Step 2.4: 端到端测试场景

1. "我选了那辆朗逸，需要买什么保险" → 推荐保险方案
2. "基础险和尊享险有什么区别" → 对比保障范围
3. "出事故了怎么理赔" → 解释理赔流程
4. "不买保险行不行" → 说明风险
5. "轮胎爆了赔吗" → 查保障范围
