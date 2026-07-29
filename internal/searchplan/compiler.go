package searchplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/vehiclecatalog"
)

type Compiler struct {
	entities *vehiclecatalog.StaticCatalog
}

func NewCompiler() *Compiler {
	return NewCompilerWithVehicleCatalog(vehiclecatalog.NewDefaultCatalog())
}

// NewCompilerWithVehicleCatalog builds a compiler with the authoritative
// catalog used to construct and verify vehicle-entity filters.
func NewCompilerWithVehicleCatalog(entities *vehiclecatalog.StaticCatalog) *Compiler {
	if entities == nil {
		entities = vehiclecatalog.NewDefaultCatalog()
	}
	return &Compiler{entities: entities}
}

func (c *Compiler) Compile(requirements []Requirement, menu []guide.MenuGroup) FilterPlan {
	index := buildMenuIndex(menu)
	redundant := redundantParents(requirements)
	conflicts := conflictingRequirements(requirements)
	plan := FilterPlan{}
	for _, requirement := range requirements {
		if requirement.Status == "removed" || requirement.Status == "superseded" {
			continue
		}
		if reason, exists := conflicts[requirement.ID]; exists {
			plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnsupported, "conflicting_requirements", reason))
			continue
		}
		if _, exists := redundant[requirement.ID]; exists {
			plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityAdvisory, "redundant_parent", "更具体的车辆实体已覆盖父级条件"))
			continue
		}
		compileRequirement(&plan, requirement, index, c.entities)
	}
	plan.MenuFilters = uniqueMenuFilters(plan.MenuFilters)
	plan.Disclosures = DisclosuresFromResolutions(plan.Resolutions)
	plan.PlanHash = planHash(plan)
	return plan
}

// conflictingRequirements catches combinations whose current execution
// semantics would otherwise become an accidental AND and produce misleading
// empty results. The intent remains in Session and the hard conflict is
// reported instead of being silently dropped or broadened.
func conflictingRequirements(requirements []Requirement) map[string]string {
	conflicts := make(map[string]string)
	positiveByFacet := make(map[string][]Requirement)
	negativeByFacet := make(map[string][]Requirement)
	var brands, series, models []Requirement
	for _, requirement := range requirements {
		if requirement.Status == "removed" || requirement.Status == "superseded" || requirement.Importance != "hard" {
			continue
		}
		switch requirement.Operator {
		case "eq":
			positiveByFacet[requirement.Facet] = append(positiveByFacet[requirement.Facet], requirement)
		case "not_eq":
			negativeByFacet[requirement.Facet] = append(negativeByFacet[requirement.Facet], requirement)
		}
		switch requirement.Facet {
		case "brand":
			brands = append(brands, requirement)
		case "vehicle_series":
			series = append(series, requirement)
		case "vehicle_model":
			models = append(models, requirement)
		}
	}
	for facet, positives := range positiveByFacet {
		for left := 0; left < len(positives); left++ {
			for right := left + 1; right < len(positives); right++ {
				if normalizedRequirementValue(positives[left]) == normalizedRequirementValue(positives[right]) {
					continue
				}
				reason := "同一诉求维度包含多个必须同时满足的不同值，当前未确认可用 OR 语义"
				conflicts[positives[left].ID] = reason
				conflicts[positives[right].ID] = reason
			}
		}
		for _, positive := range positives {
			for _, negative := range negativeByFacet[facet] {
				if normalizedRequirementValue(positive) != normalizedRequirementValue(negative) {
					continue
				}
				reason := "同一条件同时被要求和排除"
				conflicts[positive.ID] = reason
				conflicts[negative.ID] = reason
			}
		}
	}
	for _, model := range models {
		for _, brand := range brands {
			if model.EntityBrandID != "" && brand.EntityID != "" && model.EntityBrandID != brand.EntityID {
				reason := "车型与品牌属于不同品牌"
				conflicts[model.ID] = reason
				conflicts[brand.ID] = reason
			}
		}
		for _, vehicleSeries := range series {
			if model.EntityParentID != "" && vehicleSeries.EntityID != "" && model.EntityParentID != vehicleSeries.EntityID {
				reason := "车型与车系的父子关系冲突"
				conflicts[model.ID] = reason
				conflicts[vehicleSeries.ID] = reason
			}
		}
	}
	for _, vehicleSeries := range series {
		for _, brand := range brands {
			if vehicleSeries.EntityBrandID != "" && brand.EntityID != "" && vehicleSeries.EntityBrandID != brand.EntityID {
				reason := "车系与品牌属于不同品牌"
				conflicts[vehicleSeries.ID] = reason
				conflicts[brand.ID] = reason
			}
		}
	}
	return conflicts
}

func normalizedRequirementValue(requirement Requirement) string {
	if requirement.EntityID != "" {
		return requirement.EntityID
	}
	return normalize(requirement.CanonicalValue)
}

type indexedMenuItem struct {
	name string
	code string
}

type menuIndex struct {
	byFacet map[string][]indexedMenuItem
	sort    map[string]string
}

func buildMenuIndex(groups []guide.MenuGroup) menuIndex {
	index := menuIndex{byFacet: make(map[string][]indexedMenuItem), sort: make(map[string]string)}
	for _, group := range groups {
		for _, set := range group.GroupItems {
			for _, item := range set.Items {
				if item.ItemCode == "" {
					continue
				}
				if strings.HasPrefix(item.ItemCode, "sort_") {
					index.sort[normalize(item.Name)] = item.ItemCode
					continue
				}
				facet := facetForCode(item.ItemCode)
				if facet == "" {
					continue
				}
				index.byFacet[facet] = append(index.byFacet[facet], indexedMenuItem{name: item.Name, code: item.ItemCode})
			}
		}
	}
	return index
}

func facetForCode(code string) string {
	switch {
	case strings.HasPrefix(code, "filter/car_age/"):
		return "car_age"
	case strings.HasPrefix(code, "filter/seat_num/"):
		return "seat_num"
	case strings.HasPrefix(code, "filter/transmission/"):
		return "transmission"
	case strings.HasPrefix(code, "filter/fuel/"):
		return "energy_type"
	case strings.HasPrefix(code, "filter/vehcle_choice/"):
		return "vehicle_type"
	case strings.HasPrefix(code, "filter/total_fee/"), strings.HasPrefix(code, "filter/price/"):
		return "price_preference"
	case strings.HasPrefix(code, "filter/brand/"):
		return "brand"
	case strings.HasPrefix(code, "filter/vehicle_name/"), strings.HasPrefix(code, "filter/model/"):
		return "vehicle_model"
	default:
		return ""
	}
}

func compileRequirement(plan *FilterPlan, requirement Requirement, index menuIndex, entities *vehiclecatalog.StaticCatalog) {
	if requirement.EntityResolution == "ambiguous" {
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityAmbiguous, "vehicle_entity_ambiguous", "车辆名称存在多个候选"))
		return
	}
	switch requirement.Facet {
	case "seat_num":
		compileSeat(plan, requirement, index)
	case "vehicle_type":
		compileNamedMenu(plan, requirement, index, vehicleTypeAliases)
	case "energy_type":
		compileNamedMenu(plan, requirement, index, energyAliases)
	case "transmission":
		compileNamedMenu(plan, requirement, index, transmissionAliases)
	case "car_age":
		compileCarAge(plan, requirement, index)
	case "price_preference":
		compilePrice(plan, requirement, index)
	case "brand", "vehicle_series", "vehicle_model":
		compileVehicleEntity(plan, requirement, entities)
	case "comfort_preference":
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "missing_comfort_data", "Guide 当前没有可验证的舒适性字段"))
	case "custom":
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "custom_not_executable", "当前菜单和车辆字段无法可靠处理该诉求"))
	default:
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnsupported, "facet_not_supported", "当前不支持该车辆诉求类型"))
	}
}

func compileSeat(plan *FilterPlan, requirement Requirement, index menuIndex) {
	value, err := strconv.Atoi(strings.TrimSpace(requirement.CanonicalValue))
	if err != nil || value <= 0 {
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "invalid_seat_num", "座位数不是可比较的正整数"))
		return
	}
	if requirement.Importance == "soft" {
		plan.RankFactors = append(plan.RankFactors, RankFactor{RequirementID: requirement.ID, Type: RankSeatsTarget, Value: strconv.Itoa(value), Weight: 1, DataField: "vehicle.seats"})
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityRankable, "quote_rank", "按 Guide 返回的座位数字段在当前候选集中排序"))
		return
	}
	if item, prefilter, ok := index.findSeat(value, requirement.Operator); ok {
		addMenuFilterWithMode(plan, requirement, item, prefilter)
		plan.LocalVerifiers = append(plan.LocalVerifiers, LocalVerifier{
			RequirementID: requirement.ID,
			Facet:         "seat_num",
			Operator:      requirement.Operator,
			Value:         strconv.Itoa(value),
		})
		return
	}
	plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "seat_filter_not_found", "当前 Guide 座位数菜单无法无损表达该条件"))
}

func (i menuIndex) findSeat(value int, operator string) (indexedMenuItem, bool, bool) {
	var prefilter indexedMenuItem
	bestDistance := int(^uint(0) >> 1)
	for _, item := range i.byFacet["seat_num"] {
		code := strings.ToLower(item.code)
		name := strings.ToLower(item.name)
		numbers := numericParts(code + " " + name)
		if !containsNumber(numbers, float64(value)) {
			if candidate, distance, ok := seatPrefilter(item, value, operator); ok && distance < bestDistance {
				prefilter = candidate
				bestDistance = distance
			}
			continue
		}
		switch operator {
		case "eq":
			if strings.Contains(code, "/ge_") ||
				strings.Contains(code, "/gte_") ||
				hasMultipleDistinctNumbers(code) ||
				strings.Contains(name, "以上") ||
				strings.Contains(name, "-") ||
				strings.Contains(name, "至") {
				if candidate, distance, ok := seatPrefilter(item, value, operator); ok && distance < bestDistance {
					prefilter = candidate
					bestDistance = distance
				}
				continue
			}
			return item, false, true
		case "gte":
			if strings.Contains(code, "/ge_") ||
				strings.Contains(code, "/gte_") ||
				strings.Contains(name, "以上") {
				return item, false, true
			}
		}
	}
	if prefilter.code != "" {
		return prefilter, true, true
	}
	return indexedMenuItem{}, false, false
}

func seatPrefilter(item indexedMenuItem, target int, operator string) (indexedMenuItem, int, bool) {
	code := strings.ToLower(item.code)
	name := strings.ToLower(item.name)
	numbers := distinctNumbers(code + " " + name)
	switch operator {
	case "eq":
		if len(numbers) >= 2 {
			minimum, maximum := numbers[0], numbers[len(numbers)-1]
			if float64(target) >= minimum && float64(target) <= maximum {
				return item, int(maximum-minimum) + 1, true
			}
		}
		if len(numbers) == 1 &&
			(strings.Contains(code, "/ge_") ||
				strings.Contains(code, "/gte_") ||
				strings.Contains(name, "以上")) &&
			numbers[0] <= float64(target) {
			return item, target - int(numbers[0]) + 100, true
		}
	case "gte":
		if len(numbers) == 1 &&
			(strings.Contains(code, "/ge_") ||
				strings.Contains(code, "/gte_") ||
				strings.Contains(name, "以上")) &&
			numbers[0] < float64(target) {
			return item, target - int(numbers[0]), true
		}
	}
	return indexedMenuItem{}, 0, false
}

func compileNamedMenu(plan *FilterPlan, requirement Requirement, index menuIndex, aliases map[string]string) {
	if requirement.Importance == "soft" {
		switch requirement.Facet {
		case "energy_type", "transmission":
			plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "enum_rank_not_confirmed", "Guide 枚举尚未确认，不能可靠进行本地排序"))
		default:
			plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "vehicle_type_rank_not_supported", "当前车辆类别只支持筛选，不支持可靠排序"))
		}
		return
	}
	if requirement.Operator != "eq" && requirement.Operator != "in" {
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnsupported, "negative_menu_filter_not_supported", "Guide 当前菜单不支持该比较方式"))
		return
	}
	target := requirement.CanonicalValue
	if alias := aliases[normalize(target)]; alias != "" {
		target = alias
	}
	item, ok := index.find(requirement.Facet, target)
	if !ok {
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "menu_item_not_found", "当前无筛选菜单中没有对应选项"))
		return
	}
	addMenuFilter(plan, requirement, item)
}

func compileCarAge(plan *FilterPlan, requirement Requirement, index menuIndex) {
	if requirement.Importance == "soft" || requirement.CanonicalValue == "newer" {
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "car_age_rank_not_supported", "Guide 当前没有返回可比较的车辆车龄字段"))
		return
	}
	value, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(requirement.CanonicalValue), "年"))
	if err != nil || value < 1 || value > 3 || (requirement.Operator != "eq" && requirement.Operator != "lte") {
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnsupported, "car_age_not_supported", "当前菜单只支持半年、一年、两年或三年车龄"))
		return
	}
	name := map[int]string{1: "一年新车", 2: "两年车龄", 3: "三年车龄"}[value]
	if item, ok := index.find("car_age", name); ok {
		addMenuFilter(plan, requirement, item)
		return
	}
	plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "menu_item_not_found", "当前无筛选菜单中没有对应车龄选项"))
}

func compilePrice(plan *FilterPlan, requirement Requirement, index menuIndex) {
	value := strings.ToLower(strings.ReplaceAll(requirement.CanonicalValue, " ", ""))
	if requirement.Importance == "soft" && (value == "lower" || value == "便宜") {
		if code := index.sort[normalize("总价最低")]; code != "" {
			plan.ServerSort = code
			plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityRankable, "server_sort", "使用 Guide 总价最低排序"))
			return
		}
		plan.RankFactors = append(plan.RankFactors, RankFactor{RequirementID: requirement.ID, Type: RankPriceLow, Weight: 1, DataField: "total_charge.total_amount"})
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityRankable, "quote_rank", "按 Guide 返回的总价在当前候选集中排序"))
		return
	}
	if requirement.Importance == "hard" {
		if item, constraint, prefilter, ok := index.findPrice(requirement); ok {
			addMenuFilterWithMode(plan, requirement, item, prefilter)
			plan.LocalVerifiers = append(plan.LocalVerifiers, priceVerifier(requirement.ID, constraint))
			return
		}
	}
	plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "price_range_not_supported", "当前 Guide 价格菜单无法无损表达该预算范围"))
}

type priceConstraint struct {
	scope    string
	operator string
	min      *float64
	max      *float64
}

func (i menuIndex) findPrice(requirement Requirement) (indexedMenuItem, priceConstraint, bool, bool) {
	constraint, ok := priceConstraintFromRequirement(requirement)
	if !ok {
		return indexedMenuItem{}, priceConstraint{}, false, false
	}
	for _, item := range i.byFacet["price_preference"] {
		if priceItemMatches(item, constraint) {
			return item, constraint, false, true
		}
	}
	if item, ok := i.findPricePrefilter(constraint); ok {
		return item, constraint, true, true
	}
	return indexedMenuItem{}, priceConstraint{}, false, false
}

func (i menuIndex) findPricePrefilter(constraint priceConstraint) (indexedMenuItem, bool) {
	var selected indexedMenuItem
	bestDistance := math.MaxFloat64
	for _, item := range i.byFacet["price_preference"] {
		code := strings.ToLower(item.code)
		name := strings.ToLower(item.name)
		if constraint.scope == "total" && !strings.HasPrefix(code, "filter/total_fee/") {
			continue
		}
		if constraint.scope == "daily" && !strings.HasPrefix(code, "filter/price/") {
			continue
		}
		numbers := distinctNumbers(code + " " + name)
		if len(numbers) != 1 {
			continue
		}
		boundary := numbers[0]
		switch constraint.operator {
		case "lt", "lte":
			if constraint.max == nil || boundary < *constraint.max ||
				(!strings.Contains(code, "/le_") &&
					!strings.Contains(code, "/lte_") &&
					!strings.Contains(name, "以下") &&
					!strings.Contains(name, "以内")) {
				continue
			}
			if distance := boundary - *constraint.max; distance < bestDistance {
				selected = item
				bestDistance = distance
			}
		case "gt", "gte":
			if constraint.min == nil || boundary > *constraint.min ||
				(!strings.Contains(code, "/ge_") &&
					!strings.Contains(code, "/gte_") &&
					!strings.Contains(name, "以上")) {
				continue
			}
			if distance := *constraint.min - boundary; distance < bestDistance {
				selected = item
				bestDistance = distance
			}
		}
	}
	return selected, selected.code != ""
}

func priceVerifier(requirementID string, constraint priceConstraint) LocalVerifier {
	facet := "price_total"
	if constraint.scope == "daily" {
		facet = "price_daily"
	}
	verifier := LocalVerifier{
		RequirementID: requirementID,
		Facet:         facet,
		Operator:      constraint.operator,
	}
	if constraint.min != nil {
		verifier.MinValue = strconv.FormatFloat(*constraint.min, 'f', -1, 64)
	}
	if constraint.max != nil {
		verifier.MaxValue = strconv.FormatFloat(*constraint.max, 'f', -1, 64)
	}
	switch constraint.operator {
	case "lt", "lte", "eq":
		verifier.Value = verifier.MaxValue
	case "gt", "gte":
		verifier.Value = verifier.MinValue
	}
	return verifier
}

func priceConstraintFromRequirement(value Requirement) (priceConstraint, bool) {
	unit := strings.ToLower(strings.TrimSpace(value.Value.Unit))
	scope := "total"
	if unit == "daily_cny" || strings.HasPrefix(strings.ToLower(value.CanonicalValue), "daily") {
		scope = "daily"
	}
	constraint := priceConstraint{scope: scope, operator: value.Operator}
	switch value.Value.Kind {
	case "number":
		if value.Value.Number == nil || !isNumericOperator(value.Operator) {
			return priceConstraint{}, false
		}
		number := *value.Value.Number
		switch value.Operator {
		case "lt", "lte", "eq":
			constraint.max = &number
		case "gt", "gte":
			constraint.min = &number
		default:
			return priceConstraint{}, false
		}
		return constraint, true
	case "range":
		if value.Value.Range == nil {
			return priceConstraint{}, false
		}
		constraint.operator = "range"
		constraint.min = value.Value.Range.Min
		constraint.max = value.Value.Range.Max
		return constraint, constraint.min != nil || constraint.max != nil
	}

	numbers := numericParts(value.CanonicalValue)
	if len(numbers) != 1 {
		return priceConstraint{}, false
	}
	number := numbers[0]
	switch {
	case strings.Contains(value.CanonicalValue, "<="), strings.Contains(value.CanonicalValue, "≤"):
		constraint.operator = "lte"
		constraint.max = &number
	case strings.Contains(value.CanonicalValue, ">="), strings.Contains(value.CanonicalValue, "≥"):
		constraint.operator = "gte"
		constraint.min = &number
	default:
		return priceConstraint{}, false
	}
	return constraint, true
}

func priceItemMatches(item indexedMenuItem, constraint priceConstraint) bool {
	code := strings.ToLower(item.code)
	name := strings.ToLower(item.name)
	if constraint.scope == "total" && !strings.HasPrefix(code, "filter/total_fee/") {
		return false
	}
	if constraint.scope == "daily" && !strings.HasPrefix(code, "filter/price/") {
		return false
	}
	numbers := numericParts(code + " " + name)
	switch constraint.operator {
	case "lte":
		return constraint.max != nil &&
			containsNumber(numbers, *constraint.max) &&
			(strings.Contains(code, "/le_") ||
				strings.Contains(code, "/lte_") ||
				strings.Contains(name, "以下") ||
				strings.Contains(name, "以内"))
	case "gte":
		return constraint.min != nil &&
			containsNumber(numbers, *constraint.min) &&
			(strings.Contains(code, "/ge_") ||
				strings.Contains(code, "/gte_") ||
				strings.Contains(name, "以上"))
	case "eq":
		return constraint.max != nil &&
			containsNumber(numbers, *constraint.max) &&
			!strings.Contains(name, "以下") &&
			!strings.Contains(name, "以内") &&
			!strings.Contains(name, "以上") &&
			len(numbers) <= 2
	case "range":
		if constraint.min == nil || constraint.max == nil {
			return false
		}
		return containsNumber(numbers, *constraint.min) &&
			containsNumber(numbers, *constraint.max) &&
			(strings.Contains(code, "_") ||
				strings.Contains(name, "-") ||
				strings.Contains(name, "至"))
	default:
		return false
	}
}

var numberPattern = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

func numericParts(value string) []float64 {
	matches := numberPattern.FindAllString(value, -1)
	result := make([]float64, 0, len(matches))
	for _, match := range matches {
		number, err := strconv.ParseFloat(match, 64)
		if err == nil {
			result = append(result, number)
		}
	}
	return result
}

func containsNumber(values []float64, target float64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func distinctNumbers(value string) []float64 {
	seen := make(map[float64]struct{})
	var result []float64
	for _, number := range numericParts(value) {
		if _, exists := seen[number]; exists {
			continue
		}
		seen[number] = struct{}{}
		result = append(result, number)
	}
	sort.Float64s(result)
	return result
}

func hasMultipleDistinctNumbers(value string) bool {
	return len(distinctNumbers(value)) > 1
}

func compileVehicleEntity(plan *FilterPlan, requirement Requirement, entities *vehiclecatalog.StaticCatalog) {
	if requirement.Importance == "soft" {
		value := strings.TrimSpace(requirement.CanonicalValue)
		if value == "" {
			plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "vehicle_entity_empty", "车辆实体名称为空"))
			return
		}
		factorType := RankPreferredModel
		field := "vehicle.vehicle_name"
		if requirement.Facet == "brand" {
			factorType = RankPreferredBrand
			field = "vehicle.brand_name"
		}
		plan.RankFactors = append(plan.RankFactors, RankFactor{RequirementID: requirement.ID, Type: factorType, Value: value, Weight: 1, DataField: field})
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityRankable, "quote_rank", "使用 Guide 返回的车辆名称字段在当前候选集中排序"))
		return
	}
	if requirement.Operator != "eq" && requirement.Operator != "in" {
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnsupported, "vehicle_entity_operator_not_supported", "Guide 车辆实体筛选当前只支持正向等值条件"))
		return
	}
	if strings.TrimSpace(requirement.EntityID) == "" {
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "vehicle_entity_not_cataloged", "车辆实体未经过车型库确认，不能生成 Guide FilterCode"))
		return
	}
	entity, ok := entities.EntityByID(requirement.EntityID)
	if !ok || entity.Type != catalogTypeForFacet(requirement.Facet) {
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "vehicle_entity_catalog_mismatch", "车型库中不存在与诉求类型一致的车辆实体"))
		return
	}

	value := strings.TrimSpace(entity.CanonicalName)
	brandName := catalogEntityName(entities, entity.BrandID)
	expectedNames := []string{value}
	if requirement.Facet == "vehicle_series" {
		expectedNames = nil
		filterCount := len(plan.MenuFilters)
		models := entities.ModelsBySeries(entity.ID)
		if len(models) == 0 {
			plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "series_expansion_empty", "车型库中没有可用于 Guide 筛选的车系车型"))
			return
		}
		for _, model := range models {
			providerNames := entities.ProviderNames(model.ID, "guide")
			for _, providerName := range providerNames {
				plan.MenuFilters = append(plan.MenuFilters, MenuFilter{
					RequirementID: requirement.ID,
					Code:          "filter/vehicle_name/" + providerName,
					Name:          providerName,
				})
				expectedNames = append(expectedNames, model.CanonicalName, providerName)
			}
		}
		if len(plan.MenuFilters) == filterCount {
			plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "series_provider_binding_empty", "车型库车系下没有经过确认的 Guide Provider Binding"))
			return
		}
	} else {
		providerNames := entities.ProviderNames(entity.ID, "guide")
		if len(providerNames) == 0 {
			plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "vehicle_provider_binding_empty", "车型库实体没有经过确认的 Guide Provider Binding"))
			return
		}
		codePrefix := "filter/brand/"
		if requirement.Facet == "vehicle_model" {
			codePrefix = "filter/vehicle_name/"
		}
		for _, providerName := range providerNames {
			plan.MenuFilters = append(plan.MenuFilters, MenuFilter{
				RequirementID: requirement.ID,
				Code:          codePrefix + providerName,
				Name:          providerName,
			})
			expectedNames = append(expectedNames, providerName)
		}
	}
	plan.Resolutions = append(plan.Resolutions, resolution(
		requirement,
		CapabilityFilterable,
		"vehicle_catalog_filter",
		"车辆实体已由车型库归一并映射为 Guide FilterCode，返回结果将进行本地实体校验",
	))

	plan.LocalVerifiers = append(plan.LocalVerifiers, LocalVerifier{
		RequirementID: requirement.ID,
		Facet:         requirement.Facet,
		EntityID:      entity.ID,
		ExpectedBrand: brandName,
		ExpectedNames: uniqueStrings(expectedNames),
	})
}

func catalogTypeForFacet(facet string) vehiclecatalog.EntityType {
	switch facet {
	case "brand":
		return vehiclecatalog.EntityBrand
	case "vehicle_series":
		return vehiclecatalog.EntitySeries
	case "vehicle_model":
		return vehiclecatalog.EntityModel
	default:
		return ""
	}
}

func catalogEntityName(entities *vehiclecatalog.StaticCatalog, entityID string) string {
	if entities == nil {
		return ""
	}
	entity, ok := entities.EntityByID(entityID)
	if !ok {
		return ""
	}
	return strings.TrimSpace(entity.CanonicalName)
}

func qualifyVehicleName(name, brand string) string {
	name = strings.TrimSpace(name)
	brand = strings.TrimSpace(brand)
	if brand == "" || strings.Contains(normalizeQuoteText(name), normalizeQuoteText(brand)) {
		return name
	}
	return brand + name
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := normalizeQuoteText(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func addMenuFilter(plan *FilterPlan, requirement Requirement, item indexedMenuItem) {
	addMenuFilterWithMode(plan, requirement, item, false)
}

func addMenuFilterWithMode(plan *FilterPlan, requirement Requirement, item indexedMenuItem, prefilter bool) {
	plan.MenuFilters = append(plan.MenuFilters, MenuFilter{
		RequirementID: requirement.ID,
		Code:          item.code,
		Name:          item.name,
		Prefilter:     prefilter,
	})
	reasonCode := "menu_filter"
	reason := "已匹配当前无筛选菜单"
	if prefilter {
		reasonCode = "menu_prefilter"
		reason = "已使用不漏召回的 Guide 条件预筛，并将按真实返回字段严格验证"
	}
	plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityFilterable, reasonCode, reason))
}

func resolution(requirement Requirement, capability Capability, reasonCode, reason string) Resolution {
	status := string(capability)
	return Resolution{
		RequirementID: requirement.ID,
		RawText:       requirement.RawText,
		Importance:    requirement.Importance,
		Capability:    capability,
		Status:        status,
		ReasonCode:    reasonCode,
		Reason:        reason,
	}
}

func (i menuIndex) find(facet, name string) (indexedMenuItem, bool) {
	target := normalize(name)
	for _, item := range i.byFacet[facet] {
		if normalize(item.name) == target {
			return item, true
		}
	}
	return indexedMenuItem{}, false
}

func redundantParents(requirements []Requirement) map[string]struct{} {
	result := make(map[string]struct{})
	brandIDs := make(map[string][]Requirement)
	seriesIDs := make(map[string][]Requirement)
	for _, requirement := range requirements {
		switch requirement.Facet {
		case "brand":
			if requirement.EntityID != "" {
				brandIDs[requirement.EntityID] = append(brandIDs[requirement.EntityID], requirement)
			}
		case "vehicle_series":
			if requirement.EntityID != "" {
				seriesIDs[requirement.EntityID] = append(seriesIDs[requirement.EntityID], requirement)
			}
		}
	}
	for _, requirement := range requirements {
		if requirement.Operator != "eq" {
			continue
		}
		if requirement.Facet == "vehicle_model" {
			for _, parent := range brandIDs[requirement.EntityBrandID] {
				if parent.Operator == "eq" {
					result[parent.ID] = struct{}{}
				}
			}
			for _, parent := range seriesIDs[requirement.EntityParentID] {
				if parent.Operator == "eq" {
					result[parent.ID] = struct{}{}
				}
			}
		}
		if requirement.Facet == "vehicle_series" {
			for _, parent := range brandIDs[requirement.EntityBrandID] {
				if parent.Operator == "eq" {
					result[parent.ID] = struct{}{}
				}
			}
		}
	}
	return result
}

func uniqueMenuFilters(values []MenuFilter) []MenuFilter {
	seen := make(map[string]struct{})
	result := make([]MenuFilter, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value.Code]; exists {
			continue
		}
		seen[value.Code] = struct{}{}
		result = append(result, value)
	}
	return result
}

func planHash(plan FilterPlan) string {
	type hashInput struct {
		MenuFilters           []MenuFilter
		ServerSort            string
		QuoteFilters          []QuoteFilter
		LocalVerifiers        []LocalVerifier
		RankFactors           []RankFactor
		ExploratoryRanks      []ExploratoryRank
		RelaxedRequirementIDs []string
	}
	input := hashInput{
		MenuFilters:           append([]MenuFilter(nil), plan.MenuFilters...),
		ServerSort:            plan.ServerSort,
		QuoteFilters:          append([]QuoteFilter(nil), plan.QuoteFilters...),
		LocalVerifiers:        append([]LocalVerifier(nil), plan.LocalVerifiers...),
		RankFactors:           append([]RankFactor(nil), plan.RankFactors...),
		ExploratoryRanks:      append([]ExploratoryRank(nil), plan.ExploratoryRanks...),
		RelaxedRequirementIDs: append([]string(nil), plan.RelaxedRequirementIDs...),
	}
	sort.Slice(input.MenuFilters, func(i, j int) bool { return input.MenuFilters[i].Code < input.MenuFilters[j].Code })
	sort.Slice(input.QuoteFilters, func(i, j int) bool {
		if input.QuoteFilters[i].RequirementID == input.QuoteFilters[j].RequirementID {
			return input.QuoteFilters[i].Facet < input.QuoteFilters[j].Facet
		}
		return input.QuoteFilters[i].RequirementID < input.QuoteFilters[j].RequirementID
	})
	sort.Slice(input.LocalVerifiers, func(i, j int) bool {
		return input.LocalVerifiers[i].RequirementID < input.LocalVerifiers[j].RequirementID
	})
	sort.Strings(input.RelaxedRequirementIDs)
	data, _ := json.Marshal(input)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:12])
}

func normalize(value string) string {
	replacer := strings.NewReplacer(" ", "", "车型", "", "车", "", "挡", "")
	return replacer.Replace(strings.ToLower(strings.TrimSpace(value)))
}

func isNumericOperator(operator string) bool {
	switch operator {
	case "eq", "not_eq", "gt", "gte", "lt", "lte":
		return true
	default:
		return false
	}
}

var vehicleTypeAliases = map[string]string{
	normalize("越野车"): "SUV",
}

var energyAliases = map[string]string{
	normalize("纯电"):    "纯电动",
	normalize("电车"):    "纯电动",
	normalize("油电混合"):  "油电混合",
	normalize("混动"):    "油电混合",
	normalize("汽油"):    "汽油",
	normalize("燃油"):    "汽油",
	normalize("插混"):    "插电式",
	normalize("插电式混动"): "插电式",
	normalize("增程"):    "增程式",
}

var transmissionAliases = map[string]string{
	normalize("自动挡"): "自动",
	normalize("自动波"): "自动",
	normalize("手动挡"): "手动",
}
