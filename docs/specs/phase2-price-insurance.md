# Phase 2 — 价格明细 + 保险推荐 + 车型对比

> 隶属 [技术方案总纲](../technical-plan.md)。**可独立执行。**
>
> **工时:** 4-5 天 · **PR 数:** 3 · **前置依赖:** [P1](phase1-shopping-mvp.md) 已合入

---

## 1. 目标

用户选定某辆车后,能"讲清价格""给保险建议",以及**用户纠结选哪辆时做车型对比**。三块能力数据都来自 `rental_get_order_details`(对比是并发多次调用),无需新增 tyche 工具。把 P1 里占位的 `get_price_detail` / `insurance` / `compare_vehicles` 三个 Capability 落地——它们同源(都基于 get_order_details + ResolveQuoteRef),放一个 Phase 一起做最省。

---

## 2. PR 2.1 — PriceDetailCapability

`internal/agent/cap_price_detail.go`:
```
① ResolveQuoteRef(state, args.vehicle_ref) → reference_id
     多义 → 返回澄清反问(LLM 不猜);0 命中或报价过期 → 触发重搜引导
② Go 调 rental_get_order_details(context_id/reference_id/supplier 全从 state 注入)
③ LLM #2 据返回字段讲解:
     charges[] 逐项(name + amount 元),合计核对 total
     promotions[] 优惠扣减
     结尾必须"以下单时为准"
```
prompt 模板:按"日均 / 总价 / 优惠 / 明细项"结构讲清,口语化。

**验收**:"看第一辆明细" → 费用拆项 + 合计 + "以下单为准";过期报价自动重搜。

---

## 3. PR 2.2 — InsuranceCapability

`internal/agent/cap_insurance.go`:
```
① ResolveQuoteRef
② rental_get_order_details 取 charges 里 Type=3 的保险费金额
③ LLM #2 据保险费 + 驾龄 给"基础/优享/尊享要不要加"建议
```

> **已知缺口(总纲 §5)**:tyche MCP 不透出 `grantee_list`,拿不到保障范围细节(cover_glass/tpl_coverage 等),只能拿到保险费金额。**本期用兜底文案**:讲清"有哪几档、各档多少钱/天",保障范围一句"具体保障内容请在 App 内查看"。后续推动 tyche MCP 透出 `RentalGuarantee` 或直连 saas-api 时再补全。

按驾龄推荐逻辑(driver_age 从 state / 追问获得):
- 驾龄未知 → 先问
- <2 年 → 推最高档
- 2-5 年 → 推中档
- >5 年 → 介绍差异让用户自选

prompt 红线:数据 100% 来自工具,不自由发挥保障范围;结尾"保障范围以保险合同条款为准";不承诺绝对化用词。

**验收**:"那辆要加全险吗" → 基于真实保险费给建议 + 兜底文案 + 合同为准提示;驾龄未知先追问。

---

## 4. PR 2.3 — CompareCapability(车型对比)

**为什么需要**:用户挑车时高频出现"朗逸和轩逸哪个适合家庭""选 A 还是 B"。没有这个能力,对比意图会被 LLM 误塞进 `search_vehicles`(变重搜、丢对比意图)或 `get_price_detail`(只收一辆),甚至走纯回复让 LLM **脑补对比**(违反"数据来自工具"红线)。

**本质**:这是 `get_price_detail` 的"并行多辆"版 —— 印证总纲选型结论"多车对比用并行 tool_calls + 一次综合,不需要 planner / ReAct"。

`internal/agent/cap_compare.go`:
```
① 对 args.vehicle_refs(2-3 个)逐个 ResolveQuoteRef(state, ref)
     任一多义 → 返回澄清反问(LLM 不猜是哪辆)
     任一 0 命中 / 报价过期 → 引导"先帮你搜一下"(不硬对比缺失的车)
② Go 并发调多次 rental_get_order_details(每辆的 context_id/reference_id/supplier 全从 state 注入)
③ LLM #2 据多辆真实数据生成对比话术:
     - 维度:价格(日均/总价)、座位、能源、空间、适合场景
     - 结合 state.Profile(家庭/商务/价格敏感度)给"更适合你的是哪辆"倾向
     - 结尾"以下单时为准"
```

prompt 红线(`internal/prompt/compare_system.go`):
- **只对比工具返回的真实字段**,缺的维度不脑补("两辆油耗我这没有准确数据,建议 App 内看")
- 不贬低任一车型,客观陈述差异
- 给倾向建议但不替用户拍板("看你更看重空间还是价格")
- 不承诺绝对化用词

**两条触发路径**(统一收口到本 Capability):
- **自然语言**:"朗逸和轩逸哪个好" → decide 选 `compare_vehicles` → 本 Capability
- **引导胶囊点击**:搜车后点"对比朗逸和轩逸" → P5 的 PreRoute 短路直接构造 compare Decision → 本 Capability(P5 落地短路逻辑,本期先支持自然语言路径)

**验收**:
- "朗逸和轩逸哪个适合家庭" → 并发取两辆真实明细 → 对比话术 + 倾向建议 + "以下单为准"
- 其中一辆指代不到 → 引导先搜,不硬对比
- 缺失维度不脑补;不贬低
- ID 全程 Go 注入

---

## 5. 验收(整体)

- [x] 价格明细:逐项费用 + 合计核对 + 优惠 + "以下单为准"
- [x] 保险:基于真实 charges 保险费,按驾龄给建议,保障范围走兜底文案
- [x] 对比:并发取 2-3 辆真实明细,客观对比 + 结合 profile 给倾向,缺失维度不脑补
- [x] 指代解析复用 P1 的 ResolveQuoteRef(多义反问、过期重搜)
- [x] ID 全程 Go 注入,不经 LLM
- [x] `go test ./...` 全绿
- [ ] `go test ./eval/... -run TestEval/decide/compare` 通过(eval 回归集尚未落地)

---

## 6. 风险

| 风险 | 应对 |
|---|---|
| 保险保障细节缺失被用户追问 | 兜底文案 + 引导 App;在总纲缺口登记,推动 MCP 透出 |
| charges 里 Type 含义变化 | 解析做防御,Type 不识别时归"其他费用",不臆测 |
| 对比时某辆指代不到 / 报价过期 | 不硬对比,引导"先帮你搜下这辆";部分命中可只对比命中的并说明 |
| 对比维度缺数据被脑补 | prompt 强约束只用工具字段;缺维度如实说"建议 App 内看" |
| 并发调 get_order_details 慢/部分失败 | 并发 + 超时;部分失败降级为"只对比成功的几辆" |
