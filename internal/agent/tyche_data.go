package agent

import "encoding/json"

// tyche 工具返回的 data 字段是 {errno,errmsg,data,trace_id}。这里定义解析所需的最小结构。

// stdResp tyche 标准响应外层。
type stdResp struct {
	Errno  int             `json:"errno"`
	Errmsg string          `json:"errmsg"`
	Data   json.RawMessage `json:"data"`
}

// parseStdResp 解析外层,errno != 0 视为业务失败。
func parseStdResp(raw json.RawMessage) (data json.RawMessage, ok bool) {
	var r stdResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, false
	}
	if r.Errno != 0 {
		return nil, false
	}
	return r.Data, true
}

// ---- search_locations ----

type locationItem struct {
	LocationID string `json:"location_id"`
	Name       string `json:"name"`
	Address    string `json:"address"`
	CityID     int    `json:"city_id"`
}

type locationsData struct {
	Locations []locationItem `json:"locations"`
}

// ---- resolve_poi ----
//
// tyche 返回结构为 {"data":{"poi":{...}}}，parseStdResp 已剥掉 data 外壳，
// 这里仍有一层 poi。别直接把 data 反序列化到 poiData —— 会全零。

type poiData struct {
	LocationID string  `json:"location_id,omitempty"`
	Latitude   float64 `json:"latitude"`
	Longitude  float64 `json:"longitude"`
	CityID     int     `json:"city_id"`
	Name       string  `json:"name"`
}

type poiEnvelope struct {
	POI poiData `json:"poi"`
}

// ---- 报价内部结构 ----
//
// 原 rental_search_quotes MCP 已下线,quoteItem 现在只供 rental-guide veh_rates
// 转换后的统一内部表示;quotesData(MCP 响应外层)已随 fallback 删除。

type quoteItem struct {
	ReferenceID      string  `json:"reference_id"`
	CarName          string  `json:"car_name"`
	BrandName        string  `json:"brand_name"`
	CarType          string  `json:"car_type"`
	FuelType         string  `json:"fuel_type"`
	TransmissionType string  `json:"transmission_type"`
	Seats            int     `json:"seats"`
	DailyPrice       float64 `json:"daily_price"`
	TotalPrice       float64 `json:"total_price"`
	ImageURL         string  `json:"image_url"`
	Supplier         string  `json:"supplier"`
}

// ---- get_order_details ----

type chargeItem struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Type   int     `json:"type"` // 3 = 保险费(约定)
}

type promotionItem struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
}

type orderDetailsData struct {
	PriceDetail struct {
		Charges    []chargeItem    `json:"charges"`
		Promotions []promotionItem `json:"promotions"`
		DailyPrice float64         `json:"daily_price"`
		Total      float64         `json:"total"`
	} `json:"price_detail"`
}
