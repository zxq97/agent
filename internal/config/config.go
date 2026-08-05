// Package config loads runtime service configuration.
package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the repository configuration root.
type Config struct {
	Guide    GuideConfig    `yaml:"guide"`
	Maps     MapsConfig     `yaml:"maps"`
	LLM      LLMConfig      `yaml:"llm"`
	AgentHub AgentHubConfig `yaml:"agenthub"`
}

type AgentHubConfig struct {
	Endpoint   string `yaml:"endpoint"`
	Path       string `yaml:"path"`
	APIKey     string `yaml:"api_key"`
	TimeoutSec int    `yaml:"timeout_sec"`
}

type LLMConfig struct {
	Endpoint   string            `yaml:"endpoint"`
	APIKey     string            `yaml:"api_key"`
	TimeoutSec int               `yaml:"timeout_sec"`
	Harness    *LLMHarnessConfig `yaml:"harness"`
}

// LLMHarnessConfig optionally overrides the safe in-process Harness defaults.
// Tasks may override the default policy by stable LLM task ID.
type LLMHarnessConfig struct {
	LLMHarnessPolicyConfig `yaml:",inline"`
	Tasks                  map[string]*LLMHarnessPolicyConfig `yaml:"tasks"`
}

// LLMHarnessPolicyConfig contains one default or task-specific policy.
// Pointer booleans distinguish an omitted setting from an explicit false.
type LLMHarnessPolicyConfig struct {
	PrimaryModel    string `yaml:"primary_model"`
	FallbackModel   string `yaml:"fallback_model"`
	RetryOnInvalid  *bool  `yaml:"retry_on_invalid"`
	RetryOnEmpty    *bool  `yaml:"retry_on_empty"`
	RetryTransient  *bool  `yaml:"retry_transient"`
	MaxAttempts     int    `yaml:"max_attempts"`
	TotalTimeoutSec int    `yaml:"total_timeout_sec"`
}

// GuideConfig configures rental-guide quote and menu requests.
type GuideConfig struct {
	Endpoint     string                 `yaml:"endpoint"`
	Phone        string                 `yaml:"phone"`
	Timeout      int                    `yaml:"timeout"`
	VehicleEnums GuideVehicleEnumConfig `yaml:"vehicle_enums"`
}

// GuideVehicleEnumConfig records provider-owned integer enum semantics. It is
// optional; when absent, enum-based local filtering remains safely unresolved.
type GuideVehicleEnumConfig struct {
	Version           string           `yaml:"version"`
	FuelTypes         map[string][]int `yaml:"fuel_types"`
	TransmissionTypes map[string][]int `yaml:"transmission_types"`
}

// MapsConfig configures the map search and POI-resolution API.
type MapsConfig struct {
	Endpoint       string `yaml:"endpoint"`
	ProductID      string `yaml:"product_id"`
	AccKey         string `yaml:"acc_key"`
	AppVersion     string `yaml:"app_version"`
	Platform       string `yaml:"platform"`
	AppID          string `yaml:"app_id"`
	MapType        string `yaml:"map_type"`
	CoordinateType string `yaml:"coordinate_type"`
	RequesterType  string `yaml:"requester_type"`
	Lang           string `yaml:"lang"`
	CallerID       string `yaml:"caller_id"`
	PlaceType      string `yaml:"place_type"`
}

// Load reads a YAML config file and expands environment variables first.
func Load(path string) (*Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(content))), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
