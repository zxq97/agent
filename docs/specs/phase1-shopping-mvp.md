# Phase 1 — 导购 MVP（Decider + UserNeed + Capability + ID 托管）

> 隶属 [技术方案总纲](../technical-plan.md)。这是 v2 的**承重墙**。**已完成。**
>
> **前置依赖:** [P0](phase0-scaffold.md) 已合入
>
> **[演进 2026-06-30] 范围扩大**：原 P1 只含 Decider + SearchCapability + ID 托管。实际实现时，UserNeed/NeedDelta 结构化需求管理、guide storelist 数据源切换、PriceDetail/Insurance/Compare Capability 骨架一并纳入。search_vehicles 工具 schema 从 `scene_tags`（扁平参数）改为 `need_delta`（增量操作数组），因为原方案完全没有结构化需求抽取。
>
> **[补充 2026-07-02] 搜车迭代意图待补强**：现有实现具备 NeedDelta/LastSearch 基础,但还缺少对"不喜欢/换一批/预算低一点高一点/条件放宽"的显式 `search_mode`、排除列表、预算档位相对调整和对应 eval。本文档已补设计,新增验收项以未完成状态登记。
>
> **[补充 2026-07-02] 复杂筛选解析模型**：普通对话和 DecideStage 仍使用 `deepseek-chat`;复杂"自然语言 → 车型筛选需求"新增 `filter_interpreter=deepseek-reasoner` 旁路,只输出 JSON,不参与 function calling。
>
> **[补充 2026-07-02] 置信度闸门**：现有实现已有 `NeedDelta.confidence`、`UserNeed.Confidence`、`Understanding.sufficiency`,但还缺统一的低置信处理策略。本文档补 `ConfidenceGate`,用于决定直接搜、走 reasoner 解析、追问或宽松搜索。

---

## 1. 目标

浏览器打开 → 输入 User ID → 发消息"明天北京 SUV" → 看到筛选后的车辆报价（SSE 流式）。全程：
- 单轮 LLM 调用 1-2 次（决策 1 次 + 搜车后引导语 1 次）
- 关键 ID（context_id/reference_id/supplier）由 Go 托管，工具 schema 不暴露给 LLM
- 用户需求结构化管理：LLM 输出 need_delta → Go 生命周期管理 → StaticRecall 转 filter_codes → 精准搜索
- 闲聊/越界 0 tool call
- 跨轮需求累积（第 2 轮"换电车"能衰减旧需求、ADD 新需求）

---

## 2. 目标流程

```
用户输入 + state
   ▼ DecideStage（LLM #1，流式 function-calling，6 工具，tool_choice=auto）
   │   话术 content → SSE text（逐字实时下发）
   │   流末 tool_calls → Decision（含 NeedDelta / Understanding）
   │
   │   ├─ 不调 tool ──► PureReplyCapability  （闲聊/越界，0 LLM）
   │   ├─ ask ────────► AskCapability        （0 LLM，渲染 question+options）
   │   ├─ search ─────► SearchCapability     （需求合并→filter_codes→guide storelist→LLM #2 引导语）
   │   ├─ price_detail► PriceDetailCapability（Go 调 get_order_details + LLM #2 讲解）
   │   ├─ insurance ──► InsuranceCapability  （同上 + LLM #2 保险话术）
   │   ├─ compare ────► CompareCapability    （Go 并发多辆 get_order_details + LLM #2 综合对比）
   │   └─ rules ──────► 占位（P3 实现，命中时返回"该功能即将上线"）
   │
   ▼ FinalizeStage（Go：落 state、写 history）
```

---

## 3. 已实现清单

### 3.1 Decider（流式 function-calling）

| 文件 | 说明 |
|------|------|
| `internal/agent/decide.go` | `Decider.Decide`：ChatStream 流式 → 首帧 Delta 立即 emit.Text → 流末 ToolCalls 拼 Decision → 流异常 0 字时 syncFallback。`buildMessages` 组装历史回放 + 需求状态前缀（`NeedsStatePrefix`） + 本轮 user |
| `internal/agent/decide_tools.go` | 6 个 decide tool schema（**均不含 ID 字段**） |

**6 个 decide tool schema**:

| 工具 | 入参 | 说明 |
|------|------|------|
| `search_vehicles` | `search_mode`（initial/refine/page/negative_feedback/budget_down/budget_up/relax）+ `need_delta`（增量数组）+ `feedback_ref`（可选自然语言指代）+ `understanding`（自评）+ `strong_search_intent` | LLM 输出结构化 need_delta 和迭代意图；Go 侧合并需求、翻页/排除/预算档位处理，再由 StaticRecall 转 filter_codes |
| `ask` | `question, options, slot` | 信息不足追问，必带 2-4 选项 |
| `get_price_detail` | `vehicle_ref`（自然语言） | Go 侧 ResolveQuoteRef 翻译 |
| `insurance` | `vehicle_ref, question` | 同上 |
| `compare_vehicles` | `vehicle_refs`（2-3 个自然语言指代） | Go 侧逐个 ResolveQuoteRef |
| `interpret_rules` | `rule_query` | P3 实现 |

**Decision 结构**:
```go
type Decision struct {
    Tool               string
    Args               map[string]any
    Reply              string
    SearchMode         string             // search_vehicles 时的迭代模式: initial/refine/page/negative_feedback/budget_down/budget_up/relax
    FeedbackRef        string             // 用户反馈指向的自然语言对象,如"第一辆"/"比亚迪"/"SUV"
    NeedDelta          []types.NeedDelta  // search_vehicles 时的需求增量
    StrongSearchIntent bool               // 用户"直接推/别问了" → true
    Understanding      *Understanding     // 模型自评
}
```

### 3.2 System Prompt

| 文件 | 说明 |
|------|------|
| `internal/prompt/decide_system.go` | 角色「小租」+ 工具调用规约 + **search_mode/need_delta 产出规约**（type 归类、多维输入别只抓一个、翻页留空、改向必清旧车型、"不喜欢/换一批/预算高低"归入迭代意图）+ **场景推理**（带老人小孩→SUV soft、商务→商务车 soft）+ **充分度自评**（sufficiency ≥0.6 直接推，<0.6 应改调 ask）+ 红线（严禁输出 ID、不脑补、不绝对化） |

### 3.3 UserNeed 生命周期管理

| 文件 | 说明 |
|------|------|
| `internal/agent/need_state.go` | 参考 tyche `logic/agent/search/need_state.go`。常量对齐：DecayPerTurn=0.95, ConflictDecay=0.3, DormantThreshold=0.3, ActiveThreshold=0.5 |

| 函数 | 说明 |
|------|------|
| `TickNeeds(needs, turn)` | 每轮自然衰减 conf×=0.95，超 5 轮未访问的 Dormant 移除 |
| `ApplyDelta(needs, deltas, turn)` | 应用增量操作（ADD/UPDATE/NEGATE/DECAY/DELETE/REINFORCE） |
| `ApplyConflictDecay(needs, deltas)` | 冲突衰减：换品牌→衰减旧座位数（ConflictTypes 映射表） |
| `FilterActiveNeeds(needs)` | 过滤 Dormant 以下的非活跃需求 |
| `BuildNeedsFromConstraints(c)` | SearchConstraints → 扁平 UserNeed 列表 |
| `UpdateConstraints(needs)` | UserNeed 列表 → Hard/Soft/Negative 三桶 |

### 3.4 StaticRecall（needs → filter_codes）

| 文件 | 说明 |
|------|------|
| `internal/agent/filtercode.go` | `StaticRecall(needs, menu) (codes, uncovered)` — 确定性映射 + 菜单白名单校验 |

静态映射表（对齐 tyche valueToItemCode）：

| Type | Value 示例 | filter_code |
|------|-----------|-------------|
| vehicle_type | SUV/轿车/经济型/豪华型 | `filter/vehcle_choice/*` |
| energy_type | 纯电/混动/汽油 | `filter/fuel/*` |
| seat_num | ≥7 | `filter/seat_num/ge_8`（≤5 不转码） |
| transmission | 自动挡/手动挡 | `filter/transmission/*` |
| car_age | 新车/一年内/车新 | `filter/car_age/*` |

白名单校验：有 menu_group 时，生成的 filter_code 必须在 menu 的 item_code 中存在，否则归 uncovered。

### 3.5 SearchCapability（完整链路）

| 文件 | 说明 |
|------|------|
| `internal/agent/cap_search.go` | 搜车全流程 |

**执行步骤**：
1. **POI 解析**（MCP）：`rental_search_locations` + `rental_resolve_poi` → 写 state.Rental
2. **复杂筛选解析（按需）**：`ShouldUseFilterInterpreter(decision, userText, state)` 命中复杂多条件/否定+预算/相对表达/uncovered 时,用 `filter_interpreter=deepseek-reasoner` 产出修正后的 `search_mode + need_delta + feedback_ref`;失败则回退原 decision
3. **置信度闸门**：`ConfidenceGate(decision, state)` 综合 `need_delta.confidence`、`UserNeed.Confidence`、`understanding.sufficiency`、`filter_interpreter.confidence`,决定 search / ask / relaxed_search / fallback
4. **需求合并**：`BuildNeedsFromConstraints(state.Constraints)` → `TickNeeds` → `ApplyDelta(decision.NeedDelta)` → `ApplyConflictDecay` → `FilterActiveNeeds`
5. **迭代策略**：`ApplyIterationPolicy(decision.SearchMode, decision.FeedbackRef, state)` 处理翻页、排除、预算上下调、条件放宽，产出 page/排除列表/预算档位候选
6. **生成 filter_codes**：`StaticRecall(activeNeeds, state.CachedMenu)`，其中 `price_preference` 优先按 guide menu 白名单映射到 `filter/total_fee/*`
7. **搜报价**：优先 guide storelist `/car/rental/guide/store/list/agent`（带 filter_codes/page，返回 menu_group + veh_rates）；guide 不可用时 fallback 到 MCP `rental_search_quotes`
8. **结果过滤**：`FilterExcludedAndDedupe` 去掉本会话已排除/已展示且仍有替代的结果；无替代时明确说明并建议放宽条件
9. **写 state**：`SetQuotes`（context_id + 报价）+ `Constraints`（UpdateConstraints）+ `LastSearch` + `CachedMenu`（缓存菜单供下轮白名单校验）
10. **LLM 引导语**（LLM #2）：`streamGuide` 流式生成 ≤60 字引导语

### 3.5.1 SearchIterationIntent（反复提要求的识别与筛选）

用户在搜车过程中常见的连续反馈必须结构化处理,不能只依赖 history 让模型自由发挥。`search_mode` 只负责描述本轮搜索意图,具体筛选仍由 `need_delta` 和 Go state 决定。

| search_mode | 触发表达 | `need_delta` 要求 | 筛选策略 |
|---|---|---|---|
| `page` | "换一批""还有吗""再看看" | 留空,除非句子同时带新条件 | 复用 `LastSearch.FilterCodes`,page+1,过滤已展示 |
| `negative_feedback` | "不喜欢第一辆""不要比亚迪""别给我电车" | 对品牌/车型/能源输出 NEGATE;具体报价放 `feedback_ref` | Go 侧 ResolveQuoteRef 后加入排除列表,重搜或结果层过滤 |
| `budget_down` | "便宜点""预算低一点""200以内" | UPDATE `price_preference` 为更低档或明确金额 | 按 `CachedMenu` 的 `total_fee` 相邻低档生成 filter_code,page 重置 |
| `budget_up` | "预算高一点也行""贵点但车好" | UPDATE `price_preference`;可 ADD 车新/舒适 soft | 放宽价格硬约束,上调价位档,优先车新/舒适 |
| `relax` | "条件放宽点""别卡这么死" | DECAY/DELETE 低置信 soft needs | 保留 hard,逐步释放 soft;无结果再 ask |

边界规则:
- 一句话多个意图要全部落结构化字段,如"不要电车,预算再低点"=NEGATE energy_type=纯电 + `budget_down` + price_preference 更新。
- 具体报价、context_id、reference_id、supplier 不允许出现在 prompt 或 tool 参数里;`feedback_ref` 只能是用户原话指代。
- 连续两轮 `page`/`negative_feedback` 后仍无新结果,停止盲目搜索,改走 `ask` 让用户选择价格/空间/车新等关键方向。

### 3.5.2 FilterInterpreter（复杂筛选解析旁路）

FilterInterpreter 是 SearchCapability 内部的可选 LLM 步骤,只在复杂筛选理解时使用 `deepseek-reasoner`。它不注册 function tools,不直接生成 `filter_codes`,只输出和 `search_vehicles` 对齐的 JSON:

```json
{
  "search_mode": "budget_down",
  "need_delta": [
    {"op": "NEGATE", "type": "energy_type", "value": "纯电", "hardness": "hard", "confidence": 0.9},
    {"op": "UPDATE", "type": "price_preference", "value": "更低预算", "hardness": "hard", "confidence": 0.8}
  ],
  "feedback_ref": "第一辆",
  "confidence": 0.82,
  "rationale": "用户同时表达排除能源、否定具体车和降低预算"
}
```

触发规则:
- 用户本轮包含 2 个以上筛选维度,或同时包含否定/预算/相对比较。
- DecideStage 的 `understanding.sufficiency` 在 0.4-0.7 灰区。
- `StaticRecall` 出现关键 uncovered,例如"别太旧但也别太贵""空间大一点但不要商务车"。
- 用户连续反馈不满意,需要区分翻页、排除、降预算、升预算或放宽条件。

校验与回退:
- JSON schema、need type、op、hardness 必须在白名单内。
- 输出不得包含 `context_id/reference_id/supplier/filter_code`;发现则整段丢弃并记 warn。
- confidence < 0.6 或校验失败时,回退 DecideStage 原始 `need_delta`;如果原始也不足,走 `ask`。

### 3.5.3 ConfidenceGate（置信度闸门）

ConfidenceGate 把现有分散的置信度信号收口成一个明确动作,避免"低把握硬筛"和"高把握还追问"。

| 信号 | 阈值 | 动作 |
|---|---|---|
| `understanding.sufficiency >= 0.7` 且 hard needs 置信度均 `>=0.75` | 高置信 | 直接搜索 |
| `understanding.sufficiency 0.4-0.7` 或存在复杂多条件 | 灰区 | 优先触发 FilterInterpreter |
| FilterInterpreter `confidence >= 0.6` 且 JSON/schema/menu 校验通过 | 可采用 | 用修正后的 `search_mode + need_delta` |
| FilterInterpreter `<0.6` 或校验失败 | 不可靠 | 回退 DecideStage 原始 delta;仍低置信则 ask |
| hard need 置信度 `<0.6` 且不是用户明确原话 | 低置信硬条件 | 降为 soft 或追问确认 |
| `static_recall.coverage < 0.5` | 筛选覆盖不足 | 宽松搜索并说明,或 ask 一个关键维度 |

输出:
- `confidence_action`: `search / interpret / ask / relaxed_search / fallback`
- `confidence_reason`: 一句话原因,写入 trace,不直接展示给用户
- `normalized_delta`: 被降级/删除/采用后的最终需求增量

验收原则:
- 模型推断出来的低置信需求只能 soft,不能直接 hard filter。
- 用户明确表达的高置信条件优先保留,不要因为模型不确定而重复追问。
- 如果用户要求"直接推",允许宽松搜索,但必须避免把低置信猜测当硬条件。

### 3.6 其他 Capability

| 文件 | 说明 |
|------|------|
| `internal/agent/cap_simple.go` | **PureReplyCapability**（闲聊/越界，0 LLM）+ **AskCapability**（追问，渲染 question+options） |
| `internal/agent/cap_price.go` | **PriceDetailCapability**：ResolveQuoteRef → Go 注入 ID 调 get_order_details → LLM #2 讲解。含共享 `streamCapabilityLLM` 和 `fetchOrderDetails` |
| `internal/agent/cap_insurance.go` | **InsuranceCapability**：基于 get_order_details 的 charges（Type=3 保险费）+ LLM #2 保险话术 |
| `internal/agent/cap_compare.go` | **CompareCapability**：并发取多辆 get_order_details + LLM #2 综合对比。含 `ResolveMany` |

### 3.7 ResolveQuoteRef（ID 翻译）

| 文件 | 说明 |
|------|------|
| `internal/agent/resolve.go` | `ResolveQuoteRef(state, userText) (ref, clarify)` — 纯 Go 不调 LLM。匹配优先级：①序号词 ②车名/品牌精确包含 ③单候选兜底 |
| `internal/agent/resolve_test.go` | 单测覆盖序号/车名/品牌/多义/0 命中/过期 |

### 3.8 State 扩展

`internal/orchestration/state.go` 在 P0 骨架上增加的字段：

| 字段 | 说明 |
|------|------|
| `Rental RentalCtx` | 取还车 + context_id（Go 注入） |
| `LastQuotes []QuoteRef` | 上轮报价（含 ReferenceID/Supplier/CarName/BrandName/Price/Index） |
| `QuoteAt time.Time` | 15 分钟时效判定 |
| `SelectedRef string` | 用户已锁定的报价 |
| `Constraints types.SearchConstraints` | 结构化需求（Hard/Soft/Negative 三桶，跨轮累积） |
| `TurnCount int` | 累计对话轮次（need 衰减用） |
| `LastSearch *types.LastSearchState` | 上次搜索状态（search_mode/filter_codes/page/has_more/shown_refs/excluded_refs/excluded_models/price_range/relax_level） |
| `CachedMenu []types.MenuGroupView` | 菜单缓存（guide storelist 返回，供 StaticRecall 白名单校验） |

### 3.9 辅助

| 文件 | 说明 |
|------|------|
| `internal/agent/timeutil.go` | `defaultPickupTime`（明天 14:00）/ `defaultDropoffTime`（后天 12:00） |
| `internal/agent/tyche_data.go` | tyche MCP 返回数据解析：`stdResp`/`locationsData`/`poiData`/`quoteItem`/`quotesData`/`orderDetailsData` |

---

## 4. 数据流示例

```
用户: "带老人小孩，想租个电车"
  │
  ▼ DecideStage (LLM #1 流式)
  content: "一家人出行，纯电SUV是不错的选择，这就帮你看看..."  → SSE 实时下发
  tool: search_vehicles
  args.search_mode: "initial"
  args.need_delta: [
    {op:ADD, type:vehicle_type, value:"SUV", hardness:soft, confidence:0.6},    ← 场景推理
    {op:ADD, type:energy_type,  value:"纯电", hardness:hard, confidence:0.9},   ← 用户明说
    {op:ADD, type:seat_num,     value:"7",   hardness:soft, confidence:0.6}     ← 带老人小孩推
  ]
  args.understanding: {sufficiency: 0.7, covered_dims: ["vehicle_type","energy_type","seat_num"]}
  │
  ▼ CapabilityStage → SearchCapability
  ├─ TickNeeds(sessionNeeds, turn=1) → []  (首轮空)
  ├─ ApplyDelta([], need_delta, 1) → [SUV(soft), 纯电(hard), 7座(soft)]
  ├─ ApplyConflictDecay → 无冲突
  ├─ FilterActiveNeeds → [SUV, 纯电, 7座]  (全部活跃)
  ├─ StaticRecall(active, menu) → ["filter/vehcle_choice/suv", "filter/fuel/electric", "filter/seat_num/ge_8"]
  ├─ guide storelist (filter_codes=上述3个) → menu_group + 3辆纯电SUV报价 + context_id
  ├─ 写 state: Constraints={Hard:[纯电], Soft:[SUV,7座]}, LastSearch, CachedMenu, LastQuotes
  └─ LLM #2: "找到几辆纯电SUV..."  → SSE 实时下发
  │
  ▼ FinalizeStage → 写 history（user + assistant + ToolCallSnapshot）

第 2 轮: "换个轿车吧"
  ├─ args.search_mode="refine"
  ├─ TickNeeds → [SUV(conf*=0.95), 纯电(conf*=0.95), 7座(conf*=0.95)]
  ├─ ApplyDelta([{DELETE, vehicle_type}, {ADD, vehicle_type, 轿车, hard}])
  ├─ ApplyConflictDecay(vehicle_type变更 → seat_num 衰减到 0.3*0.6=0.18 → Dormant)
  ├─ FilterActiveNeeds → [轿车(hard), 纯电(hard)]  (7座已 Dormant 被过滤)
  ├─ StaticRecall → ["filter/vehcle_choice/jiaoche", "filter/fuel/electric"]
  └─ 搜到纯电轿车报价

第 3 轮: "第一辆不喜欢,预算再低一点"
  ├─ args.search_mode="budget_down", feedback_ref="第一辆"
  ├─ ApplyDelta([{UPDATE, price_preference, "更低预算", hard}])
  ├─ ResolveQuoteRef("第一辆") → 加入 LastSearch.ExcludedRefs
  ├─ ApplyIterationPolicy → page=1, price bucket 下调一档
  ├─ StaticRecall → ["filter/vehcle_choice/jiaoche", "filter/fuel/electric", "filter/total_fee/*"]
  ├─ guide storelist → 返回更低预算候选
  └─ FilterExcludedAndDedupe → 不再展示被否定的第一辆
```

---

## 5. 验收

- [x] 搜车通："明天北京 SUV" → locations→poi→[SUV→filter_code]→guide storelist → 候选 + 引导语
- [x] **need_delta**："纯电SUV" → 两条 need_delta（vehicle_type + energy_type）→ 两个 filter_code
- [x] **白名单校验**：有 menu 时 filter_code 必须在 menu 中存在
- [x] **座位特例**："2人" → 不出座位码（≤5 不转）
- [x] **跨轮累积**：第 2 轮"换 7 座的"复用 context_id，Constraints 更新
- [x] **冲突衰减**：换品牌后旧座位数衰减到 Dormant
- [x] **换一批**："换一批/还有吗" → 不新增 need_delta，复用 LastSearch.FilterCodes 翻页，不重复展示同一批
- [x] **负反馈**："不喜欢第一辆/不要比亚迪" → 识别 `negative_feedback`，Go 侧排除具体报价或品牌，下一轮不重复推荐
- [x] **预算下调**："预算低一点/便宜点/200以内" → 识别 `budget_down`，映射到更低 `total_fee` 档或金额上限
- [x] **预算上调**："预算高一点也行/贵点但车好" → 识别 `budget_up`，放宽价格并优先车新/舒适候选
- [x] **混合改口**："不要电车,预算再低点" → 同时 NEGATE energy_type 和 UPDATE price_preference，不能只处理一个意图
- [x] **复杂筛选解析模型**："空间大一点但不要商务车,预算别超过300" → 触发 `filter_interpreter=deepseek-reasoner`,输出 JSON need_delta,Go 校验后再转 filter_codes
- [x] **置信度闸门**："差不多坐得舒服点吧" → 低置信 inferred need 不硬筛;应 ask 或 relaxed_search,trace 记录 `confidence_action`
- [x] 闲聊"你好" → 0 tool call，纯话术
- [x] 越界"帮我写代码" → 拒绝+引导，0 tool call
- [x] 反问：信息不足 → ask 带 2-4 选项
- [x] 指代多义：候选多辆同名 → ResolveQuoteRef 返回澄清反问
- [x] **ID 硬保证**：工具 schema 不含 ID 字段；Go 从 state 注入；报价 >15 分钟主动重搜
- [x] **性能**：搜车 ≤2 次 LLM；闲聊 =1 次
- [x] **前端**：浏览器打开 → User ID 登录 → 对话 → SSE 流式回复 → 历史回溯
- [x] `go build ./...` + `go vet ./...` + `go test ./...` 全绿

---

## 6. 风险

| 风险 | 应对 |
|------|------|
| Decide 偶尔不调 tool 但用户在搜车 | Reply 空且无 tool → PureReply 兜底 |
| 流式 LLM 格式漂移致 tool_call 解析失败 | `syncFallback` 回退非流式重试一次 |
| ResolveQuoteRef 漏判("那辆白色的") | 0 命中 → Capability 改追问 |
| date_time 格式 LLM 省秒 | tyche client `fixDateTimeSeconds` 补 `:00`（P0 已做） |
| guide storelist 不可用 | fallback 到 MCP `rental_search_quotes`（无 filter_codes，宽松搜索） |
| need_delta 解析失败（LLM 格式不对） | `parseNeedDelta` 返回 nil → SearchCapability 以空 needs 跑（不影响主链路） |
| StaticRecall 未命中（用户说了映射表没覆盖的值） | 未命中不转码（宽松搜索），归 uncovered |
