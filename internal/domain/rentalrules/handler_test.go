package rentalrules

import (
	"context"
	"testing"
)

func newTestHandler(t *testing.T) Handler {
	t.Helper()
	handler, err := NewHandler(NewDefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestNewRequiresCatalog(t *testing.T) {
	if _, err := NewHandler(nil); err == nil {
		t.Fatal("expected missing catalog error")
	}
}

func TestHandlerReturnsMatchedGuidanceWithVerificationRequirement(t *testing.T) {
	result, err := newTestHandler(t).Handle(context.Background(), &Input{EvidenceText: "取消订单要扣多少钱"})
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
	result, err := newTestHandler(t).Handle(context.Background(), &Input{EvidenceText: "宠物清洁费是多少"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusInsufficient || len(result.Rules) != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
}
