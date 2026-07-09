package agent

import (
	"context"
	"sync"
)

type BudgetChecker interface {
	Check(ctx context.Context, uid string, tokens int) (allowed bool, remaining int, err error)
	Consume(ctx context.Context, uid string, tokens int) error
}

type MemoryBudgetChecker struct {
	mu          sync.Mutex
	userLimit   int
	globalLimit int
	userUsed    map[string]int
	globalUsed  int
}

func NewMemoryBudgetChecker(userLimit, globalLimit int) *MemoryBudgetChecker {
	if userLimit <= 0 {
		userLimit = 500_000
	}
	if globalLimit <= 0 {
		globalLimit = 10_000_000
	}
	return &MemoryBudgetChecker{
		userLimit:   userLimit,
		globalLimit: globalLimit,
		userUsed:    map[string]int{},
	}
}

func (b *MemoryBudgetChecker) Check(ctx context.Context, uid string, tokens int) (bool, int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	userRemaining := b.userLimit - b.userUsed[uid]
	globalRemaining := b.globalLimit - b.globalUsed
	remaining := userRemaining
	if globalRemaining < remaining {
		remaining = globalRemaining
	}
	return remaining >= tokens, remaining, nil
}

func (b *MemoryBudgetChecker) Consume(ctx context.Context, uid string, tokens int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.userUsed[uid] += tokens
	b.globalUsed += tokens
	return nil
}
