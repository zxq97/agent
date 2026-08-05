// Package vehiclecompare compares quotes already returned by the current
// search. It never asks an LLM to invent vehicle specifications.
package vehiclecompare

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/progress"
	"github.com/zxq97/agent/internal/searchruntime"
	"github.com/zxq97/agent/internal/session"
)

const maxComparedOptions = 4

var numberPattern = regexp.MustCompile(`\d+`)

// Handler compares vehicles already present in the active search result.
type Handler interface {
	Handle(context.Context, *session.AgentSession, *Input) (*Result, error)
}

type handler struct{}

func NewHandler() Handler {
	return &handler{}
}

func (h *handler) Handle(ctx context.Context, agentSession *session.AgentSession, input *Input) (*Result, error) {
	if agentSession == nil {
		return nil, errors.New("vehicle compare: session is required")
	}
	if input == nil || strings.TrimSpace(input.EvidenceText) == "" {
		return nil, errors.New("vehicle compare: evidence text is required")
	}
	progress.Emit(ctx, "vehicle_comparison", "正在对比当前车辆报价")
	candidates := currentOptions(agentSession)
	if len(candidates) < 2 {
		return &Result{
			Status:  StatusNoSearchResult,
			Message: "当前没有至少两个可对比的车辆报价，请先完成搜车。",
		}, nil
	}
	selected := selectOptions(input.EvidenceText, candidates)
	if len(selected) < 2 || len(selected) > maxComparedOptions {
		return &Result{
			Status:     StatusNeedsSelection,
			Message:    "请指定要对比的 2～4 个车辆序号，例如“对比1和3”。",
			Candidates: candidates,
		}, nil
	}
	comparison := &Comparison{
		Options: selected,
		Scope:   "current_search_result",
		Limitations: []string{
			"只比较 Guide 当前报价返回的字段",
			"未返回的空间、安全和配置字段不会被推断",
			"价格只代表当前搜索上下文和当前报价",
		},
	}
	comparison.Highlights = highlights(selected)
	return &Result{
		Status:     StatusSuccess,
		Message:    "已按当前搜索结果对比所选车辆；未返回的配置不会被当作事实补充。",
		Comparison: comparison,
	}, nil
}

func currentOptions(agentSession *session.AgentSession) []Option {
	if agentSession == nil || len(agentSession.Search.LastResults) == 0 {
		return nil
	}
	quotes := snapshotQuotes(agentSession.Search.ActiveSearch)
	result := make([]Option, 0, len(agentSession.Search.LastResults))
	for index, ref := range agentSession.Search.LastResults {
		quote, ok := findQuote(quotes, ref)
		if !ok || quote.Vehicle == nil {
			continue
		}
		result = append(result, optionFromQuote(index+1, quote))
	}
	return result
}

func snapshotQuotes(snapshot *session.ActiveSearchSnapshot) []searchruntime.Quote {
	if snapshot == nil {
		return nil
	}
	var result []searchruntime.Quote
	for _, batch := range snapshot.Batches {
		result = append(result, batch.Vehicles...)
	}
	return result
}

func findQuote(values []searchruntime.Quote, ref session.VehicleResultRef) (searchruntime.Quote, bool) {
	for _, value := range values {
		if ref.ReferenceID != "" && value.Reference != nil && value.Reference.ID == ref.ReferenceID {
			return value, true
		}
	}
	for _, value := range values {
		if value.Vehicle == nil || ref.VehicleCode == "" || value.Vehicle.Code != ref.VehicleCode {
			continue
		}
		if ref.SupplierCode == "" || value.SupplierCode == ref.SupplierCode {
			return value, true
		}
	}
	return searchruntime.Quote{}, false
}

func optionFromQuote(index int, value searchruntime.Quote) Option {
	option := Option{
		Index:        index,
		VehicleName:  value.Vehicle.Name,
		VehicleCode:  value.Vehicle.Code,
		BrandName:    value.Vehicle.BrandName,
		GroupName:    value.Vehicle.GroupName,
		Seats:        value.Vehicle.Seats,
		SupplierName: value.SupplierDisplayName,
	}
	if value.TotalCharge != nil && value.TotalCharge.TotalAmount > 0 {
		total := value.TotalCharge.TotalAmount
		option.TotalAmount = &total
	}
	if value.DailyDeduction > 0 {
		daily := value.DailyDeduction
		option.DailyDeductionAmount = &daily
	}
	if value.Vehicle.FuelType > 0 {
		fuel := value.Vehicle.FuelType
		option.FuelTypeCode = &fuel
	}
	if value.Vehicle.TransmissionType > 0 {
		transmission := value.Vehicle.TransmissionType
		option.TransmissionTypeCode = &transmission
	}
	return option
}

func selectOptions(text string, candidates []Option) []Option {
	selected := make(map[int]struct{})
	lower := strings.ToLower(text)
	remaining := lower
	for _, candidate := range candidates {
		name := strings.ToLower(strings.TrimSpace(candidate.VehicleName))
		if name != "" && strings.Contains(remaining, name) {
			selected[candidate.Index] = struct{}{}
			remaining = strings.ReplaceAll(remaining, name, "")
		}
	}
	for _, match := range numberPattern.FindAllString(remaining, -1) {
		index, err := strconv.Atoi(match)
		if err == nil && index >= 1 && index <= len(candidates) {
			selected[index] = struct{}{}
		}
	}
	ordinals := map[string]int{"第一": 1, "第二": 2, "第三": 3, "第四": 4}
	for word, index := range ordinals {
		if strings.Contains(text, word) && index <= len(candidates) {
			selected[index] = struct{}{}
		}
	}
	if len(selected) == 0 &&
		(len(candidates) == 2 || (len(candidates) <= maxComparedOptions &&
			(strings.Contains(text, "全部") || strings.Contains(text, "这些") || strings.Contains(text, "这几个")))) {
		for _, candidate := range candidates {
			selected[candidate.Index] = struct{}{}
		}
	}
	indexes := make([]int, 0, len(selected))
	for index := range selected {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	result := make([]Option, 0, len(indexes))
	for _, index := range indexes {
		for _, candidate := range candidates {
			if candidate.Index == index {
				result = append(result, candidate)
				break
			}
		}
	}
	return result
}

func highlights(values []Option) Highlights {
	result := Highlights{}
	minimumPrice := math.MaxFloat64
	maximumPrice := -math.MaxFloat64
	maximumSeats := 0
	for _, value := range values {
		if value.TotalAmount != nil {
			minimumPrice = math.Min(minimumPrice, *value.TotalAmount)
			maximumPrice = math.Max(maximumPrice, *value.TotalAmount)
		}
		maximumSeats = max(maximumSeats, value.Seats)
	}
	if minimumPrice != math.MaxFloat64 {
		for _, value := range values {
			if value.TotalAmount != nil && *value.TotalAmount == minimumPrice {
				result.LowestTotalPriceIndexes = append(result.LowestTotalPriceIndexes, value.Index)
			}
		}
		spread := maximumPrice - minimumPrice
		result.TotalPriceSpread = &spread
	}
	if maximumSeats > 0 {
		for _, value := range values {
			if value.Seats == maximumSeats {
				result.MostSeatsIndexes = append(result.MostSeatsIndexes, value.Index)
			}
		}
	}
	return result
}
