package router

import "testing"

func TestDecodeRouteResultSupportsMultipleActions(t *testing.T) {
	source := "后天下午虹桥取，想要7座SUV"
	result, err := decodeRouteResult(`{"candidates":[{"action":"modify_rental_context","evidence_text":"后天下午虹桥取","confidence":0.99},{"action":"update_vehicle_requirements","evidence_text":"想要7座SUV","confidence":0.98}],"unassigned_text":""}`, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 || result.Candidate(ActionModifyRentalContext) == nil || result.Candidate(ActionUpdateVehicleRequirements) == nil {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDecodeRouteResultSupportsComparisonAndRentalRules(t *testing.T) {
	source := "对比1和3，取消订单怎么收费"
	result, err := decodeRouteResult(`{"candidates":[{"action":"compare_vehicles","evidence_text":"对比1和3","confidence":0.99},{"action":"query_rental_rules","evidence_text":"取消订单怎么收费","confidence":0.98}],"unassigned_text":""}`, source)
	if err != nil {
		t.Fatal(err)
	}
	if result.Candidate(ActionCompareVehicles) == nil ||
		result.Candidate(ActionQueryRentalRules) == nil {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestDecodeRouteResultRejectsInvalidContract(t *testing.T) {
	source := "想要7座SUV"
	tests := []struct {
		name    string
		content string
	}{
		{name: "missing candidates", content: `{"unassigned_text":""}`},
		{name: "empty candidates", content: `{"candidates":[],"unassigned_text":"想要7座SUV"}`},
		{name: "unknown action", content: `{"candidates":[{"action":"unknown","evidence_text":"想要7座SUV","confidence":0.9}],"unassigned_text":""}`},
		{name: "rewritten evidence", content: `{"candidates":[{"action":"update_vehicle_requirements","evidence_text":"七座越野车","confidence":0.9}],"unassigned_text":""}`},
		{name: "duplicate action", content: `{"candidates":[{"action":"update_vehicle_requirements","evidence_text":"7座","confidence":0.9},{"action":"update_vehicle_requirements","evidence_text":"SUV","confidence":0.9}],"unassigned_text":""}`},
		{name: "confidence out of range", content: `{"candidates":[{"action":"update_vehicle_requirements","evidence_text":"想要7座SUV","confidence":1.1}],"unassigned_text":""}`},
		{name: "unknown field", content: `{"candidates":[{"action":"update_vehicle_requirements","evidence_text":"想要7座SUV","confidence":0.9,"filter_code":"suv"}],"unassigned_text":""}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeRouteResult(test.content, source); err == nil {
				t.Fatal("expected route contract error")
			}
		})
	}
}
