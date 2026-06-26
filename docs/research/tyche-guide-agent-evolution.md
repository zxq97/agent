# tyche AI 导购 Agent 版本演进研究

> 目的:把隔壁 tyche(`/Users/didi/work/tyche`,包 `logic/agent`)的 C 端 AI 导购 Agent
> 从 2026-05-12 起步到 2026-06-24 的版本演进梳理清楚 —— **每个版本的设计思路、下一版为什么要改、
> 哪些经验值得本项目(rental-agent)借鉴**。
>
> 资料来源:`git log -- logic/agent`(commit message 写得极细,是一手素材)+ 现网代码静态阅读。
> 作者署名 luchang,主力模型 Claude Opus 4.6→4.7→4.8。

---

## 0. 一句话总览

tyche 导购在 6 周内迭代了 **6 个大架构 + 一次回退**,主线是一条钟摆:

```
多段串行(intent→need→filter)   ← V1
        ↓ 慢、串行误差累积
单次决策大调用(一次 LLM 出全部)  ← V2
        ↓ 召回质量不够
召回-精排两段(宽召回+pro精排)    ← 搜索4.0
        ↓ 想要"Agent 感"/多轮/工具化
责任链 Pipeline + function calling ← V4(当前主线)
        ↓ 想做"顾问式"主动追问
充分度自评 + 双层(Belief/Policy/L1) ← V4.5(顾问式)
        ↓ 过度工程、不稳、割裂
回退到单决策 function-calling      ← 当前线上(2026-06-23 revert)
```

**最大的教训写在最前面**:6/21 的"双层重构 + 让模型自评充分度"是这条线上**唯一被整体 revert 的版本**
(commit `26a4eae69d`)。从"槽位硬门槛"反转成"模型自评 sufficiency 决定问不问",听起来更智能,
实际带来不稳定 + 话术与动作割裂,最终回退到"模型一次 function-calling 自己选 ask/search"的简单形态。
**这对本项目用 eino ReAct 是直接的警示:别急着堆决策层,让模型在一次工具调用里自然决策往往更稳。**

---

## 1. 版本年表(里程碑 commit)

| 阶段 | 日期 | 关键 commit | 标题 |
|---|---|---|---|
| V1 基础设施 | 05-12~05-13 | `ff5a6f7ce6` | 租车 C 端 AI Agent 后端实施(起步) |
| V1 单层意图 | 05-14 | `0b68cedb63` | 删关键字意图层,单层 LightLLM 直接分类 |
| V1.5 搜索Pipeline | 05-15 | `8225fec58b` | V3 搜索 Pipeline:CombinedExtractor→FilterSelector→Validator |
| 上下文工程 | 05-13 | `008c863f0d` | 多轮上下文工程(Slot+摘要+消息落库) |
| V2 单次决策 | 05-25 | `04ac07b050` | **v2 单次决策调用架构:一次调用替代 intent+need+filter 多段串行** |
| 搜索4.0 | 05-27 | `d8ad23b1b0` | **召回-精排重构:硬召回宽池+pro精排top3+导购话术** |
| 安全/审核 | 05-27 | `3f2a38b6e1` | ISM 安全审核接入 + 监管测试接口 |
| RAG 条款 | 05-28 | `1449a4399d` | rental_rules 意图 + AgentHub RAG 流式接入 |
| LLM 平台化 | 05-28 | `1da1bf69d2` | LLM 底层切到公司统一平台 ARK(doubao-seed) |
| 搜索协议4.0 | 06-04 | `5965c00681` | init/chat 请求协议改造(硬切换)+ SSE 八事件 |
| 贵必赔(价保) | 06-05~ | `40cbaad9d2` | 价格保护:视频/图片识别 + 算价补差 |
| **V4 Agent化** | 06-16 | `cb9e1d58f2` | **责任链 Pipeline + 流式 function calling + 渐进追问** |
| V4 决策载体反复 | 06-17 | `c4236dc70d`/`c6d6cac3f5`/`60087dab13` | function-calling ↔ JSON 尾段来回切,瘦身 Decide |
| V4.5 顾问式 | 06-20 | `3806c6d2c8` | **充分度门槛+信息增益反问+用户画像+解释推荐+场景知识库** |
| V4.5 双层重构 | 06-21 | `a05f6cec40` | **Belief/Policy/L1 双层重构** |
| **回退** | 06-23 | `26a4eae69d` | **revert:放弃双层+合并话术,回到单决策老逻辑** |
| 清理 | 06-20 | `20b33622f9` | 清理 V3 死代码,只留 chatV4 |

---

## 2. 各版本详解

### V1 —— 多段串行 LLM(05-12 ~ 05-19)

**设计思路**
- 起步即定下"C 端对话式导购"形态:SSE 流式 + 多轮 session。
- 决策拆成**多段独立 LLM 调用串行**:意图识别 → 需求抽取(NeedExtractor)→ 筛选项选择(FilterSelector)→ 校验(Validator)。
- 配套地基:
  - **上下文工程**(`008c863f0d`):Slot 追踪 + 会话摘要 + 消息落库,解决多轮记忆。
  - **意图层简化**(`0b68cedb63`):一开始有"关键字 + LLM"两层,很快删掉关键字层,改单层 LightLLM 直接输出枚举字符串(`4a01269ec5` 进一步去掉 JSON 解析,直接吐枚举,减少解析失败)。
  - **菜单 schema 驱动**:筛选项不是写死的,而是从实时门店菜单(menu schema)动态生成候选喂给 LLM。

**为什么要改**
- **串行慢**:每段一次 LLM,延迟叠加(后面专门做了意图与 menu 并行省 ~150ms,`abe9bf30bc`)。
- **误差累积**:意图错→需求抽取错→筛选错,前段错误无法被后段纠正。
- **JSON 解析脆**:游离引号导致 unmarshal 失败(`022bf174d0`),反复踩。

**可借鉴**
- ✅ **菜单 schema 动态喂给 LLM**,而不是把筛选项写死在 prompt —— 后端筛选维度变化时 prompt 不用改。本项目接 tyche `rental_search_quotes` 的 `grantee_list` 等也应这么做。
- ✅ **意图分类直接输出枚举字符串、不套 JSON** —— 少一层解析就少一类线上故障。
- ✅ **上下文工程三件套(Slot/摘要/落库)早早就位**,这是多轮对话的地基,不是后期优化。

---

### V2 —— 单次决策大调用(05-25, `04ac07b050`)

**设计思路**
- 把 V1 的 `intent + need + filter` 多段串行,**合并成一次 LLM 调用**(`DecisionCaller`,`deepseek-v4-pro`)。
- 单次输出结构化:`action / need_delta / filter_codes / group_code / clarification / reply`。
- LLM 之后接 **Go 确定性后处理**:`ApplyDelta`(需求增量合并)+ `Validator` 白名单校验 + `forceIncludeMustHave`。
- 薄规则层 `SubIntentRouter` 保留(翻页/回溯这类机械动作不进 LLM)。

**为什么这么改**
- 一次调用消除串行延迟和段间误差累积。
- `need_delta`(增量)而非全量重抽,天然支持"放宽预算""不够尊贵"这类**修改型多轮指令**。

**为什么后面又要改**
- 单次决策**召回质量不够**:一次调用要同时做意图+需求+选码,模型负担重,top 结果不够准 → 催生搜索4.0 的"召回-精排"。

**可借鉴**
- ✅ **"一次 LLM 决策 + Go 确定性后处理"是非常稳的范式**。LLM 负责语义理解,Go 负责校验/白名单/合并 —— 幻觉字段在后处理被拦掉。对应本项目 CLAUDE.md 的安全护栏(关键 ID 只能来自工具返回),思路完全一致。
- ✅ **need_delta 增量模型**:多轮里"改一个条件"只发增量,而不是重新抽全量,既省 token 又保留历史约束。本项目 `ConversationState` 可借鉴这种增量合并。
- ✅ **机械动作(翻页/回溯)走规则,不进 LLM** —— 省钱、零延迟、零幻觉。

---

### 搜索4.0 —— 召回/精排两段(05-27, `d8ad23b1b0`)

**设计思路**
- 借鉴推荐系统的**召回-精排**两段式:
  - **召回**:硬条件宽池召回(`filter_codes` 只保留 hard 条件,放宽软条件,扩大候选)。
  - **精排**:`pro` 模型对召回池做 rerank,选 top3 + 生成导购话术。
- `DecisionCaller` 降级到 `flash`(便宜快),把贵的 `pro` 只用在精排。
- LastSearch 扩展 `ContextID/RankedIndex/Cursor`,**池内翻页**(翻页不重新搜索,在已召回池里滑动)。

**为什么这么改**
- 单次决策"既要又要"导致召回不准 → 拆成"先广撒网(便宜模型),再精挑(贵模型)"。
- 分模型分工:flash 管决策、pro 管精排,**成本与质量平衡**。

**可借鉴**
- ✅ **召回 vs 精排分离 + 强弱模型分工**:便宜模型做粗筛、贵模型做精排/话术,是控成本的关键手段。本项目默认 DeepSeek,`deepseek-chat`(快)做工具决策 + `deepseek-reasoner`(强)做复杂解读/精排,可对标。
- ✅ **池内翻页**:"换一批/下一页"不应重新调后端搜索,在已有结果池里游标滑动。

---

### 工程化补强(05-27 ~ 06-15)

这一段没有改架构,但补齐了**生产必备件**,本项目迟早要做:

| 能力 | commit | 借鉴点 |
|---|---|---|
| **ISM 安全审核** | `3f2a38b6e1` `a159886ab0` | 所有出口(含 small_talk/兜底)统一过审;`done` 事件**延迟到审核完成后**才发,保证没有未过审内容漏出 |
| **条款 RAG** | `1449a4399d` | 新增 `rental_rules` 意图,走 AgentHub 向量检索流式接入(对应本项目 P3 BM25) |
| **LLM 平台化** | `1da1bf69d2` `7b9b328f70` | 底层 LLM 从直连切到公司统一网关(ARK/doubao),LLM 层收口为统一 Client 接口(删 qwen 分支) |
| **输出出口收口** | `b0838ccfcd` | 所有输出走单一 `finalizeOutput`,杜绝多出口各自为政导致漏审/漏 done |
| **瞬态轮** | `2a0bcd506c` | 安全拦截/越界轮标记 Ephemeral:**不入 history、不计轮次、不持久化**,避免污染后续指代消解、避免反复触发内容安全 |

**强烈建议本项目借鉴**:
- ✅ **输出单一出口 + done 延迟到审核后**:这是"绝不漏审"的工程保证。本项目 HTTP/SSE 已起步,应及早收口。
- ✅ **瞬态轮概念**:拒绝话术、安全兜底这类"零业务信号"的轮次不该进上下文,否则毒化后续多轮。这是个很容易漏掉但代价很大的细节。

---

### V4 —— Agent 化责任链 Pipeline(06-16, `cb9e1d58f2`)

**设计思路**
- 从"决策调用 + 搜索"升级为**责任链流水线**,每个 Stage 职责单一,靠 `Signal{Continue,Stop}` 流转
  (见现网 [v4_pipeline.go](../../../work/tyche/logic/agent/v4_pipeline.go)):
  ```
  PreRoute → Decide → Rules → PriceProtect → ModifyLocation → FilterCode → Search → Guide → Clarify → Finalize
  ```
- **流式 function calling**:`DecideStage` 用 DeepSeek 原生 function calling,工具 `search_vehicles`/`ask` 双工具,content 实时流式吐"思考",tool_calls 按 index 分片累积。
- **渐进追问**:信息不全时调 `ask` 工具反问,而非硬兜底。
- 全量切 RunChatV4,V3 仅留 happy-path 回归保护(一个月后 `20b33622f9` 删 V3 死代码)。

**为什么这么改**
- 想要真正的"Agent 感":多能力(搜车/改地点/条款/价保)统一编排,可插拔 Stage,每步可日志可测。
- 责任链让"加一个能力 = 加一个 Stage",扩展性远好于一坨 if-else。

**V4 内部的反复横跳(决策载体之争,06-17 一天三次)**

这段特别值得看,是**"function calling vs JSON 尾段"的反复**:

1. `c6d6cac3f5` **DecideStage 瘦身**:只产 `need_delta`,新增独立 `FilterCodeStage` 据 needs+菜单单独生成 filter_codes。→ 单一职责。
2. `60087dab13` 决策+引导从 function-calling **改回 JSON 尾段流式**:"根治丢召回"(function calling 模式下模型有时只说话不调工具,丢了搜索动作)。
3. `c4236dc70d` 又**改回原生 function calling(auto 模式)**:实测 deepseek-v3.2 在 `auto` 下 content 话术 + tool_call 能共存;但 `required` 会抑制 content(只出工具不出话术),所以用 auto 不用 required。

**这三连跳的教训**:
- function calling 的痛点:**模型可能"只说话不调工具"**,丢失关键动作(召回)。
- JSON 尾段的痛点:**话术 + JSON 两段的稳定性依赖模型**,需要本地实测。
- 最终结论:**用 `tool_choice=auto`(不强制 required)** —— required 会压制自然话术,auto 下话术和工具调用可共存。

**可借鉴(对 eino 用户极其重要)**
- ✅ **责任链/Stage 化编排**:本项目用 eino ADK supervisor,本质相同 —— 但 tyche 的 Stage + Signal 模型证明"每步可短路、可日志、可单测"的价值。
- ✅ **`tool_choice=auto` 而非 `required`**:强制工具调用会让模型丢掉自然语言话术,体验变差。这是本项目用 DeepSeek/Claude function calling 时要直接采纳的结论。
- ✅ **决策瘦身**:让决策 LLM 只产语义增量(need_delta),把"选哪个 filter_code"这种和实时菜单强相关的活儿拆到独立 Stage(可用更便宜模型甚至确定性规则)。
- ⚠️ **决策载体不要反复横跳**:tyche 一天内 function-calling↔JSON 切了三回,说明上线前应**本地实测模型在 auto/required/JSON 三种模式下的稳定性**再定,别上线后试错。

---

### V4.5 —— 顾问式改造 + 双层重构(06-20~06-21)→ **被回退**

**设计思路(野心最大的一版)**

1. **顾问式改造**(`3806c6d2c8`):把导购从"槽位门槛 + 搜索引擎"升级为"会咨询的导购"。
   - **充分度自评**:search 工具加 `understanding.sufficiency`,模型自评信息够不够(阈值 0.6),**反转**原来的"必填槽位非空"硬门槛。
   - **信息增益反问**:不再"问第一个缺的",改成"自评不够才问,且问信息增益最高的维度"。
   - **用户画像层**:`UserProfile`(出行场景/同行/价敏/调性),同一次 FC 顺带产 `profile_patch`(不加额外 LLM 调用)。
   - **解释性推荐**:引导语喂画像,做"为什么适合你"。
   - **场景知识库**:结构化规则(高原→非纯电 / 带娃→SUV / 雪天→四驱)注入 prompt。

2. **双层重构**(`a05f6cec40`):
   - **Belief 层**:query 反查纠正归类(用户说"经济实惠"≠ 筛选项"经济型")+ 唯一权威 needs 合并源。
   - **Policy 层**:确定性的问/搜门槛 + 从菜单装配反问选项,**取代模型 sufficiency 自评**(注意:这其实是在打 1 步的脸 —— 自评不靠谱,又收回确定性规则)。
   - **L1 前置分流**:doubao 轻模型做 query 改写 + 大类路由 + 秒回话术,带 fail-safe 回落。

**为什么回退(`26a4eae69d`, 06-23)**
- commit 原话:"放弃双层重构 + 合并话术 C 方案,回到 `4b503cd9` 的单决策老逻辑:**模型一次 function-calling 自己选 ask/search/modify/rules**"。
- 暴露的问题(从 6/20~6/23 一连串 fix 能看出):
  - **答非所问死循环**(`dac86474b1`):必填槽反问反复触发。
  - **话术与动作割裂**(`4473c584f1` 想用"合并话术 C 方案"救,合并成一次流式 FC,仍没救活)。
  - **充分度自评不稳** → Policy 层又用确定性规则收回去,等于左右互搏。
  - 双层 + L1 + 合并话术叠加,**复杂度爆炸、收益不明、稳定性下降**。
- 回退后形态(当前线上,见 [v4_orchestrator.go](../../../work/tyche/logic/agent/v4_orchestrator.go)):
  `PreRoute→Decide→Rules→PriceProtect→ModifyLocation→FilterCode→Search→Guide→Clarify→Finalize`,
  Decide 就是**一次 function-calling 让模型自己选工具**,不再有 Belief/Policy/L1/充分度自评。
  > 注:`search/` 包里的双层逻辑代码没删,用 `AuthoritativeNeeds != nil` 门控,agent 层不传就自动走老逻辑 —— 干净的可回退设计。

**可借鉴(本项目最该记住的一段)**
- ⚠️ **别让模型自评"信息够不够"来决定流程**:`sufficiency` 自评不稳,最后又被确定性 Policy 规则替代。**门槛判断用确定性规则,语义理解用 LLM**,职责别混。
- ⚠️ **决策层数量要克制**:Belief/Policy/L1 三层 + 顾问式 + 合并话术,一次性加太多,互相耦合难定位,最终整体回退。**一次只动一层,灰度验证再叠下一层。**
- ✅ **顾问式的"料"本身是好的,可单独借鉴而不必上整套架构**:
  - **场景知识库**(高原/带娃/雪天→车型规则)是确定性的、可单测的、产品可配的,这块价值高、风险低,值得单独抄。
  - **用户画像 piggyback**(同一次 FC 顺带产 profile_patch,不加 LLM 调用)是零成本增强。
- ✅ **可回退设计**:新逻辑用开关/门控字段(`AuthoritativeNeeds != nil`、Apollo `merge_decide_talk`)包起来,老代码全留,出问题秒回退 + 留 `backup-before-rollback` 分支。这是高频迭代的安全网。

---

## 3. 横切主题(贯穿所有版本的经验)

### 3.1 模型分层用
- 路由/改写/快筛:轻模型(doubao-lite / deepseek flash)。
- 决策/精排/话术:强模型(deepseek-pro / v3.2)。
- 本项目对标:`deepseek-chat` 做工具决策,`deepseek-reasoner` 做条款解读/复杂精排。

### 3.2 LLM 之外必须有确定性兜底
- 每个 LLM Stage 都有 Go fallback:FilterCode 失败 → 留空交 StaticRecall 兜底;Guide 失败 → `buildDynamicGuide`;L1 失败 → fail-safe 回落单决策。
- **LLM 是尽力而为,确定性规则托底** —— 这是生产可用的前提。

### 3.3 配置热化(Apollo)
- 大模型调用(model/温度/tokens/prompt)逐步上 Apollo 热配(`dcdfe634b3`),但**JSON 格式段固定在代码**防产品配错(`4acb75c1dc`)。
- 借鉴:**可调的(话术风格、阈值、模型)放配置;不可错的(数据格式、白名单)锁代码。**

### 3.4 多轮上下文的反复踩坑
- 反问轮丢上下文(`ed7655956b`:每轮 user+assistant 必落 History)。
- 瞬态轮污染(`2a0bcd506c`)。
- 城市/取还车上下文注入格式从 XML 标签改回普通 user/assistant 数组(`6fa752c9b2`)。
- 借鉴:**History 完整性 + 瞬态隔离**是多轮质量的命脉。

### 3.5 流式体验的精细打磨
- thinking_box 最短展示 1s(太快一闪而过,`2896743b3f`)。
- thinking 头 start/done 严格配对(前端按对计数,多 start 少 done 关不掉,`de15f7e857`/`4cd847b76a`)。
- 缺槽反问时**不下发**"正在筛选"思考框(否则和反问割裂,`6d047bf223`)。
- 借鉴:SSE 流式的"思考动效"要和真实动作严格对齐,否则体验割裂。本项目 P4 SSE 要注意。

---

## 4. 给本项目(rental-agent / eino)的行动清单

按"直接抄 / 谨慎参考 / 反面教材"分类:

### ✅ 直接借鉴
1. **一次 LLM 决策 + Go 确定性后处理**(白名单校验 / 关键 ID 只认工具返回)—— 和现有安全护栏同源,强化它。
2. **need_delta 增量需求模型** —— 多轮"改一个条件"发增量,`ConversationState` 里实现。
3. **tool_choice=auto 不用 required** —— eino function calling 直接采纳,保留自然话术。
4. **菜单/grantee_list 动态喂 LLM**,不写死筛选/保险项。
5. **召回-精排 + 强弱模型分工** —— chat 决策、reasoner 精排/解读。
6. **场景知识库**(确定性规则注入 prompt)—— 高价值低风险,单独做。
7. **输出单一出口 + done 延迟到审核后 + 瞬态轮隔离** —— 生产工程三件套。
8. **可回退设计**(新逻辑用开关门控,老代码保留)—— 高频迭代安全网。

### ⚠️ 谨慎参考(tyche 踩过坑)
9. **别让模型自评 sufficiency 决定问不问** —— 门槛用确定性规则。
10. **决策层数量克制,一次只加一层并灰度** —— 别学双层重构一次性堆叠。
11. **决策载体(FC/JSON)上线前本地实测三种模式** —— 别上线试错。

### ❌ 反面教材
12. **顾问式 + Belief/Policy/L1 全家桶一次性上线** —— 被整体 revert。要的是里面的"料"(场景库/画像),不是那套架构。

---

## 5. 关键文件索引(tyche 现网)

| 文件 | 作用 |
|---|---|
| [logic/agent/v4_orchestrator.go](../../../work/tyche/logic/agent/v4_orchestrator.go) | V4 主流程入口 RunChatV4 |
| [logic/agent/v4_pipeline.go](../../../work/tyche/logic/agent/v4_pipeline.go) | 责任链 Pipeline + Signal |
| [logic/agent/v4_stage_decide.go](../../../work/tyche/logic/agent/v4_stage_decide.go) | 决策 Stage(function calling) |
| [logic/agent/v4_stage_filtercode.go](../../../work/tyche/logic/agent/v4_stage_filtercode.go) | 选码 Stage(needs+菜单→filter_codes) |
| [logic/agent/search/decision_caller.go](../../../work/tyche/logic/agent/search/decision_caller.go) | V2 单次决策遗产 |
| [logic/agent/search/pipeline.go](../../../work/tyche/logic/agent/search/pipeline.go) | 搜索召回-精排 |
| [logic/agent/scene_knowledge.go](../../../work/tyche/logic/agent/scene_knowledge.go) | 场景知识库(可借鉴) |
| [controller/mcp/controller.go](../../../work/tyche/controller/mcp/controller.go) | 对外 MCP 7 工具(本项目消费面) |
| [library/agenthub/client.go](../../../work/tyche/library/agenthub/client.go) | LLM 网关 + RAG 检索 |

---

*生成于 2026-06-25,基于 tyche `master` @ `e878c208f8`。*
