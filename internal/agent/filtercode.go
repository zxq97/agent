package agent

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/zxq97/rental-agent/internal/types"
)

// staticNeedMap 需求值 → filter_code 静态映射表。
// 对齐 tyche 的 valueToItemCode + attribute_resolver。
// key 格式:"type:value"(小写归一化)。
var staticNeedMap = map[string]string{
	// vehicle_type 车型大类
	"vehicle_type:suv": "filter/vehcle_choice/suv",
	"vehicle_type:轿车":  "filter/vehcle_choice/jiaoche",
	"vehicle_type:商务车": "filter/vehcle_choice/shangwu",
	"vehicle_type:mpv": "filter/vehcle_choice/shangwu",
	"vehicle_type:经济型": "filter/vehcle_choice/jingji",
	"vehicle_type:舒适型": "filter/vehcle_choice/shushi",
	"vehicle_type:豪华型": "filter/vehcle_choice/haohua",
	"vehicle_type:跑车":  "filter/vehcle_choice/paoche",

	// energy_type 能源类型
	"energy_type:纯电":  "filter/fuel/electric",
	"energy_type:电车":  "filter/fuel/electric",
	"energy_type:电动":  "filter/fuel/electric",
	"energy_type:混动":  "filter/fuel/hybrid",
	"energy_type:插混":  "filter/fuel/phev",
	"energy_type:汽油":  "filter/fuel/gasoline",
	"energy_type:燃油":  "filter/fuel/gasoline",
	"energy_type:油车":  "filter/fuel/gasoline",
	"energy_type:柴油":  "filter/fuel/diesel",
	"energy_type:增程":  "filter/fuel/reev",
	"energy_type:新能源": "filter/fuel/new_energy",

	// transmission 变速箱
	"transmission:自动挡": "filter/transmission/auto",
	"transmission:自动":  "filter/transmission/auto",
	"transmission:手动挡": "filter/transmission/manual",
	"transmission:手动":  "filter/transmission/manual",

	// car_age 车龄
	"car_age:新车":   "filter/car_age/new",
	"car_age:半年":   "filter/car_age/half_year",
	"car_age:一年内":  "filter/car_age/one_year",
	"car_age:一年":   "filter/car_age/one_year",
	"car_age:车新":   "filter/car_age/one_year",
	"car_age:车况好":  "filter/car_age/one_year",
	"car_age:两年内":  "filter/car_age/two_year",
	"car_age:三年以上": "filter/car_age/three_year_above",
}

var menuEntityAliases = map[string]string{
	normalizeMenuEntity("tesla"):     normalizeMenuEntity("特斯拉"),
	normalizeMenuEntity("byd"):       normalizeMenuEntity("比亚迪"),
	normalizeMenuEntity("毛豆3"):       normalizeMenuEntity("Model 3"),
	normalizeMenuEntity("毛豆y"):       normalizeMenuEntity("Model Y"),
	normalizeMenuEntity("毛豆Y"):       normalizeMenuEntity("Model Y"),
	normalizeMenuEntity("特斯拉model3"): normalizeMenuEntity("Model 3"),
	normalizeMenuEntity("特斯拉modely"): normalizeMenuEntity("Model Y"),
}

// seatThreshold 座位需求 ≥ 此值时才转 filter_code。
// 原因:≤5 座是绝大多数车型默认,不加 filter 反而搜得更宽。
// 只有 ≥6 座或用户明确要"N座车"时才按座位档筛。
const seatThreshold = 6

// StaticRecall 把活跃 needs 映射成 filter_codes(确定性,0 延迟 0 token)。
// 返回命中的 codes 和未覆盖的 needs。
// 有 menu 时做白名单校验(生成的 code 必须在 menu 里存在);menu 为空时不校验。
func StaticRecall(needs []types.UserNeed, menu []types.MenuGroupView) (codes []string, uncovered []types.UserNeed) {
	menuSet := buildMenuSet(menu)

	for _, n := range needs {
		if n.Negative {
			// 排除条件不转 filter_code(filter_code 只做正向筛选)
			continue
		}
		code := lookupFilterCodeWithMenu(n, menu)
		if code == "" {
			uncovered = append(uncovered, n)
			continue
		}
		// 白名单校验:有 menu 时,生成的 code 必须存在
		if len(menuSet) > 0 {
			if _, ok := menuSet[code]; !ok {
				uncovered = append(uncovered, n)
				continue
			}
		}
		codes = append(codes, code)
	}
	return
}

func lookupFilterCodeWithMenu(n types.UserNeed, menu []types.MenuGroupView) string {
	if isMenuEntityNeed(n.Type) {
		if code := menuEntityToFilterCode(n.Type, needValueString(n.Value), menu); code != "" {
			return code
		}
	}
	if n.Type == "price_preference" {
		if code := budgetToFilterCode(needValueString(n.Value), menu); code != "" {
			return code
		}
	}
	return lookupFilterCode(n)
}

func isMenuEntityNeed(needType string) bool {
	switch needType {
	case "brand", "vehicle_model", "vehicle_series":
		return true
	default:
		return false
	}
}

func menuEntityToFilterCode(needType, val string, menu []types.MenuGroupView) string {
	candidates := menuEntityCandidates(val)
	if len(candidates) == 0 || len(menu) == 0 {
		return ""
	}

	for _, group := range menu {
		if !menuGroupMatchesEntityNeed(group, needType) {
			continue
		}
		if code := matchMenuItemsByName(group.Items, candidates); code != "" {
			return code
		}
	}
	return ""
}

func menuEntityCandidates(val string) map[string]struct{} {
	normalized := normalizeMenuEntity(val)
	if normalized == "" {
		return nil
	}
	candidates := map[string]struct{}{normalized: {}}
	if alias, ok := menuEntityAliases[normalized]; ok {
		candidates[alias] = struct{}{}
	}
	return candidates
}

func menuGroupMatchesEntityNeed(group types.MenuGroupView, needType string) bool {
	code := normalizeMenuEntity(group.GroupCode)
	name := normalizeMenuEntity(group.GroupName)
	switch needType {
	case "brand":
		return strings.Contains(code, "brand") || strings.Contains(name, "品牌")
	case "vehicle_model":
		return strings.Contains(code, "model") || strings.Contains(code, "vehiclemodel") ||
			strings.Contains(name, "车型") || strings.Contains(name, "车款")
	case "vehicle_series":
		return strings.Contains(code, "series") || strings.Contains(code, "vehicleseries") ||
			strings.Contains(name, "车系")
	default:
		return false
	}
}

func matchMenuItemsByName(items []types.MenuItemView, candidates map[string]struct{}) string {
	for _, item := range items {
		if _, ok := candidates[normalizeMenuEntity(item.Name)]; ok {
			return item.ItemCode
		}
	}
	return ""
}

func normalizeMenuEntity(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// lookupFilterCode 根据单条 need 查静态映射表。
func lookupFilterCode(n types.UserNeed) string {
	val := needValueString(n.Value)
	if val == "" {
		return ""
	}

	// 座位特例
	if n.Type == "seat_num" {
		return seatToFilterCode(val)
	}

	// 查映射表(小写归一化)
	key := n.Type + ":" + strings.ToLower(val)
	if code, ok := staticNeedMap[key]; ok {
		return code
	}
	return ""
}

func budgetToFilterCode(val string, menu []types.MenuGroupView) string {
	items := budgetMenuItems(menu)
	if len(items) == 0 {
		return ""
	}
	want := parseIntFromValue(val)
	if want > 0 {
		for _, item := range items {
			if itemMatchesBudgetLimit(item, want) {
				return item.ItemCode
			}
		}
	}
	lower := strings.ToLower(val)
	if strings.Contains(lower, "低") || strings.Contains(lower, "便宜") || strings.Contains(lower, "省") {
		return items[0].ItemCode
	}
	if strings.Contains(lower, "高") || strings.Contains(lower, "贵") || strings.Contains(lower, "好") {
		return items[len(items)-1].ItemCode
	}
	return ""
}

func budgetMenuItems(menu []types.MenuGroupView) []types.MenuItemView {
	for _, group := range menu {
		if strings.Contains(group.GroupCode, "total_fee") {
			return group.Items
		}
		for _, item := range group.Items {
			if strings.Contains(item.ItemCode, "total_fee") {
				return group.Items
			}
		}
	}
	return nil
}

func itemMatchesBudgetLimit(item types.MenuItemView, want int) bool {
	code := strings.ToLower(item.ItemCode)
	name := strings.ToLower(item.Name)
	if strings.Contains(code, "le_"+strconv.Itoa(want)) || strings.Contains(code, "lt_"+strconv.Itoa(want)) {
		return true
	}
	if strings.Contains(name, strconv.Itoa(want)) && (strings.Contains(name, "以下") || strings.Contains(name, "以内") || strings.Contains(name, "内")) {
		return true
	}
	return false
}

// seatToFilterCode 座位数 → filter_code。
// ≤5 不转(避免"2人"误筛成 2 座跑车);≥6 按座位档选。
func seatToFilterCode(val string) string {
	num := parseIntFromValue(val)
	if num < seatThreshold {
		return "" // 不转,宽松搜索
	}
	if num >= 8 {
		return "filter/seat_num/ge_8"
	}
	// 6-7 座
	return "filter/seat_num/6_7"
}

// buildMenuSet 把 menu_group 所有 item_code 收集成 set,供白名单校验。
func buildMenuSet(menu []types.MenuGroupView) map[string]struct{} {
	if len(menu) == 0 {
		return nil
	}
	set := make(map[string]struct{})
	for _, g := range menu {
		for _, item := range g.Items {
			set[item.ItemCode] = struct{}{}
		}
	}
	return set
}

// needValueString 把 UserNeed.Value (interface{}) 转成字符串。
func needValueString(v interface{}) string {
	switch s := v.(type) {
	case string:
		return s
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	case int:
		return strconv.Itoa(s)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// parseIntFromValue 从需求值中解析数字(如"7座"→7, "5"→5)。
func parseIntFromValue(val string) int {
	// 先尝试直接解析
	if n, err := strconv.Atoi(val); err == nil {
		return n
	}
	// 提取首个数字序列("7座"→"7")
	var digits []byte
	for _, b := range []byte(val) {
		if b >= '0' && b <= '9' {
			digits = append(digits, b)
		} else if len(digits) > 0 {
			break
		}
	}
	if len(digits) > 0 {
		if n, err := strconv.Atoi(string(digits)); err == nil {
			return n
		}
	}
	return 0
}
