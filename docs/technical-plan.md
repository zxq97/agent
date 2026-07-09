# 租车智能体 v2 — 技术方案总纲

> 面向 C 端租车用户的对话式 AI 助手。本方案是 **从零重建** 的 v2 —— 不在旧 scaffold(feat/initial-scaffold 的 supervisor + ReAct 形态)上打补丁,而是把"旧 agent 的可取点 + tyche AI 导购的可取点 + 旧 agent 踩过的坑"从第一天就设计进地基。
>
> **分支:** `feat/agent-v2-pipeline`(从 main 切出,干净起点)

---

## 0. 为什么重建 / 这版跟旧 scaffold 的区别

旧 scaffold(feat/initial-scaffold)实测暴露的问题,本版从地基规避:

| 旧 scaffold 的坑 | v2 的设计 |
|---|---|
| supervisor + ChatModelAgent ReAct,一次搜车 ~5-6 次 LLM 调用 | **责任链 Pipeline + Decider 单次流式 function-calling**,1-2 次/轮 |
| `context_id`/`reference_id` 靠 prompt 让 LLM 在 history 文本里"自己保存"(软约束,易丢/改/编) | **ID 由 Go 从 state 注入,工具 schema 不暴露给 LLM**;`ResolveQuoteRef` 解析"第一辆/朗逸" |
| `LastQuoteIDs`/`SelectedQuoteID` 是死字段(定义了无人读写) | state 字段从设计起就有写入/读取闭环,带单测 |
| 本地 P5 工具(资质/油费/停车费)是占位经验常数,会让 LLM 报错误数字 | **默认不注册占位 tool**,真业务接入前不上 |
| SSE 只有整段 `message`,调工具空档完全沉默,首字节 3-5s | **流式 content 边吐边出 + thinking_tips/box**,首字节 < 1s |
| history 拍平成 assistant 文本,模型看不到上轮"真发过工具调用" | **history 回放** `assistant(tool_calls)+tool(content)` |
| 无审核/无并发锁/无反馈/无 metric | **生产化基建从 P6 占骨架**,P6 真上线只填实现 |
| 多轮 history 全量送 LLM,长会话触顶 | **对话摘要**(模板化非 LLM)+ 结构化状态前缀 |

---

## 1. 背景与目标

### 1.1 目标用户与能力边界

**C 端租车用户**(非客服坐席、非商家):
- ✅ 可以:推荐车型报价、解读价格明细、推荐保险、解读租车条款、决策辅助、引导跳转 App
- ❌ 不能:在 agent 内闭环下单 / 改单 / 退款 / 理赔;不替代人工客服处理申诉

### 1.2 核心能力

| 能力 | 阶段 | 实现 |
|---|---|---|
| 车型推荐 + 报价 | P1 | tyche MCP `rental_search_locations`+`rental_resolve_poi`+`rental_search_quotes` |
| 需求 → filter_codes | P1 | 用户描述(SUV/纯电/7座/便宜)→ 静态映射表转成 `filter/{group}/{item}` 筛选码传给 search_quotes(见 §3.2) |
| 价格明细解读 | P2 | tyche MCP `rental_get_order_details` |
| 保险推荐 | P2 | 基于 `rental_get_order_details` 的 charges(grantee_list 缺口见 §5) |
| 车型对比 | P2 | 用户纠结选哪辆时,并发取多辆 `rental_get_order_details` + LLM 综合(自然语言 + 引导胶囊两路触发) |
| 条款规则解读 | P3 | 接 AgentHub 平台检索(不自建知识库),据检索文本 grounded 生成 |
| 决策辅助 / 资质 / 售后 | 后续 | P5 真业务接入(本方案只留 Capability 挂载点) |

---

## 2. 技术栈

- **语言**:Go 1.24+
- **LLM**:可插拔,主链路默认 `deepseek-chat`;**复杂自然语言→筛选需求解析**可走 `filter_interpreter=deepseek-reasoner` 旁路(不使用 function calling,详见 §3.2.1 / §4.1),`internal/llm` 工厂按 `AgentBindings` 可给不同环节绑不同模型,可换 Claude / 千问 / 豆包
- **LLM 调用层**:**纯 Go OpenAI-compatible client**(不依赖 eino),自带流式 SSE 解析 + tool_calls 分片累积 + syncFallback;`internal/llm` Factory 接口抽象保留,后续接非 OpenAI 协议(Claude/Gemini)只需新增 adapter
- **编排**:**纯 Go 责任链 Pipeline**(不使用 eino ADK / supervisor / ReAct —— 这是与旧 scaffold 最大的区别)
- **后端调用**:**tyche MCP**(POI 解析,JSON-RPC 2.0 over HTTP)+ **rental-guide `guide/store/list/agent`**(报价+菜单,HTTP POST);`internal/tyche` 同时提供两个 client。**[演进 2026-06-30]** 原方案只用 MCP,但 MCP 不暴露 menu_group 导致无法做菜单白名单校验;改为报价走 guide storelist(返回 menu_group + veh_rates),POI 仍走 MCP,guide 不可用时 fallback 到 MCP `rental_search_quotes`
- **需求管理**:**UserNeed + NeedDelta 结构化需求体系**(参考 tyche),LLM 输出 need_delta 增量操作,Go 侧做生命周期管理(衰减/冲突/活跃过滤),StaticRecall 确定性映射 needs→filter_codes。**[演进 2026-06-30]** 原 search_vehicles 只有 scene_tags 参数,无结构化需求抽取;参考 tyche UserNeed 体系重建
- **Session**:MemoryStore(开发阶段),Redis(后续生产)
- **规则检索**:**接公司 AgentHub(Dify 风格 RAG 平台)**,检索/向量/切片托管在平台侧,agent 只发 query 拿回知识文本(P3)——不自建本地 BM25/知识库
- **服务形态**:HTTP + SSE + Web 前端(`cmd/http/main.go` + `web/index.html`),CLI 保留供本地调试。**[演进 2026-06-30]** 原方案 CLI(P1-P3)→HTTP(P4),用户要求跳过 CLI 直接做 HTTP + 前端

---

## 3. 目标架构

```
HTTP /agent/chat
   │  middleware: recover → trace → auth → ratelimit → access_lock(P6)
   ▼
[加载 ConversationState] ← Redis (RentalCtx / LastQuotes / Profile / Summary)
   │
   ▼
ChatPipeline(责任链,Stage 单一职责,Signal = Continue / Stop)
   ├─ PreRouteStage      Go 零LLM:反问解挂、action_click(slot_patch)短路、安全红线快路
   ├─ DecideStage        ★ LLM #1 一次流式 function-calling(DeepSeek)
   │                       content → SSE text(逐字),末帧 tool_calls → Decision
   │                       工具集 = {search_vehicles, ask, get_price_detail,
   │                                 insurance, compare_vehicles, interpret_rules}
   │                       tool_choice = auto;不调 = 纯回复(闲聊/越界)
   │   ├─ 不调 tool ──► PureReplyCapability  ← 0 个额外 LLM
   │   ├─ ask ────────► AskCapability        ← 0 个额外 LLM(话术复用 Decide content)
   │   ├─ search ─────► SearchCapability     ← Go: 复杂筛选可先走 FilterInterpreter(reasoner JSON)→需求合并(TickNeeds→ApplyDelta→ConflictDecay→FilterActiveNeeds)→StaticRecall(needs→filter_codes)→guide storelist(报价+菜单)/MCP fallback
   │   ├─ price_detail► PriceDetailCapability← Go 调 get_order_details + LLM #2 讲解
   │   ├─ insurance ──► InsuranceCapability  ← 同上 + LLM #2 保险话术
   │   ├─ compare ────► CompareCapability    ← Go 并发多辆 get_order_details + LLM #2 综合对比
   │   └─ rules ──────► RulesCapability      ← AgentHub 检索(P3) + LLM #2 grounded 答
   ├─ GuideStage         ★ 仅 Search 命中且有车:LLM #2 流式产引导语 + slot_patch/compare 引导胶囊
   ├─ ClarifyStage       反问渲染(question + options)
   └─ FinalizeStage      落 state、写 history、对话摘要、推 SSE done、异步落 Redis

   旁路:
   • AsyncAuditor   输入审 + 输出分段送审,命中 → done{guardrail}
   • TraceSink      LLM / Tool / Stage 三层 metric + 流式收尾日志

LLM Factory(可插拔 provider,所有调用走流式 + 收尾日志):
   DeepSeek(默认) / Claude / 千问 / 豆包
```

**关键不变量**:
- 单轮 LLM 调用 1-2 次(闲聊 1 次,搜车/明细 2 次)
- 用户首字节 < 1s(content 流式)
- ID(context_id/reference_id/supplier)全程 Go 托管,LLM 不经手
- 加一个能力 = 加一个 Capability + 一条 decide tool schema,不动 Pipeline 主体

### 3.1 上下文管理(Context Manager)

> **演进记录 [2026-06-29]**:参考[火山引擎 AI 搜索 UP-ReAct 架构文章](https://mp.weixin.qq.com/s/hol76ebv7-OB5TNUIWVVYA)补充。原方案中 history 回放、状态前缀、对话摘要三件事**散落在三个文件**(`history_replay.go`/`state_prefix.go`/`summarizer.go`),没有统一的"谁来决定喂给 LLM 什么"的抽象。文章实测结论是**"洗干净上下文反而更准"——TTFT 降 30% 的同时准确率也涨了**,因为去掉中间冗余后模型注意力聚焦。我们多轮搜车后 history 会堆大量报价 JSON,正是文章描述的"Context Thrashing / 注意力稀释"风险。

**决策**:新增 `internal/orchestration/context_manager.go`,统一收口"构造喂给 LLM 的消息列表"这件事。所有需要调 LLM 的地方(Decider / 各 Capability 二次 LLM)都走它,不再各自直接操作 history。

**Context Manager 三层策略**(对齐文章的三个统一之"统一状态"):

| 策略 | 做什么 | 解决什么问题 |
|---|---|---|
| **① Token 预算窗口** | 传入 `maxTokens`(如 4096),从最新一条往回装,装满为止;system prompt 和状态前缀有保底预留(如 2500),剩余分给 history | 避免长会话 token 失控(条数截断挡不住"一条报价明细 2000 token"的情况);直接关联成本/预算 |
| **② 工具结果降噪** | history 里 `ToolCall.Result` 存的永远是**精炼摘要**("已展示3辆车:朗逸¥198/天、卡罗拉¥215/天、雅阁¥238/天"),绝不存 tyche 返回的原始 JSON;报错只存核心提示,丢弃 debug 栈 | 直接砍掉多轮对话**最大的 token 膨胀源**;文章原话:"中间工具调用的长 JSON 报错栈?立刻提取核心错误码后丢弃原始文本" |
| **③ 记忆分级驱逐** | 分三级驻留:`常驻`(system prompt + 状态前缀,含 RentalCtx/LastQuotes/Profile)、`正常`(近 6 轮 history)、`可压缩`(>6 轮用模板摘要压缩) | 避免用户长期偏好被挤出;中间过程快速降温 |

**接口骨架**(P4 实现,P1 先用简单版:截断 N 条 + 工具降噪):
```go
// internal/orchestration/context_manager.go
type ContextManager struct {
    maxTokenBudget int  // 喂给 LLM 的 messages 总 token 上限
    systemReserve  int  // system prompt + 状态前缀保底
}

// BuildMessages 构造喂给 LLM 的消息列表(统一入口)。
// 所有调 LLM 的地方(Decider / Capability 二次 LLM)都走这里,不直接操作 history。
func (cm *ContextManager) BuildMessages(
    systemPrompt string,
    statePrefix  string,         // BuildStatePrefix(state) 的结果
    history      []*HistoryEntry,
    userInput    string,
) []Message {
    // 1. 计算 system + prefix 占用的 token 预留
    // 2. 从 history 最新一条往回装,每条取 Result 的精炼摘要版(不存原始 JSON)
    // 3. 超出 token 预算时,最早的条目用模板摘要压缩
    // 4. 工具调用条目回放为 assistant(tool_calls) + tool(content),非工具调用原样保留
    // 5. 末尾追加本轮 user(状态前缀拼在 user 消息前)
}

// SummarizeOldest 把 history 最早 N 条压成一句摘要(模板化,不调 LLM)。
func (cm *ContextManager) SummarizeOldest(entries []*HistoryEntry, n int) string
```

**与已有设计的关系**:
- P4 的 `history_replay.go`、`state_prefix.go`、P6 的 `summarizer.go` **合并收口到 ContextManager**,不再是三个散落的函数
- HistoryEntry 里 `ToolCall.Result` 的存储格式在 `FinalizeStage` 写入时就**强制降噪**(只存摘要,不存原始 JSON),ContextManager 读的时候已经是干净的

> **不做**(文章提到但我们不需要):"语义折叠——连续同质化搜索结果用小模型静默压缩"。原因:它需要额外一次小模型调用(成本+延迟),而我们的模板化摘要 + token 预算窗口已经能解决问题。如果数据证明不够再上。

### 3.2 需求 → filter_codes(用户描述转筛选码)

**概念**:`filter_code` 是 tyche storelist 平台约定的筛选码,结构 `filter/{group}/{item}`,作为 `rental_search_quotes` 的 `filter_codes` 入参做精筛。例:

| 用户说 | filter_code |
|---|---|
| SUV | `filter/vehcle_choice/suv` |
| 纯电/电车 | `filter/fuel/electric` |
| 7座/8座 | `filter/seat_num/ge_8`(座位档) |
| 自动挡 | `filter/transmission/auto` |
| 经济型/豪华型 | `filter/vehcle_choice/jingji` / `.../haohua` |
| 便宜/某价位 | `filter/total_fee/*`(价位档) |

**SearchCapability 内的转换位置**:`locations → resolve_poi → 【TickNeeds→ApplyDelta→ConflictDecay→FilterActiveNeeds→StaticRecall(needs→filter_codes)】→ guide storelist / MCP fallback`。**[演进 2026-06-30]** 原为调 MCP `rental_search_quotes`；改为报价走 rental-guide `guide/store/list/agent`（返回 menu_group + veh_rates + context_id），POI 仍走 MCP。需求从 DecideStage 的 `need_delta` 增量操作中提取，Go 侧做生命周期管理后转 filter_codes。

**选型:静态映射 + 菜单白名单校验**:

| | tyche v4 | v2(本方案) |
|---|---|---|
| 取菜单 | `GetMenuSchema(context_id)` 从 storelist 拿全量 menu_group | **guide storelist 返回 menu_group**,缓存在 state.CachedMenu |
| 主路 | 静态高频值映射(`valueToItemCode`) | **`StaticRecall(needs, menu)` 静态映射 + menu 白名单校验**(`internal/agent/filtercode.go`) |
| 兜底 | LLM `FilterSelector` 从菜单白名单挑码 | **暂不做** —— StaticRecall 足够覆盖高频需求;后续可加 FilterSelector LLM |

> **[演进 2026-06-30]** 原方案写"MCP 不暴露 menu_group → 没有白名单可校验 → 不做 LLM 挑码"。改用 guide storelist 后**有了 menu_group**,StaticRecall 生成的 filter_code 可做白名单校验(code 必须在 menu 的 item_code 中存在,否则丢弃)。FilterSelector LLM 兜底留作后续优化。

**实现要点**:
- `internal/agent/filtercode.go`:`StaticRecall(needs []types.UserNeed, menu []types.MenuGroupView) (codes []string, uncovered []types.UserNeed)`,维护 `type:value → filter_code` 映射表(车型/能源/座位/变速箱/价位档/车龄),口径对齐 tyche 的 `valueToItemCode` + `attribute_resolver`。有 menu 时做白名单校验。
- **座位特例**(抄 tyche 的坑):座位 ≤5 不要转 `filter/seat_num/*`(会把"2人"误筛成 2 座跑车),仅人数 ≥6 或明确要"N座车"时才按座位档选。
- 映射表是平台约定,后续平台加筛选项时**在表里加一行**即可,不动主流程。

### 3.2.1 复杂筛选语义解析模型(FilterInterpreter)

> **补充 [2026-07-02]**:当前自然语言→筛选条件主要在 DecideStage 里用 `deepseek-chat` 通过 function calling 产出 `need_delta`,和普通对话共用同一个模型。这个设计快,但对复杂口语筛选("别太贵但也别太旧""不要电车,预算再低点,最好能坐 6 个人""比刚才宽敞点但别上商务车")容易漏维度或误判相对预算。新增 **FilterInterpreter** 旁路:只在复杂筛选理解场景启用 `deepseek-reasoner`,输出严格 JSON,再交给 Go 校验和执行。

**为什么不直接把 DecideStage 换成 reasoner**:
- DeepSeek reasoner 当前不支持 function calling;DecideStage 必须用 tool_calls 选择 `search_vehicles/ask/price_detail/...`,因此仍绑定 `deepseek-chat`。
- reasoner 首字节慢、成本更高,不适合作为每轮默认决策模型。
- 筛选码仍不能让 LLM 自由生成;reasoner 只输出 `search_mode + need_delta + rationale`,最终 `filter_codes` 继续由 `StaticRecall + menu 白名单` 生成。

**触发条件**(任一命中才调用,简单场景不加延迟):
- `understanding.sufficiency` 处于灰区(0.4-0.7),但用户有明确找车/筛选意图。
- 本轮包含 2 个以上筛选维度、否定+预算混合、相对表达("低一点/高一点/比刚才...")、或场景推理("一家六口带行李")。
- `StaticRecall` 返回 uncovered 且该 uncovered 可能影响搜索质量。
- 连续一轮搜索后用户表达不满意,需要判断是翻页、排除、预算调整还是放宽条件。

**接口约束**:
- 新增 binding:`filter_interpreter: deepseek-reasoner`。它不注册 tools,只用普通 Chat/JSON 输出。
- 输出 schema 与 `search_vehicles` 对齐:`search_mode`, `need_delta[]`, `feedback_ref`, `confidence`, `rationale`。禁止输出 `context_id/reference_id/supplier/filter_code`。
- Go 侧做三道校验:JSON schema 校验、NeedDelta 类型白名单、menu 白名单筛选码生成。校验失败时回退到 DecideStage 原始 `need_delta` 或 ask。
- trace 必须记录 `filter_interpreter_used=true/false`,触发原因,模型名,耗时,以及最终采用/回退原因,便于评估是否值得扩大使用。

### 3.3 搜车过程中的迭代意图管理

> **补充 [2026-07-02]**:P1 已有 `UserNeed + NeedDelta` 和 `LastSearch`,能支撑跨轮筛选,但原文只明确了"换轿车"和"换一批"的少量样例。用户真实搜车会连续表达"不喜欢""再便宜点""预算高一点也行""换一批""不要这个品牌"等反馈,这些不能只靠历史自然语言让 LLM 猜。新增一层 **SearchIterationIntent** 作为 `search_vehicles` 的意图模式,让 LLM 负责识别语义,Go 负责确定性改状态和筛选。

**意图模式**:

| mode | 用户表达 | LLM 输出 | Go 侧动作 |
|---|---|---|---|
| `initial` | "明天北京 SUV" | ADD 取还车/车型/能源等需求 | 从第 1 页按 active needs 搜 |
| `refine` | "换电车""7座的""要自动挡" | ADD/UPDATE/DELETE/NEGATE 对应 need | 合并需求后重搜,page 重置为 1 |
| `page` | "换一批""还有吗""再看看" | need_delta 为空,mode=page | 复用上次 filter_codes,`LastSearch.Page+1`;没有更多时进入 relax/ask |
| `negative_feedback` | "不喜欢这辆""不要比亚迪""别给我 SUV" | NEGATE 车型/品牌/车系/具体车;必要时 DECAY 相关软偏好 | 记录排除项,重搜并过滤已排除/已展示结果;具体报价 ID 仍只由 Go 管理 |
| `budget_down` | "便宜点""预算低一点""200以内" | UPDATE `price_preference` 或预算上限 | 根据 menu 的 `total_fee` 档位下调/命中价位码;重搜 |
| `budget_up` | "预算高一点也行""贵点但车好" | UPDATE `price_preference`,可 ADD `car_age=车新`/`vehicle_type=舒适型` soft | 上调价位档或放宽价格硬约束,再按舒适/车新偏好排序 |
| `relax` | "条件放宽点""别卡这么死" | DECAY/DELETE 低置信 soft needs | 先放宽 soft,保留用户明确 hard;仍无结果再 ask |

**Decider 规约**:
- `search_vehicles` 增加 `search_mode` 字段,枚举见上表。未识别时默认 `refine`,不要让自由文本控制后续流程。
- "不喜欢/不要/别给我"必须优先判断是否指向**具体报价、品牌、车型、能源、价格**。能解析到具体报价时只输出自然语言指代,由 Go 的 `ResolveQuoteRef` 映射到内部 ref 并加入排除列表;LLM 不接触 ref/id。
- "低一点/高一点"是**相对预算**:没有明确金额时,基于 `LastSearch.PriceRange` 和 `CachedMenu` 找相邻价位档;有明确金额时写入 `price_preference` 的数值约束。
- 混合输入要拆成多条 delta,如"不要电车,预算再低点"=NEGATE energy_type=纯电 + UPDATE price_preference=lower。
- 重复两轮 `page` 或 `negative_feedback` 后仍没有新结果,不要死循环重搜,改为 ask 一个关键维度("更看重价格、空间还是车新?")。

**Go 侧状态与筛选**:
- `LastSearchState` 扩展 `SearchMode/Page/HasMore/FilterCodes/ShownRefs/ExcludedRefs/ExcludedModels/PriceRange/RelaxLevel`。这些字段只进 state 和 trace,不暴露给 LLM。
- SearchCapability 的顺序调整为:`ApplyDelta` → `ApplyIterationPolicy(mode,state)` → `StaticRecall` → guide storelist → `FilterExcludedAndDedupe` → 写回 `LastSearch`。
- `StaticRecall` 补齐 `price_preference → filter/total_fee/*` 映射:优先按 guide menu 白名单选相邻价位档;无法命中时保留 uncovered,不臆造 code。
- 排除条件分两层:能映射成正向替代筛选的走 filter_codes;不能映射的负向偏好在结果层过滤或降权,避免因平台不支持 negative filter 而丢失用户意图。

**验收口径**:
- "换一批"不能重复展示同一批车;无更多结果时要明确说明并引导放宽条件。
- "不喜欢第一辆"下一次结果不再出现该报价;如果用户说"不喜欢比亚迪",同品牌结果过滤或降权。
- "预算低一点"应下调价位档或设置更低预算上限;不能继续返回明显更贵的车。
- "预算高一点也行"应放宽价格限制,允许更高价但用车新/舒适/空间做排序理由。
- "不要电车,换便宜点"必须同时处理能源否定和预算下调,不能只识别其中一个。

### 3.4 置信度治理(ConfidenceGate)

> **补充 [2026-07-02]**:当前实现已有局部置信度:`NeedDelta.confidence` 表示单条需求抽取把握,`UserNeed.Confidence` 经过衰减/强化后表示会话内需求强度,`Understanding.sufficiency` 表示本轮信息是否足够推车。但原方案缺一个统一的"低置信时怎么决策"闸门,容易出现模型低把握仍硬搜、或高把握却重复追问。新增 `ConfidenceGate`,放在 SearchCapability 内,在 `FilterInterpreter` 与 `StaticRecall` 之间执行。

**输入信号**:

| 信号 | 来源 | 含义 |
|---|---|---|
| `need_delta[].confidence` | DecideStage / FilterInterpreter | 单个需求抽取置信度,如"一家六口"推 `seat_num=6` |
| `UserNeed.Confidence` | Go NeedState | 跨轮累积后的需求强度,会随轮次衰减、被用户强化或否定 |
| `understanding.sufficiency` | DecideStage | 当前信息是否足够直接搜车 |
| `filter_interpreter.confidence` | FilterInterpreter | 复杂筛选解析整体置信度 |
| `static_recall.coverage` | Go | active needs 中能映射成合法 filter_code 的比例 |

**决策阈值**:

| 条件 | 动作 |
|---|---|
| `sufficiency >= 0.7` 且 hard need 置信度均 `>=0.75` | 直接搜索 |
| `sufficiency 0.4-0.7` 或多条件/否定/相对预算混合 | 先走 `FilterInterpreter(reasoner JSON)` |
| FilterInterpreter `confidence >= 0.6` 且 schema/menu 校验通过 | 采用 reasoner 修正后的 `search_mode + need_delta` |
| FilterInterpreter `<0.6` 或校验失败 | 回退 DecideStage 原始 delta;若原始 hard need 也低置信则 ask |
| `static_recall.coverage < 0.5` 且关键 hard need 未覆盖 | ask 一个关键维度或宽松搜索并明示"先按较宽条件看看" |
| 用户 `strong_search_intent=true` | 可跳过 ask,但低置信需求只能作为 soft,不得硬筛 |

**落地要求**:
- 新增 `ConfidenceGate(decision,state,uncovered) -> action`。action 枚举:`search / interpret / ask / relaxed_search / fallback`。
- 所有低置信 hard need 在进入 `StaticRecall` 前降为 soft 或触发 ask,避免把模型猜测当硬筛选。
- trace 记录:`sufficiency`, `min_hard_confidence`, `filter_interpreter_confidence`, `static_recall_coverage`, `confidence_action`。
- Eval 增加低置信场景:模糊预算、含糊人数、矛盾条件、低覆盖 filter_code,要求不要硬推或不要重复追问。

### 3.5 Prompt / Context / Tool Schema 版本化与回放对比

> **补充 [2026-07-02]**:Prompt、context prefix 和 tool schema 都会影响模型行为。只记录"用户说了什么"不够,线上 badcase 回放时必须恢复当时的完整模型输入和工具协议。新增 **PromptVersionSet**:每次 LLM 调用都带一组版本号/内容 hash,并把最终 messages/tool schema 快照写入 trace/replay store。

**版本对象**:

| 对象 | 当前来源 | 版本化方式 |
|---|---|---|
| system prompt | `internal/prompt/decide_system.go`, `capability_system.go` | 每个 prompt 模板声明 `PromptID`, `Version`, `ContentHash` |
| context prefix | `NeedsStatePrefix`, 后续 `ContextManager.BuildMessages` | 声明 `ContextBuilderID`, `Version`, `PolicyHash`(token 窗口/摘要/降噪策略变化也算变更) |
| tool schema | `internal/agent/decide_tools.go` | 每个 tool schema 声明 `ToolSchemaVersion`, 对 JSON schema 做 canonical hash |
| output parser | `parseNeedDelta`, `parseUnderstanding`, `ResolveQuoteRef` 等 | 声明 `ParserVersion`, 防止解析逻辑变更导致 replay 结果不一致 |
| model binding | `conf/*.yaml` `agent_bindings` | 记录 binding key、provider key、model name、temperature/max_tokens |

**Manifest 设计**:

新增 `internal/versioning/manifest.go`:

```go
type PromptVersionSet struct {
    PromptID       string `json:"prompt_id"`
    PromptVersion  string `json:"prompt_version"`
    PromptHash     string `json:"prompt_hash"`
    ContextID      string `json:"context_id"`
    ContextVersion string `json:"context_version"`
    ContextHash    string `json:"context_hash"`
    ToolSchemaSet  string `json:"tool_schema_set"`
    ToolSchemaHash string `json:"tool_schema_hash"`
    ParserVersion  string `json:"parser_version"`
    ModelBinding   string `json:"model_binding"`
    ModelName      string `json:"model_name"`
}
```

版本号用 semver,hash 用 sha256 前 12 位。**版本号表达意图,hash 保证内容可追溯**:只要模板文本、context builder 策略、tool schema JSON、parser 白名单发生变化,hash 必变;是否升 semver 由代码 review 保证。

**运行存证**:
- 每次 `Chat` / `ChatStream` 调用都在 trace 里写 `prompt_version_set`。
- LLM 请求落 `LLMCallSnapshot`:system prompt hash、最终 messages hash、tools hash、模型名、temperature、tool_choice、输出 tool_calls、usage、trace_id/session_id/user_id。
- Debug/预发可保存完整 system/messages/tools;生产默认保存 hash + preview,遇到 badcase/负反馈时保存完整快照到 replay store。
- ToolCallSnapshot 扩展 `ToolSchemaHash`,确保历史工具调用能知道当时用的是哪版 schema。

**回放对比**:

新增 `cmd/replay` 或 `go test ./eval/replay`:

```bash
go run ./cmd/replay \
  --trace_id trace_xxx \
  --mode frozen        # 用当时 prompt/context/tool/model 重跑

go run ./cmd/replay \
  --trace_id trace_xxx \
  --mode compare \
  --target current     # 用当前版本重跑并生成 diff
```

对比维度:
- tool 是否一致(`tool_selection_diff`)
- tool arguments JSON diff(`args_diff`,重点看 `need_delta/search_mode/vehicle_ref`)
- 文本是否触发禁词/ID 泄露
- token/latency/cost 差异
- `confidence_action` / `filter_interpreter_used` 是否变化

**工程约束**:
- 改 `internal/prompt/**`, `internal/orchestration/context_manager.go`, `internal/orchestration/state.go` 中的 prefix 逻辑,或 `internal/agent/decide_tools.go` 时,CI 必须跑版本快照测试和相关 eval/replay。
- 如果内容 hash 变了但 manifest 版本号没变,测试失败,提示开发者升版本或明确标记 `metadata_only=true`。
- PR 描述必须写:变更对象(prompt/context/tool/parser)、旧版本、新版本、eval/replay 对比结果。

| # | 决策 | 替代方案 | 理由 |
|---|---|---|---|
| 1 | 责任链 Pipeline + Decider,不用 ADK supervisor | eino ADK supervisor + ReAct | 旧 scaffold + tyche V4 双重实战结论:supervisor 路由额外耗 token+latency 且常路由错;单 LLM 决策 + Go 编排 1-2 次/轮 |
| 2 | **纯 Go OpenAI-compatible client,不依赖 eino** | eino ChatModel 层 / 手写 | v2 不用 ADK/ReAct/graph,eino 退化为"一个带历史包袱的 OpenAI client";旧 scaffold 已踩坑(DeepSeek 流末才出 tool_calls,eino 默认 checker 漏判被迫绕路);纯 Go 完全掌控流式解析 + tool_calls 累积 + fallback;tyche 同业务验证了这条路 ~400 行 |
| 3 | LLM 抽象层(Provider/Factory) | 直接 import deepseek | LLM 可换;新增 provider 成本最低 |
| 4 | DeepSeek 默认 | 直接上 Claude/豆包 | 中文好、便宜、流式 function-calling 实测可用 |
| 5 | tyche MCP 直连,不在 saas-api 再造一套 | 自己直连 saas-api | tyche 已有 7 个 C 端工具,字段质量好,复用最省心 |
| 6 | **ID 由 Go 从 state 注入,工具 schema 不暴露 ID** | LLM 在 prompt 里自己保存 | 旧 scaffold 软约束易漂移;Go 托管硬保证不幻造 |
| 7 | 流式 function-calling 单流(content + tool_calls) | 等 LLM 调用结束整段下发 | 首字节从数秒降至 <1s |
| 8 | 不注册写操作 tool | create_order tool 给 LLM | 写操作风险高 |
| 9 | **规则检索接 AgentHub 平台,不自建知识库** | 本地 BM25 + knowledge/ 灌库 / 自建向量 | tyche 实战结论:知识由运营在平台维护不发版即生效、口径统一、检索质量平台调优、省合规;agent 只发 query 拿回知识文本,工程量从"切片/索引/灌库"降到"一个 HTTP client" |
| 10 | 占位 P5 工具默认不注册 | 注册占位常数工具 | 占位数字会被 LLM 当真报给用户,比没有更糟 |
| 11 | SSE 协议增量扩展 + 向后兼容 | 大版本切换 | 旧客户端按 `Accept` 头降级;新事件增量加 |
| 12 | 生产化基建(审核/锁/metric)P6 占骨架 | P6 大改架构 | 默认 PassThrough/FailOpen,P6 只填实现 |
| 13 | **Function calling 决策层必须 deepseek-chat,reasoner 只做无工具旁路** | DecideStage 直接绑 deepseek-reasoner | reasoner 不支持 function calling(决策层用不了)、首字节慢、输出贵;DecideStage 仍 chat,复杂筛选解析可用 FilterInterpreter(reasoner JSON),车型对比也可灰度(见 §3.2.1 / §4.1) |
| 14 | **需求→filter_codes 用静态映射表,不让 LLM 挑码** | LLM FilterSelector 从菜单挑(tyche v4) | MCP 不暴露 menu_group,没有白名单可校验 → LLM 编码会幻造无效码;filter_code 可枚举,规则更稳;0 延迟 0 token;未命中不臆造(见 §3.2) |
| 15 | **搜车追问/改口走 SearchIterationIntent + NeedDelta,Go 确定性执行** | 仅靠 history 让 LLM 自然理解"不喜欢/换一批/便宜点" | 搜车是连续筛选过程,必须把 page/refine/negative/budget/relax 分清;LLM 只做意图识别和结构化 delta,翻页、排除、预算档位、去重由 Go 基于 state 执行,避免重复推荐和条件丢失(见 §3.3) |
| 16 | **复杂自然语言→筛选需求用 FilterInterpreter(reasoner JSON) 旁路** | 全部搜索理解都只靠 DecideStage chat | Decide 不能绑 reasoner,但复杂筛选解析确实需要更强推理;用 reasoner 做无工具 JSON 归一化,再由 Go 校验并转 filter_codes,兼顾效果、成本和 function calling 约束(见 §3.2.1) |
| 17 | **置信度统一走 ConfidenceGate,不让低把握需求直接硬筛** | 各字段各用各的,靠 prompt 自觉 | 现有 confidence/sufficiency 是局部信号,需要统一决策为 search/interpret/ask/relaxed_search/fallback;避免低置信硬筛、重复追问和 reasoner 无节制调用(见 §3.4) |
| 18 | **Prompt/Context/Tool Schema 全部版本化并可回放对比** | 只靠 git diff 和日志文本排查 badcase | 模型行为由 prompt、context builder、tool schema、parser、model binding 共同决定;必须记录版本向量和最终输入快照,才能判断 badcase 是哪一层变更导致(见 §3.5) |

### 4.1 模型选型策略(chat vs reasoner)

**核心判断**:v2 是"工具决策 + 把工具返回的真实数据讲清楚"型 agent,不是全链路开放推理型。`deepseek-reasoner`(R1)**不支持 function calling**,所以 DecideStage 不能直接绑定 reasoner;但"复杂自然语言 → 车型/预算/排除条件"属于无工具 JSON 归一化任务,可以作为 `FilterInterpreter` 旁路使用 reasoner,再由 Go 校验并执行。

| 维度 | deepseek-chat (V3) | deepseek-reasoner (R1) |
|---|---|---|
| function calling | ✅ | ❌(致命:决策层用不了) |
| 流式 content+tool_calls 单流 | ✅ | ❌ |
| 首字节延迟 | 快 | 慢(先吐思维链) |
| 输出价格 | 便宜 | 贵数倍 |
| 适合 | 工具决策、话术生成、数据转述、简单结构化抽取 | 复杂筛选语义解析、多步权衡推理、无工具 JSON 归一化 |

**逐环节默认选型**:

| 环节 | 模型 | 理由 |
|---|---|---|
| DecideStage | deepseek-chat | 强依赖 function calling;要首字节快 |
| **FilterInterpreter** | **deepseek-reasoner** | 仅复杂筛选理解触发;不注册 tools,只输出 JSON;补足 chat 在多条件、否定、相对预算上的漏判 |
| Search 引导语 / GuideStage | deepseek-chat(或更便宜 lite) | 60 字话术,要快要便宜 |
| PriceDetail / Insurance | deepseek-chat | 照 charges/规则转述,非推理 |
| Rules | deepseek-chat | grounded 转述检索文本,反而要**抑制**模型自己推理 |
| Compare | deepseek-chat(可灰度 reasoner) | 多车权衡是 LLM #2,不需 function calling,技术上可换 reasoner;但默认先 chat,灰度对比效果再决定 |

**落地方式**:复用 Factory 的 `AgentBindings`,选型落配置不硬编码。`decide` 必须绑 chat;`filter_interpreter` 默认绑 reasoner;复杂筛选未命中触发条件时不调用 reasoner,避免每轮加成本和延迟。

---

## 5. 已知缺口与风险登记

| 风险 / 缺口 | 影响 | 规避 |
|---|---|---|
| **保险数据源**:tyche MCP `rental_search_quotes` 不含 `grantee_list`,`get_order_details` 只透出 charges(Type=3 保险费),拿不到保障细节 | 保险推荐只能给金额,讲不清保障范围 | P2 先用兜底文案("具体保障内容请在 App 内查看");后续推动 tyche MCP 透出 `RentalGuarantee` 或直连 saas-api |
| LLM 幻造关键 ID | 用错 ID 调工具失败 | Go 从 state 注入,schema 不暴露;`ResolveQuoteRef` 解析指代;15 分钟过期主动重搜 |
| 报价时效 | 用户拿过期价 | tool 返回"以下单为准";`state.QuoteAt` 15 分钟硬判 |
| LLM 幻觉条款 | 误导/投诉 | 强制走 AgentHub 检索 + 强 grounded prompt;检索无结果/平台不可用统一兜底转客服,不编造;防泄露铁律(内部运营内容不复述) |
| 技术错误透露给用户 | 体验差/信息泄漏 | tool 错误包 `{is_error,user_msg,debug}`,prompt 只透传 user_msg |
| 流式与异步审核 race | 命中审核但话术已流出 | 命中时 `audit.Done()` 触发,Decider `select{case <-audit.Done()}` 提前退出 |
| 分布式锁 Redis 故障 | 主链路阻塞 | `AccessLock.FailOpen=true`(默认)异常放行,只 warn |
| 对话摘要抹掉关键信息 | 多轮丢上下文 | 只压最早 2 条,保留 6 轮原文;前缀同时带 summary + last_quotes 双保险 |
| 写操作泄漏 | 资损 | 白名单驱动,只放只读工具;CI 检测 toolset 黑名单 |

---

## 6. 安全护栏(贯穿全程)

1. **严禁幻造数据**:`location_id` / `poi.*` / `reference_id` / `context_id` / `supplier` / `order_id` 只能来自对应工具返回原文。**v2 由 Go 托管这些 ID,LLM 不经手**(根除幻造,不靠 prompt 软约束)。唯一可由 LLM 推断的是 `date_time`。
2. **写操作禁忌**:严禁注册 `rental_create_order` / `pay` / `refund` / `modify_order` 到 LLM 可见 toolset(白名单维护,见 `internal/tools/common.go`)。
3. agent 不闭环下单,通过 deeplink 跳 App。
4. 报价答复必须显式说"以下单时为准"。
5. 保险话术 100% 基于真实工具返回,不允许 LLM 自由发挥保障范围。
6. 条款问答强制走 AgentHub 检索 + 强 grounded 生成,检索无结果/平台不可用引导转人工,严禁凭模型知识补规则;检索回内部运营内容不复述(防泄露)。
7. 工具返回 `is_error:true` 时只对用户说 user_msg,严禁透露 debug/技术错误/JSON 原文。
8. 违法操作 / 理赔申诉由 PreRoute 关键词 + prompt 拦截转人工;P6 接 ISM 异步审核。

---

## 7. Phase 拆分与索引

总共 8 个 Phase,P0-P4 是最小可生产闭环,P5-P7 是体验与生产化补强。每个 Phase 一份独立可执行 spec。

| Phase | 范围 | 工时 | 文档 |
|---|---|---|---|
| **P0** | 项目骨架:go.mod、目录结构、config、LLM 工厂(DeepSeek)、tyche MCP client、Pipeline/Stage/Capability 接口骨架、CLI 入口 | 2-3 天 | [phase0-scaffold.md](specs/phase0-scaffold.md) |
| **P1** | 导购 MVP:Decider 流式 function-calling + Search/Ask/PureReply Capability + ID 托管 + ResolveQuoteRef + state 闭环;CLI 跑通"明天北京 SUV → 报价" | 5-6 天 | [phase1-shopping-mvp.md](specs/phase1-shopping-mvp.md) |
| **P2** | 价格明细 + 保险 + 车型对比:PriceDetail / Insurance / Compare Capability(同源,基于 `get_order_details` + ResolveQuoteRef) | 4-5 天 | [phase2-price-insurance.md](specs/phase2-price-insurance.md) |
| **P3** | 规则解读:接 AgentHub 检索 client + RulesCapability(检索 + 强 grounded 生成),不自建知识库 | 3-4 天 | [phase3-rag-rules.md](specs/phase3-rag-rules.md) |
| **P4** | ~~HTTP 服务化~~ **[演进 2026-06-30] HTTP 基础已提前实现**(cmd/http + httphandler + SSE + MemoryStore + web 前端)。**剩余**:Redis session 持久化 + BuildStatePrefix 完善 + history 回放归入后续 | 2-3 天 | [phase4-http-session.md](specs/phase4-http-session.md) |
| **P5** | SSE 协议升级 + 引导胶囊 + 反馈:thinking_tips/box + quick_action/slot_patch + 反馈采集 | 3-4 天 | [phase5-sse-guide-feedback.md](specs/phase5-sse-guide-feedback.md) |
| **P6** | 生产化基建:异步审核管道 + 分布式锁 + 对话摘要 + LLM/Tool/Stage 三层 metric + Replay Store/回放对比 | 5-6 天 | [phase6-productionize-infra.md](specs/phase6-productionize-infra.md) |
| **P7** | Prompt 工程化:Prompt/Context/Tool Schema 版本化 + 场景知识库表 + required_slots + profile_patch + 中文铁律 | 3-4 天 | [phase7-prompt-knowledge.md](specs/phase7-prompt-knowledge.md) |
| **合计** | | **~33-43 天 / 7-9 周** | |

**补充分步计划 [2026-07-02]**:

| 编号 | 范围 | 工时 | 归属 |
|---|---|---|---|
| **P1.1** | 搜车迭代意图补强:`search_mode` schema、`filter_interpreter=deepseek-reasoner` 复杂筛选解析旁路、`ConfidenceGate` 置信度闸门、`ApplyIterationPolicy`、排除/去重状态、预算上下调价位档、`price_preference → total_fee` 映射、decide eval 覆盖"不喜欢/换一批/预算高低/混合改口/低置信追问" | 3-4 天 | [phase1-shopping-mvp.md](specs/phase1-shopping-mvp.md) |

**依赖关系**:P0 → P1 是硬前置;P2/P3 依赖 P1;P5 依赖 P4 剩余部分;P6 依赖 P1(可与 P4/P5 并行);P7 依赖 P1(建议 P4 之后)。

**[演进 2026-06-30] 实际进度**:
- P0 已完成
- P1 已完成,并在 P1 阶段一并实现了 **UserNeed + NeedDelta 结构化需求管理体系**(原未列入任何 Phase)和 **guide storelist 数据源切换**
- P4 的 HTTP 基础(cmd/http + httphandler + SSE + MemoryStore + web 前端)已提前实现;剩余 Redis session 归入后续
- **[补充 2026-07-02]** P1.1 尚未实现:需要把搜车过程中的"不喜欢/换一批/预算高低/条件放宽/低置信需求"从泛化自然语言理解升级为显式 `SearchIterationIntent + FilterInterpreter(reasoner JSON) + ConfidenceGate + NeedDelta + Go 确定性策略`。

**最小可生产灰度** = P0 + P1 + P2 + P4(剩余) + P6-审核/锁。

---

## 8. 项目结构(目标态)

```
.
├── cmd/
│   ├── cli/main.go         # P0:本地调试 CLI(保留,非主入口)
│   └── http/main.go        # HTTP + SSE 服务(已实现,原 P4 提前)
├── internal/
│   ├── agent/              # Pipeline + Stage + Decider + Capability + NeedState + FilterCode + ResolveQuoteRef
│   ├── orchestration/      # ConversationState 唯一定义(含 Constraints/TurnCount/LastSearch/CachedMenu)
│   ├── tools/              # tyche MCP tool wrap(Go 托管 ID)+ 白名单
│   ├── tyche/              # tyche MCP JSON-RPC client + guide storelist HTTP client
│   ├── llm/                # provider 工厂(deepseek 默认)+ 纯 Go OpenAI-compatible client + 流式日志层
│   ├── httphandler/        # HTTP handler + SSE emitter + middleware(已实现;原设计 internal/http/ 因与 stdlib 同名改名)
│   ├── session/            # Store 接口 + MemoryStore(已实现;Redis 实现后续补)
│   ├── agenthub/           # AgentHub 检索 client(P3,规则知识托管平台)
│   ├── guardrail/          # 异步审核管道(P6)
│   ├── metric/             # LLM/Tool/Stage 三层指标(P6)
│   ├── config/             # yaml + env 装载(含 GuideConf)
│   ├── types/              # 跨模块共享类型(UserNeed / NeedDelta / SearchConstraints / MenuGroupView)
│   └── prompt/             # decide_system / 各 Capability prompt / 场景知识库(P7)
├── conf/                   # dev / pre / prod 多环境 yaml
├── web/                    # 前端单页应用(已实现;vanilla HTML/CSS/JS,User ID 登录 + 对话 + 历史回溯)
└── docs/                   # 本方案 + 各 Phase spec
```

---

## 9. 可观测性设计(Trace / Logging / Metrics / Cost)

### 9.1 全链路 Trace(每请求一条完整轨迹)

每次 `/agent/chat` 请求生成一个 `trace_id`(入口中间件已有)。整条链路上所有组件把 trace_id **结构化地带进日志**,可用一个 trace_id 串起全部步骤：

```
trace_id=abc123
├─ [stage] PreRoute         0ms
├─ [stage] Decide           +230ms
│     └─ [llm] decide       prompt_tokens=3842 completion_tokens=312 cache_hit=2100
│                            model=deepseek-chat duration_ms=1850 finish_reason=tool_calls
│                            tool_called=search_vehicles
├─ [stage] Capability       +2080ms
│     ├─ [tool] rental_search_locations    args={keyword:"首都机场"} duration_ms=120 status=ok
│     ├─ [tool] rental_resolve_poi         args={location_id:"xxx"} duration_ms=85 status=ok
│     └─ [tool] rental_search_quotes       args={...} duration_ms=680 status=ok quotes=3
│     └─ [llm] search_guide  prompt_tokens=1520 completion_tokens=180 duration_ms=920
├─ [stage] Guide            +3200ms   (if applicable)
│     └─ [llm] guide         prompt_tokens=980 completion_tokens=120 duration_ms=650
├─ [stage] Finalize         +3850ms
│     └─ session_save=ok summarize=false tokens_this_turn=5854
└─ done                     total_ms=3900
```

### 9.2 结构化日志(每次 LLM/Tool 调用落一条)

#### LLM 调用日志(internal/llm/openai_client.go 收尾时写)

| 字段 | 说明 |
|---|---|
| `trace_id` | 全链路串联 |
| `session_id` | 会话维度 |
| `user_id` | 用户维度 |
| `stage` | "decide" / "search_guide" / "price_detail" / "insurance" / "rules" / "guide" |
| `model` | "deepseek-chat" |
| `prompt_tokens` | 从 response usage 取 |
| `completion_tokens` | 从 response usage 取 |
| `cache_hit_tokens` | DeepSeek 前缀缓存命中(有此字段时) |
| `total_tokens` | prompt + completion |
| `duration_ms` | 从发起到收尾 |
| `finish_reason` | "stop" / "tool_calls" / "length" |
| `status` | "ok" / "fallback_sync" / "error" |
| `prompt_preview` | system prompt 前 200 字(脱敏) + messages 条数 + 末条 user 前 100 字 |
| `response_content` | completion 全文(用于 badcase 回放)——**生产环境按 cfg 可关** |
| `response_tool_calls` | `[{name, arguments}]` JSON(用于回放) |
| `error_msg` | 失败时错误信息 |

> **流式特殊处理**:流式调用收尾时(`defer + 累积 buffer`)一次性落这条日志。需要自写 `CopyCtxWithSameTraceID`(原 ctx 在请求结束后被 cancel,参考 tyche 做法)。

#### Tool 调用日志(internal/tools/logging.go 包装层写)

| 字段 | 说明 |
|---|---|
| `trace_id` / `session_id` / `user_id` | 同上 |
| `tool_name` | "rental_search_locations" 等 |
| `capability` | 从哪个 Capability 调的("search" / "price_detail" / ...) |
| `arguments` | 入参 JSON(可截断) |
| `response_preview` | 出参前 500 字(可截断) |
| `is_error` | 工具返回 is_error:true |
| `duration_ms` | tyche RPC 耗时 |
| `status` | "ok" / "error" |

#### Pipeline Stage 日志(ChatPipeline.Run 每 stage 前后打)

| 字段 | 说明 |
|---|---|
| `trace_id` / `session_id` | 同上 |
| `stage` | Stage.Name() |
| `signal` | "continue" / "stop" |
| `duration_ms` | stage 执行耗时 |
| `stage_info` | stage 内填的摘要 map(如 Decide: `{tool: "search_vehicles", reply_len: 85}`) |

### 9.3 Metrics(聚合指标,用于 dashboard 和告警)

`internal/metric/` 包,本地先用 `expvar` / Prometheus-compatible 文本,P6+ 接公司 metric SDK。

#### LLM 维度

| 指标 | 类型 | labels | 说明 |
|---|---|---|---|
| `llm_calls_total` | counter | `stage, model, status` | LLM 调用次数 |
| `llm_tokens_total` | counter | `stage, model, type=prompt\|completion\|cache_hit` | token 用量 |
| `llm_duration_ms` | histogram | `stage, model` | 调用延迟分布 |
| `llm_cost_yuan` | counter | `stage, model` | 估算成本(按定价公式实时算) |

#### Tool 维度

| 指标 | 类型 | labels | 说明 |
|---|---|---|---|
| `tool_calls_total` | counter | `name, capability, status` | 工具调用次数 |
| `tool_duration_ms` | histogram | `name` | RPC 延迟 |
| `tool_error_total` | counter | `name, error_type` | 工具错误(is_error / timeout / network) |

#### Pipeline/会话 维度

| 指标 | 类型 | labels | 说明 |
|---|---|---|---|
| `stage_duration_ms` | histogram | `stage` | 每 stage 耗时 |
| `pipeline_duration_ms` | histogram | — | 单轮总耗时(首帧到 done) |
| `session_turns_total` | counter | — | 会话轮次 |
| `session_tokens_total` | counter | — | 单会话累计 token |
| `first_byte_ms` | histogram | — | 首字节延迟(用户感知) |

#### 成本维度

| 指标 | 类型 | labels | 说明 |
|---|---|---|---|
| `cost_per_turn_yuan` | histogram | `stage` | 单轮成本(prompt×单价 + completion×单价 - cache_hit×折扣) |
| `cost_per_session_yuan` | histogram | — | 单会话成本(state.TokenUsage 算) |
| `cost_daily_yuan` | gauge | — | 当日累计成本 |

### 9.4 成本估算公式

```go
// internal/metric/cost.go
func EstimateCost(usage UsageRecord) float64 {
    // DeepSeek 定价(2024.12):
    //   输入: ¥1/M token    缓存命中: ¥0.1/M token
    //   输出: ¥2/M token
    promptCost := float64(usage.PromptTokens-usage.CacheHitTokens) * 1.0 / 1_000_000
    cacheCost  := float64(usage.CacheHitTokens) * 0.1 / 1_000_000
    completionCost := float64(usage.CompletionTokens) * 2.0 / 1_000_000
    return promptCost + cacheCost + completionCost
}
```

> 定价系数放 config,provider 切换时改系数即可。

### 9.5 预算控制(P6 实现,本节预定接口)

```go
// internal/agent/budget.go
type BudgetChecker interface {
    // Check 在调 LLM 前检查预算是否允许
    Check(ctx context.Context, uid string) (allowed bool, remaining int, err error)
    // Consume 在 FinalizeStage 记录本轮消耗
    Consume(ctx context.Context, uid string, tokens int) error
}
```

三道闸:
1. **用户日限额**(500k token/天)→ 超限返回友好提示,不调 LLM
2. **单会话限额**(100k token)→ 引导新开会话
3. **全局日熔断**(按 budget 设)→ 降级:闲聊模板回复不走 LLM,搜车仍走

### 9.6 告警规则(P6+)

| 规则 | 阈值 | 含义 |
|---|---|---|
| 单请求 token 异常高 | prompt_tokens > 10k 或 completion > 2k | history 失控 / prompt 注入攻击 |
| 单 session LLM 调用异常 | > 30 次/session | 逻辑 bug(死循环)或攻击 |
| 全局 token 增速异常 | 5min 窗口 > 正常均值 3× | 流量突增或攻击 |
| LLM 成功率下降 | < 95% | provider 故障 |
| 首字节 P95 劣化 | > 3s | 模型侧延迟 / 网络问题 |
| 前缀缓存命中率突降 | < 40%(正常 60-80%) | system prompt 被改坏 |

### 9.7 落地路径

| 什么 | 在哪个 Phase | 备注 |
|---|---|---|
| openai_client 收尾取 usage 记日志 | **P0**(client 自带) | 最基础的"每次 LLM 调用记了什么" |
| tool 调用日志层 | **P0**(logging.go) | 记 tool name/args/duration/status |
| Pipeline stage start/done 日志 | **P1**(ChatPipeline.Run) | 带 stage_info |
| state.TokenUsage 累计 | **P4**(FinalizeStage) | 会话级累计 |
| metric 包 + 全部指标上报 | **P6** | Prometheus-compatible |
| BudgetChecker 三道闸 | **P6** | Redis INCRBY |
| 告警规则 | **P6+** | 接公司告警平台 |
| 成本 Dashboard | **P6+** | 接公司 metric 平台 |

---

## 10. Prompt 评测体系(Eval)

### 10.1 设计目标

不启动项目(不需要 tyche / Redis / HTTP server),只要能连 LLM(DeepSeek),就能跑 prompt 效果评测。用 `go test ./eval/...` 驱动,**像跑单测一样跑 eval**,在 CI / 本地都能执行,每次 prompt 改动前后对比。

### 10.2 Eval 目录结构

```
eval/
├── eval.go              # 核心框架:EvalRunner / EvalCase / Judge / Report
├── eval_test.go         # go test 入口(跑全部 case)
├── judges.go            # 评判函数库:包含匹配/LLM-as-judge/结构校验等
├── helpers.go           # 工具:调 LLM、解析 tool_calls、mock state 构造
├── testdata/
│   ├── decide/          # DecideStage prompt 评测 case
│   │   ├── search_basic.yaml
│   │   ├── search_multi_turn.yaml
│   │   ├── ask_info_insufficient.yaml
│   │   ├── price_detail.yaml
│   │   ├── insurance.yaml
│   │   ├── rules.yaml
│   │   ├── chitchat.yaml
│   │   ├── out_of_scope.yaml
│   │   └── id_hallucination.yaml    # 专项:验证不幻造 ID
│   ├── guide/           # GuideStage 引导语 eval case
│   ├── capability/      # 各 Capability 的二次 LLM prompt eval case
│   └── regression/      # badcase 回归集(从反馈胶囊收集)
└── report/              # 运行结果输出(JSON/Markdown)
```

### 10.3 EvalCase 格式(YAML)

```yaml
name: "搜车_基础_明天北京SUV"
category: decide         # decide / guide / capability
tags: [search, basic, p1]

# 输入
system_prompt: "{{decide_system}}"   # 引用模板名,运行时渲染
messages:
  - role: user
    content: "明天下午想在北京首都机场租辆SUV,两天"
state_prefix: |           # 可选:模拟 BuildStatePrefix 的内容
  ## 当前会话状态
  【当前时间】2026-06-29 14:00 周日
  【当前取还车】（未设置）

# 期望
expects:
  tool_called: search_vehicles        # 期望调哪个工具(或 "none" = 纯回复)
  tool_not_called: [get_price_detail, insurance]   # 不应调的工具
  args_contain:                        # tool_call args 必须含/不含的字段
    must_have: [scene_tags]
    must_not_have: [context_id, reference_id, supplier]
  reply_contains: []                   # content 里必须出现的关键词
  reply_not_contains: [context_id, reference_id, fake_, "500"]  # 不应出现的(ID 泄漏/幻造检测)
  reply_max_length: 300                # content 字数上限
  
# 可选:LLM-as-judge 评判
judge:
  enabled: true
  criteria: |
    1. 是否正确选择了 search_vehicles 工具
    2. 话术是否自然口语(不像机器人)
    3. 是否泄漏了任何 ID/技术字段
    4. 是否承诺了绝对化用词(100%/一定/保证)
  pass_threshold: 0.8   # 评分 ≥ 此值视为通过
```

### 10.4 评判函数(Judges)

| Judge | 类型 | 说明 |
|---|---|---|
| `ToolCallJudge` | 规则 | 检查 tool_called / tool_not_called / args 白名单黑名单 |
| `ContentJudge` | 规则 | reply_contains / reply_not_contains / max_length / 禁词表 |
| `IDLeakJudge` | 规则 | 专项:response 里不应出现任何 `reference_id=` / `context_id=` / 长度>20 的 hex/uuid 模式 |
| `FormatJudge` | 规则 | 结构校验:返回了合法 JSON tool_calls / finish_reason 符合预期 |
| `LLMJudge` | LLM | 用另一个 LLM(可以是同模型低温度)按 criteria 打分(0-1),超 threshold 为 pass |
| `RegressionJudge` | 规则 | 对比本次输出与历史 baseline:tool 选择一致 / 关键词稳定 |
| `CostJudge` | 规则 | prompt_tokens / completion_tokens 不超上限(检测 prompt 膨胀) |

### 10.5 运行方式

```bash
# 跑全部 eval(需要设 DEEPSEEK_API_KEY 环境变量)
DEEPSEEK_API_KEY=sk-xxx go test ./eval/... -v -timeout 5m

# 只跑 decide 类目
go test ./eval/... -run TestEval/decide -v

# 只跑某个具体 case
go test ./eval/... -run TestEval/decide/search_basic -v

# 生成报告
go test ./eval/... -v -json > eval/report/latest.json
```

**环境要求**:只需 `DEEPSEEK_API_KEY`。不需要 tyche / Redis / 任何后端。

### 10.6.1 聚合指标(报告顶层)

> **演进记录 [2026-06-29]**:参考火山引擎 UP-ReAct 文章补充"工具调用准确率"和"中间态阻断率"两个维度。文章观点:生产级 Eval 不能只看"回答好不好",必须把**决策层选对工具**这件最影响体验的事变成可量化指标。我们最担心的"对比意图被误塞进 search"正是这类问题。

除 `pass_rate` 外,报告顶层额外输出以下聚合指标:

| 指标 | 计算 | 含义 |
|---|---|---|
| `tool_selection_accuracy` | 预期 `tool_called` 与实际一致的 case 数 / 有 `tool_called` 期望的总 case 数 | **决策层选对工具的比例**——最影响用户体验的单一指标;低于 90% 说明 prompt 或工具 schema 有问题 |
| `wrong_tool_rate` | `tool_not_called` 被违反的 case 数 / 有 `tool_not_called` 期望的总 case 数 | **误选工具的比例**(比如"对比意图走了 search"/"闲聊却调了 tool")——这比漏选更危险,因为用户会拿到错误结果 |
| `id_leak_rate` | IDLeakJudge 失败的 case 数 / 总 case 数 | **ID 泄漏/幻造率**——安全硬指标,必须 = 0 |

报告 JSON 示例追加:
```json
{
  "tool_selection_accuracy": 0.92,
  "wrong_tool_rate": 0.04,
  "id_leak_rate": 0.0,
  ...
}
```

### 10.6 报告输出

每次运行生成结构化报告:

```json
{
  "run_at": "2026-06-29T14:00:00Z",
  "model": "deepseek-chat",
  "total": 25,
  "passed": 22,
  "failed": 3,
  "pass_rate": 0.88,
  "total_tokens": 45000,
  "total_cost_yuan": 0.06,
  "cases": [
    {
      "name": "搜车_基础_明天北京SUV",
      "status": "pass",
      "tool_called": "search_vehicles",
      "reply_preview": "好的,我来帮你看看首都机场附近的 SUV...",
      "tokens": { "prompt": 3200, "completion": 150 },
      "duration_ms": 1850,
      "judges": { "ToolCallJudge": "pass", "IDLeakJudge": "pass", "LLMJudge": 0.92 }
    },
    {
      "name": "闲聊_你好",
      "status": "pass",
      "tool_called": "none",
      "reply_preview": "你好!我是租车助手小租...",
      ...
    }
  ]
}
```

同时输出 Markdown 摘要(适合 PR comment / 群通知)。

### 10.7 核心 Eval 场景(初始 case 集)

| 类目 | case 数 | 覆盖点 |
|---|---|---|
| **decide/search** | 5 | 首轮搜车 / 多轮续搜 / 翻页 / 信息够直接推 / 需要追问 |
| **decide/search_iteration** | 6 | 不喜欢具体车 / 不要某品牌 / 换一批去重 / 预算低一点 / 预算高一点 / 混合改口 |
| **decide/confidence_gate** | 4 | 低置信推断降级 / 灰区触发 FilterInterpreter / 高置信直接搜 / coverage 低时 ask 或宽松搜索 |
| **decide/ask** | 4 | 信息不足反问 / 选项合理性 / 不重复问已答维度 / 跳过即作罢 |
| **replay/versioning** | 4 | prompt 变更 / context prefix 变更 / tool schema 变更 / parser 变更后的 frozen vs current diff |
| **decide/price_detail** | 3 | 看明细 / vehicle_ref 自然语言 / 多义指代 |
| **decide/insurance** | 2 | 问保险 / 驾龄推荐逻辑 |
| **decide/rules** | 2 | 规则问答 / 越界不进 rules |
| **decide/chitchat** | 3 | 闲聊0工具 / 越界拒绝 / 不暴露AI身份 |
| **decide/id_hallucination** | 3 | prompt 注入假ID → 不泄漏 / 不幻造 / 无 ID 字段输出 |
| **decide/redline** | 3 | 不绝对化 / 不贬低竞品 / 理赔转人工 |
| **guide/引导语** | 3 | ≤60字 / 口语化 / JSON 胶囊格式合法 |
| **regression/** | 动态 | 从反馈胶囊收集的 badcase 转入 |
| **合计** | ~42 | |

### 10.8 与开发流程的集成

| 时机 | 做什么 |
|---|---|
| **改 prompt 前** | 先跑 eval 记录 baseline |
| **改 prompt 后** | 再跑 eval,对比 pass_rate / 回归 case 是否挂 |
| **PR 提交** | CI 自动跑 eval,pass_rate < 阈值(如 85%)阻断合入 |
| **定期(周)** | 跑全量 eval + LLM-as-judge,输出趋势报告 |
| **badcase 收到** | 从反馈胶囊/人工标注转成 regression case,永久回归 |

### 10.9 落地路径

| 什么 | 在哪个 Phase |
|---|---|
| eval 框架骨架(EvalRunner / YAML 加载 / ToolCallJudge / ContentJudge / IDLeakJudge) | **P1**（和 decide_system prompt 一起,改 prompt 必先有 eval） |
| 初始 decide case 集(~15 条) | **P1** |
| LLMJudge | **P4**（有足够 case 积累后再上,避免早期 cost 高） |
| guide eval case | **P5** |
| BudgetJudge / CostJudge | **P6** |
| CI 集成 + PR 阻断 | **P6** |
| regression case 自动从反馈转入 | **P5+P6** |

---

## 11. 编码规范

- **ConversationState 唯一定义**:只在 `internal/orchestration/state.go`,任何子包 import。
- **Stage / Capability 统一签名**:`Stage.Handle(ctx, *AgentContext) (Signal, error)`;`Capability.Run(ctx, CapabilityInput) (*CapabilityResult, error)`。加能力不改 Pipeline 主体。
- **Tool 描述写给 LLM 看**:中文 OK,写清"何时调、必填项、返回什么";decide tool schema **不含 ID 字段**。
- **错误处理**:tool 内 error 转 `{is_error,user_msg,debug}`,user_msg 是人话。
- **配置**:pre/prod 走 `${ENV}` 占位;dev 允许明文 key(入库前自查)。
- **import 别名**:不加别名,唯一例外是同文件 import 两个同名包消歧。
- **prompt 即代码**:≥3 处用到的领域规则抽成 Go 表 + render 函数,不在字符串里散落(P7)。
- **日志必含结构化字段**:任何 LLM / tool / stage 日志必须带 `trace_id` / `session_id` / `user_id`(见 §9)。
- **工具结果入 history 必须降噪**:FinalizeStage 写 history 时,ToolCall.Result 永远存精炼摘要(如"已展示3辆车:朗逸¥198/天、卡罗拉¥215/天"),**绝不存 tyche 原始 JSON / debug 栈**——这是多轮对话最大的 token 膨胀源,也是避免 Context Thrashing(注意力稀释)的关键(见 §3.1)。
- **所有调 LLM 的地方走 ContextManager.BuildMessages**,不直接操作 state.History 构造 messages;确保 token 预算窗口 + 降噪 + 分级驱逐三层策略全局生效。

---

## 附录 A. 方案演进日志

> 每次方案改进都在此留痕:改了什么、为什么、解决/提升了什么、参考来源。便于项目回溯时看出当时做了哪些思考,避免"不知道当初为什么这么设计"。

| 日期 | 改进项 | 触发原因 | 改了什么 | 解决了什么问题 / 带来什么好处 | 参考来源 |
|---|---|---|---|---|---|
| 2026-06-29 | **去 eino,纯 Go LLM client** | v2 不用 ADK/ReAct/graph,eino 退化成"带包袱的 OpenAI client";旧 scaffold 踩坑(流末 tool_calls 漏判) | §2 技术栈、§4 决策 #2、P0 §2.3 全部改写;LLM 调用层改为 ~400 行纯 Go `openai_client.go` | **完全掌控流式解析**:tool_calls 累积 + syncFallback 自己控制,不再绕框架;消除 eino 依赖(减少二进制 + 升级负担) | tyche `library/llm/client.go` 同业务验证 |
| 2026-06-29 | **Token 预算 & 成本方案** | 需要提前估算和监控 LLM 成本,不能等上线后才发现账单 | 总纲 §9 新增 7 个子节(Token 构成 → 预估公式 → 三层监控 → 三道闸预算 → 前缀缓存 → 告警 → 落地路径);P0 `ChatResponse.Usage` 拆 prompt/completion/cache_hit 三字段;P6 D2 展开为完整实现清单 | **成本可控可预测**:单轮 ¥0.004-0.008/次,一次会话 ¥0.04-0.07;**缓存命中省 60%**;超限三道闸(用户日/会话/全局)兜底 | DeepSeek 前缀缓存文档 + tyche 实测数据 |
| 2026-06-29 | **Eval 评测体系** | prompt 改动缺乏量化验证,只能"看一看觉得还行" | 总纲 §10 新增完整 Eval 方案:YAML case 声明式 + 6 种 Judge + JSON 报告 + CI 阻断;P1 验收加 eval 通过要求 | **prompt 改动可回归**:改前跑 baseline → 改后对比 pass_rate;badcase 转 regression case 永久回归;CI 门禁(P6) | 行业通用做法 + tyche 评测集思路 |
| 2026-06-29 | **Eval 聚合指标:tool_selection_accuracy + wrong_tool_rate** | 参考火山引擎 UP-ReAct 文章"生产级 Eval = 代码判分 + 中间态阻断率 + 工具调用准确率" | §10.6.1 新增 3 个聚合指标(工具选对率/误选率/ID泄漏率) | 把"决策层选对工具"这件**最影响用户体验**的事变成可量化、可回归的硬指标;能精准捕捉"对比意图被误塞进 search"这类问题 | [火山引擎 AI 搜索 UP-ReAct](https://mp.weixin.qq.com/s/hol76ebv7-OB5TNUIWVVYA) |
| 2026-06-29 | **条款规则从本地 BM25 改接 AgentHub** | 对比 tyche 实战:本地灌库维护成本高、口径易漂移、检索质量靠自己调 | P3 spec 整篇重写(去 rag/knowledge/Retriever,改 `internal/agenthub/` client);总纲 7 处同步更新 | **知识不发版即生效**(运营在平台维护);口径统一;省合规审批;工程量从"切片/索引/灌库"降到"一个 HTTP client" | tyche `library/agenthub/client.go` |
| 2026-06-29 | **车型对比能力(CompareCapability)** | 原方案缺失:用户说"朗逸和轩逸哪个好"会被误塞进 search 或让 LLM 脑补(违反"数据来自工具"红线) | P1 工具集 5→6 加 `compare_vehicles` 占位;P2 加 PR 2.3 CompareCapability(并发取明细 + LLM 综合);P5 加 compare 胶囊点击短路;总纲同步 | **覆盖高频决策场景**;两条路径(自然语言 + 胶囊点击)统一收口;印证"并行 tool_calls + 一次综合,不需要 planner/ReAct"的选型 | tyche `vehicle_compare.go` |
| 2026-06-29 | **模型选型策略:主链路 chat,reasoner 不进 function calling 决策层** | DeepSeek reasoner 不支持 function calling → 决策层直接用不了;贵+慢 | 总纲 §4.1 新增完整选型策略;§4 决策 #13;P0 config 示例加 agent_bindings 注释。2026-07-02 调整为:DecideStage 仍 chat,复杂筛选解析可走 FilterInterpreter(reasoner JSON) | **避免误用 reasoner 导致决策层挂掉**(铁律);成本可控;复杂筛选可用更强模型,但不参与 tool calling | DeepSeek API 文档 |
| 2026-06-29 | **filter_code 静态映射(§3.2)** | tyche MCP 不暴露 menu_group → 没有白名单给 LLM 校验 → LLM 自由编码会幻造无效码 | 总纲 §3.2 新增完整设计;§4 决策 #14 | **0 延迟 0 token**(比 tyche 的 LLM 兜底 ~1.9s 更快);不幻造;加筛选项只需在表里加一行 | tyche `static_recall.go` + `attribute_resolver.go` |
| 2026-06-29 | **Context Manager 统一上下文管理(§3.1)** | 参考火山引擎文章:history/前缀/摘要散落三处;多轮搜车后报价 JSON 堆积导致 Context Thrashing 和注意力稀释 | §3.1 新增完整设计(token 预算窗口 + 工具降噪 + 分级驱逐);§11 加两条编码规范 | **避免长会话 token 失控**;文章实测"洗干净上下文反而更准——TTFT 降 30%";统一入口便于全局优化 | [火山引擎 AI 搜索 UP-ReAct](https://mp.weixin.qq.com/s/hol76ebv7-OB5TNUIWVVYA) |
| 2026-06-30 | **HTTP 服务化提前 + 包名 httphandler** | 用户要求"不需要 CLI 调试了,直接做 HTTP + 前端页面" | P4 的 HTTP 基础提前到 P0 之后实现:新增 `cmd/http/main.go` + `internal/httphandler/`(SSE handler/middleware) + `internal/session/`(MemoryStore) + `web/index.html`(前端 SPA);包名从设计中的 `internal/http/` 改为 `internal/httphandler/` 避免与 stdlib 同名 | **跳过 CLI 阶段直接可视化调试**;前端支持 User ID 登录 + 对话 + 历史回溯;包名改动遵守"不加 import 别名"规范 | 用户需求 |
| 2026-06-30 | **引入 UserNeed + NeedDelta 结构化需求管理** | 原 `search_vehicles` 只有 `scene_tags` 参数,完全没有结构化需求抽取;搜索无筛选条件 | 新增 `types/need.go`(UserNeed/NeedDelta/SearchConstraints)、`agent/need_state.go`(ApplyDelta/TickNeeds/ConflictDecay/FilterActiveNeeds);search_vehicles schema 改为 need_delta 增量数组 + understanding 自评;Decision 增加 NeedDelta/Understanding 字段;state 增加 Constraints/TurnCount/LastSearch/CachedMenu;prompt 大幅增强 | **跨轮需求累积**:用户第 2 轮"换电车"能衰减旧 SUV、ADD 新能源;**结构化筛选**:need→filter_code→精准搜索;**自评放行**:避免信息不够硬推 | tyche `common/dto/agent/internal.go` + `logic/agent/search/need_state.go` |
| 2026-06-30 | **报价数据源:guide storelist + MCP fallback** | tyche MCP 不暴露 menu_group → StaticRecall 无法做菜单白名单校验 | 新增 `tyche/guide_client.go` 调 rental-guide `/car/rental/guide/store/list/agent`;SearchCapability 报价改走 guide(返回 menu_group + veh_rates);MCP 作 fallback;`filtercode.go` 改为 `StaticRecall(needs, menu)` 支持白名单;config 增加 `GuideConf` | **有了 menu_group**:StaticRecall 可校验生成的 filter_code 是否在菜单中存在,防无效码;guide 返回更完整的车型数据 | tyche `logic/agent/tools/guide_store_list_client.go` |
| 2026-07-02 | **复杂筛选解析引入 FilterInterpreter(reasoner JSON)** | 用户问"自然语言到车型筛选"是否会使用推理能力更强的 DeepSeek;原设计与普通对话一样走 decide chat | 总纲 §3.2.1 / §4.1 新增 FilterInterpreter;config 增加 `deepseek-reasoner` provider 和 `filter_interpreter` binding;P1.1 计划/P1 spec 增加触发、校验、回退、验收 | 复杂多条件、否定、相对预算、连续不满意等场景可用更强模型解析;仍不让 reasoner 做 function calling 或生成 filter_code,保证稳定和可控成本 | DeepSeek reasoner 不支持 function calling 的模型约束 + 本项目筛选链路设计 |
| 2026-07-02 | **Prompt/Context/Tool Schema 版本化 + 回放对比** | 用户提出每次 prompt、context prefix、tool schema 变更都要可追踪版本并支持回放对比 | 总纲 §3.5 新增 PromptVersionSet / manifest / LLMCallSnapshot / replay diff;P7 增加 `internal/versioning`;P6 增加 Replay Store 和 `cmd/replay` | badcase 能恢复当时完整模型输入和工具协议;PR 能看 frozen vs current diff;定位问题从"感觉 prompt 变了"变成"明确是哪一层版本导致" | 12-Factor Agents / Context Engineering / Eval & Tracing 学习笔记 |
