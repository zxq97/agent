package vehiclecatalog

import (
	"testing"

	"github.com/zxq97/agent/api/agenthub"
)

func TestCandidateSelectorRejectsIDOutsideCandidates(t *testing.T) {
	input := &CandidateSelectionInput{Query: "model y", EntityType: "model", Candidates: []agenthub.RecallCandidate{
		{CandidateID: "candidate-1", Name: "Model Y", EntityType: "model"},
		{CandidateID: "candidate-2", Name: "Model 3", EntityType: "model"},
	}}
	output, err := decodeCandidateSelection(`{"candidate_id":"invented","confidence":0.9}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCandidateSelectionOutput(input, output); err == nil {
		t.Fatal("candidate outside input whitelist was accepted")
	}
}

func TestCandidateSelectorStrictDecodeRejectsExtraFields(t *testing.T) {
	if _, err := decodeCandidateSelection(`{"candidate_id":"candidate-1","confidence":0.9,"filter_code":"invented"}`); err == nil {
		t.Fatal("unexpected field was accepted")
	}
}
