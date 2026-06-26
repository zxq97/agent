# Phase 3.5.2 — 决策核心重构(Decider + Capability + ID 托管)

> 隶属 [phase3.5 重构路线图](phase3.5-decide-capability-refactor.md) 的第 2 步,是整套改造的**承重墙**。**可独立执行**(完成后即是"单 LLM 决策 + Go 编排 + ID 托管"形态)。
>
> **工时:** 5-7 天 · **PR 数:** 4(A1~A4) · **前置依赖:** [Phase 3.5.1](phase3.5.1-cleanup.md) 已合入

---

## 1. 动机

P3 当前架构(supervisor + ChatModelAgent ReAct)实测问题:

| 问题 | 表现 |
|---|---|
| LLM 调用次数多 | 一次"明天北京租 SUV 报价"≈ 6 次:Supervisor 路由 + ShoppingAgent ReAct 内 locations→poi→quotes→话术 + Supervisor 收尾 |
| 闲聊/越界也走全套 | "你好"也要 Supervisor 一轮 + 子 agent 一轮 |
| ID 幻造风险高 | `context_id`/`reference_id` 靠 prompt 让 LLM 在 assistant 文本里"自己保存",多轮易丢/改/编(护栏实质是软约束) |
| 话术不连贯 | Supervisor 与子 agent 各生成一段文字拼接,语气割裂 |

**借鉴 tyche V4 的两点**:
- **决策范式**:抄 `DecideStage` 一次流式 function-calling —— content 出话术 + tool_calls 选动作,**不进 ReAct 循环**。
- **ID 托管**:ID 存 state 结构化字段、Go 注入工具入参、不暴露给 LLM;`ResolveQuoteRef` 把"第一辆/朗逸"翻译成 reference_id。

---

## 2. 目标形态

```
用户输入 + state
   │
   ▼ PreRoute(Go,零LLM:反问解挂)
   ▼ DecideStage(LLM #1,流式 function-calling,工具集 5 个,tool_choice=auto)
   │   ├─ 不调 tool ──► PureReplyCapability  (0 LLM,闲聊/越界)
   │   ├─ ask ────────► AskCapability        (0 LLM)
   │   ├─ search ─────► SearchCapability     (Go 编排 + LLM #2 引导语)
   │   ├─ get_price_detail► PriceDetailCapability (Go + LLM #2)
   │   ├─ insurance ──► InsuranceCapability  (Go + LLM #2)
   │   └─ interpret_rules► RulesCapability    (RAG + LLM #2)
   ▼ Finalize(Go:落 state、写 history)
```

**LLM 调用从 ~6 次降到 1-2 次。**

---

## 3. 落地步骤(4 个 PR)

| PR | 内容 | 关键文件 |
|---|---|---|
| **A1** | State 扩展 + ResolveQuoteRef | `internal/orchestration/state.go`、新增 `internal/agent/resolve.go` |
| **A2** | Decider(**直接走流式 `ChatModel.Stream`**)+ 5 Capability 骨架 + decide_system prompt | 新增 `internal/agent/{decide,capability,cap_*}.go`、`internal/prompt/decide_system.go` |
| **A3** | tyche_wrap schema 改写 + ID 注入 | `internal/tools/tyche_wrap.go`、`internal/tools/common.go` |
| **A4** | handler 切流量(灰度开关)+ 删 supervisor 代码 + 改 CLAUDE.md 护栏 | `internal/http/handler.go`、`internal/agent/adk.go`、`CLAUDE.md` |

---

## 4. 详细设计

### 4.1 `internal/orchestration/state.go` 扩展(A1)

把"用户说第一辆"这类指代真正能解析出 reference_id 所需的最小信息集存进 state,并加 `context_id` / `quote_at`(15 分钟时效判定)。

```go
// QuoteRef 一条报价的最小指代信息。LLM 不直接读 ReferenceID,
// 只在被指代时由 ResolveQuoteRef(state, userText) 翻译。
type QuoteRef struct {
    ReferenceID string
    Supplier    string
    CarName     string
    BrandName   string
    DailyPrice  float64
    TotalPrice  float64
    Index       int      // 1-based 序号,匹配"第一辆/第 2 个"
}

// RentalCtx 一对取还车 + 它的 context_id。
// context_id 由 rental_search_quotes 返回后由 Go 写入,不依赖 LLM。
type RentalCtx struct {
    PickupCityID  int
    PickupName    string
    PickupTime    time.Time
    DropoffCityID int
    DropoffName   string
    DropoffTime   time.Time
    ContextID     string  // ← Go 注入到下游工具入参
}

type ConversationState struct {
    mu sync.Mutex
    // ... 已有字段 ...

    Rental       RentalCtx   // 取还车 + context_id(替代旧 Slot 散字段;Slot 仍保留过渡)
    LastQuotes   []QuoteRef  // 上一轮报价
    QuoteAt      time.Time   // 15 分钟时效判定
    SelectedRef  string      // 用户已锁定的报价(说"看第一辆明细"后 Go 解析填)
    LastQuoteIDs []string    // Deprecated: 用 LastQuotes 替代
}

// 新增 helper(都持锁):
func (s *ConversationState) SetQuotes(ctxID string, quotes []QuoteRef)
func (s *ConversationState) SnapshotQuotes() (ctxID string, quotes []QuoteRef, age time.Duration)
func (s *ConversationState) SelectQuote(ref string)
func (s *ConversationState) IsQuoteStale(ttl time.Duration) bool
```

> **设计要点:** 工具 schema 里**没有** `context_id`/`reference_id` 字段,LLM 不读、不填、不写。

### 4.2 `internal/agent/resolve.go` 新增(A1)

```go
// ResolveQuoteRef 把"第一辆/朗逸/那辆 SUV"翻译成 reference_id。纯 Go,不调 LLM。
// 返回三种状态:
//   命中 1 个:ref != "", clarify == nil
//   命中多个:ref == "", clarify != nil(让用户选)
//   命中 0 个:ref == "", clarify == nil(调用方降级)
func ResolveQuoteRef(state *ConversationState, userText string) (ref string, clarify *Clarification) {
    _, quotes, age := state.SnapshotQuotes()
    if age > 15*time.Minute {
        return "", nil // 报价过期,调用方应触发重搜
    }
    matches := matchQuotes(userText, quotes)
    switch len(matches) {
    case 1:  return matches[0].ReferenceID, nil
    case 0:  return "", nil
    default: return "", buildRefClarification(matches)
    }
}

// matchQuotes 匹配规则(按优先级):
//   1. 序号词:"第一辆"/"第 2 个"/"1"/"②" → Index
//   2. 车名/品牌精确包含:"朗逸"/"大众 朗逸" → CarName/BrandName
//   3. 唯一品牌:候选里只有一辆大众,用户说"那辆大众"
func matchQuotes(text string, quotes []QuoteRef) []QuoteRef { ... }
```

### 4.3 `internal/agent/decide.go` 新增(A2)

仿 tyche `DecideStage`,**直接走 eino `ChatModel.Stream`,不进 ChatModelAgent 的 ReAct loop**。流式实现一次到位(content 边吐边出,Phase 3.5.3 不用再改)。

```go
type Decision struct {
    Tool  string                 // "" = 没调 tool(闲聊/越界)
    Args  map[string]interface{} // 工具入参 JSON
    Reply string                 // content 流式吐的话术(供 PureReply / Clarify 复用)
}

type Decider struct {
    model     model.ChatModel
    tools     []*schema.ToolInfo  // 5 个 decide tool 的 schema(不绑可执行)
    sysPrompt string              // internal/prompt/decide_system.go 渲染
}

func (d *Decider) Decide(ctx, state, userInput) (*Decision, error) {
    msgs := buildMessages(state, userInput)        // system + 历史 + 本轮
    sr, _ := d.model.Stream(ctx, msgs, opts...)    // 流式
    // 收到首帧 Delta.Content 立即投 SSEWriter;流末把 ToolCalls 拼成 Decision
    return parseFromStream(sr), nil
}
```

**Tool schema(5 个,均不含 ID 字段):**

| 工具 | 描述 | 入参 |
|---|---|---|
| `search_vehicles` | 信息够,检索报价 | `scene_tags`(可空);无 context_id/filter_codes |
| `ask` | 信息不足,追问一个维度,必带选项 | `slot, question, options` |
| `get_price_detail` | 看某辆车费用明细 | `vehicle_ref`(自然语言"第一辆/朗逸",Go 侧 ResolveQuoteRef 翻译) |
| `insurance` | 问保险/保障/全险 | `vehicle_ref, question` |
| `interpret_rules` | 问规则/流程/异地还车/证件 | `rule_query`(原问) |

**关键:** `tool_choice=auto`(不强制);`Decision.Tool == ""` 即纯话术分支;入参不含 ID。

### 4.4 `internal/agent/capability.go` 新增(A2)

仿 tyche 认领式编排。

```go
type Capability interface {
    Name() string
    Run(ctx, in CapabilityInput) (*CapabilityResult, error)
}
type CapabilityInput  struct { State *ConversationState; UserInput string; Decision *Decision; Deps *Deps }
type CapabilityResult struct { Text string; Events []Event; StatePatch *StatePatch }

type CapabilityOrchestrator struct { caps map[string]Capability }
func (o *CapabilityOrchestrator) Handle(ctx, decision, state, userInput) (*Result, error) {
    cap, ok := o.caps[decision.Tool]
    if !ok { return &Result{Text: decision.Reply}, nil } // 没调 tool = 纯回复
    return cap.Run(ctx, CapabilityInput{...})
}
```

**6 个 Capability:**

| Capability | LLM #2 | Go 编排 |
|---|---|---|
| `SearchCapability` | 1(引导语) | ① locations(未确定时)② resolve_poi ③ search_quotes ④ 写 `state.Rental.ContextID`+`state.LastQuotes` ⑤ LLM 引导语 |
| `AskCapability` | 0 | 渲染 `Decision.Args` 的 question+options,复用 Decision.Reply 当前置话术 |
| `PriceDetailCapability` | 1 | ① `ResolveQuoteRef`(多义→澄清反问)② `rental_get_order_details`(state 注入 ID)③ LLM 讲解费用拆项 |
| `InsuranceCapability` | 1 | ① ResolveQuoteRef ② `rental_get_order_details` 取 `charges`(Type=3,见 §4.6 备注)③ LLM 保险建议 |
| `RulesCapability` | 1 | 复用 P3 RAG:`rag.Retrieve(args.rule_query)` → LLM 带 `[来源]` |
| `PureReplyCapability` | 0 | 闲聊/越界,Decision.Reply 直接返 |

> **重要:** ResolveQuoteRef 命中多义时**不让 LLM 猜**,返回澄清反问让用户选。

### 4.5 `internal/agent/adk.go` 重构(A4)

**从** Supervisor + 2 ChatModelAgent + supervisor.New **到** Decider + CapabilityOrchestrator。

```go
type RentalAgent struct { decider *Decider; orch *CapabilityOrchestrator; deps *Deps }

func New(ctx, d SystemDeps) (*RentalAgent, error) {
    decider, _ := NewDecider(ctx, d.ChatModelFactory.Get("decide"), prompt.RenderDecideSystem(...))
    orch := &CapabilityOrchestrator{caps: map[string]Capability{
        "search_vehicles": &SearchCapability{deps: &d}, "ask": &AskCapability{},
        "get_price_detail": &PriceDetailCapability{deps: &d}, "insurance": &InsuranceCapability{deps: &d},
        "interpret_rules": &RulesCapability{deps: &d},
    }}
    return &RentalAgent{decider, orch, &d}, nil
}

func (a *RentalAgent) Run(ctx, state, userInput) (*Result, error) {
    userInput = a.preRoute(state, userInput)          // 反问解挂(P3 已有,复用)
    decision, _ := a.decider.Decide(ctx, state, userInput)  // LLM #1
    result, _ := a.orch.Handle(ctx, decision, state, userInput)
    a.finalize(state, userInput, decision, result)    // 落 state、写 history
    return result, nil
}
```

### 4.6 `internal/tools/tyche_wrap.go` 修改:工具入参不再暴露 ID(A3)

wrap 成 InvokableTool 时**改写 JSON Schema 去掉 `context_id`/`reference_id`/`supplier`**;`InvokableRun` 内部从 state 注入。

```go
func injectIDs(args map[string]any, toolName string, state *ConversationState) map[string]any {
    switch toolName {
    case "rental_search_quotes":
        if state.Rental.ContextID != "" { args["context_id"] = state.Rental.ContextID }
    case "rental_get_order_details":
        ctxID, quotes, _ := state.SnapshotQuotes()
        args["context_id"]  = ctxID
        args["reference_id"] = state.SelectedRef
        args["supplier"]    = lookupSupplier(quotes, state.SelectedRef)
    }
    return args
}
```

> **注意 1:** 这些 wrap 函数现在**只有 Capability 内部调用**,不再注册到 LLM 可见 tools。LLM 见到的工具集来自 `decideTools`,只是 schema,不可执行。
>
> **注意 2(保险数据源):** tyche MCP 的 `rental_search_quotes` 响应**不含 `grantee_list`**。InsuranceCapability 当前只能从 `rental_get_order_details` 的 `charges`(Type=3)拿到保险费金额,**拿不到 `cover_glass`/`tpl_coverage` 等保障细节**。本期先用兜底文案("具体保障内容请在 App 内查看"),P4+ 推动 tyche MCP 透出 `RentalGuarantee` 或直连 saas-api。CLAUDE.md 里"基于 grantee_list 做保险推荐"的描述需同步修订。

### 4.7 `internal/http/handler.go` 修改(A4)

```go
// handleChat 内部
- iter := s.runner.Run(ctx, history)
- collected, finalText := s.consumeIter(ctx, iter, sw)
+ state, _ := s.stateStore.Get(ctx, uid, req.SessionID)
+ if state == nil { state = orchestration.New(req.SessionID, uid) }
+ result, err := s.agent.Run(ctx, state, req.Message)
+ s.streamToSSE(sw, result)
+ s.stateStore.Save(ctx, uid, req.SessionID, state)
```

SSE 协议层不变,前端无感。**灰度开关** `cfg.Agent.Mode = "supervisor" | "pipeline"`(默认 pipeline),按 session_id hash 灰度;1 周稳定后再物理删除 supervisor 代码。

---

## 5. Prompt 设计:`decide_system.go`(A2)

替代 supervisor + shopping + insurance 三份。要点:
1. 角色:租车助手「小租」,职责覆盖推荐/报价/价格明细/保险/规则解读/闲聊。
2. **工具调用规约**(写给 LLM):信息够→`search_vehicles`;不够→`ask`(带 2-4 选项);指代某辆车看明细→`get_price_detail`,`vehicle_ref` 填用户原话;问保险→`insurance`;问规则→`interpret_rules`;闲聊/越界→不调任何工具。
3. **严禁**:不要"自己保存 context_id/reference_id";不要让用户报这些 ID;不要在文本里输出 ID。
4. 越界文案模板(轻量,严重红线交给 P6 ISM)。

> 各 Capability 内的二次 LLM prompt:SearchCapability 引导语≤60 字、只读 quotes 摘要不输出 ID(Phase 3.5.4 起迁到独立 GuideStage);PriceDetail 按"日均/总价/优惠券/明细项"模板;Insurance 基于 charges 保险费给建议;Rules 强 grounded、无来源命中转人工。

---

## 6. 验收

### 6.1 功能
- [ ] 搜车流程通(明天北京 SUV → 报价列表)
- [ ] 看明细通(看第一辆 → 费用拆项)
- [ ] 保险问答通(那辆要加全险吗 → 基于 charges)
- [ ] 规则问答通(异地还车多少钱 → RAG 答 + [来源])
- [ ] 闲聊通(你好 → 寒暄,**0 个 tool call**)
- [ ] 越界通(帮我写代码 → 拒绝 + 引导,0 个 tool call)
- [ ] 反问解挂通(给两个选项后选一个 → 继续搜)
- [ ] 指代多义反问(候选 3 辆都叫朗逸 → 让用户选,**LLM 不猜**)

### 6.2 ID 托管硬保证
- [ ] LLM 看到的工具 schema **不含** `context_id`/`reference_id`/`supplier`
- [ ] 故意在 prompt 注入"假的 reference_id=fake_123",Go 端忽略,实际用真 ID
- [ ] 报价超过 15 分钟 → 看明细前 Go 主动重搜

### 6.3 性能
- [ ] 一次搜车 LLM 调用 ≤ 2 次(对比 P3 ~5-6 次)
- [ ] 闲聊 LLM 调用 = 1 次
- [ ] 同一 case 双跑(supervisor vs pipeline),LLM 调用次数 ≥ 50% 下降

---

## 7. 风险与回滚

| 风险 | 应对 |
|---|---|
| 4 个 PR 中间状态不可用 | A 内 4 PR 严格独立可发布;A4 用 `cfg.Agent.Mode` 一行回滚,session_id hash 灰度 |
| Decide 偶尔不调 tool 但用户在搜车 | Decision.Reply 空且无 tool → 降级触发 ask 兜底 |
| 流式 LLM 格式漂移致 tool_call 解析失败 | `syncFallback` 回退非流式重试一次 |
| 工具 schema 改写破坏 tyche 兼容 | wrap 层只裁剪入参,出参不动;集成测试覆盖全 7 个 tool |
| ResolveQuoteRef 漏判("那辆白色的") | 漏判返回 0 命中 → Capability 改为追问 |
| Supervisor 老代码 | 灰度跑稳 ≥1 周再物理删除,期间一行切回 |
