package tools

import (
	"context"
	"fmt"
	"math"

	"github.com/cloudwego/eino/components/tool"
)

// EstimateTripCostInput 行程总费估算入参。
type EstimateTripCostInput struct {
	Days            int     `json:"days"               jsonschema:"description=租期天数。必填,至少 1"`
	CarDailyYuan    float64 `json:"car_daily_yuan"     jsonschema:"description=车辆日租金(元/天),来自 rental_search_quotes 的 daily_price。必填"`
	OneWayKm        float64 `json:"one_way_km"         jsonschema:"description=单程公里数(可选),如北京到天津约 120。用于估算油费和高速费;往返按 2 倍算"`
	RoundTrip       bool    `json:"round_trip"         jsonschema:"description=是否往返(可选,默认 true)。true=往返按 2 倍里程算"`
	FuelType        string  `json:"fuel_type"          jsonschema:"description=燃料类型(可选):gasoline=汽油(默认) ev=纯电。影响能耗费用估算"`
	ParkingDaysYuan float64 `json:"parking_days_yuan"  jsonschema:"description=每天停车费估值(元,可选),不传按 30/天"`
	InsuranceYuanDay float64 `json:"insurance_yuan_day" jsonschema:"description=每天保险加购费(元,可选),来自 guarantee_list 的 day_amount。不传按 0"`
}

// TripCostLine 单个费用项。
type TripCostLine struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"` // 元
	Note   string  `json:"note,omitempty"`
}

// EstimateTripCostOutput 行程总费估算结果。
type EstimateTripCostOutput struct {
	Lines     []TripCostLine `json:"lines"`
	TotalYuan float64        `json:"total_yuan"`
	Note      string         `json:"note"`
}

// 估算用经验系数(占位,非精确):
const (
	fuelPricePerL    = 7.8   // 元/升,92#
	fuelConsumePer100 = 8.0  // 升/百公里,汽油经验值
	evPricePerKwh    = 1.8   // 元/度(对外充电桩均价)
	evConsumePer100  = 15.0  // 度/百公里
	tollPerKm        = 0.45  // 元/公里,高速过路费经验值
	etcDays          = 0.0   // ETC 本身不额外计费,过路费已含在 toll
)

// NewEstimateTripCostTool 构造行程总费估算 tool(纯本地估算)。
func NewEstimateTripCostTool() (tool.InvokableTool, error) {
	return wrapInfer(
		"estimate_trip_cost",
		"估算一次自驾行程的总花费(车费+油费/电费+高速费+停车费+保险),给用户一个预算参考。"+
			"用户问'XX 到 YY N 天大概多少钱'时调用。"+
			"重要:这是粗略估算,不是精确报价,车费请优先用 rental_search_quotes 的真实日租金传入。",
		func(ctx context.Context, in EstimateTripCostInput) (EstimateTripCostOutput, error) {
			if in.Days < 1 {
				return EstimateTripCostOutput{}, fmt.Errorf("days 至少为 1")
			}
			if in.CarDailyYuan <= 0 {
				return EstimateTripCostOutput{}, fmt.Errorf("car_daily_yuan 必填且 > 0(用 rental_search_quotes 的 daily_price)")
			}

			lines := make([]TripCostLine, 0, 5)

			// 1. 车费
			carFee := round2(in.CarDailyYuan * float64(in.Days))
			lines = append(lines, TripCostLine{
				Name:   "车辆租金",
				Amount: carFee,
				Note:   fmt.Sprintf("%.0f 元/天 × %d 天", in.CarDailyYuan, in.Days),
			})

			// 2. 里程相关:油费/电费 + 高速费
			totalKm := in.OneWayKm
			roundTrip := in.RoundTrip || (in.OneWayKm > 0 && !in.RoundTrip) // 默认往返
			if in.OneWayKm > 0 {
				if roundTrip {
					totalKm = in.OneWayKm * 2
				}
				// 能耗费
				var energyFee float64
				var energyNote string
				if in.FuelType == "ev" {
					energyFee = round2(totalKm / 100 * evConsumePer100 * evPricePerKwh)
					energyNote = fmt.Sprintf("纯电 %.0f km × %.0f度/百公里 × %.1f元/度", totalKm, evConsumePer100, evPricePerKwh)
				} else {
					energyFee = round2(totalKm / 100 * fuelConsumePer100 * fuelPricePerL)
					energyNote = fmt.Sprintf("汽油 %.0f km × %.0f升/百公里 × %.1f元/升", totalKm, fuelConsumePer100, fuelPricePerL)
				}
				lines = append(lines, TripCostLine{Name: "能耗费", Amount: energyFee, Note: energyNote})

				// 高速费
				tollFee := round2(totalKm * tollPerKm)
				lines = append(lines, TripCostLine{
					Name:   "高速过路费",
					Amount: tollFee,
					Note:   fmt.Sprintf("%.0f km × %.2f 元/km(经验值)", totalKm, tollPerKm),
				})
			}

			// 3. 停车费
			parkPerDay := in.ParkingDaysYuan
			if parkPerDay <= 0 {
				parkPerDay = 30
			}
			parkFee := round2(parkPerDay * float64(in.Days))
			lines = append(lines, TripCostLine{
				Name:   "停车费",
				Amount: parkFee,
				Note:   fmt.Sprintf("%.0f 元/天 × %d 天(估值)", parkPerDay, in.Days),
			})

			// 4. 保险加购
			if in.InsuranceYuanDay > 0 {
				insFee := round2(in.InsuranceYuanDay * float64(in.Days))
				lines = append(lines, TripCostLine{
					Name:   "保险加购",
					Amount: insFee,
					Note:   fmt.Sprintf("%.0f 元/天 × %d 天", in.InsuranceYuanDay, in.Days),
				})
			}

			var total float64
			for _, l := range lines {
				total += l.Amount
			}
			return EstimateTripCostOutput{
				Lines:     lines,
				TotalYuan: round2(total),
				Note:      "⚠️ 以上为粗略估算(油价/能耗/过路/停车均为经验值),非精确报价,实际以下单及行程中实际消费为准。",
			}, nil
		},
	)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
