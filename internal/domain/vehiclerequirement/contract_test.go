package vehiclerequirement

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeExtractResult(t *testing.T) {
	content := `{"requirements":[{"facet":"vehicle_model","raw_text":"特斯拉 Model Y","raw_value":"Model Y","operation":"replace","operator":"eq","importance":"hard","confidence":0.99,"entity_context":{"brand_hint":"特斯拉","series_hint":""}},{"facet":"seat_num","raw_text":"最好7座","raw_value":"7","operation":"replace","operator":"eq","importance":"soft","confidence":0.95,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`
	result, err := decodeExtractResult(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Requirements) != 2 || result.Requirements[0].Facet != FacetVehicleModel {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDecodeExtractResultRejectsSearchWithoutRequirements(t *testing.T) {
	if _, err := decodeExtractResult(`{"requirements":[],"domain_matched":true}`); err == nil {
		t.Fatal("expected domain_matched contract error")
	}
}

func TestDecodeExtractResultAllowsRemoveFacet(t *testing.T) {
	result, err := decodeExtractResult(`{"requirements":[{"facet":"brand","raw_text":"品牌不限","raw_value":"","operation":"remove","operator":"eq","importance":"hard","confidence":1,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`)
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
		`{"requirements":[{"brand":"特斯拉","raw_text":"特斯拉","raw_value":"特斯拉","operation":"add","operator":"eq","importance":"hard","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
		`{"requirements":[{"facet":"brand","raw_text":"特斯拉","raw_value":"特斯拉","operation":"add","operator":"eq","importance":"hard","entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
		`{"requirements":[{"facet":"model","raw_text":"Model Y","raw_value":"Model Y","operation":"add","operator":"eq","importance":"hard","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
		`{"requirements":[{"facet":"brand","raw_text":"特斯拉","raw_value":"特斯拉","operation":"merge","operator":"eq","importance":"hard","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
		`{"requirements":[{"facet":"brand","raw_text":"特斯拉","raw_value":"特斯拉","operation":"add","operator":"eq","importance":"hard","confidence":1.1,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
		`{"requirements":[{"facet":"brand","raw_text":"特斯拉","raw_value":"特斯拉","operation":"add","operator":"eq","importance":"hard","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":false}`,
		`{"requirements":[{"id":"llm-id","facet":"brand","raw_text":"特斯拉","raw_value":"特斯拉","operation":"add","operator":"eq","importance":"hard","confidence":0.9,"entity_context":{"brand_hint":"","series_hint":""}}],"domain_matched":true}`,
	}
	for _, content := range tests {
		if _, err := decodeExtractResult(content); err == nil {
			t.Fatalf("expected contract error for %s", content)
		}
	}
}

func TestExtractionInputContainsOnlyRequirementContext(t *testing.T) {
	input := ExtractionInput{
		SourceText: "改成Model Y",
		CurrentRequirements: []RequirementView{{
			Facet:          "brand",
			RawValue:       "丰田",
			CanonicalValue: "丰田",
			Operator:       "eq",
			Importance:     "hard",
			Status:         "active",
		}},
		RecentDomainHistory: []string{"想看丰田"},
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	value := string(data)
	for _, key := range []string{`"source_text"`, `"current_requirements"`, `"recent_domain_history"`, `"facet"`, `"raw_value"`, `"canonical_value"`, `"operator"`, `"importance"`, `"status"`} {
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
