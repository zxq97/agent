package agent

import (
	"context"
	"testing"
)

func TestMemoryBudgetCheckerBlocksWhenUserLimitExceeded(t *testing.T) {
	b := NewMemoryBudgetChecker(100, 1000)
	if err := b.Consume(context.Background(), "u1", 90); err != nil {
		t.Fatal(err)
	}

	allowed, remaining, err := b.Check(context.Background(), "u1", 20)

	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("allowed=true, want false")
	}
	if remaining != 10 {
		t.Fatalf("remaining=%d, want 10", remaining)
	}
}
