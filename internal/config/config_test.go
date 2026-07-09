package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	os.Setenv("FOO_KEY", "secret123")
	defer os.Unsetenv("FOO_KEY")

	cases := []struct {
		in   string
		want string
	}{
		{"api_key: ${FOO_KEY}", "api_key: secret123"},
		{"x: ${MISSING:-default_val}", "x: default_val"},
		{"x: ${FOO_KEY:-fallback}", "x: secret123"},
		{"plain text", "plain text"},
	}
	for _, c := range cases {
		if got := expandEnv(c.in); got != c.want {
			t.Errorf("expandEnv(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLoadAndDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	content := `
env: test
llm:
  default: deepseek-chat
  providers:
    deepseek-chat:
      name: deepseek
      model: deepseek-chat
      api_key: sk-test
  agent_bindings:
    decide: deepseek-chat
tyche:
  endpoint: http://localhost:8877/mcp
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Env != "test" {
		t.Errorf("env = %q", cfg.Env)
	}
	// 默认值
	if cfg.Tyche.Timeout != 30 {
		t.Errorf("tyche timeout default = %d, want 30", cfg.Tyche.Timeout)
	}
	if cfg.Agent.Mode != "pipeline" {
		t.Errorf("agent mode default = %q, want pipeline", cfg.Agent.Mode)
	}
	if cfg.Agent.EnableLocalTools != false {
		t.Errorf("enable_local_tools default should be false")
	}
	if cfg.Session.TTLHours != 24 {
		t.Errorf("session ttl default = %d, want 24", cfg.Session.TTLHours)
	}
	if cfg.Audit.SegmentChars != 300 {
		t.Errorf("audit segment chars default = %d, want 300", cfg.Audit.SegmentChars)
	}
	if cfg.AccessLock.TTLSeconds != 60 || !cfg.AccessLock.FailOpen {
		t.Errorf("access lock defaults = ttl %d fail_open %v, want 60 true", cfg.AccessLock.TTLSeconds, cfg.AccessLock.FailOpen)
	}
	p := cfg.LLM.Providers["deepseek-chat"]
	if p.BaseURL != "https://api.deepseek.com" {
		t.Errorf("base_url default = %q", p.BaseURL)
	}
	if p.Timeout != 60 {
		t.Errorf("provider timeout default = %d, want 60", p.Timeout)
	}
}
