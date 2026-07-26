package vehiclerequirement

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/internal/config"
)

const requirementTestConfigPath = "../../../conf/dev.yaml"

func TestLLMExtractorWithRemoteService(t *testing.T) {
	cfg, err := config.Load(requirementTestConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(cfg.LLM.APIKey) == "" {
		t.Skip("LLM API key is required for remote vehicle requirement tests")
	}
	client, err := llm.NewHTTPClient(&llm.HTTPConfig{Endpoint: cfg.LLM.Endpoint, APIKey: cfg.LLM.APIKey, TimeoutSec: cfg.LLM.TimeoutSec})
	if err != nil {
		t.Fatal(err)
	}
	extractor, err := NewLLMExtractor(client)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("specific model uses model with brand hint", func(t *testing.T) {
		result := extractRemote(t, extractor, "想看特斯拉 Model Y")
		if !result.DomainMatched || len(result.Requirements) != 1 {
			t.Fatalf("unexpected result: %#v", result)
		}
		requirement := result.Requirements[0]
		if requirement.Facet != FacetVehicleModel || !strings.EqualFold(requirement.RawValue, "Model Y") || requirement.EntityContext.BrandHint != "特斯拉" {
			t.Fatalf("unexpected requirement: %#v", requirement)
		}
	})

	t.Run("mixed rental context extracts vehicle requirement only", func(t *testing.T) {
		result := extractRemote(t, extractor, "明天虹桥机场取车，必须7座")
		if !result.DomainMatched || len(result.Requirements) != 1 || result.Requirements[0].Facet != FacetSeatNum || result.Requirements[0].RawValue != "7" {
			t.Fatalf("unexpected result: %#v", result)
		}
	})

	t.Run("search control is not a vehicle requirement", func(t *testing.T) {
		result := extractRemote(t, extractor, "换一批")
		if result.DomainMatched || len(result.Requirements) != 0 {
			t.Fatalf("unexpected result: %#v", result)
		}
	})
}

func extractRemote(t *testing.T, extractor Extractor, text string) *ExtractResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := extractor.Extract(ctx, &ExtractionInput{SourceText: text, CurrentRequirements: []RequirementView{}, RecentDomainHistory: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
