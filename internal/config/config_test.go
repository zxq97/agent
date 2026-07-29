package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExpandsEnvironment(t *testing.T) {
	t.Setenv("GUIDE_ENDPOINT", "https://guide.example.test")
	path := filepath.Join(t.TempDir(), "dev.yaml")
	content := []byte("guide:\n  endpoint: ${GUIDE_ENDPOINT}\n  phone: 13800000000\n  timeout: 30\nmaps:\n  endpoint: http://maps.example.test\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Guide.Endpoint != "https://guide.example.test" || cfg.Guide.Timeout != 30 || cfg.Maps.Endpoint != "http://maps.example.test" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadLLMHarnessTaskPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := []byte(`llm:
  endpoint: https://example.test
  api_key: test
  timeout_sec: 30
  harness:
    primary_model: fast-model
    retry_on_invalid: true
    max_attempts: 1
    tasks:
      router.route:
        primary_model: router-model
        fallback_model: reasoning-model
        max_attempts: 2
        total_timeout_sec: 12
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLM.Harness == nil ||
		cfg.LLM.Harness.PrimaryModel != "fast-model" ||
		cfg.LLM.Harness.RetryOnInvalid == nil ||
		!*cfg.LLM.Harness.RetryOnInvalid {
		t.Fatalf("unexpected default harness config: %#v", cfg.LLM.Harness)
	}
	routerPolicy := cfg.LLM.Harness.Tasks["router.route"]
	if routerPolicy == nil ||
		routerPolicy.PrimaryModel != "router-model" ||
		routerPolicy.FallbackModel != "reasoning-model" ||
		routerPolicy.MaxAttempts != 2 ||
		routerPolicy.TotalTimeoutSec != 12 {
		t.Fatalf("unexpected router policy: %#v", routerPolicy)
	}
}
