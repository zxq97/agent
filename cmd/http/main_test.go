package main

import (
	"testing"
	"time"

	"github.com/zxq97/agent/internal/config"
)

func TestBuildHarnessPolicyAppliesTaskOverride(t *testing.T) {
	disabled := false
	cfg := &config.LLMHarnessConfig{
		LLMHarnessPolicyConfig: config.LLMHarnessPolicyConfig{
			PrimaryModel:   "default-model",
			MaxAttempts:    1,
			RetryOnInvalid: &disabled,
		},
		Tasks: map[string]*config.LLMHarnessPolicyConfig{
			"router.route": {
				PrimaryModel:    "router-model",
				FallbackModel:   "router-fallback",
				MaxAttempts:     2,
				TotalTimeoutSec: 12,
			},
		},
	}

	policy := buildHarnessPolicy(cfg, "router.route")
	if policy.PrimaryModel != "router-model" ||
		policy.FallbackModel != "router-fallback" ||
		policy.MaxAttempts != 2 ||
		policy.TotalTimeout != 12*time.Second ||
		policy.RetryOnInvalid {
		t.Fatalf("unexpected task policy: %#v", policy)
	}

	defaultPolicy := buildHarnessPolicy(cfg, "general_reply.generate")
	if defaultPolicy.PrimaryModel != "default-model" ||
		defaultPolicy.MaxAttempts != 1 ||
		defaultPolicy.RetryOnInvalid {
		t.Fatalf("unexpected default policy: %#v", defaultPolicy)
	}
}
