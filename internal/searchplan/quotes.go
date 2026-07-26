package searchplan

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/zxq97/agent/api/guide"
)

func ApplyQuoteFilters(values []guide.VehRate, filters []QuoteFilter) []guide.VehRate {
	if len(filters) == 0 {
		return append([]guide.VehRate(nil), values...)
	}
	result := make([]guide.VehRate, 0, len(values))
	for _, value := range values {
		if matchesAll(value, filters) {
			result = append(result, value)
		}
	}
	return result
}

func Rerank(values []guide.VehRate, factors []RankFactor) []guide.VehRate {
	result := append([]guide.VehRate(nil), values...)
	if len(factors) == 0 {
		return result
	}
	sort.SliceStable(result, func(i, j int) bool {
		return quoteScore(result[i], factors) > quoteScore(result[j], factors)
	})
	return result
}

func matchesAll(value guide.VehRate, filters []QuoteFilter) bool {
	for _, filter := range filters {
		if !matches(value, filter) {
			return false
		}
	}
	return true
}

func matches(value guide.VehRate, filter QuoteFilter) bool {
	if value.Vehicle == nil {
		return false
	}
	switch filter.Facet {
	case "seat_num":
		target, err := strconv.Atoi(filter.Value)
		if err != nil {
			return false
		}
		return compareNumber(value.Vehicle.Seats, target, filter.Operator)
	case "brand":
		return compareText(value.Vehicle.BrandName, filter.Value, filter.Operator, false)
	case "vehicle_series", "vehicle_model":
		return compareText(value.Vehicle.VehicleName, filter.Value, filter.Operator, true)
	default:
		return false
	}
}

func compareNumber(actual, target int, operator string) bool {
	switch operator {
	case "eq":
		return actual == target
	case "not_eq":
		return actual != target
	case "gt":
		return actual > target
	case "gte":
		return actual >= target
	case "lt":
		return actual < target
	case "lte":
		return actual <= target
	default:
		return false
	}
}

func compareText(actual, target, operator string, contains bool) bool {
	actual = normalizeQuoteText(actual)
	target = normalizeQuoteText(target)
	equal := actual == target
	if contains {
		equal = strings.Contains(actual, target)
	}
	switch operator {
	case "eq", "contains":
		return equal
	case "not_eq":
		return !equal
	default:
		return false
	}
}

func quoteScore(value guide.VehRate, factors []RankFactor) float64 {
	var score float64
	for _, factor := range factors {
		switch factor.Type {
		case RankPriceLow:
			if value.TotalCharge != nil {
				score -= value.TotalCharge.TotalAmount * factor.Weight
			}
		case RankSeatsTarget:
			if value.Vehicle == nil {
				continue
			}
			target, err := strconv.Atoi(factor.Value)
			if err == nil {
				score -= math.Abs(float64(value.Vehicle.Seats-target)) * factor.Weight
				if value.Vehicle.Seats == target {
					score += 100 * factor.Weight
				}
			}
		case RankPreferredBrand:
			if value.Vehicle != nil && normalizeQuoteText(value.Vehicle.BrandName) == normalizeQuoteText(factor.Value) {
				score += 100 * factor.Weight
			}
		case RankPreferredModel:
			if value.Vehicle != nil && strings.Contains(normalizeQuoteText(value.Vehicle.VehicleName), normalizeQuoteText(factor.Value)) {
				score += 100 * factor.Weight
			}
		}
	}
	return score
}

func normalizeQuoteText(value string) string {
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "", "车型", "")
	return replacer.Replace(strings.ToLower(strings.TrimSpace(value)))
}
