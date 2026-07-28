package maps

import (
	"context"
	"os"
	"testing"

	"github.com/zxq97/agent/internal/config"
	"github.com/zxq97/agent/pkg/log"
)

const devConfigPath = "../../conf/dev.yaml"

func TestRemoteSearch(t *testing.T) {
	if os.Getenv("RUN_REMOTE_INTEGRATION") != "1" {
		t.Skip("set RUN_REMOTE_INTEGRATION=1 to run real Maps integration tests")
	}
	search, err := newRemoteClient(t).Search(remoteContext("remote-maps-search-test"), &SearchRequest{Keyword: "南京路"})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Candidates) == 0 || search.Candidates[0].ID == "" {
		t.Fatal("remote maps search returned no resolvable candidate")
	}
}

func newRemoteClient(t *testing.T) Client {
	t.Helper()
	cfg, err := config.Load(devConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Maps.Endpoint == "" {
		t.Fatal("maps.endpoint is required in conf/dev.yaml")
	}
	return NewHTTPClient(&HTTPConfig{
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
	})
}

func remoteContext(traceID string) context.Context {
	logger, _, err := log.NewDailyFileLogger("../../.logs", "agent")
	if err != nil {
		return context.Background()
	}
	log.Init(logger)
	return log.WithTraceID(context.Background(), traceID)
}
