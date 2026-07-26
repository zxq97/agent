package rentalcontext

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/api/maps"
	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/session"
)

const rentalContextTestConfigPath = "../../../conf/dev.yaml"

func TestModifyRentalContextHandlerWithRemoteServices(t *testing.T) {
	cfg, err := config.Load(rentalContextTestConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(cfg.LLM.APIKey) == "" {
		t.Skip("DEEPSEEK_API_KEY is required for remote rental-context tests")
	}
	client, err := llm.NewHTTPClient(&llm.HTTPConfig{Endpoint: cfg.LLM.Endpoint, APIKey: cfg.LLM.APIKey, TimeoutSec: cfg.LLM.TimeoutSec})
	if err != nil {
		t.Fatal(err)
	}
	extractor, err := NewLLMCommandExtractor(client)
	if err != nil {
		t.Fatal(err)
	}
	mapClient := maps.NewHTTPClient(&maps.HTTPConfig{Endpoint: cfg.Maps.Endpoint, ProductID: cfg.Maps.ProductID, AccKey: cfg.Maps.AccKey, AppVersion: cfg.Maps.AppVersion, Platform: cfg.Maps.Platform, AppID: cfg.Maps.AppID, MapType: cfg.Maps.MapType, CoordinateType: cfg.Maps.CoordinateType, RequesterType: cfg.Maps.RequesterType, Lang: cfg.Maps.Lang, CallerID: cfg.Maps.CallerID, PlaceType: cfg.Maps.PlaceType})
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewModifyRentalContextHandler(extractor, mapClient, funcID("remote-pending"), time.Now, zone, DefaultAmbiguityConfig())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	t.Run("time only", func(t *testing.T) {
		result, err := handler.Handle(ctx, &session.AgentSession{}, &ModifyRentalContextInput{SourceText: "明天下午3点取车"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != ResultSuccess || result.PickupTime == nil {
			t.Fatalf("unexpected result: %#v", result)
		}
	})

	t.Run("multi area", func(t *testing.T) {
		result, err := handler.Handle(ctx, &session.AgentSession{}, &ModifyRentalContextInput{SourceText: "换到南京路"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != ResultWaitingUser || result.InteractionID == "" || len(result.LocationOptions) < 2 {
			t.Fatalf("unexpected result: %#v", result)
		}
	})

	t.Run("domain mismatch", func(t *testing.T) {
		_, err := handler.Handle(ctx, &session.AgentSession{}, &ModifyRentalContextInput{SourceText: "找300以内SUV"})
		if err != ErrDomainMismatch {
			t.Fatalf("error = %v, want %v", err, ErrDomainMismatch)
		}
	})
}
