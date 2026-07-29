// Command http starts the local browser UI and its JSON/SSE API.
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/api/maps"
	"github.com/zxq97/agent/internal/capability"
	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/domain/generalreply"
	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/domain/vehiclerequirement"
	"github.com/zxq97/agent/internal/httphandler"
	"github.com/zxq97/agent/internal/llmharness"
	"github.com/zxq97/agent/internal/orchestrator"
	"github.com/zxq97/agent/internal/router"
	"github.com/zxq97/agent/internal/searchplan"
	"github.com/zxq97/agent/internal/searchpolicy"
	"github.com/zxq97/agent/internal/vehiclecatalog"
	"github.com/zxq97/agent/internal/webchat"
	"github.com/zxq97/agent/pkg/log"
)

type randomIDGenerator struct{}

func (randomIDGenerator) NewID() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("pending-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", value)
}

func main() {
	configPath := flag.String("config", "conf/dev.yaml", "configuration file")
	address := flag.String("addr", ":8080", "HTTP listen address")
	webDir := flag.String("web-dir", "web", "static frontend directory")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	exitOn(err, "load config")
	logger := log.NewJSONLogger(os.Stderr)

	llmClient, err := llm.NewHTTPClient(&llm.HTTPConfig{Endpoint: cfg.LLM.Endpoint, APIKey: cfg.LLM.APIKey, TimeoutSec: cfg.LLM.TimeoutSec})
	exitOn(err, "create llm client")
	rentalExtractor, err := rentalcontext.NewLLMCommandExtractor(llmClient, buildHarnessPolicy(cfg.LLM.Harness, rentalcontext.LLMTaskID))
	exitOn(err, "create rental context extractor")
	requirementExtractor, err := vehiclerequirement.NewLLMExtractor(llmClient, buildHarnessPolicy(cfg.LLM.Harness, vehiclerequirement.LLMTaskID))
	exitOn(err, "create requirement extractor")
	generalReplyHandler, err := generalreply.NewLLMHandler(llmClient, buildHarnessPolicy(cfg.LLM.Harness, generalreply.LLMTaskID))
	exitOn(err, "create general reply handler")

	mapsClient := maps.NewHTTPClient(&maps.HTTPConfig{Endpoint: cfg.Maps.Endpoint, ProductID: cfg.Maps.ProductID, AccKey: cfg.Maps.AccKey, AppVersion: cfg.Maps.AppVersion, Platform: cfg.Maps.Platform, AppID: cfg.Maps.AppID, MapType: cfg.Maps.MapType, CoordinateType: cfg.Maps.CoordinateType, RequesterType: cfg.Maps.RequesterType, Lang: cfg.Maps.Lang, CallerID: cfg.Maps.CallerID, PlaceType: cfg.Maps.PlaceType})
	guideClient := guide.NewHTTPClient(&guide.HTTPConfig{Endpoint: cfg.Guide.Endpoint, Phone: cfg.Guide.Phone, TimeoutSec: cfg.Guide.Timeout})
	zone, err := time.LoadLocation("Asia/Shanghai")
	exitOn(err, "load timezone")
	rentalHandler, err := rentalcontext.NewModifyRentalContextHandler(rentalExtractor, mapsClient, randomIDGenerator{}, time.Now, zone, rentalcontext.DefaultAmbiguityConfig())
	exitOn(err, "create rental context handler")
	requirementHandler, err := vehiclerequirement.NewHandler(requirementExtractor, vehiclecatalog.NewDefaultCatalog())
	exitOn(err, "create vehicle requirement handler")
	capabilityMatcher, err := capability.NewLLMMatcher(llmClient, buildHarnessPolicy(cfg.LLM.Harness, capability.LLMTaskID))
	exitOn(err, "create capability matcher")
	capabilityResolver := capability.NewResolver(capability.NewDefaultCatalog(), capabilityMatcher)
	searchHandler, err := searchcar.NewSearchCarHandler(guideClient, searchplan.NewCompiler(), time.Now, capabilityResolver)
	exitOn(err, "create search car handler")
	intentRouter, err := router.NewLLMRouter(llmClient, buildHarnessPolicy(cfg.LLM.Harness, router.LLMTaskID))
	exitOn(err, "create intent router")

	turnOrchestrator := orchestrator.New(rentalHandler, requirementHandler, searchHandler, searchpolicy.New(1, time.Now), time.Now, generalReplyHandler)
	chatService, err := webchat.NewService(turnOrchestrator, intentRouter, webchat.NewMemoryStore(time.Now), time.Now)
	exitOn(err, "create web chat service")
	handler, err := httphandler.New(chatService, logger)
	exitOn(err, "create HTTP handler")

	server := &http.Server{Addr: *address, Handler: handler.Mux(*webDir), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 120 * time.Second}
	fmt.Printf("租车智能体页面已启动：http://localhost%s\n", *address)
	fmt.Printf("配置：%s，前端目录：%s\n", *configPath, *webDir)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "fatal: HTTP server: %v\n", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
	}
}

func buildHarnessPolicy(cfg *config.LLMHarnessConfig, taskID string) llmharness.Policy {
	policy := llmharness.DefaultPolicy()
	if cfg == nil {
		return policy
	}
	applyHarnessPolicy(&policy, &cfg.LLMHarnessPolicyConfig)
	if taskPolicy := cfg.Tasks[taskID]; taskPolicy != nil {
		applyHarnessPolicy(&policy, taskPolicy)
	}
	return policy
}

func applyHarnessPolicy(policy *llmharness.Policy, cfg *config.LLMHarnessPolicyConfig) {
	if cfg.PrimaryModel != "" {
		policy.PrimaryModel = cfg.PrimaryModel
	}
	if cfg.FallbackModel != "" {
		policy.FallbackModel = cfg.FallbackModel
	}
	if cfg.RetryOnInvalid != nil {
		policy.RetryOnInvalid = *cfg.RetryOnInvalid
	}
	if cfg.RetryOnEmpty != nil {
		policy.RetryOnEmpty = *cfg.RetryOnEmpty
	}
	if cfg.RetryTransient != nil {
		policy.RetryTransient = *cfg.RetryTransient
	}
	if cfg.MaxAttempts != 0 {
		policy.MaxAttempts = cfg.MaxAttempts
	}
	if cfg.TotalTimeoutSec != 0 {
		policy.TotalTimeout = time.Duration(cfg.TotalTimeoutSec) * time.Second
	}
}

func exitOn(err error, operation string) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "fatal: %s: %v\n", operation, err)
	os.Exit(1)
}
