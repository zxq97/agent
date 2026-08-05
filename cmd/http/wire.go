//go:build wireinject

package main

import (
	"github.com/google/wire"
	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/internal/httphandler"
	"github.com/zxq97/agent/pkg/log"
)

func initializeHTTPHandler(cfg *config.Config, logger log.Logger) (*httphandler.Handler, error) {
	wire.Build(
		provideLLMClient,
		provideMapsClient,
		provideGuideClient,
		provideRentalExtractor,
		provideRequirementExtractor,
		provideGeneralReplyHandler,
		provideTimezone,
		provideRentalContextHandler,
		provideVehicleCatalog,
		provideVehicleResolver,
		provideVehicleRequirementHandler,
		provideCapabilityResolver,
		provideSearchCompiler,
		provideSearchCarHandler,
		provideIntentRouter,
		provideSearchPolicy,
		provideVehicleComparisonHandler,
		provideRentalRulesHandler,
		provideOrchestrator,
		provideStore,
		provideWebChatService,
		httphandler.New,
	)
	return nil, nil
}
