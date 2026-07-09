package guardrail

import (
	"context"
	"testing"
)

func TestAsyncAuditorBlocksWhenClientFlagsInput(t *testing.T) {
	auditor := NewAsyncAuditor(blockingClient{block: true}, 300)
	handle := auditor.Start(context.Background())

	handle.Submit("input", "越界内容")
	handle.Wait()

	if !handle.Blocked() {
		t.Fatal("Blocked=false, want true")
	}
	if handle.Reason() == "" {
		t.Fatal("Reason is empty")
	}
}

func TestPassThroughAuditorAllowsContent(t *testing.T) {
	auditor := NewAsyncAuditor(PassThroughClient{}, 300)
	handle := auditor.Start(context.Background())

	handle.Submit("input", "明天北京租车")
	handle.Submit("output", "给你找几辆车")
	handle.Wait()

	if handle.Blocked() {
		t.Fatalf("Blocked=true reason=%q, want false", handle.Reason())
	}
}

type blockingClient struct {
	block bool
}

func (c blockingClient) Audit(context.Context, Request) (Decision, error) {
	if c.block {
		return Decision{Allowed: false, Reason: "blocked by test"}, nil
	}
	return Decision{Allowed: true}, nil
}
