package main

import (
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/agenthub"
	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/api/maps"
	"github.com/zxq97/agent/internal/capability"
	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/domain/generalreply"
	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/domain/rentalrules"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/domain/vehiclecompare"
	"github.com/zxq97/agent/internal/domain/vehiclerequirement"
	"github.com/zxq97/agent/internal/llmharness"
	"github.com/zxq97/agent/internal/orchestrator"
	"github.com/zxq97/agent/internal/router"
	"github.com/zxq97/agent/internal/searchplan"
	"github.com/zxq97/agent/internal/searchpolicy"
	"github.com/zxq97/agent/internal/vehiclecatalog"
	"github.com/zxq97/agent/internal/webchat"
)

type randomIDGenerator struct{}

func (randomIDGenerator) NewID() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("pending-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", value)
}

func provideLLMClient(cfg *config.Config) (llm.Client, error) {
	if cfg == nil {
		return nil, errors.New("http initialization: config is required")
	}
	return llm.NewHTTPClient(&llm.HTTPConfig{
		Endpoint:   cfg.LLM.Endpoint,
		APIKey:     cfg.LLM.APIKey,
		TimeoutSec: cfg.LLM.TimeoutSec,
	})
}

func provideMapsClient(cfg *config.Config) (maps.Client, error) {
	if cfg == nil {
		return nil, errors.New("http initialization: config is required")
	}
	if strings.TrimSpace(cfg.Maps.Endpoint) == "" {
		return nil, errors.New("http initialization: maps endpoint is required")
	}
	return maps.NewHTTPClient(&maps.HTTPConfig{
		Endpoint:       cfg.Maps.Endpoint,
		ProductID:      cfg.Maps.ProductID,
		AccKey:         cfg.Maps.AccKey,
		AppVersion:     cfg.Maps.AppVersion,
		Platform:       cfg.Maps.Platform,
		AppID:          cfg.Maps.AppID,
		MapType:        cfg.Maps.MapType,
		CoordinateType: cfg.Maps.CoordinateType,
		RequesterType:  cfg.Maps.RequesterType,
		Lang:           cfg.Maps.Lang,
		CallerID:       cfg.Maps.CallerID,
		PlaceType:      cfg.Maps.PlaceType,
	}), nil
}

func provideGuideClient(cfg *config.Config) (guide.Client, error) {
	if cfg == nil {
		return nil, errors.New("http initialization: config is required")
	}
	if strings.TrimSpace(cfg.Guide.Endpoint) == "" {
		return nil, errors.New("http initialization: guide endpoint is required")
	}
	return guide.NewHTTPClient(&guide.HTTPConfig{
		Endpoint:   cfg.Guide.Endpoint,
		Phone:      cfg.Guide.Phone,
		TimeoutSec: cfg.Guide.Timeout,
	}), nil
}

func provideRentalExtractor(client llm.Client, cfg *config.Config) (rentalcontext.Extractor, error) {
	return rentalcontext.NewExtractor(
		client,
		buildHarnessPolicy(cfg.LLM.Harness, rentalcontext.LLMTaskID),
	)
}

func provideRequirementExtractor(client llm.Client, cfg *config.Config) (vehiclerequirement.Extractor, error) {
	return vehiclerequirement.NewExtractor(
		client,
		buildHarnessPolicy(cfg.LLM.Harness, vehiclerequirement.LLMTaskID),
	)
}

func provideGeneralReplyHandler(client llm.Client, cfg *config.Config) (generalreply.Handler, error) {
	return generalreply.NewHandler(
		client,
		buildHarnessPolicy(cfg.LLM.Harness, generalreply.LLMTaskID),
	)
}

func provideTimezone() (*time.Location, error) {
	return time.LoadLocation("Asia/Shanghai")
}

func provideRentalContextHandler(
	extractor rentalcontext.Extractor,
	mapsClient maps.Client,
	timezone *time.Location,
) (rentalcontext.Handler, error) {
	return rentalcontext.NewHandler(
		extractor,
		mapsClient,
		randomIDGenerator{},
		time.Now,
		timezone,
		rentalcontext.DefaultAmbiguityConfig(),
	)
}

func provideVehicleCatalog() *vehiclecatalog.StaticCatalog {
	return vehiclecatalog.NewDefaultCatalog()
}

func provideVehicleResolver(
	cfg *config.Config,
	client llm.Client,
	catalog *vehiclecatalog.StaticCatalog,
) (vehiclecatalog.Resolver, error) {
	if strings.TrimSpace(cfg.AgentHub.Endpoint) == "" {
		return catalog, nil
	}
	agentHubClient := agenthub.NewHTTPClient(&agenthub.HTTPConfig{
		Endpoint:   cfg.AgentHub.Endpoint,
		Path:       cfg.AgentHub.Path,
		APIKey:     cfg.AgentHub.APIKey,
		TimeoutSec: cfg.AgentHub.TimeoutSec,
	})
	selector, err := vehiclecatalog.NewLLMCandidateSelector(
		client,
		buildHarnessPolicy(cfg.LLM.Harness, vehiclecatalog.CandidateSelectorTaskID),
	)
	if err != nil {
		return nil, err
	}
	return vehiclecatalog.NewRecallResolver(catalog, agentHubClient, selector)
}

func provideVehicleRequirementHandler(
	extractor vehiclerequirement.Extractor,
	resolver vehiclecatalog.Resolver,
) (vehiclerequirement.Handler, error) {
	return vehiclerequirement.NewHandler(extractor, resolver)
}

func provideCapabilityResolver(client llm.Client, cfg *config.Config) (capability.Resolver, error) {
	matcher, err := capability.NewLLMMatcher(
		client,
		buildHarnessPolicy(cfg.LLM.Harness, capability.LLMTaskID),
	)
	if err != nil {
		return nil, err
	}
	return capability.NewResolver(capability.NewDefaultCatalog(), matcher)
}

func provideSearchCompiler(catalog *vehiclecatalog.StaticCatalog, cfg *config.Config) (*searchplan.Compiler, error) {
	if cfg == nil {
		return nil, errors.New("http initialization: config is required")
	}
	return searchplan.NewCompilerWithProviderEnums(catalog, searchplan.ProviderEnumCatalog{
		Version:           cfg.Guide.VehicleEnums.Version,
		FuelTypes:         cfg.Guide.VehicleEnums.FuelTypes,
		TransmissionTypes: cfg.Guide.VehicleEnums.TransmissionTypes,
	})
}

func provideSearchCarHandler(
	client guide.Client,
	compiler *searchplan.Compiler,
	resolver capability.Resolver,
) (searchcar.Handler, error) {
	return searchcar.NewHandler(client, compiler, time.Now, resolver)
}

func provideIntentRouter(client llm.Client, cfg *config.Config) (router.Router, error) {
	return router.NewLLMRouter(
		client,
		buildHarnessPolicy(cfg.LLM.Harness, router.LLMTaskID),
	)
}

func provideSearchPolicy() orchestrator.SearchPolicy {
	return searchpolicy.New(1, time.Now)
}

func provideVehicleComparisonHandler() vehiclecompare.Handler {
	return vehiclecompare.NewHandler()
}

func provideRentalRulesHandler() (rentalrules.Handler, error) {
	return rentalrules.NewHandler(rentalrules.NewDefaultCatalog())
}

func provideOrchestrator(
	rental rentalcontext.Handler,
	requirement vehiclerequirement.Handler,
	search searchcar.Handler,
	policy orchestrator.SearchPolicy,
	general generalreply.Handler,
	comparison vehiclecompare.Handler,
	rules rentalrules.Handler,
) (*orchestrator.Orchestrator, error) {
	return orchestrator.NewWithExtensions(
		rental,
		requirement,
		search,
		policy,
		time.Now,
		general,
		comparison,
		rules,
	)
}

func provideStore() webchat.Store {
	return webchat.NewMemoryStore(time.Now)
}

func provideWebChatService(
	turnOrchestrator *orchestrator.Orchestrator,
	turnRouter router.Router,
	store webchat.Store,
) (*webchat.Service, error) {
	return webchat.NewService(turnOrchestrator, turnRouter, store, time.Now)
}

func buildHarnessPolicy(cfg *config.LLMHarnessConfig, taskID string) llmharness.Policy {
	policy := defaultHarnessPolicy(taskID)
	if cfg == nil {
		return policy
	}
	applyHarnessPolicy(&policy, &cfg.LLMHarnessPolicyConfig)
	if taskPolicy := cfg.Tasks[taskID]; taskPolicy != nil {
		applyHarnessPolicy(&policy, taskPolicy)
	}
	return policy
}

func defaultHarnessPolicy(taskID string) llmharness.Policy {
	policy := llmharness.DefaultPolicy()
	policy.PrimaryModel = llm.ModelFlash
	policy.FallbackModel = llm.ModelPro
	switch taskID {
	case vehiclerequirement.LLMTaskID, capability.LLMTaskID, vehiclecatalog.CandidateSelectorTaskID:
		policy.PrimaryModel = llm.ModelPro
		policy.FallbackModel = ""
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
