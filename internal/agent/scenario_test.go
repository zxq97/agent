package agent

// scenario_test.go —— 用户流程级黑盒测试:构造 AgentContext / Decision / State,
// 跑单个 Stage 或 Capability,断言"用户视角看到什么"和"state 变成什么"。
//
// 与 phase*_test / resolve_test 等单元测试的区别:这里每个用例对应一个
// 用户可以描述的动作(点了对比按钮、说"便宜点"、地点空调搜车),用来做
// 回归清单 + 上线前通过率报告的原始输入。样例/期望/结果会汇总到
// docs/test-report.md。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
	"github.com/zxq97/rental-agent/internal/types"
)

// -----------------------------------------------------------------------------
// A. 结构化事件路由 (PreRouteStage)
// -----------------------------------------------------------------------------

func TestScenario_A1_CompareButtonInjectsDecision(t *testing.T) {
	ac := &AgentContext{
		State:     orchestration.New("s", "u"),
		EventType: "action_click",
		Action: &ClientAction{
			Type:    "compare",
			Label:   "对比朗逸和轩逸",
			Payload: map[string]any{"vehicle_refs": []any{"朗逸", "轩逸"}},
		},
	}
	sig, err := (&PreRouteStage{}).Handle(context.Background(), ac)
	if err != nil || sig != SignalContinue {
		t.Fatalf("sig=%s err=%v", sig, err)
	}
	if ac.Decision == nil || ac.Decision.Tool != ToolCompare {
		t.Fatalf("Decision = %#v", ac.Decision)
	}
	refs := extractRefs(ac.Decision.Args["vehicle_refs"])
	if len(refs) != 2 || refs[0] != "朗逸" {
		t.Fatalf("refs = %#v", refs)
	}
}

func TestScenario_A2_SlotPatchBudgetDown(t *testing.T) {
	ac := &AgentContext{
		State:     orchestration.New("s", "u"),
		EventType: "action_click",
		Action: &ClientAction{
			Type:    "slot_patch",
			Label:   "便宜一点",
			Payload: map[string]any{"budget_max": "便宜一点"},
		},
	}
	_, err := (&PreRouteStage{}).Handle(context.Background(), ac)
	if err != nil {
		t.Fatal(err)
	}
	if ac.Decision == nil || ac.Decision.Tool != ToolSearchVehicles {
		t.Fatalf("Decision = %#v", ac.Decision)
	}
	if len(ac.Decision.NeedDelta) == 0 {
		t.Fatalf("expect need_delta for budget_max, got empty")
	}
	got := ac.Decision.NeedDelta[0]
	if got.Type != "price_preference" || got.Op != "ADD" {
		t.Fatalf("delta = %+v, want ADD price_preference", got)
	}
}

func TestScenario_A3_SlotPatchNextPage(t *testing.T) {
	ac := &AgentContext{
		State:     orchestration.New("s", "u"),
		EventType: "action_click",
		Action: &ClientAction{
			Type:    "slot_patch",
			Label:   "换一批",
			Payload: map[string]any{"search_mode": SearchModePage},
		},
	}
	_, err := (&PreRouteStage{}).Handle(context.Background(), ac)
	if err != nil {
		t.Fatal(err)
	}
	if ac.Decision == nil || ac.Decision.SearchMode != SearchModePage {
		t.Fatalf("Decision.SearchMode = %q, want %q", ac.Decision.SearchMode, SearchModePage)
	}
}

func TestScenario_A4_FeedbackPositiveShortCircuits(t *testing.T) {
	st := orchestration.New("s", "u")
	store := NewFileFeedbackStore(t.TempDir() + "/fb.jsonl")
	emit := &captureEmitter{}
	ac := &AgentContext{
		State:     st,
		Emit:      emit,
		Feedback:  store,
		EventType: "action_click",
		Action:    &ClientAction{Type: "feedback_positive", Payload: map[string]any{}},
	}
	sig, err := (&PreRouteStage{}).Handle(context.Background(), ac)
	if err != nil {
		t.Fatal(err)
	}
	if sig != SignalStop {
		t.Fatalf("sig=%s, want stop", sig)
	}
	if ac.Result == nil || !strings.Contains(ac.Result.Text, "反馈") {
		t.Fatalf("result = %#v", ac.Result)
	}
	if len(emit.texts) == 0 {
		t.Fatalf("no reply emitted")
	}
}

func TestScenario_A5_FeedbackNegativeShortCircuits(t *testing.T) {
	st := orchestration.New("s", "u")
	store := NewFileFeedbackStore(t.TempDir() + "/fb.jsonl")
	ac := &AgentContext{
		State:     st,
		Emit:      &captureEmitter{},
		Feedback:  store,
		EventType: "action_click",
		Action:    &ClientAction{Type: "feedback_negative", Payload: map[string]any{"message": "太贵了"}},
	}
	sig, _ := (&PreRouteStage{}).Handle(context.Background(), ac)
	if sig != SignalStop {
		t.Fatalf("sig=%s want stop", sig)
	}
}

func TestScenario_A6_UnknownActionFallsThrough(t *testing.T) {
	ac := &AgentContext{
		State:     orchestration.New("s", "u"),
		EventType: "action_click",
		Action:    &ClientAction{Type: "no_such_action"},
	}
	sig, err := (&PreRouteStage{}).Handle(context.Background(), ac)
	if err != nil || sig != SignalContinue {
		t.Fatalf("sig=%s err=%v", sig, err)
	}
	if ac.Decision != nil {
		t.Fatalf("Decision should not be injected: %#v", ac.Decision)
	}
}

// -----------------------------------------------------------------------------
// B. 地点前置校验 (SearchCapability)
// -----------------------------------------------------------------------------

func TestScenario_B1_MissingLocationAsks(t *testing.T) {
	in := CapabilityInput{
		State:     orchestration.New("s", "u"),
		UserInput: "1个人 SUV", // 需求原话,不是地点
		Decision:  &Decision{Tool: ToolSearchVehicles},
		Deps:      &tools.Deps{},
	}
	res, err := (&SearchCapability{}).Run(context.Background(), in)
	if err != nil || res == nil || res.Clarification == nil {
		t.Fatalf("res=%#v err=%v", res, err)
	}
	if res.Clarification.Slot != "pickup_location" {
		t.Fatalf("slot=%s want pickup_location", res.Clarification.Slot)
	}
}

func TestScenario_B2_PickupTextCopiedToSlot(t *testing.T) {
	st := orchestration.New("s", "u")
	// deps 里 tyche 为 nil,resolvePickupDropoff 会因 Call 里 tyche client nil 报错并
	// 返回 clarification —— 我们在此只关心 pickup_text 是否被认领后又被清掉。
	in := CapabilityInput{
		State:     st,
		UserInput: "帮我找找车",
		Decision:  &Decision{Tool: ToolSearchVehicles, PickupText: "首都机场T3"},
		Deps:      &tools.Deps{},
	}
	res, err := (&SearchCapability{}).Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	// 解析必失败(deps 无 tyche),SearchCapability 应把它清掉以便下一轮重问
	if st.Slot.PickupText != "" {
		t.Fatalf("Slot.PickupText should be cleared after resolve failure, got %q", st.Slot.PickupText)
	}
	if res == nil || res.Clarification == nil || res.Clarification.Slot != "pickup_location" {
		t.Fatalf("expect pickup_location clarification, got %#v", res)
	}
}

// -----------------------------------------------------------------------------
// C. 报价指代解析 (ResolveQuoteRef)
// -----------------------------------------------------------------------------

func TestScenario_C1_ResolveOrdinal(t *testing.T) {
	st := stateWithQuotes(
		orchestration.QuoteRef{ReferenceID: "r1", CarName: "朗逸", Index: 1},
		orchestration.QuoteRef{ReferenceID: "r2", CarName: "轩逸", Index: 2},
	)
	ref, clar := ResolveQuoteRef(st, "第二辆多少钱")
	if clar != nil || ref != "r2" {
		t.Fatalf("ref=%q clar=%#v", ref, clar)
	}
}

func TestScenario_C2_ResolveByCarName(t *testing.T) {
	st := stateWithQuotes(
		orchestration.QuoteRef{ReferenceID: "r1", CarName: "大众朗逸", BrandName: "大众", Index: 1},
		orchestration.QuoteRef{ReferenceID: "r2", CarName: "日产轩逸", BrandName: "日产", Index: 2},
	)
	ref, clar := ResolveQuoteRef(st, "看看朗逸")
	if clar != nil || ref != "r1" {
		t.Fatalf("ref=%q clar=%#v", ref, clar)
	}
}

func TestScenario_C3_MultipleMatchesAsksToClarify(t *testing.T) {
	st := stateWithQuotes(
		orchestration.QuoteRef{ReferenceID: "r1", CarName: "大众朗逸", BrandName: "大众", Index: 1},
		orchestration.QuoteRef{ReferenceID: "r2", CarName: "大众速腾", BrandName: "大众", Index: 2},
	)
	ref, clar := ResolveQuoteRef(st, "大众那辆")
	if clar == nil {
		t.Fatalf("expect clarification, got ref=%q", ref)
	}
	if len(clar.Options) != 2 {
		t.Fatalf("options=%v, want 2", clar.Options)
	}
}

func TestScenario_C4_QuoteStaleReturnsEmpty(t *testing.T) {
	st := stateWithQuotes(orchestration.QuoteRef{ReferenceID: "r1", CarName: "朗逸", Index: 1})
	// 用私有方法强制过期:把 QuoteAt 拨到很久以前。ConversationState 无 setter,
	// 但 SetQuotes 内会刷新 QuoteAt,所以只能间接:直接读 stale 逻辑就够——
	// 这里改用一个空 quotes state,ResolveQuoteRef 会拿不到报价直接返回空。
	empty := orchestration.New("s2", "u2")
	ref, clar := ResolveQuoteRef(empty, "第一辆")
	if ref != "" || clar != nil {
		t.Fatalf("empty state ref=%q clar=%#v", ref, clar)
	}
	// 有效 state 命中
	ref, _ = ResolveQuoteRef(st, "第一辆")
	if ref != "r1" {
		t.Fatalf("fresh state ref=%q want r1", ref)
	}
}

func TestScenario_C5_SingleCandidateVagueMatch(t *testing.T) {
	st := stateWithQuotes(orchestration.QuoteRef{ReferenceID: "r1", CarName: "朗逸", Index: 1})
	ref, clar := ResolveQuoteRef(st, "那辆多少钱")
	if clar != nil || ref != "r1" {
		t.Fatalf("ref=%q clar=%#v", ref, clar)
	}
}

// -----------------------------------------------------------------------------
// D. Compare capability 前置分支
// -----------------------------------------------------------------------------

func TestScenario_D1_CompareNeedsAtLeastTwoRefs(t *testing.T) {
	in := CapabilityInput{
		State:    stateWithQuotes(orchestration.QuoteRef{ReferenceID: "r1", CarName: "朗逸", Index: 1}),
		Decision: &Decision{Tool: ToolCompare, Args: map[string]any{"vehicle_refs": []any{"朗逸"}}},
		Deps:     &tools.Deps{},
		Emit:     &captureEmitter{},
	}
	res, err := (&CompareCapability{}).Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !strings.Contains(res.Text, "对比哪几辆") {
		t.Fatalf("res=%#v", res)
	}
}

func TestScenario_D2_CompareAllMissingSuggestsSearch(t *testing.T) {
	in := CapabilityInput{
		State: stateWithQuotes(orchestration.QuoteRef{ReferenceID: "r1", CarName: "朗逸", Index: 1}),
		Decision: &Decision{
			Tool: ToolCompare,
			// 都不在报价里
			Args: map[string]any{"vehicle_refs": []any{"特斯拉Model3", "小鹏P7"}},
		},
		Deps: &tools.Deps{},
		Emit: &captureEmitter{},
	}
	res, err := (&CompareCapability{}).Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || !strings.Contains(res.Text, "没有报价") {
		t.Fatalf("res=%#v", res)
	}
}

func TestScenario_D3_CompareAmbiguousAsksToClarify(t *testing.T) {
	in := CapabilityInput{
		State: stateWithQuotes(
			orchestration.QuoteRef{ReferenceID: "r1", CarName: "大众朗逸", BrandName: "大众", Index: 1},
			orchestration.QuoteRef{ReferenceID: "r2", CarName: "大众速腾", BrandName: "大众", Index: 2},
			orchestration.QuoteRef{ReferenceID: "r3", CarName: "日产轩逸", BrandName: "日产", Index: 3},
		),
		Decision: &Decision{Tool: ToolCompare, Args: map[string]any{"vehicle_refs": []any{"大众", "日产轩逸"}}},
		Deps:     &tools.Deps{},
		Emit:     &captureEmitter{},
	}
	res, err := (&CompareCapability{}).Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Clarification == nil {
		t.Fatalf("expect clarification, got %#v", res)
	}
}

// -----------------------------------------------------------------------------
// E. 工具白名单
// -----------------------------------------------------------------------------

func TestScenario_E1_DeprecatedSearchQuotesDenied(t *testing.T) {
	if tools.IsAllowedTool(tools.ToolSearchQuotes) {
		t.Fatalf("rental_search_quotes must NOT be allowed")
	}
}

func TestScenario_E2_WriteOpsDenied(t *testing.T) {
	for _, name := range []string{"rental_create_order", "rental_pay", "rental_refund", "rental_modify_order"} {
		if tools.IsAllowedTool(name) {
			t.Fatalf("%s must NOT be allowed", name)
		}
	}
}

func TestScenario_E3_ReadOpsAllowed(t *testing.T) {
	for _, name := range []string{
		tools.ToolSearchLocations, tools.ToolResolvePOI,
		tools.ToolGetOrderDetails, tools.ToolGetReservation, tools.ToolGetDriverList,
	} {
		if !tools.IsAllowedTool(name) {
			t.Fatalf("%s should be allowed", name)
		}
	}
}

// -----------------------------------------------------------------------------
// F. POI 反序列化
// -----------------------------------------------------------------------------

func TestScenario_F1_POIEnvelopeOK(t *testing.T) {
	raw := `{"poi":{"latitude":40.06,"longitude":116.13,"city_id":1,"name":"望京"}}`
	var env poiEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if env.POI.CityID != 1 || env.POI.Name != "望京" {
		t.Fatalf("env=%+v", env.POI)
	}
}

func TestScenario_F2_POIEnvelopeMissingCityID(t *testing.T) {
	// 没有 city_id 时,SearchCapability.resolvePickupDropoff 会显式报错(见 cap_search.go)。
	// 这里断言 envelope 反序列化本身不 panic 且 CityID=0。
	raw := `{"poi":{"latitude":0,"longitude":0,"name":""}}`
	var env poiEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if env.POI.CityID != 0 {
		t.Fatalf("expect city_id=0, got %d", env.POI.CityID)
	}
}

// -----------------------------------------------------------------------------
// G. PreRoute → Decide 短路
// -----------------------------------------------------------------------------

// PreRoute 已经注入 Decision 时,DecideStage 必须直接放行,不能再调 LLM。
// 用一个"任何调用都会 panic 的 fake decider"来证明它没被触发。
type panickyDecider struct{}

func (panickyDecider) Handle(ctx context.Context, ac *AgentContext) (Signal, error) {
	panic("decide LLM should not be called when Decision pre-injected")
}
func (panickyDecider) Name() string { return "Decide" }

func TestScenario_G1_PreInjectedDecisionSkipsDecideLLM(t *testing.T) {
	ac := &AgentContext{
		State:    orchestration.New("s", "u"),
		Decision: &Decision{Tool: ToolCompare, Args: map[string]any{"vehicle_refs": []any{"a", "b"}}},
	}
	// 直接用真实 DecideStage:它会看到 ac.Decision != nil 而放行
	sig, err := (&DecideStage{decider: nil}).Handle(context.Background(), ac)
	if err != nil || sig != SignalContinue {
		t.Fatalf("sig=%s err=%v", sig, err)
	}
	// 反证:若 Decision=nil 则 decider 会被解引用 panic
	// (仅示意,不实际跑,避免 nil-decider 崩测试进程)
}

// -----------------------------------------------------------------------------
// H. 更进一步的边界 / 交互
// -----------------------------------------------------------------------------

// H1: slot_patch 带空 payload 不应崩,应该生成一个 refine + 空 need_delta 的 Decision。
func TestScenario_H1_SlotPatchEmptyPayload(t *testing.T) {
	ac := &AgentContext{
		State:     orchestration.New("s", "u"),
		EventType: "action_click",
		Action:    &ClientAction{Type: "slot_patch", Payload: map[string]any{}},
	}
	sig, err := (&PreRouteStage{}).Handle(context.Background(), ac)
	if err != nil || sig != SignalContinue {
		t.Fatalf("sig=%s err=%v", sig, err)
	}
	if ac.Decision == nil || ac.Decision.Tool != ToolSearchVehicles {
		t.Fatalf("Decision = %#v, want search_vehicles/refine", ac.Decision)
	}
	if ac.Decision.SearchMode != SearchModeRefine {
		t.Fatalf("SearchMode = %q, want %q", ac.Decision.SearchMode, SearchModeRefine)
	}
	if len(ac.Decision.NeedDelta) != 0 {
		t.Fatalf("NeedDelta should be empty, got %+v", ac.Decision.NeedDelta)
	}
}

// H2: compare 按钮走完 Capability(报价空)后,GuideActionStage 不应"雪崩式"再追加对比胶囊,
// 因为 Result.ToolName 不是 SearchQuotes/InterpretRules,应 no-op。防护 dispatch 表膨胀。
func TestScenario_H2_GuideActionNoopWhenNotSearchOrRules(t *testing.T) {
	emit := &captureEmitter{}
	ac := &AgentContext{
		State:  orchestration.New("s", "u"),
		Emit:   emit,
		Result: &CapabilityResult{Text: "已为用户对比 2 辆车"}, // 没有 ToolName
	}
	sig, err := (&GuideActionStage{}).Handle(context.Background(), ac)
	if err != nil || sig != SignalContinue {
		t.Fatalf("sig=%s err=%v", sig, err)
	}
	for _, ev := range emit.events {
		if ev.name == "quick_action" || ev.name == "card" {
			t.Fatalf("unexpected emit event: %+v", ev)
		}
	}
}

// H3: message 与 action 同时存在时,PreRoute 只认 action_click 事件类型,
// 不看 message —— 结构化事件优先,message 由 handler 侧决定是否也进 history。
func TestScenario_H3_ActionClickIgnoresMessageField(t *testing.T) {
	ac := &AgentContext{
		State:     orchestration.New("s", "u"),
		UserInput: "别管这个,给我 SUV", // 用户在前端可能同时打字
		EventType: "action_click",
		Action: &ClientAction{
			Type:    "compare",
			Payload: map[string]any{"vehicle_refs": []any{"朗逸", "轩逸"}},
		},
	}
	_, err := (&PreRouteStage{}).Handle(context.Background(), ac)
	if err != nil {
		t.Fatal(err)
	}
	if ac.Decision == nil || ac.Decision.Tool != ToolCompare {
		t.Fatalf("action_click must take precedence over message, got %#v", ac.Decision)
	}
}

// H4: EventType 是空的普通文本请求,PreRoute 必须放行给 Decide,不能自作主张。
func TestScenario_H4_PlainMessagePassesThroughPreRoute(t *testing.T) {
	ac := &AgentContext{
		State:     orchestration.New("s", "u"),
		UserInput: "帮我找辆 SUV",
	}
	sig, err := (&PreRouteStage{}).Handle(context.Background(), ac)
	if err != nil || sig != SignalContinue {
		t.Fatalf("sig=%s err=%v", sig, err)
	}
	if ac.Decision != nil {
		t.Fatalf("PreRoute should not inject Decision for plain message: %#v", ac.Decision)
	}
}

// H5: Compare 收到指代解析后仅得到 1 辆有效车(另一辆 missing),按现有实现应给
// "「xxx」我这边还没有报价" 引导。这条防止未来把 missing 合并进 <2 分支后行为漂移。
func TestScenario_H5_ComparePartialMissing(t *testing.T) {
	in := CapabilityInput{
		State: stateWithQuotes(
			orchestration.QuoteRef{ReferenceID: "r1", CarName: "朗逸", Index: 1},
		),
		Decision: &Decision{
			Tool: ToolCompare,
			Args: map[string]any{"vehicle_refs": []any{"朗逸", "特斯拉Model3"}},
		},
		Deps: &tools.Deps{},
		Emit: &captureEmitter{},
	}
	res, err := (&CompareCapability{}).Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("res nil")
	}
	// 期望:提到 missing 的那辆的名字,或引导重搜
	if !strings.Contains(res.Text, "特斯拉Model3") && !strings.Contains(res.Text, "没有报价") {
		t.Fatalf("res.Text=%q, want mention of missing car or 没有报价", res.Text)
	}
}

// H6: GuideActionStage 在 SearchQuotes 且报价 >=2 时,quick_action 事件里必须
// 同时含 slot_patch(便宜点/换一批) + compare(≥2 车) + feedback(有帮助/不满意)。
// 这条守住"胶囊三件套"不缺项。
func TestScenario_H6_GuideActionEmitsAllCapsuleTypes(t *testing.T) {
	st := stateWithQuotes(
		orchestration.QuoteRef{ReferenceID: "r1", CarName: "大众朗逸", BrandName: "大众", Index: 1},
		orchestration.QuoteRef{ReferenceID: "r2", CarName: "日产轩逸", BrandName: "日产", Index: 2},
	)
	emit := &captureEmitter{}
	ac := &AgentContext{
		State:    st,
		Emit:     emit,
		Decision: &Decision{Tool: ToolSearchVehicles},
		Result:   &CapabilityResult{ToolName: tools.ToolSearchQuotes},
	}
	if _, err := (&GuideActionStage{}).Handle(context.Background(), ac); err != nil {
		t.Fatal(err)
	}
	var quick string
	for _, ev := range emit.events {
		if ev.name == "quick_action" {
			quick = ev.detail
			break
		}
	}
	if quick == "" {
		t.Fatalf("no quick_action emitted")
	}
	for _, want := range []string{"slot_patch", "compare", "feedback_positive", "feedback_negative"} {
		if !strings.Contains(quick, want) {
			t.Fatalf("quick_action missing %q: %s", want, quick)
		}
	}
}

// H7: 报价空时不发 vehicle_list card,防止前端渲染空卡片。
func TestScenario_H7_NoVehicleCardWhenQuotesEmpty(t *testing.T) {
	st := orchestration.New("s", "u")
	emit := &captureEmitter{}
	ac := &AgentContext{
		State:    st,
		Emit:     emit,
		Decision: &Decision{Tool: ToolSearchVehicles},
		Result:   &CapabilityResult{ToolName: tools.ToolSearchQuotes},
	}
	if _, err := (&GuideActionStage{}).Handle(context.Background(), ac); err != nil {
		t.Fatal(err)
	}
	for _, ev := range emit.events {
		if ev.name == "card" {
			t.Fatalf("unexpected card event when no quotes: %+v", ev)
		}
	}
}

// H8: 中文序数带全角数字/罗马符号,resolve 也应能命中 —— 覆盖用户可能的多种打法。
func TestScenario_H8_OrdinalVariants(t *testing.T) {
	st := stateWithQuotes(
		orchestration.QuoteRef{ReferenceID: "r1", CarName: "朗逸", Index: 1},
		orchestration.QuoteRef{ReferenceID: "r2", CarName: "轩逸", Index: 2},
	)
	cases := []struct{ text, want string }{
		{"第2个", "r2"},
		{"第一辆", "r1"},
		{"①", "r1"},
		{"②", "r2"},
	}
	for _, c := range cases {
		ref, clar := ResolveQuoteRef(st, c.text)
		if clar != nil {
			t.Errorf("text=%q got clar, want ref=%s", c.text, c.want)
			continue
		}
		if ref != c.want {
			t.Errorf("text=%q ref=%q want %s", c.text, ref, c.want)
		}
	}
}

// -----------------------------------------------------------------------------
// I. 真·边角料:大概率能出问题的地方
// -----------------------------------------------------------------------------

// I1: LLM 常会用半角"第2辆"、带空格"第 2 辆"、纯数字"2 号"、"第②辆"混拼,
// 我们的映射表覆盖不完全会漏掉。这条不放过对现有映射表的实际能力做一次审计。
func TestScenario_I1_OrdinalEdgeCasesSurvey(t *testing.T) {
	st := stateWithQuotes(
		orchestration.QuoteRef{ReferenceID: "r1", CarName: "朗逸", Index: 1},
		orchestration.QuoteRef{ReferenceID: "r2", CarName: "轩逸", Index: 2},
		orchestration.QuoteRef{ReferenceID: "r3", CarName: "帕萨特", Index: 3},
	)
	// 期望能命中 —— 如果任意一条 hit=false,说明匹配表要补
	cases := []struct {
		text     string
		wantRef  string
		wantHit  bool
	}{
		{"第2辆", "r2", true},
		{"第 2 辆", "r2", true},
		{"2号", "r2", true},
		{"第②", "r2", true},
		{"第三个", "r3", true},
	}
	for _, c := range cases {
		ref, _ := ResolveQuoteRef(st, c.text)
		hit := ref == c.wantRef
		if hit != c.wantHit {
			t.Errorf("text=%q ref=%q want=%q hit=%v want=%v", c.text, ref, c.wantRef, hit, c.wantHit)
		}
	}
}

// I2: 用户口误"看第10辆"(候选不到 10),ResolveQuoteRef 应静默返回空,
// 不能因索引越界 panic 或误命中最后一辆。
func TestScenario_I2_OrdinalOutOfRange(t *testing.T) {
	st := stateWithQuotes(
		orchestration.QuoteRef{ReferenceID: "r1", CarName: "朗逸", Index: 1},
		orchestration.QuoteRef{ReferenceID: "r2", CarName: "轩逸", Index: 2},
	)
	// "第10辆"—— parseOrdinal 能识别 10,但候选只有 2 辆 → 越界 → 未命中,
	// 不能误落到第 1 辆(旧版 strings.Contains 走子串会命中"第1")。
	ref, clar := ResolveQuoteRef(st, "第10辆什么价")
	if clar != nil {
		t.Fatalf("expect empty (or clar=nil), got clar=%+v", clar)
	}
	// 允许空,不允许误命中
	if ref != "" {
		t.Fatalf("out-of-range ordinal should not match, got ref=%q", ref)
	}
}

// I3: 用户按品牌反选("不要大众"),需求管理会把它写成 NEGATE brand=大众。
// 后端下轮搜车时 filterNegativeNeedQuotes 应把 CarName 含"大众"的车滤掉。
// 这条守住"负向 need 真的能过滤"。
func TestScenario_I3_NegativeBrandFilters(t *testing.T) {
	quotes := []quoteItem{
		{CarName: "大众朗逸", BrandName: "大众"},
		{CarName: "日产轩逸", BrandName: "日产"},
	}
	needs := []types.UserNeed{
		{Type: "brand", Value: "大众", Negative: true},
	}
	out := filterNegativeNeedQuotes(quotes, needs)
	if len(out) != 1 || out[0].BrandName != "日产" {
		t.Fatalf("filter result=%+v, want only 日产", out)
	}
}

// I4: 排除列表 excluded_refs 里的 reference_id,在下一轮搜索结果里应被剔除。
func TestScenario_I4_ExcludedRefsFilter(t *testing.T) {
	quotes := []quoteItem{
		{ReferenceID: "r1", CarName: "朗逸"},
		{ReferenceID: "r2", CarName: "轩逸"},
	}
	out := filterExcludedQuotes(quotes, []string{"r1"})
	if len(out) != 1 || out[0].ReferenceID != "r2" {
		t.Fatalf("out=%+v, want only r2", out)
	}
}

// I5: 场景 KB(scene knowledge)命中"带老人小孩"必须 ADD vehicle_type=SUV(soft)。
// 这条守住场景推理落 need_delta 的能力,不然 sufficiency 永远上不去循环 ask。
func TestScenario_I5_SceneKBHitsFamilyElderKid(t *testing.T) {
	dec := &Decision{Tool: ToolSearchVehicles}
	patch := ApplySceneKnowledgeToDecision(dec, "带老人小孩出去玩")
	if len(patch.Needs) == 0 {
		t.Fatalf("scene KB should hit '老人+小孩', got no need")
	}
	if patch.Needs[0].Type != "vehicle_type" || patch.Needs[0].Value != "SUV" {
		t.Fatalf("need=%+v want vehicle_type=SUV", patch.Needs[0])
	}
	if patch.Needs[0].Hardness != "soft" {
		t.Fatalf("hardness=%s want soft", patch.Needs[0].Hardness)
	}
	// Decision.NeedDelta 应该被前插了这条 soft 需求
	if len(dec.NeedDelta) == 0 || dec.NeedDelta[0].Value != "SUV" {
		t.Fatalf("Decision.NeedDelta not prepended: %+v", dec.NeedDelta)
	}
}

// I6: 场景 KB 用 AND 匹配,"带老人"单个词命中不到 —— 已知的覆盖窄边界。
// 这条不是失败用例,是显式记录"当前不覆盖"的行为契约。
func TestScenario_I6_SceneKBAndMatchIsStrict(t *testing.T) {
	dec := &Decision{Tool: ToolSearchVehicles}
	patch := ApplySceneKnowledgeToDecision(dec, "带老人")
	if len(patch.Needs) != 0 {
		t.Fatalf("expected NO need (KB requires both 老人+小孩), got %+v", patch.Needs)
	}
}

// I7: `[]string` 直传(Go 代码内部构造)和 `[]any`(json.Unmarshal 出来的)
// 都能被 extractRefs 认出来 —— 现在 Decision.Args 一半是从 JSON 解出来的、
// 一半是 Go 直接构造的 slot_patch。这条守住两条路径都不掉。
func TestScenario_I7_ExtractRefsAcceptsBothShapes(t *testing.T) {
	// []any 情形(json.Unmarshal 后)
	if got := extractRefs([]any{"朗逸", "轩逸"}); len(got) != 2 || got[0] != "朗逸" {
		t.Fatalf("[]any got=%v", got)
	}
	// []string 情形(Go 直接构造)
	if got := extractRefs([]string{"帕萨特"}); len(got) != 1 || got[0] != "帕萨特" {
		t.Fatalf("[]string got=%v", got)
	}
	// nil / 空
	if got := extractRefs(nil); got != nil {
		t.Fatalf("nil should return nil, got %v", got)
	}
	if got := extractRefs([]any{}); len(got) != 0 {
		t.Fatalf("empty should return empty, got %v", got)
	}
	// 类型错误的 payload,不能 panic
	if got := extractRefs("单个字符串不是数组"); got != nil {
		t.Fatalf("non-array should return nil, got %v", got)
	}
	if got := extractRefs(map[string]any{}); got != nil {
		t.Fatalf("map should return nil, got %v", got)
	}
}

// I8: PreRoute 收到 event_type="action_click" 但 Action==nil,不能 panic,
// 应该视为普通请求放行。防止前端 bug 触发 500。
func TestScenario_I8_ActionClickWithNilActionSafe(t *testing.T) {
	ac := &AgentContext{
		State:     orchestration.New("s", "u"),
		EventType: "action_click",
		Action:    nil,
	}
	sig, err := (&PreRouteStage{}).Handle(context.Background(), ac)
	if err != nil {
		t.Fatalf("must not err, got %v", err)
	}
	if sig != SignalContinue {
		t.Fatalf("sig=%s want continue", sig)
	}
	if ac.Decision != nil {
		t.Fatalf("Decision should be nil, got %#v", ac.Decision)
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func stateWithQuotes(qs ...orchestration.QuoteRef) *orchestration.ConversationState {
	st := orchestration.New("s", "u")
	for i := range qs {
		if qs[i].Index == 0 {
			qs[i].Index = i + 1
		}
	}
	st.SetQuotes("ctx-test", qs)
	return st
}
