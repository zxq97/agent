package tools

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
)

// CheckQualificationInput 资质预检入参。
type CheckQualificationInput struct {
	DriverAgeYears int    `json:"driver_age_years" jsonschema:"description=驾龄(年),如 1 表示拿证满 1 年。必填"`
	VehicleClass   string `json:"vehicle_class"    jsonschema:"description=想租的车型档次(可选):economy=经济型 suv=SUV mpv=MPV luxury=豪华型。留空则返回所有档次的资质要求"`
}

// QualificationRule 单个车型档次的资质要求。
type QualificationRule struct {
	VehicleClass string `json:"vehicle_class"`  // economy / suv / mpv / luxury
	ClassName    string `json:"class_name"`     // 中文名
	MinAgeYears  int    `json:"min_age_years"`  // 最低驾龄要求(年)
	Eligible     bool   `json:"eligible"`       // 当前用户驾龄是否满足
}

// CheckQualificationOutput 资质预检结果。
type CheckQualificationOutput struct {
	DriverAgeYears int                 `json:"driver_age_years"`
	Rules          []QualificationRule `json:"rules"`
	Note           string              `json:"note"`
}

// ⚠️ 占位规则表 —— 真实驾龄/车型要求由业务方提供后替换。
// 数据来源:经验值,仅作 P5 框架跑通用,**不代表真实业务规则**。
var qualificationRules = []struct {
	class    string
	name     string
	minYears int
}{
	{"economy", "经济型", 1},
	{"suv", "SUV", 2},
	{"mpv", "MPV", 2},
	{"luxury", "豪华型", 3},
}

// NewCheckQualificationTool 构造资质预检 tool(纯本地规则,不调后端)。
func NewCheckQualificationTool() (tool.InvokableTool, error) {
	return wrapInfer(
		"check_qualification",
		"根据用户驾龄判断可租哪些车型档次。本地规则,不查后端。"+
			"用户问'我驾龄 X 年能租 SUV 吗 / 能租什么车'时调用。"+
			"注意:返回的是资质门槛,不代表有车,具体车型可用性请用 rental_search_quotes 查。",
		func(ctx context.Context, in CheckQualificationInput) (CheckQualificationOutput, error) {
			if in.DriverAgeYears < 0 {
				return CheckQualificationOutput{}, fmt.Errorf("driver_age_years 不能为负")
			}
			out := CheckQualificationOutput{
				DriverAgeYears: in.DriverAgeYears,
				Note:           "资质门槛为占位规则,最终以下单页面/门店实际要求为准。",
			}
			for _, r := range qualificationRules {
				// 指定了 vehicle_class 时只返回该档
				if in.VehicleClass != "" && in.VehicleClass != r.class {
					continue
				}
				out.Rules = append(out.Rules, QualificationRule{
					VehicleClass: r.class,
					ClassName:    r.name,
					MinAgeYears:  r.minYears,
					Eligible:     in.DriverAgeYears >= r.minYears,
				})
			}
			if len(out.Rules) == 0 {
				return out, fmt.Errorf("未知车型档次 %q,可选:economy/suv/mpv/luxury", in.VehicleClass)
			}
			return out, nil
		},
	)
}
