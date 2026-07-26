package rentalcontext

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDecodeExtractResult(t *testing.T) {
	valid := `{"location_query":"虹桥机场","pickup_time":{"status":"resolved","raw":"明天上午十点","value":"2026-07-24T10:00:00+08:00"},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":true}`
	result, err := decodeExtractResult(valid)
	if err != nil {
		t.Fatal(err)
	}
	if result.LocationQuery != "虹桥机场" || result.PickupTime.Value == nil || result.ReturnTime.Status != ResolutionAbsent {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDecodeExtractResultRejectsInvalidContract(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "missing top-level key",
			content: `{"location_query":"","pickup_time":{"status":"absent","raw":"","value":null},"domain_matched":false}`,
		},
		{
			name:    "location is object",
			content: `{"location_query":{"name":"虹桥机场"},"pickup_time":{"status":"absent","raw":"","value":null},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":true}`,
		},
		{
			name:    "missing nested key",
			content: `{"location_query":"","pickup_time":{"status":"ambiguous","raw":"晚上"},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":true}`,
		},
		{
			name:    "invalid status",
			content: `{"location_query":"","pickup_time":{"status":"uncertain","raw":"晚上","value":null},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":true}`,
		},
		{
			name:    "resolved without value",
			content: `{"location_query":"","pickup_time":{"status":"resolved","raw":"明天十点","value":null},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":true}`,
		},
		{
			name:    "domain mismatch carries modification",
			content: `{"location_query":"虹桥机场","pickup_time":{"status":"absent","raw":"","value":null},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":false}`,
		},
		{
			name:    "matched without modification",
			content: `{"location_query":"","pickup_time":{"status":"absent","raw":"","value":null},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":true}`,
		},
		{
			name:    "unknown field",
			content: `{"location_query":"","pickup_time":{"status":"absent","raw":"","value":null},"return_time":{"status":"absent","raw":"","value":null},"domain_matched":false,"city_id":"310000"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeExtractResult(test.content); err == nil {
				t.Fatal("expected contract error")
			}
		})
	}
}

func TestExtractionInputUsesStableJSONKeys(t *testing.T) {
	pickup := time.Date(2026, 7, 24, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	input := ExtractionInput{
		SourceText: "明天取车",
		CurrentState: CurrentRentalContext{
			LocationName: "虹桥机场",
			PickupTime:   &pickup,
		},
		RecentDomainHistory: []DomainHistoryItem{{UserText: "后天还车"}},
		Now:                 time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC),
		Timezone:            "Asia/Shanghai",
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	value := string(data)
	for _, key := range []string{`"source_text"`, `"current_state"`, `"location_name"`, `"recent_domain_history"`, `"user_text"`, `"now"`, `"timezone"`} {
		if !strings.Contains(value, key) {
			t.Fatalf("missing key %s in %s", key, value)
		}
	}
	if strings.Contains(value, `"SourceText"`) || strings.Contains(value, `"CurrentState"`) {
		t.Fatalf("Go field names leaked into JSON: %s", value)
	}
}
