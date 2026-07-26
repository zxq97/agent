package webchat

import (
	"fmt"
	"strings"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/domain/rentalcontext"
	"github.com/zxq97/agent/internal/domain/searchcar"
	"github.com/zxq97/agent/internal/orchestrator"
	"github.com/zxq97/agent/internal/session"
)

func formatTurn(state *session.AgentSession, turn *orchestrator.TurnResult) TurnResponse {
	var parts []string
	for _, result := range turn.RentalContext {
		if result == nil {
			continue
		}
		switch result.Status {
		case rentalcontext.ResultRejected:
			parts = append(parts, result.Message)
		case rentalcontext.ResultSuccess:
			if text := rentalChangeText(result.ModifiedFields); text != "" {
				parts = append(parts, text)
			}
		}
	}
	if turn.VehicleRequirement != nil && turn.VehicleRequirement.Changed {
		parts = append(parts, "已更新车辆要求。")
	}
	vehicles := []VehicleView{}
	if turn.SearchCar != nil {
		result := turn.SearchCar
		switch result.Status {
		case searchcar.ResultNeedsContext:
			parts = append(parts, "还需要补充"+missingFieldText(result.MissingFields)+"，才能开始搜车。")
		case searchcar.ResultNeedsRequirement:
			parts = append(parts, result.Message)
		case searchcar.ResultNoResults:
			if result.Message != "" {
				parts = append(parts, result.Message)
			} else {
				parts = append(parts, "当前条件下暂时没有找到可用车辆，可以调整一个条件后再试。")
			}
		case searchcar.ResultCapabilityLimit, searchcar.ResultRejected:
			parts = append(parts, result.Message)
		case searchcar.ResultSuccess, searchcar.ResultPartial:
			vehicles = vehicleViews(result.Vehicles)
			parts = append(parts, fmt.Sprintf("找到 %d 个车辆报价，先为你展示当前结果。", len(vehicles)))
			if result.Status == searchcar.ResultPartial && len(result.UnresolvedRequirements) > 0 {
				parts = append(parts, "其中有部分诉求当前无法验证，未当作已经满足的筛选条件。")
			}
			if result.RankingScope == "fetched_set" {
				parts = append(parts, "软偏好排序只作用于本次已获取的候选车辆。")
			}
		}
	}
	if turn.GeneralReply != nil {
		parts = append(parts, turn.GeneralReply.Message)
	}
	pending := pendingView(state.Pending.Active)
	if pending != nil {
		parts = append(parts, pending.Question)
	}
	if len(parts) == 0 {
		parts = append(parts, "我可以帮你修改取还车地点和时间，也可以按品牌、车型、座位、价格等条件搜车。")
	}
	return TurnResponse{Message: strings.Join(nonEmpty(parts), "\n"), Pending: pending, Vehicles: vehicles, State: stateView(state)}
}

func rentalChangeText(fields []rentalcontext.ModifiedField) string {
	var values []string
	for _, field := range fields {
		switch field {
		case rentalcontext.ModifiedLocation:
			values = append(values, "取还车地点")
		case rentalcontext.ModifiedPickupTime:
			values = append(values, "取车时间")
		case rentalcontext.ModifiedReturnTime:
			values = append(values, "还车时间")
		}
	}
	if len(values) == 0 {
		return ""
	}
	return "已更新" + strings.Join(values, "、") + "。"
}

func missingFieldText(fields []searchcar.SearchMissingField) string {
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		switch field {
		case searchcar.MissingLocation:
			values = append(values, "取还车地点")
		case searchcar.MissingPickupTime:
			values = append(values, "取车时间")
		case searchcar.MissingReturnTime:
			values = append(values, "还车时间")
		}
	}
	return strings.Join(values, "、")
}

func stateView(state *session.AgentSession) StateView {
	view := StateView{Requirements: []RequirementView{}, ResultCount: len(state.Search.LastResults)}
	if state.Search.Location != nil {
		view.Location = &LocationView{Name: state.Search.Location.Name, Address: state.Search.Location.Address, CityID: state.Search.Location.CityID}
	}
	view.PickupTime = state.Search.PickupTime
	view.ReturnTime = state.Search.ReturnTime
	for _, requirement := range state.Search.Requirements {
		view.Requirements = append(view.Requirements, RequirementView{Type: requirement.Facet, Value: requirement.CanonicalValue, RawText: requirement.RawText, Importance: requirement.Importance, Status: requirement.Status})
	}
	view.Pending = pendingView(state.Pending.Active)
	return view
}

func pendingView(active *session.PendingInteraction) *PendingView {
	if active == nil {
		return nil
	}
	view := &PendingView{Type: string(active.Type), Question: active.Question, Options: []PendingOptionView{}, ExpireAt: active.ExpireAt}
	for index, option := range active.Options {
		view.Options = append(view.Options, PendingOptionView{Index: index + 1, Label: option.Label, Value: option.Value})
	}
	return view
}

func vehicleViews(values []guide.VehRate) []VehicleView {
	result := make([]VehicleView, 0, len(values))
	for index, value := range values {
		view := VehicleView{Index: index + 1, Supplier: value.SupplierDisplayName}
		if value.Vehicle != nil {
			view.Name = value.Vehicle.VehicleName
			view.Brand = value.Vehicle.BrandName
			view.Seats = value.Vehicle.Seats
		}
		if value.TotalCharge != nil {
			total := value.TotalCharge.TotalAmount
			deduction := value.TotalCharge.DeductionAmount
			view.TotalAmount = &total
			if deduction != 0 {
				view.DeductionAmount = &deduction
			}
		}
		if view.Name != "" {
			result = append(result, view)
		}
	}
	return result
}

func nonEmpty(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}
