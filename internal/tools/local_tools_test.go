package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// TestCheckQualification 验证驾龄门槛判定。
func TestCheckQualification(t *testing.T) {
	tl, err := NewCheckQualificationTool()
	if err != nil {
		t.Fatal(err)
	}

	// 驾龄 1 年:经济型可租,SUV/MPV/豪华不可租
	args, _ := json.Marshal(CheckQualificationInput{DriverAgeYears: 1})
	outStr, err := tl.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatal(err)
	}
	var out CheckQualificationOutput
	if err := json.Unmarshal([]byte(outStr), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Rules) != 4 {
		t.Fatalf("expect 4 rules, got %d", len(out.Rules))
	}
	for _, r := range out.Rules {
		switch r.VehicleClass {
		case "economy":
			if !r.Eligible {
				t.Errorf("驾龄1年应可租经济型")
			}
		case "suv", "mpv", "luxury":
			if r.Eligible {
				t.Errorf("驾龄1年不应可租 %s", r.VehicleClass)
			}
		}
	}
}

// TestCheckQualification_FilterClass 验证指定车型档次只返回该档。
func TestCheckQualification_FilterClass(t *testing.T) {
	tl, _ := NewCheckQualificationTool()
	args, _ := json.Marshal(CheckQualificationInput{DriverAgeYears: 3, VehicleClass: "luxury"})
	outStr, err := tl.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatal(err)
	}
	var out CheckQualificationOutput
	_ = json.Unmarshal([]byte(outStr), &out)
	if len(out.Rules) != 1 || out.Rules[0].VehicleClass != "luxury" {
		t.Fatalf("expect only luxury rule, got %+v", out.Rules)
	}
	if !out.Rules[0].Eligible {
		t.Errorf("驾龄3年应可租豪华型")
	}
}

// TestEstimateTripCost 验证拆项求和正确。
func TestEstimateTripCost(t *testing.T) {
	tl, _ := NewEstimateTripCostTool()
	args, _ := json.Marshal(EstimateTripCostInput{
		Days:         2,
		CarDailyYuan: 150,
		OneWayKm:     120,
		RoundTrip:    true,
		FuelType:     "gasoline",
	})
	outStr, err := tl.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatal(err)
	}
	var out EstimateTripCostOutput
	_ = json.Unmarshal([]byte(outStr), &out)

	// 车费 = 150*2 = 300
	var carFee float64
	var sum float64
	for _, l := range out.Lines {
		if l.Name == "车辆租金" {
			carFee = l.Amount
		}
		sum += l.Amount
	}
	if carFee != 300 {
		t.Errorf("车费应为 300, got %v", carFee)
	}
	if out.TotalYuan != round2(sum) {
		t.Errorf("total %v != sum %v", out.TotalYuan, sum)
	}
	// 应含能耗+高速+停车
	if len(out.Lines) < 4 {
		t.Errorf("expect ≥4 lines (车+油+高速+停车), got %d", len(out.Lines))
	}
}

// TestEstimateTripCost_MissingDaily 验证缺日租金报错。
func TestEstimateTripCost_MissingDaily(t *testing.T) {
	tl, _ := NewEstimateTripCostTool()
	args, _ := json.Marshal(EstimateTripCostInput{Days: 2})
	_, err := tl.InvokableRun(context.Background(), string(args))
	if err == nil {
		t.Fatal("expect error when car_daily_yuan missing")
	}
}

// TestOptimizePickupTime 验证缩短租期能降车费。
func TestOptimizePickupTime(t *testing.T) {
	tl, _ := NewOptimizePickupTimeTool()
	args, _ := json.Marshal(OptimizePickupTimeInput{
		PickupTime:   "2026-06-20 09:00:00",
		ReturnTime:   "2026-06-22 19:00:00",
		CarDailyYuan: 138,
	})
	outStr, err := tl.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatal(err)
	}
	var out OptimizePickupTimeOutput
	_ = json.Unmarshal([]byte(outStr), &out)
	if len(out.Plans) == 0 {
		t.Fatal("expect ≥1 plan")
	}
	// 原方案必须在第一位
	if out.Plans[0].Label != "原方案" {
		t.Errorf("first plan should be 原方案, got %s", out.Plans[0].Label)
	}
	// 所有方案车费 = billed_days * daily
	for _, p := range out.Plans {
		want := round2(p.BilledDays * 138)
		if p.CostYuan != want {
			t.Errorf("plan %s cost %v != %v", p.Label, p.CostYuan, want)
		}
	}
}

// TestOptimizePickupTime_BadTime 验证还车不晚于取车时报错。
func TestOptimizePickupTime_BadTime(t *testing.T) {
	tl, _ := NewOptimizePickupTimeTool()
	args, _ := json.Marshal(OptimizePickupTimeInput{
		PickupTime:   "2026-06-20 10:00:00",
		ReturnTime:   "2026-06-20 09:00:00",
		CarDailyYuan: 138,
	})
	_, err := tl.InvokableRun(context.Background(), string(args))
	if err == nil {
		t.Fatal("expect error when return <= pickup")
	}
}
