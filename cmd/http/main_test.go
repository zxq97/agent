package main

import (
	"testing"
	"time"

	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/internal/capability"
	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/domain/vehiclerequirement"
	"github.com/zxq97/agent/internal/router"
	"github.com/zxq97/agent/internal/vehiclecatalog"
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

func TestDefaultHarnessPolicyRoutesFlashAndProTasks(t *testing.T) {
	for _, taskID := range []string{router.LLMTaskID, "rental_context.extract", "general_reply.generate"} {
		policy := buildHarnessPolicy(nil, taskID)
		if policy.PrimaryModel != llm.ModelFlash || policy.FallbackModel != llm.ModelPro {
			t.Fatalf("task %q should use Flash then Pro: %#v", taskID, policy)
		}
	}
	for _, taskID := range []string{
		vehiclerequirement.LLMTaskID,
		capability.LLMTaskID,
		vehiclecatalog.CandidateSelectorTaskID,
	} {
		policy := buildHarnessPolicy(nil, taskID)
		if policy.PrimaryModel != llm.ModelPro || policy.FallbackModel != "" {
			t.Fatalf("task %q should use Pro: %#v", taskID, policy)
		}
	}
}
