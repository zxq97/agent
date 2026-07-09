package agent

import (
	"testing"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/types"
)

func TestBuildDecisionParsesSearchModeAndFeedbackRef(t *testing.T) {
	dec := buildDecision([]llm.ToolCall{{
		Function: llm.FunctionCall{
			Name: ToolSearchVehicles,
			Arguments: `{
				"search_mode":"negative_feedback",
				"feedback_ref":"第一辆",
				"need_delta":[
					{"op":"NEGATE","type":"brand","value":"比亚迪","hardness":"hard","confidence":0.9}
				]
			}`,
		},
	}}, "")

	if dec.SearchMode != SearchModeNegativeFeedback {
		t.Fatalf("SearchMode = %q, want %q", dec.SearchMode, SearchModeNegativeFeedback)
	}
	if dec.FeedbackRef != "第一辆" {
		t.Fatalf("FeedbackRef = %q, want 第一辆", dec.FeedbackRef)
	}
}

func TestBuildDecisionParsesRentalTimesForSearch(t *testing.T) {
	dec := buildDecision([]llm.ToolCall{{
		Function: llm.FunctionCall{
			Name: ToolSearchVehicles,
			Arguments: `{
				"search_mode":"initial",
				"pickup_text":"北京首都机场",
				"pickup_time":"2026-07-12 10:00",
				"dropoff_time":"2026-07-13 18:00"
			}`,
		},
	}}, "")

	if dec.PickupText != "北京首都机场" {
		t.Fatalf("PickupText=%q", dec.PickupText)
	}
	if dec.PickupTimeText != "2026-07-12 10:00" || dec.DropoffTimeText != "2026-07-13 18:00" {
		t.Fatalf("times pickup=%q dropoff=%q", dec.PickupTimeText, dec.DropoffTimeText)
	}
}

func TestBuildDecisionRepairsTrailingGarbageInSearchArguments(t *testing.T) {
	dec := buildDecision([]llm.ToolCall{{
		Function: llm.FunctionCall{
			Name:      ToolSearchVehicles,
			Arguments: `{"search_mode":"initial","pickup_text":"首都机场T3","pickup_time":"2026-07-09 18:00","dropoff_time":"2026-07-11 12:00"}}`,
		},
	}}, "")

	if dec.SearchMode != SearchModeInitial {
		t.Fatalf("SearchMode=%q, want %q", dec.SearchMode, SearchModeInitial)
	}
	if dec.PickupText != "首都机场T3" {
		t.Fatalf("PickupText=%q", dec.PickupText)
	}
	if dec.PickupTimeText != "2026-07-09 18:00" || dec.DropoffTimeText != "2026-07-11 12:00" {
		t.Fatalf("times pickup=%q dropoff=%q", dec.PickupTimeText, dec.DropoffTimeText)
	}
	if dec.ArgsDiag == nil || !dec.ArgsDiag.Repaired || dec.ArgsDiag.ParseError == "" {
		t.Fatalf("ArgsDiag=%#v, want repaired parse diagnostic", dec.ArgsDiag)
	}
}

func TestBuildDecisionCapturesUnrepairableArgumentsParseError(t *testing.T) {
	dec := buildDecision([]llm.ToolCall{{
		Function: llm.FunctionCall{
			Name:      ToolSearchVehicles,
			Arguments: `{"search_mode":`,
		},
	}}, "")

	if dec.ArgsDiag == nil || dec.ArgsDiag.ParseError == "" {
		t.Fatalf("ArgsDiag=%#v, want parse error", dec.ArgsDiag)
	}
	if len(dec.Args) != 0 {
		t.Fatalf("Args=%v, want empty args after unrepairable parse error", dec.Args)
	}
}

func TestBuildDecisionValidatesSearchArgumentsSchemaAndBusiness(t *testing.T) {
	dec := buildDecision([]llm.ToolCall{{
		Function: llm.FunctionCall{
			Name: ToolSearchVehicles,
			Arguments: `{
				"search_mode":"unknown",
				"pickup_text":"SUV",
				"pickup_time":"明天下午"
			}`,
		},
	}}, "")

	if dec.ArgsDiag == nil || len(dec.ArgsDiag.ValidationErrors) == 0 {
		t.Fatalf("ArgsDiag=%#v, want validation errors", dec.ArgsDiag)
	}
	want := []string{"search_mode invalid", "pickup_text looks like vehicle/filter need", "pickup_time invalid"}
	for _, w := range want {
		if !containsValidationError(dec.ArgsDiag.ValidationErrors, w) {
			t.Fatalf("ValidationErrors=%v, missing %q", dec.ArgsDiag.ValidationErrors, w)
		}
	}
}

func TestBuildDecisionValidatesUpdateRentalArgumentsBusiness(t *testing.T) {
	dec := buildDecision([]llm.ToolCall{{
		Function: llm.FunctionCall{
			Name:      ToolUpdateRental,
			Arguments: `{"pickup_text":"轿车","pickup_time":"2026-07-09 18:00"}`,
		},
	}}, "")

	if dec.ArgsDiag == nil || !containsValidationError(dec.ArgsDiag.ValidationErrors, "pickup_text looks like vehicle/filter need") {
		t.Fatalf("ArgsDiag=%#v, want pickup_text business validation error", dec.ArgsDiag)
	}
}

func containsValidationError(errors []string, want string) bool {
	for _, err := range errors {
		if err == want {
			return true
		}
	}
	return false
}

func TestApplyConfidenceGateDowngradesLowConfidenceHardDelta(t *testing.T) {
	dec := &Decision{
		NeedDelta: []types.NeedDelta{{
			Op:         DeltaAdd,
			Type:       "vehicle_type",
			Value:      "SUV",
			Hardness:   "hard",
			Confidence: 0.45,
		}},
		Understanding: &Understanding{Sufficiency: 0.8},
	}

	result := ApplyConfidenceGate(dec, nil)

	if result.Action != ConfidenceActionSearch {
		t.Fatalf("Action = %q, want %q", result.Action, ConfidenceActionSearch)
	}
	if got := result.NormalizedDelta[0].Hardness; got != "soft" {
		t.Fatalf("low-confidence hard delta hardness = %q, want soft", got)
	}
}

func TestApplyConfidenceGateRoutesGraySufficiencyToInterpreter(t *testing.T) {
	dec := &Decision{
		NeedDelta: []types.NeedDelta{{
			Op:         DeltaUpdate,
			Type:       "price_preference",
			Value:      "更低预算",
			Hardness:   "hard",
			Confidence: 0.8,
		}},
		Understanding: &Understanding{Sufficiency: 0.55},
	}

	result := ApplyConfidenceGate(dec, nil)

	if result.Action != ConfidenceActionInterpret {
		t.Fatalf("Action = %q, want %q", result.Action, ConfidenceActionInterpret)
	}
}

func TestApplyIterationPolicyPagesFromLastSearch(t *testing.T) {
	st := orchestration.New("s1", "u1")
	st.LastSearch = &types.LastSearchState{
		FilterCodes: []string{"filter/vehcle_choice/suv"},
		Page:        2,
		PageSize:    6,
		HasMore:     true,
		ShownRefs:   []string{"ref_old"},
	}
	plan := ApplyIterationPolicy(&Decision{SearchMode: SearchModePage}, st, []string{"ignored"})

	if plan.Page != 3 {
		t.Fatalf("Page = %d, want 3", plan.Page)
	}
	if got := plan.FilterCodes[0]; got != "filter/vehcle_choice/suv" {
		t.Fatalf("FilterCodes[0] = %q, want previous filter", got)
	}
	if len(plan.ExcludedRefs) != 1 || plan.ExcludedRefs[0] != "ref_old" {
		t.Fatalf("ExcludedRefs = %#v, want previous shown ref", plan.ExcludedRefs)
	}
}

func TestApplyIterationPolicyExcludesFeedbackRef(t *testing.T) {
	st := orchestration.New("s1", "u1")
	st.SetQuotes("ctx1", []orchestration.QuoteRef{{
		ReferenceID: "ref1",
		Supplier:    "supplier1",
		CarName:     "大众朗逸",
		BrandName:   "大众",
		Index:       1,
	}})

	plan := ApplyIterationPolicy(&Decision{
		SearchMode:  SearchModeNegativeFeedback,
		FeedbackRef: "第一辆",
	}, st, []string{"filter/vehcle_choice/jiaoche"})

	if len(plan.ExcludedRefs) != 1 || plan.ExcludedRefs[0] != "ref1" {
		t.Fatalf("ExcludedRefs = %#v, want [ref1]", plan.ExcludedRefs)
	}
	if plan.Page != 1 {
		t.Fatalf("Page = %d, want 1", plan.Page)
	}
}

func TestStaticRecallMapsBudgetPreferenceFromMenu(t *testing.T) {
	codes, uncovered := StaticRecall([]types.UserNeed{{
		Type:       "price_preference",
		Value:      "200以内",
		Hardness:   "hard",
		Confidence: 0.9,
	}}, []types.MenuGroupView{{
		GroupCode: "filter/total_fee",
		GroupName: "价格",
		Items: []types.MenuItemView{
			{ItemCode: "filter/total_fee/le_200", Name: "200元以下"},
			{ItemCode: "filter/total_fee/200_300", Name: "200-300元"},
		},
	}})

	if len(uncovered) != 0 {
		t.Fatalf("uncovered = %#v, want none", uncovered)
	}
	if len(codes) != 1 || codes[0] != "filter/total_fee/le_200" {
		t.Fatalf("codes = %#v, want [filter/total_fee/le_200]", codes)
	}
}

func TestStaticRecallMapsRelativeBudgetPreferences(t *testing.T) {
	menu := []types.MenuGroupView{{
		GroupCode: "filter/total_fee",
		Items: []types.MenuItemView{
			{ItemCode: "filter/total_fee/le_200", Name: "200元以下"},
			{ItemCode: "filter/total_fee/300_500", Name: "300-500元"},
		},
	}}

	lowCodes, _ := StaticRecall([]types.UserNeed{{Type: "price_preference", Value: "更低预算"}}, menu)
	highCodes, _ := StaticRecall([]types.UserNeed{{Type: "price_preference", Value: "预算高一点也行"}}, menu)

	if len(lowCodes) != 1 || lowCodes[0] != "filter/total_fee/le_200" {
		t.Fatalf("lowCodes = %#v, want low budget bucket", lowCodes)
	}
	if len(highCodes) != 1 || highCodes[0] != "filter/total_fee/300_500" {
		t.Fatalf("highCodes = %#v, want high budget bucket", highCodes)
	}
}

func TestStaticRecallMapsBrandFromMenu(t *testing.T) {
	menu := []types.MenuGroupView{{
		GroupCode: "filter/brand",
		GroupName: "品牌",
		Items: []types.MenuItemView{
			{ItemCode: "filter/brand/tesla", Name: "特斯拉"},
			{ItemCode: "filter/brand/byd", Name: "比亚迪"},
		},
	}}

	codes, uncovered := StaticRecall([]types.UserNeed{{Type: "brand", Value: "特斯拉"}}, menu)

	if len(uncovered) != 0 {
		t.Fatalf("uncovered = %#v, want none", uncovered)
	}
	if len(codes) != 1 || codes[0] != "filter/brand/tesla" {
		t.Fatalf("codes = %#v, want tesla brand code", codes)
	}
}

func TestStaticRecallMapsVehicleModelFromMenuWithNormalization(t *testing.T) {
	menu := []types.MenuGroupView{{
		GroupCode: "filter/model",
		GroupName: "车型",
		Items: []types.MenuItemView{
			{ItemCode: "filter/model/model_y", Name: "Model Y"},
		},
	}}

	codes, uncovered := StaticRecall([]types.UserNeed{{Type: "vehicle_model", Value: "MODEL Y"}}, menu)

	if len(uncovered) != 0 {
		t.Fatalf("uncovered = %#v, want none", uncovered)
	}
	if len(codes) != 1 || codes[0] != "filter/model/model_y" {
		t.Fatalf("codes = %#v, want model y code", codes)
	}
}

func TestStaticRecallMapsVehicleModelFromMemoryAlias(t *testing.T) {
	menu := []types.MenuGroupView{{
		GroupCode: "filter/model",
		GroupName: "车型",
		Items: []types.MenuItemView{
			{ItemCode: "filter/model/model_y", Name: "Model Y"},
		},
	}}

	codes, uncovered := StaticRecall([]types.UserNeed{{Type: "vehicle_model", Value: "毛豆Y"}}, menu)

	if len(uncovered) != 0 {
		t.Fatalf("uncovered = %#v, want none", uncovered)
	}
	if len(codes) != 1 || codes[0] != "filter/model/model_y" {
		t.Fatalf("codes = %#v, want model y code", codes)
	}
}

func TestFilterNegativeNeedQuotesRemovesBrand(t *testing.T) {
	quotes := []quoteItem{
		{ReferenceID: "ref1", BrandName: "比亚迪", CarName: "比亚迪秦"},
		{ReferenceID: "ref2", BrandName: "大众", CarName: "大众朗逸"},
	}
	filtered := filterNegativeNeedQuotes(quotes, []types.UserNeed{{
		Type:     "brand",
		Value:    "比亚迪",
		Negative: true,
	}})

	if len(filtered) != 1 || filtered[0].ReferenceID != "ref2" {
		t.Fatalf("filtered = %#v, want only ref2", filtered)
	}
}
