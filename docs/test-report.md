# 场景测试报告 (2026-07-06)

## 概述

针对 **`internal/agent` pipeline / capability / 解析 / 白名单 / context prompt harness** 面写了
一批黑盒场景测试(`internal/agent/scenario_test.go`)和工程契约单测
(`internal/agent/context_prompt_harness_test.go`),把"用户看得见的动作"以及
"模型输入输出契约"逐条断言:结构化按钮点击、地点前置校验、报价指代解析、
compare/slot_patch/feedback 快捷动作、GuideAction 胶囊、工具白名单、POI
反序列化、自然语言意图到 `need_delta/search_mode` 的解析契约、上下文注入、
多轮置信度衰退与 LLM stream fallback。

- **场景用例数**:30
- **Context/Prompt Harness 用例数**:4
- **首轮通过率**:28 / 30 = **93.3%**
- **抓到 bug 数**:2(都在报价指代解析 `matchQuotes` / `ordinalWords`)
- **修复后场景通过率**:30 / 30 = **100%**
- **Harness 通过率**:4 / 4 = **100%**
- **回归**:`go test ./...` 全绿,原有其它单测/集成测试不受影响

## 用例矩阵

| 编号 | 分组 | 场景 | 首轮 | 修复后 |
|---|---|---|---|---|
| A1 | 结构化事件 | compare 按钮 → 注入 `ToolCompare` Decision | ✅ | ✅ |
| A2 | 结构化事件 | slot_patch `budget_max="便宜一点"` → 生成 `price_preference` need_delta | ✅ | ✅ |
| A3 | 结构化事件 | slot_patch `search_mode=page` → SearchMode 正确 | ✅ | ✅ |
| A4 | 结构化事件 | feedback_positive 短路,吐感谢语,写 store | ✅ | ✅ |
| A5 | 结构化事件 | feedback_negative 短路 | ✅ | ✅ |
| A6 | 结构化事件 | 未知 action.type,放行给 Decide | ✅ | ✅ |
| B1 | 地点前置 | pickup 未确认(用户原话是需求),强制反问 `pickup_location`,不搜 | ✅ | ✅ |
| B2 | 地点前置 | Decision 带 pickup_text,进 slot;解析失败清 slot 并反问 | ✅ | ✅ |
| C1 | 报价指代 | 序数词"第二辆"命中 Index=2 | ✅ | ✅ |
| C2 | 报价指代 | 车名精确"朗逸"命中 | ✅ | ✅ |
| C3 | 报价指代 | 多候选"大众那辆" → 澄清 | ✅ | ✅ |
| C4 | 报价指代 | 无报价 state → 直接空返回 | ✅ | ✅ |
| C5 | 报价指代 | 单候选"那辆" → 兜底命中 | ✅ | ✅ |
| D1 | Compare | <2 refs 引导追问 | ✅ | ✅ |
| D2 | Compare | 全部 missing → 建议先搜 | ✅ | ✅ |
| D3 | Compare | 多义 → 澄清 | ✅ | ✅ |
| E1 | 工具白名单 | `rental_search_quotes` 被拒 | ✅ | ✅ |
| E2 | 工具白名单 | 写操作全拒 | ✅ | ✅ |
| E3 | 工具白名单 | 读操作全允 | ✅ | ✅ |
| F1 | POI | envelope 正常解 | ✅ | ✅ |
| F2 | POI | 缺 city_id 时不 panic | ✅ | ✅ |
| G1 | Pipeline | 预注入 Decision 时 DecideStage 短路,不调 LLM | ✅ | ✅ |
| H1 | 边界 | slot_patch 空 payload → refine 空 delta | ✅ | ✅ |
| H2 | 边界 | Result.ToolName 空 → GuideAction 不追加胶囊 | ✅ | ✅ |
| H3 | 边界 | action_click 与 message 同存时,以 action 为准 | ✅ | ✅ |
| H4 | 边界 | 纯文本消息不被 PreRoute 短路 | ✅ | ✅ |
| H5 | 边界 | Compare 部分 missing 给出提示 | ✅ | ✅ |
| H6 | 边界 | 搜车成功后 quick_action 含 slot_patch+compare+feedback 三件套 | ✅ | ✅ |
| H7 | 边界 | 报价空时不发 vehicle_list card | ✅ | ✅ |
| H8 | 边界 | 序数词多种打法(第2辆/第一辆/①/②) | ✅ | ✅ |
| I1 | 真边角 | **"第 2 辆"(带空格)、"2号"能否命中** | ❌ | ✅ |
| I2 | 真边角 | **"第10辆什么价"越界,不能误命中第 1 辆** | ❌ | ✅ |
| I3 | 真边角 | `filterNegativeNeedQuotes` 排除品牌"大众" | ✅ | ✅ |
| I4 | 真边角 | `filterExcludedQuotes` 按 reference_id 剔除 | ✅ | ✅ |
| I5 | 真边角 | 场景 KB"带老人小孩" → soft vehicle_type=SUV | ✅ | ✅ |
| I6 | 真边角 | 场景 KB 是 AND 匹配,"带老人"单词命中不到(契约记录) | ✅ | ✅ |
| I7 | 真边角 | extractRefs 兼容 `[]string / []any / nil / 空 / 类型错` | ✅ | ✅ |
| I8 | 真边角 | `action_click` 但 Action=nil 不 panic | ✅ | ✅ |

> **首轮 3 项 fail 明细**:实际是 2 个 bug 触发 3 条断言(I1 里有 2 条 assertion fail、I2 有 1 条),下文按 bug 归并。

---

## Bug #1 — 序数词表覆盖不全:`"第 2 辆"`(带空格)、`"2号"`(无"第"前缀)识别失败

**用例**:`TestScenario_I1_OrdinalEdgeCasesSurvey`

**输入 → 期望 → 实际**:

| 输入文本 | 期望命中 | 首轮实际 | 结果 |
|---|---|---|---|
| `"第2辆"` | r2 | r2 | ✅ |
| `"第 2 辆"` | r2 | `""` | ❌ |
| `"2号"` | r2 | `""` | ❌ |
| `"第②"` | r2 | r2 | ✅ |
| `"第三个"` | r3 | r3 | ✅ |

**根因**([internal/agent/resolve.go:11-17](internal/agent/resolve.go#L11-L17) 旧代码):
```go
var ordinalWords = map[string]int{
    "第一": 1, "第1": 1, "第一辆": 1, ...
    "第二": 2, "第2": 2, ...
}
// matchQuotes:
for w, idx := range ordinalWords {
    if strings.Contains(t, w) { ... }
}
```
- 词表只穷举了紧邻写法,**带内嵌空格**("第 2 辆")就 strings.Contains 命中不了。
- 词表**没有裸数字+号**("2号"),只有中文"一号/二号"。

**影响**:用户在移动端很容易多打空格或用"X号"简写,直接命中不到 → CompareCapability 走 missing 分支报"没有报价"、PriceDetail 走"没找到这辆车"——用户体验断层。

---

## Bug #2 — **严重**:序数词用 `strings.Contains` 子串匹配,`"第10辆"` 被误当 `"第1"` 命中第一辆

**用例**:`TestScenario_I2_OrdinalOutOfRange`

**输入 → 期望 → 实际**:

| 输入文本 | 候选报价 | 期望 | 首轮实际 |
|---|---|---|---|
| `"第10辆什么价"` | r1(Index=1), r2(Index=2) | 未命中(越界) | **误命中 r1** |

**根因**:
- 词表里有 `"第1": 1`
- 用户说"第10辆什么价"—— `strings.Contains("第10辆什么价", "第1")` 为 **true** → 直接返回 Index=1 的报价
- 用户以为在看"第 10 辆",实际系统给的是"第 1 辆"的价格/明细/保险,**给出完全错误的产品数据**

**为什么严重**:这不是"找不到"的降级问题,而是"找错了但显示得像找对了"的**数据一致性事故**。任何 `第1X辆`(11~19)都会命中第 1 辆;类似 `"第2X辆"` 命中第 2 辆,以此类推。

---

## 修复

**思路**:抛弃"词表 + 子串 Contains",改成**正则抽取"第 X 辆/个" 或 "X 号"**,X 支持阿拉伯数字(1-99)/中文数字(一~十)/圆圈符号(①~⑨),数字最长匹配,天然规避子串问题;另外**越界索引明确返回空**,不再退化到车名/品牌分支避免二次误匹配。

**改动**:[internal/agent/resolve.go](internal/agent/resolve.go)

```go
var circledDigits = map[rune]int{'①':1,'②':2, ... '⑨':9}
var cnDigits      = map[string]int{"一":1,"二":2, ... "十":10}

// 允许 X 前后有空白/全角空格;数字整段捕获(1-2 位),避免子串问题
var ordinalRe = regexp.MustCompile(`第\s*([一二三四五六七八九十]|\d{1,2})\s*(?:辆|个)?|(\d{1,2})\s*号|([一二三四五六七八九十])号`)

func parseOrdinal(text string) int {
    // 1. 圆圈符号先处理,rune 级独立字符
    for _, r := range text {
        if n, ok := circledDigits[r]; ok { return n }
    }
    // 2. 正则匹配 第X / X号
    m := ordinalRe.FindStringSubmatch(text)
    if m == nil { return 0 }
    for _, g := range m[1:] {
        if g == "" { continue }
        if n, ok := cnDigits[g]; ok { return n }
        if n, err := strconv.Atoi(g); err == nil { return n }
    }
    return 0
}

// matchQuotes:
if idx := parseOrdinal(t); idx > 0 {
    for _, q := range quotes {
        if q.Index == idx { return []QuoteRef{q} }
    }
    // 越界 → 明确未命中,防止 fall-through 到车名/品牌分支再次误匹配
    return nil
}
```

**修复后回归**:

- `TestScenario_I1_OrdinalEdgeCasesSurvey` 全部 5 条子用例通过
- `TestScenario_I2_OrdinalOutOfRange` 通过
- 其它 28 条既有场景用例仍全绿
- `go test ./...` 全仓库无回归

---

## 通过率总结

| 阶段 | 通过 / 总数 | 通过率 |
|---|---|---|
| 首轮 | 28 / 30 | 93.3% |
| 修复后 | 30 / 30 | **100%** |

## Context/Prompt Harness 补测 (2026-07-07)

新增 `internal/agent/context_prompt_harness_test.go`,专门覆盖用户反复提要求和上下文工程:

| 用例 | 覆盖点 | 结果 |
|---|---|---|
| `TestPromptHarnessIntentBadcasesParseToSearchControls` | "不喜欢第一辆/换一批/预算低一点/预算高一点/带老人小孩" 等坏例输入对应的 `search_mode`、`feedback_ref`、`need_delta`、`profile_patch` 解析契约 | ✅ |
| `TestPromptHarnessBuildMessagesInjectsStateAndReplaysToolHistory` | `Decider.buildMessages` 注入 summary/profile/needs/last_quotes,回放 assistant tool_call + tool result,且不泄露 `context_id/reference_id/supplier` | ✅ |
| `TestPromptHarnessNeedConfidenceDecaysAcrossTurnsAndRestoresOnReinforce` | 多轮自然衰退、冲突衰退导致 dormant、`FilterActiveNeeds` 过滤、用户再次明确后 `REINFORCE` 恢复 hard/0.85 | ✅ |
| `TestPromptHarnessDeciderUsesSyncFallbackWhenStreamCannotStart` | Decide 流式调用启动失败时走同步 fallback,继续解析 tool_call 并向用户吐出同步内容 | ✅ |

**说明**:这些测试验证的是确定性的 Go 工程契约:prompt 上下文拼装、tool schema/parser
兼容、need 生命周期和 fallback 行为。真实大模型是否把任意自然语言都推断成正确
tool arguments,仍需要后续用 golden/eval 跑配置的 LLM 绑定来验证。

## 覆盖仍不到位的地方(未来补)

1. **compare 走完后 Finalize 是否正确记 history**:目前测的是单个 Stage,未跑 pipeline 全链路。
2. **真实 LLM 语义抽取 golden/eval**:当前 harness 用确定性 tool_call JSON 验证工程契约,还需要接入 DeepSeek/当前绑定模型跑"原始自然语言 → tool arguments"的离线评测。
3. **`SearchCapability` 完整成功路径**:需要 mock guide client 返回真报价 → 断言 state 里落了 LastQuotes / CachedMenu / LastSearch。当前只测了失败/前置反问分支。
4. **session TTL 过期后重新 New 的行为**:目前无场景覆盖。

这些留到后续增量补入 `scenario_test.go`,复用同一份报告模板。

---

## 运行方式

```bash
# 只跑场景测试
go test ./internal/agent/ -run '^TestScenario_' -v

# 只跑 context/prompt harness 测试
go test ./internal/agent/ -run '^TestPromptHarness' -v

# 全仓库回归
go test ./... -count=1
```

日志会带 `stage=` / `[trace=xxx]` 前缀落盘到 `.logs/agent-YYYY-MM-DD.log`(见 [internal/logsink](internal/logsink))。
