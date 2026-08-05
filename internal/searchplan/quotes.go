package searchplan

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/zxq97/agent/api/guide"
)

type VehicleVerificationStatus string

const (
	VehicleVerificationMatch    VehicleVerificationStatus = "match"
	VehicleVerificationMismatch VehicleVerificationStatus = "mismatch"
	VehicleVerificationUnknown  VehicleVerificationStatus = "unknown"
)

type VerificationCounts struct {
	Match    int
	Mismatch int
	Unknown  int
}

// VerificationReport summarizes deterministic post-filter verification.
type VerificationReport struct {
	ByRequirement  map[string]VerificationCounts
	MatchedQuotes  int
	RejectedQuotes int
}

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

// ApplyLocalVerifiers keeps only quotes that match every deterministic
// postcondition already represented by a remote filter or safe prefilter.
func ApplyLocalVerifiers(values []guide.VehRate, verifiers []LocalVerifier) ([]guide.VehRate, VerificationReport) {
	report := VerificationReport{ByRequirement: make(map[string]VerificationCounts)}
	if len(verifiers) == 0 {
		report.MatchedQuotes = len(values)
		return append([]guide.VehRate(nil), values...), report
	}
	result := make([]guide.VehRate, 0, len(values))
	for _, value := range values {
		matched := true
		for _, verifier := range verifiers {
			status := VerifyLocal(value, verifier)
			counts := report.ByRequirement[verifier.RequirementID]
			switch status {
			case VehicleVerificationMatch:
				counts.Match++
			case VehicleVerificationMismatch:
				counts.Mismatch++
				matched = false
			default:
				counts.Unknown++
				matched = false
			}
			report.ByRequirement[verifier.RequirementID] = counts
		}
		if matched {
			result = append(result, value)
			report.MatchedQuotes++
		} else {
			report.RejectedQuotes++
		}
	}
	return result, report
}

// ApplyVehicleVerifiers is the compatibility wrapper for vehicle-only callers.
func ApplyVehicleVerifiers(values []guide.VehRate, verifiers []VehicleVerifier) []guide.VehRate {
	result, _ := ApplyLocalVerifiers(values, verifiers)
	return result
}

// VerifyLocal checks one quote against one deterministic postcondition.
func VerifyLocal(value guide.VehRate, verifier LocalVerifier) VehicleVerificationStatus {
	if len(verifier.Alternatives) > 0 {
		hasUnknown := false
		for _, alternative := range verifier.Alternatives {
			switch VerifyLocal(value, alternative) {
			case VehicleVerificationMatch:
				return VehicleVerificationMatch
			case VehicleVerificationUnknown:
				hasUnknown = true
			}
		}
		if hasUnknown {
			return VehicleVerificationUnknown
		}
		return VehicleVerificationMismatch
	}
	switch verifier.Facet {
	case "seat_num":
		if value.Vehicle == nil || value.Vehicle.Seats <= 0 {
			return VehicleVerificationUnknown
		}
		target, err := strconv.ParseFloat(verifier.Value, 64)
		if err != nil {
			return VehicleVerificationUnknown
		}
		if compareFloat(float64(value.Vehicle.Seats), target, verifier.Operator) {
			return VehicleVerificationMatch
		}
		return VehicleVerificationMismatch
	case "price_total":
		if value.TotalCharge == nil {
			return VehicleVerificationUnknown
		}
		return verifyNumericValue(value.TotalCharge.TotalAmount, verifier)
	case "price_daily":
		if value.DailyDeductionAmount <= 0 {
			return VehicleVerificationUnknown
		}
		return verifyNumericValue(value.DailyDeductionAmount, verifier)
	case "brand", "vehicle_series", "vehicle_model":
		return VerifyVehicle(value, verifier)
	case "vehicle_type":
		if value.Vehicle == nil || strings.TrimSpace(value.Vehicle.GroupName) == "" || len(verifier.ExpectedNames) == 0 {
			return VehicleVerificationUnknown
		}
		return verificationForExpectedMatch(
			matchesExpectedName(value.Vehicle.GroupName, verifier.ExpectedNames, true),
			verifier.Operator,
		)
	case "energy_type":
		if value.Vehicle == nil || value.Vehicle.FuelType <= 0 || len(verifier.ProviderValues) == 0 {
			return VehicleVerificationUnknown
		}
		return verificationForExpectedMatch(containsInt(verifier.ProviderValues, value.Vehicle.FuelType), verifier.Operator)
	case "transmission":
		if value.Vehicle == nil || value.Vehicle.TransmissionType <= 0 || len(verifier.ProviderValues) == 0 {
			return VehicleVerificationUnknown
		}
		return verificationForExpectedMatch(containsInt(verifier.ProviderValues, value.Vehicle.TransmissionType), verifier.Operator)
	default:
		return VehicleVerificationUnknown
	}
}

func verifyNumericValue(actual float64, verifier LocalVerifier) VehicleVerificationStatus {
	if verifier.Operator == "range" {
		minimum, minErr := strconv.ParseFloat(verifier.MinValue, 64)
		maximum, maxErr := strconv.ParseFloat(verifier.MaxValue, 64)
		if minErr != nil || maxErr != nil {
			return VehicleVerificationUnknown
		}
		if actual >= minimum && actual <= maximum {
			return VehicleVerificationMatch
		}
		return VehicleVerificationMismatch
	}
	target, err := strconv.ParseFloat(verifier.Value, 64)
	if err != nil {
		return VehicleVerificationUnknown
	}
	if compareFloat(actual, target, verifier.Operator) {
		return VehicleVerificationMatch
	}
	return VehicleVerificationMismatch
}

// VerifyVehicle checks one Guide quote against one catalog-backed vehicle
// condition without inventing missing response data.
func VerifyVehicle(value guide.VehRate, verifier VehicleVerifier) VehicleVerificationStatus {
	if value.Vehicle == nil {
		return VehicleVerificationUnknown
	}
	vehicle := value.Vehicle
	if verifier.Facet == "brand" {
		if vehicle.BrandName == "" {
			return VehicleVerificationUnknown
		}
		return verificationForExpectedMatch(
			matchesExpectedName(vehicle.BrandName, verifier.ExpectedNames, false),
			verifier.Operator,
		)
	}
	if verifier.Facet != "vehicle_series" && verifier.Facet != "vehicle_model" {
		return VehicleVerificationUnknown
	}
	if verifier.ExpectedBrand != "" {
		if vehicle.BrandName == "" {
			return VehicleVerificationUnknown
		}
		if normalizeQuoteText(vehicle.BrandName) != normalizeQuoteText(verifier.ExpectedBrand) {
			return verificationForExpectedMatch(false, verifier.Operator)
		}
	}
	if vehicle.VehicleName == "" && vehicle.GroupName == "" {
		return VehicleVerificationUnknown
	}
	return verificationForExpectedMatch(
		matchesExpectedName(vehicle.VehicleName, verifier.ExpectedNames, true) ||
			matchesExpectedName(vehicle.GroupName, verifier.ExpectedNames, true),
		verifier.Operator,
	)
}

func verificationForExpectedMatch(expectedMatch bool, operator string) VehicleVerificationStatus {
	negative := operator == "not_eq" || operator == "not_in"
	if expectedMatch == negative {
		return VehicleVerificationMismatch
	}
	return VehicleVerificationMatch
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func matchesExpectedName(actual string, expected []string, contains bool) bool {
	actual = normalizeQuoteText(actual)
	if actual == "" {
		return false
	}
	for _, value := range expected {
		target := normalizeQuoteText(value)
		if target == "" {
			continue
		}
		if actual == target || (contains && strings.Contains(actual, target)) {
			return true
		}
	}
	return false
}

func Rerank(values []guide.VehRate, factors []RankFactor) []guide.VehRate {
	result := append([]guide.VehRate(nil), values...)
	if len(factors) == 0 {
		return result
	}
	prices := priceBounds(result)
	sort.SliceStable(result, func(i, j int) bool {
		return quoteScore(result[i], factors, prices) > quoteScore(result[j], factors, prices)
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
	switch filter.Facet {
	case "seat_num":
		if value.Vehicle == nil {
			return false
		}
		target, err := strconv.Atoi(filter.Value)
		if err != nil {
			return false
		}
		return compareNumber(value.Vehicle.Seats, target, filter.Operator)
	case "brand":
		if value.Vehicle == nil {
			return false
		}
		return compareText(value.Vehicle.BrandName, filter.Value, filter.Operator, false)
	case "vehicle_series", "vehicle_model":
		if value.Vehicle == nil {
			return false
		}
		return compareText(value.Vehicle.VehicleName, filter.Value, filter.Operator, true)
	case "price_total":
		if value.TotalCharge == nil {
			return false
		}
		target, err := strconv.ParseFloat(filter.Value, 64)
		if err != nil {
			return false
		}
		return compareFloat(value.TotalCharge.TotalAmount, target, filter.Operator)
	default:
		return false
	}
}

func compareFloat(actual, target float64, operator string) bool {
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

type numericBounds struct {
	minimum float64
	maximum float64
	valid   bool
}

func priceBounds(values []guide.VehRate) numericBounds {
	bounds := numericBounds{minimum: math.MaxFloat64, maximum: -math.MaxFloat64}
	for _, value := range values {
		if value.TotalCharge == nil || value.TotalCharge.TotalAmount < 0 {
			continue
		}
		bounds.valid = true
		bounds.minimum = math.Min(bounds.minimum, value.TotalCharge.TotalAmount)
		bounds.maximum = math.Max(bounds.maximum, value.TotalCharge.TotalAmount)
	}
	return bounds
}

func quoteScore(value guide.VehRate, factors []RankFactor, prices numericBounds) float64 {
	var weightedScore float64
	var totalWeight float64
	for _, factor := range factors {
		if factor.Weight <= 0 {
			continue
		}
		factorScore, available := rankFactorScore(value, factor, prices)
		if !available {
			continue
		}
		weightedScore += factorScore * factor.Weight
		totalWeight += factor.Weight
	}
	if totalWeight == 0 {
		return 0
	}
	return weightedScore / totalWeight
}

func rankFactorScore(value guide.VehRate, factor RankFactor, prices numericBounds) (float64, bool) {
	switch factor.Type {
	case RankPriceLow:
		if value.TotalCharge == nil || !prices.valid {
			return 0, false
		}
		if prices.maximum == prices.minimum {
			return 1, true
		}
		return (prices.maximum - value.TotalCharge.TotalAmount) / (prices.maximum - prices.minimum), true
	case RankSeatsTarget:
		if value.Vehicle == nil || value.Vehicle.Seats <= 0 {
			return 0, false
		}
		target, err := strconv.Atoi(factor.Value)
		if err != nil {
			return 0, false
		}
		return 1 / (1 + math.Abs(float64(value.Vehicle.Seats-target))), true
	case RankPreferredBrand:
		if value.Vehicle == nil || strings.TrimSpace(value.Vehicle.BrandName) == "" {
			return 0, false
		}
		if normalizeQuoteText(value.Vehicle.BrandName) == normalizeQuoteText(factor.Value) {
			return 1, true
		}
		return 0, true
	case RankPreferredModel:
		if value.Vehicle == nil || strings.TrimSpace(value.Vehicle.VehicleName) == "" {
			return 0, false
		}
		if strings.Contains(normalizeQuoteText(value.Vehicle.VehicleName), normalizeQuoteText(factor.Value)) {
			return 1, true
		}
		return 0, true
	case RankPreferredEnergy:
		if value.Vehicle == nil || value.Vehicle.FuelType <= 0 || len(factor.ProviderValues) == 0 {
			return 0, false
		}
		if containsInt(factor.ProviderValues, value.Vehicle.FuelType) {
			return 1, true
		}
		return 0, true
	case RankPreferredTransmission:
		if value.Vehicle == nil || value.Vehicle.TransmissionType <= 0 || len(factor.ProviderValues) == 0 {
			return 0, false
		}
		if containsInt(factor.ProviderValues, value.Vehicle.TransmissionType) {
			return 1, true
		}
		return 0, true
	case RankPreferredVehicleType:
		if value.Vehicle == nil || strings.TrimSpace(value.Vehicle.GroupName) == "" {
			return 0, false
		}
		if matchesExpectedName(value.Vehicle.GroupName, []string{factor.Value}, true) {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func normalizeQuoteText(value string) string {
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "", "车型", "")
	return replacer.Replace(strings.ToLower(strings.TrimSpace(value)))
}
