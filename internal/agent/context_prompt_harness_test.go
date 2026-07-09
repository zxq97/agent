package agent

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/types"
)

func TestPromptHarnessIntentBadcasesParseToSearchControls(t *testing.T) {
	cases := []struct {
		name        string
		userText    string
		args        string
		wantMode    string
		wantRef     string
		wantDeltas  []string
		wantProfile string
	}{
		{
			name:     "dislike first car excludes concrete feedback ref and brand",
			userText: "第一辆不喜欢,不要大众",
			args: `{
				"search_mode":"negative_feedback",
				"feedback_ref":"第一辆",
				"need_delta":[{"op":"NEGATE","type":"brand","value":"大众","hardness":"hard","confidence":0.92}]
			}`,
			wantMode:   SearchModeNegativeFeedback,
			wantRef:    "第一辆",
			wantDeltas: []string{"NEGATE:brand:大众"},
		},
		{
			name:     "next batch keeps filters and does not invent need deltas",
			userText: "换一批看看",
			args: `{
				"search_mode":"page",
				"need_delta":[]
			}`,
			wantMode: SearchModePage,
		},
		{
			name:     "budget down is preserved as relative price preference",
			userText: "预算再低一点",
			args: `{
				"search_mode":"budget_down",
				"need_delta":[{"op":"UPDATE","type":"price_preference","value":"更低预算","hardness":"hard","confidence":0.84}],
				"understanding":{"sufficiency":0.76,"covered_dims":["price_preference"]}
			}`,
			wantMode:   SearchModeBudgetDown,
			wantDeltas: []string{"UPDATE:price_preference:更低预算"},
		},
		{
			name:     "budget up is preserved as relative price preference",
			userText: "贵一点也行,车好点",
			args: `{
				"search_mode":"budget_up",
				"need_delta":[{"op":"UPDATE","type":"price_preference","value":"预算高一点也行","hardness":"soft","confidence":0.78}]
			}`,
			wantMode:   SearchModeBudgetUp,
			wantDeltas: []string{"UPDATE:price_preference:预算高一点也行"},
		},
		{
			name:     "family scene writes soft inferred need and profile patch",
			userText: "带老人小孩出去玩,空间舒服点",
			args: `{
				"search_mode":"initial",
				"need_delta":[
					{"op":"ADD","type":"vehicle_type","value":"SUV","hardness":"soft","confidence":0.72},
					{"op":"ADD","type":"comfort_preference","value":"空间舒适","hardness":"soft","confidence":0.8}
				],
				"profile_patch":{"trip_scene":"家庭出游","companions":"老人小孩","style_preference":"空间舒适"}
			}`,
			wantMode:    SearchModeInitial,
			wantDeltas:  []string{"ADD:vehicle_type:SUV", "ADD:comfort_preference:空间舒适"},
			wantProfile: "老人小孩",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dec := buildDecision([]llm.ToolCall{{Function: llm.FunctionCall{Name: ToolSearchVehicles, Arguments: tt.args}}}, "")
			if dec.SearchMode != tt.wantMode {
				t.Fatalf("%q SearchMode=%q, want %q", tt.userText, dec.SearchMode, tt.wantMode)
			}
			if dec.FeedbackRef != tt.wantRef {
				t.Fatalf("%q FeedbackRef=%q, want %q", tt.userText, dec.FeedbackRef, tt.wantRef)
			}
			gotDeltas := make([]string, 0, len(dec.NeedDelta))
			for _, d := range dec.NeedDelta {
				gotDeltas = append(gotDeltas, d.Op+":"+d.Type+":"+stringValue(d.Value))
			}
			if strings.Join(gotDeltas, "|") != strings.Join(tt.wantDeltas, "|") {
				t.Fatalf("%q deltas=%v, want %v", tt.userText, gotDeltas, tt.wantDeltas)
			}
			if tt.wantProfile != "" {
				if dec.ProfilePatch == nil || dec.ProfilePatch.Companions != tt.wantProfile {
					t.Fatalf("%q ProfilePatch=%+v, want companions=%q", tt.userText, dec.ProfilePatch, tt.wantProfile)
				}
			}
		})
	}
}

func TestPromptHarnessBuildMessagesInjectsStateAndReplaysToolHistory(t *testing.T) {
	st := orchestration.New("sess-harness", "user-harness")
	st.Summary = "用户上一轮看过经济型车,觉得第一辆偏贵。"
	st.Rental.PickupName = "首都机场T3"
	st.Rental.PickupCityID = 1
	st.Rental.ContextID = "ctx-secret"
	st.Profile = orchestration.Profile{TripScene: "家庭出游", Companions: "老人小孩", PriceSensitivity: "high"}
	st.Constraints = types.SearchConstraints{
		Hard:     []types.UserNeed{{Type: "seat_num", Value: 5, Hardness: "hard", Confidence: 0.9}},
		Soft:     []types.UserNeed{{Type: "vehicle_type", Value: "SUV", Hardness: "soft", Confidence: 0.72}},
		Negative: []types.UserNeed{{Type: "brand", Value: "大众", Hardness: "hard", Negative: true}},
	}
	st.SetQuotes("ctx-secret", []orchestration.QuoteRef{
		{ReferenceID: "ref-secret-1", Supplier: "supplier-secret-1", CarName: "大众朗逸", BrandName: "大众", DailyPrice: 188, TotalPrice: 564, Index: 1},
		{ReferenceID: "ref-secret-2", Supplier: "supplier-secret-2", CarName: "日产轩逸", BrandName: "日产", DailyPrice: 208, TotalPrice: 624, Index: 2},
	})
	st.AppendMessage(&llm.Message{Role: llm.RoleUser, Content: "我带老人小孩,想舒服点"}, nil)
	st.AppendMessage(nil, &types.ToolCallSnapshot{
		Name:      ToolSearchVehicles,
		Arguments: `{"search_mode":"initial"}`,
		Result:    "返回 2 辆候选: 大众朗逸、日产轩逸",
	})

	msgs := NewDecider(nil, "").buildMessages(st, "换一批,预算低一点")
	if len(msgs) < 4 {
		t.Fatalf("messages len=%d, want history + tool replay + current user", len(msgs))
	}
	if msgs[1].Role != llm.RoleAssistant || len(msgs[1].ToolCalls) != 1 {
		t.Fatalf("tool history assistant replay = %+v", msgs[1])
	}
	if msgs[2].Role != llm.RoleTool || !strings.Contains(msgs[2].Content, "返回 2 辆候选") {
		t.Fatalf("tool history result replay = %+v", msgs[2])
	}

	last := msgs[len(msgs)-1]
	if last.Role != llm.RoleUser {
		t.Fatalf("last role=%q, want user", last.Role)
	}
	mustContainAll(t, last.Content,
		"## 当前会话状态",
		"summary: 用户上一轮看过经济型车",
		"profile:",
		"companions: 老人小孩",
		"seat_num=5",
		"vehicle_type=SUV",
		"排除:brand=大众",
		"last_quotes:",
		"index=1, name=大众朗逸",
		"换一批,预算低一点",
	)
	mustNotContainAny(t, last.Content, "ctx-secret", "ref-secret", "supplier-secret")
}

func TestPromptHarnessNeedConfidenceDecaysAcrossTurnsAndRestoresOnReinforce(t *testing.T) {
	needs := []types.UserNeed{
		{Type: "vehicle_model", Value: "朗逸", Hardness: "hard", Confidence: 0.9, BornTurn: 1, LastReinforced: 1},
		{Type: "price_preference", Value: "200左右", Hardness: "hard", Confidence: 0.88, BornTurn: 1, LastReinforced: 1},
	}

	for turn := 2; turn <= 4; turn++ {
		needs = TickNeeds(needs, turn)
	}
	if stateOf(needs, "vehicle_model") != types.NeedActiveSoft {
		t.Fatalf("vehicle_model state after natural decay=%s, want active_soft", stateOf(needs, "vehicle_model"))
	}

	deltas := []types.NeedDelta{{Op: DeltaAdd, Type: "vehicle_type", Value: "经济型", Hardness: "hard", Confidence: 0.86}}
	needs = ApplyDelta(needs, deltas, 5)
	needs = ApplyConflictDecay(needs, deltas)
	if stateOf(needs, "vehicle_model") != types.NeedDormant {
		t.Fatalf("vehicle_model state after conflict=%s, want dormant", stateOf(needs, "vehicle_model"))
	}
	if active := FilterActiveNeeds(needs); containsNeed(active, "vehicle_model") {
		t.Fatalf("dormant vehicle_model should not be active: %+v", active)
	}

	needs = ApplyDelta(needs, []types.NeedDelta{{Op: DeltaReinforce, Type: "vehicle_model"}}, 6)
	if got := confidenceOf(needs, "vehicle_model"); got != ReinforceRestore {
		t.Fatalf("reinforced confidence=%.2f, want %.2f", got, ReinforceRestore)
	}
	if stateOf(needs, "vehicle_model") != types.NeedActiveHard {
		t.Fatalf("vehicle_model state after reinforce=%s, want active_hard", stateOf(needs, "vehicle_model"))
	}
	c := UpdateConstraints(needs)
	if !containsNeed(c.Hard, "vehicle_model") {
		t.Fatalf("reinforced vehicle_model should be hard constraint: %+v", c.Hard)
	}
}

func TestPromptHarnessDeciderUsesSyncFallbackWhenStreamCannotStart(t *testing.T) {
	model := &streamStartErrorModel{
		resp: &llm.ChatResponse{
			Content: "我先按更低预算帮你换一批。",
			ToolCalls: []llm.ToolCall{{Function: llm.FunctionCall{Name: ToolSearchVehicles, Arguments: `{
				"search_mode":"budget_down",
				"need_delta":[{"op":"UPDATE","type":"price_preference","value":"更低预算","hardness":"hard","confidence":0.82}]
			}`}}},
			Usage: llm.Usage{TotalTokens: 42},
		},
	}
	st := orchestration.New("sess", "user")
	emit := &captureEmitter{}
	var logs bytes.Buffer

	dec, err := NewDecider(model, "system prompt").Decide(context.Background(), st, "预算低一点", emit, &logs)
	if err != nil {
		t.Fatal(err)
	}
	if !model.chatCalled {
		t.Fatalf("sync Chat was not called after stream start error")
	}
	if dec.SearchMode != SearchModeBudgetDown {
		t.Fatalf("SearchMode=%q, want %q", dec.SearchMode, SearchModeBudgetDown)
	}
	if got := strings.Join(emit.texts, ""); got != "我先按更低预算帮你换一批。" {
		t.Fatalf("emitted text=%q", got)
	}
	mustContainAll(t, logs.String(), "mode=stream start_error", "mode=sync_fallback status=ok")
}

type streamStartErrorModel struct {
	resp       *llm.ChatResponse
	chatCalled bool
	lastReq    llm.ChatRequest
}

func (m *streamStartErrorModel) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.chatCalled = true
	m.lastReq = req
	return m.resp, nil
}

func (m *streamStartErrorModel) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	m.lastReq = req
	return nil, errors.New("stream unavailable")
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func mustContainAll(t *testing.T, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
	}
}

func mustNotContainAny(t *testing.T, text string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			t.Fatalf("text leaked %q:\n%s", needle, text)
		}
	}
}

func stateOf(needs []types.UserNeed, typ string) types.NeedState {
	for i := range needs {
		if needs[i].Type == typ {
			return needs[i].State()
		}
	}
	return types.NeedDormant
}

func confidenceOf(needs []types.UserNeed, typ string) float64 {
	for _, n := range needs {
		if n.Type == typ {
			return n.Confidence
		}
	}
	return 0
}

func containsNeed(needs []types.UserNeed, typ string) bool {
	for _, n := range needs {
		if n.Type == typ {
			return true
		}
	}
	return false
}
