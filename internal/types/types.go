// Package types holds shared cross-module value types.
// 跨模块共享的值类型放这里,避免子包互相 import 形成环。
package types

import "time"

// AddressInfo 对齐 rental-saas-api 的 dto_common.AddressInfo,
// 给 tool 层调用 inner 接口时拼请求用。
type AddressInfo struct {
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
	Address   string `json:"address"`
	CityID    int    `json:"city_id"`
	Time      string `json:"time"` // 格式 "2006-01-02 15:04:05"
}

// QuoteSlot 是导购对话的关键槽位,缺哪个就主动追问。
type QuoteSlot struct {
	CityID     int
	CityName   string
	PickupAt   *time.Time
	ReturnAt   *time.Time
	PickupAddr *AddressInfo
	ReturnAddr *AddressInfo
	StoreCode  string
	Budget     int    // 单位:分,0 表示未设置
	Usage      string // "商务" / "旅游" / "婚庆" / "搬家" 等
	Passenger  int    // 乘车人数
	DriverAge  int    // 驾龄(年)
}

// IsReady 判定能否直接调 list_quotes(必填:取还城市/时间/地址)。
func (s *QuoteSlot) IsReady() bool {
	if s.PickupAt == nil || s.ReturnAt == nil {
		return false
	}
	if s.PickupAddr == nil || s.PickupAddr.CityID <= 0 {
		return false
	}
	if s.ReturnAddr == nil || s.ReturnAddr.CityID <= 0 {
		return false
	}
	return true
}

// Phase 表示对话阶段,用于 supervisor 路由(P3 起用到)。
type Phase string

const (
	PhaseShopping  Phase = "shopping"
	PhaseInsurance Phase = "insurance"
	PhaseKnowledge Phase = "knowledge"
	PhaseAfter     Phase = "aftersales"
)
