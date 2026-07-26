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
