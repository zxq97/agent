package guardrail

import (
	"context"
	"sync"
)

type Request struct {
	Kind string
	Text string
}

type Decision struct {
	Allowed bool
	Reason  string
}

type SecurityClient interface {
	Audit(ctx context.Context, req Request) (Decision, error)
}

type PassThroughClient struct{}

func (PassThroughClient) Audit(context.Context, Request) (Decision, error) {
	return Decision{Allowed: true}, nil
}

type AsyncAuditor struct {
	client       SecurityClient
	segmentChars int
}

func NewAsyncAuditor(client SecurityClient, segmentChars int) *AsyncAuditor {
	if client == nil {
		client = PassThroughClient{}
	}
	if segmentChars <= 0 {
		segmentChars = 300
	}
	return &AsyncAuditor{client: client, segmentChars: segmentChars}
}

func (a *AsyncAuditor) Start(ctx context.Context) *AuditHandle {
	return &AuditHandle{ctx: ctx, client: a.client, segmentChars: a.segmentChars}
}

type AuditHandle struct {
	ctx          context.Context
	client       SecurityClient
	segmentChars int

	mu      sync.Mutex
	wg      sync.WaitGroup
	blocked bool
	reason  string
	output  string
}

func (h *AuditHandle) Submit(kind, text string) {
	if text == "" {
		return
	}
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		dec, err := h.client.Audit(h.ctx, Request{Kind: kind, Text: text})
		if err != nil || dec.Allowed {
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		h.blocked = true
		h.reason = dec.Reason
	}()
}

func (h *AuditHandle) SubmitOutput(delta string) {
	h.mu.Lock()
	h.output += delta
	if len([]rune(h.output)) < h.segmentChars {
		h.mu.Unlock()
		return
	}
	text := h.output
	h.output = ""
	h.mu.Unlock()
	h.Submit("output", text)
}

func (h *AuditHandle) Wait() {
	h.wg.Wait()
}

func (h *AuditHandle) Blocked() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.blocked
}

func (h *AuditHandle) Reason() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reason
}
