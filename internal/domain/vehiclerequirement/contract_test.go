package vehiclerequirement

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zxq97/agent/internal/requirement"
)

func TestDecodeExtractResult(t *testing.T) {
	content := `{"requirements":[{"raw_text":"特斯拉 Model Y","semantic_label":"","category":"vehicle","canonical_type":"vehicle_model","value":{"kind":"text","text":"Model Y","unit":""},"operation":"replace","operator":"eq","importance":"hard","confidence":0.99,"entity_context":{"brand_hint":"特斯拉","series_hint":""}},{"raw_text":"最好7座","semantic_label":"","category":"configuration","canonical_type":"seat_num","value":{"kind":"number","number":7,"unit":"seat"},"operation":"replace","operator":"eq","importance":"soft","confidence":0.95,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`
	result, err := decodeExtractResult(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Requirements) != 2 ||
		result.Requirements[0].CanonicalType != FacetVehicleModel ||
		result.Requirements[0].RawValue != "Model Y" ||
		result.Requirements[1].Value.Kind != requirement.ValueNumber ||
		result.Requirements[1].RawValue != "7" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDecodeExtractResultPreservesOpenSemanticRequirement(t *testing.T) {
	content := `{"requirements":[{"raw_text":"适合带老人出行","semantic_label":"elderly_friendly","category":"usage_scenario","canonical_type":null,"value":null,"operation":"add","operator":"eq","importance":"soft","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`
	result, err := decodeExtractResult(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Requirements) != 1 ||
		result.Requirements[0].CanonicalType != "" ||
		result.Requirements[0].SemanticLabel != "elderly_friendly" ||
		result.Requirements[0].Category != requirement.CategoryUsageScenario {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDecodeExtractResultRejectsSearchWithoutRequirements(t *testing.T) {
	if _, err := decodeExtractResult(`{"requirements":[],"domain_matched":true}`); err == nil {
		t.Fatal("expected domain_matched contract error")
	}
}

func TestDecodeExtractResultAllowsRemoveCanonicalType(t *testing.T) {
	result, err := decodeExtractResult(`{"requirements":[{"raw_text":"品牌不限","semantic_label":"","category":"vehicle","canonical_type":"brand","value":null,"operation":"remove","operator":"eq","importance":"hard","confidence":1,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Requirements) != 1 || result.Requirements[0].Operation != OperationRemove {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDecodeExtractResultRejectsInvalidContract(t *testing.T) {
	tests := []string{
		`{"requirements":[]}`,
		`{"requirements":[{"raw_text":"特斯拉","semantic_label":"","category":"vehicle","canonical_type":"unknown_type","value":{"kind":"text","text":"特斯拉"},"operation":"add","operator":"eq","importance":"hard","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
		`{"requirements":[{"raw_text":"适合老人","semantic_label":"","category":"usage_scenario","canonical_type":null,"value":null,"operation":"add","operator":"eq","importance":"soft","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
		`{"requirements":[{"raw_text":"7座","semantic_label":"","category":"configuration","canonical_type":"seat_num","value":{"kind":"number","text":"7"},"operation":"add","operator":"eq","importance":"hard","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
		`{"requirements":[{"raw_text":"7座","semantic_label":"","category":"configuration","canonical_type":"seat_num","value":{"kind":"text","text":"7"},"operation":"add","operator":"eq","importance":"hard","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
		`{"requirements":[{"raw_text":"特斯拉","semantic_label":"","category":"price","canonical_type":"brand","value":{"kind":"text","text":"特斯拉"},"operation":"add","operator":"eq","importance":"hard","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
		`{"requirements":[{"raw_text":"特斯拉","semantic_label":"","category":"vehicle","canonical_type":"brand","value":{"kind":"entity","text":"brand:tesla"},"operation":"add","operator":"eq","importance":"hard","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
		`{"requirements":[{"id":"llm-id","raw_text":"特斯拉","semantic_label":"","category":"vehicle","canonical_type":"brand","value":{"kind":"text","text":"特斯拉"},"operation":"add","operator":"eq","importance":"hard","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
	}
	for _, content := range tests {
		if _, err := decodeExtractResult(content); err == nil {
			t.Fatalf("expected contract error for %s", content)
		}
	}
}

func TestExtractionInputContainsOpenRequirementContext(t *testing.T) {
	input := ExtractionInput{
		SourceText: "改成Model Y",
		CurrentRequirements: []RequirementView{{
			RawText:       "适合老人",
			SemanticLabel: "elderly_friendly",
			Category:      requirement.CategoryUsageScenario,
			Value:         requirement.Value{Kind: requirement.ValueNone},
			Operator:      "eq",
			Importance:    "soft",
			Status:        "unresolved",
		}},
		RecentDomainHistory: []string{"想看丰田"},
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	value := string(data)
	for _, key := range []string{`"source_text"`, `"current_requirements"`, `"recent_domain_history"`, `"raw_text"`, `"semantic_label"`, `"category"`, `"canonical_type"`, `"value"`, `"operator"`, `"importance"`, `"status"`} {
		if !strings.Contains(value, key) {
			t.Fatalf("missing key %s in %s", key, value)
		}
	}
	for _, forbidden := range []string{`"location_name"`, `"pickup_time"`, `"return_time"`, `"filter_code"`, `"context_id"`} {
		if strings.Contains(value, forbidden) {
			t.Fatalf("unexpected key %s in %s", forbidden, value)
		}
	}
}
