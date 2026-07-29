package rentalrules

import (
	"context"
	"testing"
)

func TestHandlerReturnsMatchedGuidanceWithVerificationRequirement(t *testing.T) {
	result, err := New(nil).Handle(context.Background(), &Input{EvidenceText: "取消订单要扣多少钱"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSuccess || len(result.Rules) != 1 ||
		result.Rules[0].Category != "cancellation" ||
		!result.Rules[0].VerificationRequired {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestHandlerDoesNotInventUnknownRule(t *testing.T) {
	result, err := New(nil).Handle(context.Background(), &Input{EvidenceText: "宠物清洁费是多少"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInsufficient || len(result.Rules) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}
