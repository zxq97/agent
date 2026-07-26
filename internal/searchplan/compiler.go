package searchplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/zxq97/agent/api/guide"
)

type Compiler struct{}

func NewCompiler() *Compiler {
	return &Compiler{}
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
		compileRequirement(&plan, requirement, index)
	}
	plan.MenuFilters = uniqueMenuFilters(plan.MenuFilters)
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
	default:
		return ""
	}
}

func compileRequirement(plan *FilterPlan, requirement Requirement, index menuIndex) {
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
		compileVehicleEntity(plan, requirement)
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
	if requirement.Operator == "eq" {
		name := strconv.Itoa(value) + "座"
		if item, ok := index.find("seat_num", name); ok {
			addMenuFilter(plan, requirement, item)
			return
		}
	}
	if requirement.Operator == "gte" && value >= 8 {
		if item, ok := index.find("seat_num", "8座及以上"); ok {
			addMenuFilter(plan, requirement, item)
			return
		}
	}
	if isNumericOperator(requirement.Operator) {
		plan.QuoteFilters = append(plan.QuoteFilters, QuoteFilter{RequirementID: requirement.ID, Facet: requirement.Facet, Operator: requirement.Operator, Value: strconv.Itoa(value)})
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityVerifiable, "quote_filter", "使用 Guide 返回的座位数字段验证当前候选集"))
		return
	}
	plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnsupported, "seat_operator_not_supported", "当前无法执行该座位数比较方式"))
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
	targetName := ""
	switch value {
	case "total<=300cny":
		targetName = "￥300以下"
	case "daily<=100cny":
		targetName = "￥100以下"
	}
	if targetName != "" {
		if item, ok := index.find("price_preference", targetName); ok {
			addMenuFilter(plan, requirement, item)
			return
		}
	}
	plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "price_range_not_supported", "当前菜单无法无损表达该预算范围"))
}

func compileVehicleEntity(plan *FilterPlan, requirement Requirement) {
	value := strings.TrimSpace(requirement.CanonicalValue)
	if value == "" {
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnverifiable, "vehicle_entity_empty", "车辆实体名称为空"))
		return
	}
	if requirement.Importance == "soft" {
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
	operator := requirement.Operator
	if operator != "eq" && operator != "not_eq" && operator != "contains" {
		plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityUnsupported, "vehicle_entity_operator_not_supported", "当前车辆字段不支持该比较方式"))
		return
	}
	plan.QuoteFilters = append(plan.QuoteFilters, QuoteFilter{RequirementID: requirement.ID, Facet: requirement.Facet, Operator: operator, Value: value})
	plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityVerifiable, "fetched_set_quote_filter", "Guide 没有品牌/车型菜单，使用返回车辆字段验证当前候选集"))
}

func addMenuFilter(plan *FilterPlan, requirement Requirement, item indexedMenuItem) {
	plan.MenuFilters = append(plan.MenuFilters, MenuFilter{RequirementID: requirement.ID, Code: item.code, Name: item.name})
	plan.Resolutions = append(plan.Resolutions, resolution(requirement, CapabilityFilterable, "menu_filter", "已匹配当前无筛选菜单"))
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
		MenuFilters  []MenuFilter
		ServerSort   string
		QuoteFilters []QuoteFilter
		RankFactors  []RankFactor
	}
	input := hashInput{
		MenuFilters:  append([]MenuFilter(nil), plan.MenuFilters...),
		ServerSort:   plan.ServerSort,
		QuoteFilters: append([]QuoteFilter(nil), plan.QuoteFilters...),
		RankFactors:  append([]RankFactor(nil), plan.RankFactors...),
	}
	sort.Slice(input.MenuFilters, func(i, j int) bool { return input.MenuFilters[i].Code < input.MenuFilters[j].Code })
	sort.Slice(input.QuoteFilters, func(i, j int) bool {
		if input.QuoteFilters[i].RequirementID == input.QuoteFilters[j].RequirementID {
			return input.QuoteFilters[i].Facet < input.QuoteFilters[j].Facet
		}
		return input.QuoteFilters[i].RequirementID < input.QuoteFilters[j].RequirementID
	})
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
