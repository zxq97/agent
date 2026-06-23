package tools

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/cloudwego/eino/components/tool"
)

// OptimizePickupTimeInput 取还时间优化入参。
type OptimizePickupTimeInput struct {
	PickupTime   string  `json:"pickup_time"   jsonschema:"description=当前计划取车时间,格式 'YYYY-MM-DD HH:MM:SS'。必填"`
	ReturnTime   string  `json:"return_time"   jsonschema:"description=当前计划还车时间,格式 'YYYY-MM-DD HH:MM:SS'。必填"`
	CarDailyYuan float64 `json:"car_daily_yuan" jsonschema:"description=车辆日租金(元/天),来自 rental_search_quotes 的 daily_price。必填"`
}

// TimePlan 一个取还时间方案及其计费。
type TimePlan struct {
	Label      string  `json:"label"`        // 方案说明,如"原方案" / "晚 2 小时取车"
	PickupTime string  `json:"pickup_time"`
	ReturnTime string  `json:"return_time"`
	BilledDays float64 `json:"billed_days"`  // 计费天数(可能含小时折算)
	CostYuan   float64 `json:"cost_yuan"`    // 估算车费
}

// OptimizePickupTimeOutput 时间优化对比结果。
type OptimizePickupTimeOutput struct {
	Plans []TimePlan `json:"plans"`
	Note  string     `json:"note"`
}

// 计费规则(占位,与租车行业常见规则对齐;真实规则以后端为准):
//   - 不足 24h 的部分:超出 < 4h 按小时计(日租/24 × 小时,封顶不超过日租);
//     超出 ≥ 4h 按整天计。这里简化为"向上按 0.5 天台阶取整"做演示。
// ⚠️ 仅用于"给用户一个省钱方向的参考",真实计费以下单页面为准。

// NewOptimizePickupTimeTool 构造取还时间优化 tool(纯本地估算)。
func NewOptimizePickupTimeTool() (tool.InvokableTool, error) {
	return wrapInfer(
		"optimize_pickup_time",
		"对比不同取还车时间下的车费差异,帮用户找省钱的取还时间。"+
			"用户问'晚 X 小时取能省多少 / 提前还划不划算 / 租 N 天 vs N+1 天'时调用。"+
			"返回若干方案的计费天数和估算车费对比。这是估算,实际计费以下单为准。",
		func(ctx context.Context, in OptimizePickupTimeInput) (OptimizePickupTimeOutput, error) {
			pt, err := parseTime(in.PickupTime)
			if err != nil {
				return OptimizePickupTimeOutput{}, fmt.Errorf("pickup_time: %w", err)
			}
			rt, err := parseTime(in.ReturnTime)
			if err != nil {
				return OptimizePickupTimeOutput{}, fmt.Errorf("return_time: %w", err)
			}
			if !rt.After(pt) {
				return OptimizePickupTimeOutput{}, fmt.Errorf("还车时间必须晚于取车时间")
			}
			if in.CarDailyYuan <= 0 {
				return OptimizePickupTimeOutput{}, fmt.Errorf("car_daily_yuan 必填且 > 0")
			}

			hours := rt.Sub(pt).Hours()
			origDays := billedDays(hours)

			plans := []TimePlan{
				{
					Label:      "原方案",
					PickupTime: in.PickupTime,
					ReturnTime: in.ReturnTime,
					BilledDays: origDays,
					CostYuan:   round2(origDays * in.CarDailyYuan),
				},
			}

			// 备选 1:晚 2 小时取车(还车不变)→ 缩短租期
			alt1Hours := hours - 2
			if alt1Hours > 0 {
				d := billedDays(alt1Hours)
				plans = append(plans, TimePlan{
					Label:      "晚 2 小时取车(还车时间不变)",
					PickupTime: pt.Add(2 * time.Hour).Format(timeLayout),
					ReturnTime: in.ReturnTime,
					BilledDays: d,
					CostYuan:   round2(d * in.CarDailyYuan),
				})
			}

			// 备选 2:早 2 小时还车(取车不变)→ 缩短租期
			alt2Hours := hours - 2
			if alt2Hours > 0 {
				d := billedDays(alt2Hours)
				plans = append(plans, TimePlan{
					Label:      "提前 2 小时还车(取车时间不变)",
					PickupTime: in.PickupTime,
					ReturnTime: rt.Add(-2 * time.Hour).Format(timeLayout),
					BilledDays: d,
					CostYuan:   round2(d * in.CarDailyYuan),
				})
			}

			return OptimizePickupTimeOutput{
				Plans: plans,
				Note:  "⚠️ 计费天数为占位估算规则(不足整天按 0.5 天台阶),实际计费规则以下单页面为准。",
			}, nil
		},
	)
}

// billedDays 把租用小时数折算成计费天数(占位规则:按 0.5 天台阶向上取整)。
func billedDays(hours float64) float64 {
	if hours <= 0 {
		return 0
	}
	return math.Ceil(hours/12) * 0.5
}
