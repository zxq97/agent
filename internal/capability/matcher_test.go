package capability

import "testing"

func TestDecodeMatchesRejectsInvalidContract(t *testing.T) {
	tests := []string{
		`{}`,
		`{"matches":[{"capability_id":"large_space","relation":"maybe","confidence":0.8}]}`,
		`{"matches":[{"capability_id":"large_space","relation":"exact","confidence":1.1}]}`,
		`{"matches":[{"capability_id":"large_space","relation":"exact","confidence":0.8,"filter_code":"x"}]}`,
		`{"matches":[{"capability_id":"large_space","relation":"exact","confidence":0.8},{"capability_id":"family_trip","relation":"relevant","confidence":0.7}]}`,
	}
	for _, value := range tests {
		if _, err := decodeMatches(value); err == nil {
			t.Fatalf("expected error for %s", value)
		}
	}
}

func TestDecodeMatchesAllowsEmptyOrSingleCandidate(t *testing.T) {
	empty, err := decodeMatches(`{"matches":[]}`)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty=%#v err=%v", empty, err)
	}
	matches, err := decodeMatches(`{"matches":[{"capability_id":"large_space","relation":"relevant","confidence":0.8}]}`)
	if err != nil || len(matches) != 1 || matches[0].CapabilityID != "large_space" {
		t.Fatalf("matches=%#v err=%v", matches, err)
	}
}

func TestValidateMatchesRejectsCandidateOutsideInput(t *testing.T) {
	input := &MatchRequest{Candidates: []MatchCandidate{{ID: "allowed-a"}, {ID: "allowed-b"}}}
	matches := []Match{{CapabilityID: "fabricated", Relation: "exact", Confidence: 0.9}}
	if err := validateMatches(input, &matches); err == nil {
		t.Fatal("expected candidate whitelist error")
	}
}
