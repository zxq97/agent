package rentalrules

import "strings"

const DefaultCatalogVersion = "rental-rule-guidance-v1"

type Catalog interface {
	Search(string) []Rule
	Version() string
}

type StaticCatalog struct {
	version string
	rules   []catalogRule
}

type catalogRule struct {
	Rule
	Keywords []string
}

func NewDefaultCatalog() *StaticCatalog {
	return &StaticCatalog{
		version: DefaultCatalogVersion,
		rules: []catalogRule{
			{Rule: guidanceRule("documents", "证件要求", "取车证件由国家/地区、供应商和门店决定；下单前需要核对订单页列出的身份证明、驾驶证及可能要求的翻译件或国际驾照。"), Keywords: []string{"证件", "身份证", "驾驶证", "驾照", "国际驾照", "翻译件"}},
			{Rule: guidanceRule("driver_qualification", "驾驶人资格", "最低年龄、最高年龄和驾龄门槛因供应商、门店及车型而异；当前系统没有供应商资格规则接口，不能给出统一年龄或驾龄结论。"), Keywords: []string{"年龄", "驾龄", "新手", "驾驶人资格", "多少岁"}},
			{Rule: guidanceRule("deposit", "押金与预授权", "车辆押金、违章押金、信用卡预授权及退还时间由供应商和订单决定；应以当前报价的费用明细与取车条款为准。"), Keywords: []string{"押金", "预授权", "信用卡", "免押", "违章押金"}},
			{Rule: guidanceRule("cancellation", "取消与改期", "免费取消时间、取消费和是否允许改期属于订单级规则；当前系统未接入订单取消条款，需在提交订单前查看确认页。"), Keywords: []string{"取消", "退款", "退订", "改期", "修改订单"}},
			{Rule: guidanceRule("mileage", "里程限制", "是否不限里程、超里程计费标准和适用区域由具体报价决定；不能从车型名称推断里程规则。"), Keywords: []string{"里程", "限公里", "不限里程", "超里程"}},
			{Rule: guidanceRule("fuel_charge", "燃油与充电", "满油取还、同油位取还、充电电量要求及服务费由供应商和车型能源类型决定；取车时应记录油量或电量并核对订单条款。"), Keywords: []string{"油费", "加油", "油量", "充电", "电量", "满油"}},
			{Rule: guidanceRule("late_return", "超时与续租", "宽限时间、超时计费和续租方式由门店及订单决定；需要续租时应在原还车时间前联系供应商确认。"), Keywords: []string{"超时", "晚还", "续租", "延期", "宽限"}},
			{Rule: guidanceRule("usage_area", "使用区域与异地还车", "跨城、跨省、跨境、轮渡及异地还车限制由供应商和门店网络决定；必须在下单前确认允许的使用区域和可能产生的附加费。"), Keywords: []string{"异地还车", "跨城", "跨省", "跨境", "出境", "轮渡", "使用区域"}},
			{Rule: guidanceRule("additional_driver", "附加驾驶人", "附加驾驶人是否允许、需要哪些证件及是否收费由订单规则决定；未登记的驾驶人可能不受订单保障。"), Keywords: []string{"附加驾驶人", "第二驾驶人", "多人开", "换着开", "代驾"}},
			{Rule: guidanceRule("protection", "保障与责任", "保障项目、免赔额和不承保情形必须以具体保障条款为准；车型或价格不能证明保障范围。"), Keywords: []string{"保险", "保障", "免赔", "事故", "车损", "责任"}},
		},
	}
}

func guidanceRule(id, title, guidance string) Rule {
	return Rule{
		ID: id, Category: id, Title: title, Guidance: guidance,
		Scope: "general_guidance", Source: "订单页、供应商和门店最终条款",
		VerificationRequired: true,
	}
}

func (c *StaticCatalog) Version() string {
	return c.version
}

func (c *StaticCatalog) Search(query string) []Rule {
	query = normalizeRuleQuery(query)
	var result []Rule
	for _, rule := range c.rules {
		for _, keyword := range rule.Keywords {
			if strings.Contains(query, normalizeRuleQuery(keyword)) {
				result = append(result, rule.Rule)
				break
			}
		}
	}
	if len(result) == 0 && isOverviewQuery(query) {
		for _, rule := range c.rules {
			result = append(result, rule.Rule)
		}
	}
	return result
}

func normalizeRuleQuery(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

func isOverviewQuery(value string) bool {
	for _, keyword := range []string{"租车规则", "租车须知", "注意事项", "有哪些规则", "取车规则"} {
		if strings.Contains(value, normalizeRuleQuery(keyword)) {
			return true
		}
	}
	return false
}
