package metric

import (
	"strings"
	"testing"
)

func TestRegistryRendersPrometheusText(t *testing.T) {
	r := NewRegistry()
	r.Inc("llm_calls_total", Labels{"stage": "decide", "status": "ok"}, 1)
	r.Observe("stage_duration_ms", Labels{"stage": "Decide"}, 12)

	text := r.Render()

	if !strings.Contains(text, `llm_calls_total{stage="decide",status="ok"} 1`) {
		t.Fatalf("metrics missing counter:\n%s", text)
	}
	if !strings.Contains(text, `stage_duration_ms{stage="Decide"} 12`) {
		t.Fatalf("metrics missing gauge:\n%s", text)
	}
}

func TestEstimateCost(t *testing.T) {
	got := EstimateCost(UsageRecord{
		PromptTokens:     1000,
		CacheHitTokens:   200,
		CompletionTokens: 500,
		PricePerMInput:   2,
		PricePerMCache:   0.5,
		PricePerMOutput:  8,
	})

	want := 0.0057
	if got != want {
		t.Fatalf("cost = %.4f, want %.4f", got, want)
	}
}
