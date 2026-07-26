package maps

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/pkg/http"
)

const textSearchPath = "/map/mapapi/textsearch"

type httpClient struct {
	endpoint string
	config   HTTPConfig
	hc       *httpclient.Client
}

type textSearchResponse struct {
	Errno  int `json:"errno"`
	Result []struct {
		BaseInfo struct {
			POIID       string  `json:"poi_id"`
			DisplayName string  `json:"displayname"`
			Address     string  `json:"address"`
			CityID      int     `json:"city_id"`
			CityName    string  `json:"city_name"`
			Latitude    float64 `json:"lat"`
			Longitude   float64 `json:"lng"`
		} `json:"base_info"`
	} `json:"result"`
}

// NewHTTPClient creates a concrete HTTP map client while exposing only Client
// to callers. Paths and timeout are part of the map service contract.
func NewHTTPClient(cfg *HTTPConfig) Client {
	if cfg == nil {
		cfg = &HTTPConfig{}
	}
	return &httpClient{
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		config:   *cfg,
		hc: httpclient.NewClient(&httpclient.Config{
			TimeoutSec: 10,
		}),
	}
}

func (c *httpClient) Search(ctx context.Context, input *SearchRequest) (*SearchResponse, error) {
	if input == nil || strings.TrimSpace(input.Keyword) == "" {
		return nil, errors.New("maps search: keyword is required")
	}
	if input.Limit <= 0 {
		input.Limit = 10
	}
	values := c.textSearchValues(input.Keyword)
	responseBody, err := c.hc.Get(ctx, "maps text_search", c.endpoint+textSearchPath+"?"+values.Encode())
	if err != nil {
		return nil, err
	}
	var raw textSearchResponse
	if err := json.Unmarshal(responseBody, &raw); err != nil {
		return nil, err
	}
	if raw.Errno != 0 {
		return nil, errors.Errorf("maps text_search: failed errno=%d", raw.Errno)
	}
	response := &SearchResponse{Candidates: make([]Candidate, 0, len(raw.Result))}
	for _, item := range raw.Result {
		poi := item.BaseInfo
		if poi.POIID == "" {
			continue
		}
		response.Candidates = append(response.Candidates, Candidate{ID: poi.POIID, Name: poi.DisplayName, Address: poi.Address, CityID: poi.CityID, CityName: poi.CityName, Latitude: poi.Latitude, Longitude: poi.Longitude})
	}
	return response, nil
}

func (c *httpClient) textSearchValues(keyword string) url.Values {
	values := url.Values{}
	values.Set("product_id", c.config.ProductID)
	values.Set("acc_key", c.config.AccKey)
	values.Set("app_version", c.config.AppVersion)
	values.Set("platform", c.config.Platform)
	values.Set("app_id", c.config.AppID)
	values.Set("map_type", c.config.MapType)
	values.Set("coordinate_type", c.config.CoordinateType)
	values.Set("requester_type", c.config.RequesterType)
	values.Set("lang", c.config.Lang)
	values.Set("caller_id", c.config.CallerID)
	values.Set("place_type", c.config.PlaceType)
	values.Set("query", keyword)
	values.Set("is_nation_search", "1")
	return values
}
