package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/pkg/log"
)

func TestInitializeHTTPHandlerBuildsCompleteGraph(t *testing.T) {
	cfg := validTestConfig()
	handler, err := initializeHTTPHandler(cfg, log.NewJSONLogger(&bytes.Buffer{}))
	if err != nil {
		t.Fatal(err)
	}
	if handler == nil {
		t.Fatal("expected initialized HTTP handler")
	}
}

func TestInitializeHTTPHandlerRejectsInvalidConfiguration(t *testing.T) {
	cfg := validTestConfig()
	cfg.Maps.Endpoint = ""
	_, err := initializeHTTPHandler(cfg, log.NewJSONLogger(&bytes.Buffer{}))
	if err == nil || !strings.Contains(err.Error(), "maps endpoint is required") {
		t.Fatalf("error=%v", err)
	}
}

func TestInitializeHTTPHandlerRequiresLogger(t *testing.T) {
	_, err := initializeHTTPHandler(validTestConfig(), nil)
	if err == nil || !strings.Contains(err.Error(), "logger is required") {
		t.Fatalf("error=%v", err)
	}
}

func validTestConfig() *config.Config {
	return &config.Config{
		LLM: config.LLMConfig{
			Endpoint: "http://unused.invalid",
			APIKey:   "test-only",
		},
		Maps: config.MapsConfig{
			Endpoint: "http://unused.invalid",
		},
		Guide: config.GuideConfig{
			Endpoint: "http://unused.invalid",
		},
	}
}
