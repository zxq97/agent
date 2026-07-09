package tyche

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GuideClient 调 rental-guide 集群 /car/rental/guide/store/list/agent 获取菜单+报价。
// 该接口返回 menu_group(筛选菜单) + veh_rates(报价) + context_id。
type GuideClient struct {
	endpoint string // rental-guide 集群地址(如 http://10.78.133.4:8877)
	phone    string // Bearer 鉴权手机号
	hc       *http.Client
	logOut   io.Writer
}

const guidePath = "/car/rental/guide/store/list/agent"

// NewGuideClient 构造 guide client。
func NewGuideClient(endpoint, phone string, timeoutSec int, logOut io.Writer) *GuideClient {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	return &GuideClient{
		endpoint: endpoint,
		phone:    phone,
		hc:       &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
		logOut:   logOut,
	}
}

// GuideRentalInfo 取还车信息(对齐 tyche dto.RentalInfo 必要字段)。
type GuideRentalInfo struct {
	CityID       int                `json:"city_id"`
	LocationName string             `json:"location_name,omitempty"`
	DateTime     string             `json:"date_time,omitempty"`
	POI          *GuideRentalInfoPOI `json:"poi,omitempty"`
}

// GuideRentalInfoPOI 经纬度。
type GuideRentalInfoPOI struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// GuideSearchRequest 调 guide/store/list/agent 的入参。
// 对齐 tyche dto.StoreListParam 必要字段。
type GuideSearchRequest struct {
	PickupRentalInfo  *GuideRentalInfo `json:"pickup_rental_info"`
	DropoffRentalInfo *GuideRentalInfo `json:"dropoff_rental_info"`
	Filter            GuideFilterInfo  `json:"filter_info"`
	Page              int              `json:"page"`
	PageSize          int              `json:"page_size"`
	ContextID         string           `json:"context_id,omitempty"`
	Xenv              string           `json:"xenv,omitempty"`
}

// GuideFilterInfo 筛选信息。
type GuideFilterInfo struct {
	FilterCodes []string `json:"filter_codes"`
	SortCode    string   `json:"sort_code,omitempty"`
	GroupCode   string   `json:"group_code,omitempty"`
}

// GuideSearchResponse guide/store/list/agent 响应数据。
type GuideSearchResponse struct {
	ContextID string           `json:"context_id"`
	MenuGroup []GuideMenuGroup `json:"menu_group"`
	VehRates  []GuideVehRate   `json:"veh_rates"`
}

// GuideMenuGroup 菜单分组。
type GuideMenuGroup struct {
	Name       string           `json:"name"`
	GroupCode  string           `json:"group_code"`
	GroupType  string           `json:"group_type"`
	GroupItems []GuideGroupItem `json:"group_items"`
}

// GuideGroupItem 菜单子分组。
type GuideGroupItem struct {
	Items []GuideItem `json:"items"`
}

// GuideItem 菜单单项。
type GuideItem struct {
	Name     string `json:"name"`
	ItemCode string `json:"item_code"`
}

// GuideVehRate 报价信息(从 guide storelist 返回)。
type GuideVehRate struct {
	SupplierCode        string          `json:"supplier_code"`
	SupplierDisplayName string          `json:"supplier_display_name"`
	Vehicle             *guideVehicle   `json:"vehicle"`
	DailyDeductionAmount float64        `json:"daily_deduction_amount"` // 券后日均价(元)
	TotalCharge         *guideTotalCharge `json:"total_charge"`
	ReferenceInfo       *guideRefInfo   `json:"reference_info"`
	FreeDepositType     int             `json:"free_deposit_type"`
}

type guideVehicle struct {
	VehicleName      string `json:"vehicle_name"`
	VehicleCode      string `json:"vehicle_code"`
	BrandName        string `json:"brand_name"`
	GroupName        string `json:"group_name"` // 经济型/SUV/商务车/豪华型
	Seats            int    `json:"seats"`
	FuelType         int    `json:"fuel_type"`         // 1.汽油 2.柴油 3.混动 4.纯电
	TransmissionType int    `json:"transmission_type"` // 1.手动 2.自动
}

type guideTotalCharge struct {
	TotalAmount        float64 `json:"total_amount"`         // 原价总计(元)
	DeductionAmount    float64 `json:"deduction_amount"`     // 券后总价(元)
	DeductionAmountInt int64   `json:"deduction_amount_int"` // 券后总价(分)
}

type guideRefInfo struct {
	ReferenceID string `json:"reference_id"`
}

type guideEnvelope struct {
	Errno  int                 `json:"errno"`
	Errmsg string              `json:"errmsg"`
	Data   GuideSearchResponse `json:"data"`
}

// SearchQuotes 调 guide/store/list/agent。
func (c *GuideClient) SearchQuotes(ctx context.Context, req GuideSearchRequest) (*GuideSearchResponse, error) {
	// page_size 最低为 6(guide controller 校验 >5)
	if req.PageSize < 6 {
		req.PageSize = 6
	}
	if req.Page < 1 {
		req.Page = 1
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("guide: marshal request: %w", err)
	}

	url := c.endpoint + guidePath
	if c.logOut != nil {
		fmt.Fprintf(c.logOut, "\n[guide] -> POST %s\n[guide] req: %s\n", url, truncate(string(body), 8192))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("guide: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.phone != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.phone)
	}

	start := time.Now()
	resp, err := c.hc.Do(httpReq)
	if err != nil {
		if c.logOut != nil {
			fmt.Fprintf(c.logOut, "[guide] ERR after %s: %v\n", time.Since(start), err)
		}
		return nil, fmt.Errorf("guide: http do: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("guide: read resp: %w", err)
	}
	if c.logOut != nil {
		fmt.Fprintf(c.logOut, "[guide] %d in %s, resp: %s\n", resp.StatusCode, time.Since(start), truncate(string(respBody), 8192))
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("guide: http %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var env guideEnvelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return nil, fmt.Errorf("guide: unmarshal: %w", err)
	}
	if env.Errno != 0 {
		return nil, fmt.Errorf("guide: errno=%d errmsg=%s", env.Errno, env.Errmsg)
	}

	return &env.Data, nil
}
