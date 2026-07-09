package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
)

func TestDepsForRequestUsesRequestLoggerWithoutMutatingBaseDeps(t *testing.T) {
	baseLogger := &strings.Builder{}
	requestLogger := &strings.Builder{}
	base := &tools.Deps{Logger: baseLogger}

	got := depsForRequest(base, requestLogger)
	if got == base {
		t.Fatalf("want a shallow copy, got base deps")
	}
	if got.Logger != requestLogger {
		t.Fatalf("want request logger, got %#v", got.Logger)
	}
	if base.Logger != baseLogger {
		t.Fatalf("base deps logger was mutated")
	}
}

func TestDepsForRequestKeepsBaseDepsWhenRequestLoggerNil(t *testing.T) {
	base := &tools.Deps{Logger: &strings.Builder{}}
	if got := depsForRequest(base, nil); got != base {
		t.Fatalf("want base deps when request logger is nil")
	}
}

func TestCapabilityOrchestratorBlocksInvalidToolArguments(t *testing.T) {
	dec := &Decision{
		Tool: ToolSearchVehicles,
		ArgsDiag: &ToolArgsDiagnostics{
			Raw:              `{"pickup_text":"SUV"}`,
			ValidationErrors: []string{"pickup_text looks like vehicle/filter need"},
		},
	}
	ac := &AgentContext{
		State:    orchestration.New("s", "u"),
		Decision: dec,
		Deps:     &tools.Deps{},
	}

	res, err := (&CapabilityOrchestrator{caps: map[string]Capability{
		ToolSearchVehicles: &SearchCapability{},
	}}).Handle(context.Background(), ac)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Clarification == nil || res.Clarification.Slot != "pickup_location" {
		t.Fatalf("res=%#v, want pickup_location clarification", res)
	}
}
