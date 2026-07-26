package guide

import (
	"context"
	"testing"
	"time"

	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/pkg/log"
)

const devConfigPath = "../../conf/dev.yaml"

func TestRemoteSearchQuotes(t *testing.T) {
	cfg, err := config.Load(devConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Guide.Endpoint == "" {
		t.Fatal("guide.endpoint is required in conf/dev.yaml")
	}

	client := NewHTTPClient(&HTTPConfig{
		Endpoint:   cfg.Guide.Endpoint,
		Phone:      cfg.Guide.Phone,
		TimeoutSec: cfg.Guide.Timeout,
	})
	response, err := client.SearchQuotes(remoteContext("remote-guide-test"), guideTestRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.ContextID == "" {
		t.Fatal("remote guide response has empty context_id")
	}

	t.Log(response.MenuGroup, response.MenuGroup)
}

func remoteContext(traceID string) context.Context {
	logger, _, err := log.NewDailyFileLogger("../../.logs", "agent")
	if err != nil {
		return context.Background()
	}
	log.Init(logger)
	return log.WithTraceID(context.Background(), traceID)
}

func guideTestRequest() *SearchRequest {
	pickup := time.Now().AddDate(0, 0, 1).Format("2006-01-02 15:04:05")
	returnTime := time.Now().AddDate(0, 0, 2).Format("2006-01-02 15:04:05")
	location := &Location{Latitude: 40.0801, Longitude: 116.5846}
	return &SearchRequest{
		PickupRentalInfo:  &RentalInfo{CityID: 10, LocationName: "北京首都国际机场", DateTime: pickup, POI: location},
		DropoffRentalInfo: &RentalInfo{CityID: 10, LocationName: "北京首都国际机场", DateTime: returnTime, POI: location},
		Page:              1,
		PageSize:          10,
	}
}
