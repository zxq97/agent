package capability

import (
	"strings"

	"github.com/zxq97/agent/internal/requirement"
)

const DefaultCatalogVersion = "capability-v2"

type ExecutionDefinition struct {
	RequiredFields []string
	RequiredMenus  []string
	Operation      string
	Value          string
}

type Definition struct {
	ID          string
	Name        string
	Description string

	CanonicalTypes []string
	Categories     []requirement.Category
	Aliases        []string
	Examples       []string

	RemoteFilter *ExecutionDefinition
	RemoteSort   *ExecutionDefinition
	LocalFilter  *ExecutionDefinition
	LocalRank    *ExecutionDefinition
}

type Catalog struct {
	version     string
	definitions []Definition
	byID        map[string]Definition
}

func NewCatalog(version string, definitions []Definition) *Catalog {
	version = strings.TrimSpace(version)
	if version == "" {
		version = DefaultCatalogVersion
	}
	catalog := &Catalog{
		version:     version,
		definitions: append([]Definition(nil), definitions...),
		byID:        make(map[string]Definition, len(definitions)),
	}
	for _, definition := range definitions {
		if definition.ID != "" {
			catalog.byID[definition.ID] = definition
		}
	}
	return catalog
}

func NewDefaultCatalog() *Catalog {
	return NewCatalog(DefaultCatalogVersion, []Definition{
		{
			ID: "budget_friendly", Name: "价格优先", Description: "在当前候选车辆中优先展示总价较低的车辆",
			Categories: []requirement.Category{requirement.CategoryPrice, requirement.CategoryPreference},
			Aliases:    []string{"budget_friendly", "便宜", "省钱", "价格低"},
			Examples:   []string{"价格便宜点", "优先便宜的"},
			LocalRank: &ExecutionDefinition{
				RequiredFields: []string{"total_charge.total_amount"},
				Operation:      "price_low",
			},
		},
		{
			ID: "elderly_friendly", Name: "老人出行", Description: "适合老人乘坐和上下车的综合场景",
			Categories: []requirement.Category{requirement.CategoryUsageScenario},
			Aliases:    []string{"elderly_friendly", "适合老人", "老人出行"},
			Examples:   []string{"适合带老人出行"},
			LocalRank: &ExecutionDefinition{
				RequiredFields: []string{"vehicle.seats"},
				Operation:      "scenario:elderly_friendly_v1",
			},
		},
		{
			ID: "family_trip", Name: "家庭出行", Description: "家庭多人出行的综合场景",
			Categories: []requirement.Category{requirement.CategoryUsageScenario},
			Aliases:    []string{"family_trip", "家庭出行", "一家人出行"},
			LocalRank: &ExecutionDefinition{
				RequiredFields: []string{"vehicle.seats"},
				Operation:      "scenario:family_trip_v1",
			},
		},
		{
			ID: "beginner_friendly", Name: "新手友好", Description: "适合驾驶经验较少用户的综合场景",
			Categories: []requirement.Category{requirement.CategoryUsageScenario},
			Aliases:    []string{"beginner_friendly", "适合新手", "新手友好"},
		},
		{
			ID: "large_space", Name: "空间大", Description: "乘坐或装载空间较大的车辆",
			Categories: []requirement.Category{requirement.CategoryPreference, requirement.CategoryUsageScenario},
			Aliases:    []string{"large_space", "空间大", "后排宽敞"},
			LocalRank: &ExecutionDefinition{
				RequiredFields: []string{"vehicle.seats"},
				Operation:      "scenario:large_space_v1",
			},
		},
		{
			ID: "long_distance", Name: "长途出行", Description: "适合长距离行程的综合场景",
			Categories: []requirement.Category{requirement.CategoryUsageScenario},
			Aliases:    []string{"long_distance", "长途", "长距离出行"},
			LocalRank: &ExecutionDefinition{
				RequiredFields: []string{"vehicle.group_name"},
				Operation:      "scenario:long_distance_v1",
			},
		},
		{
			ID: "winter_driving", Name: "冬季驾驶", Description: "适合冬季驾驶的综合场景",
			Categories: []requirement.Category{requirement.CategoryUsageScenario},
			Aliases:    []string{"winter_driving", "冬季驾驶", "冰雪路面"},
		},
		{
			ID: "large_luggage", Name: "大件行李", Description: "需要可靠容纳较多或大件行李",
			Categories: []requirement.Category{requirement.CategoryUsageScenario},
			Aliases:    []string{"large_luggage", "大件行李", "行李多"},
			LocalRank: &ExecutionDefinition{
				RequiredFields: []string{"vehicle.seats"},
				Operation:      "scenario:large_luggage_v1",
			},
		},
	})
}

func (c *Catalog) Version() string {
	if c == nil {
		return ""
	}
	return c.version
}

func (c *Catalog) Get(id string) (Definition, bool) {
	if c == nil {
		return Definition{}, false
	}
	value, ok := c.byID[id]
	return value, ok
}

func (c *Catalog) Candidates(value Requirement, limit int) []Definition {
	if c == nil {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	var exact []Definition
	var category []Definition
	for _, definition := range c.definitions {
		if !supportsCategory(definition, value.Category) {
			continue
		}
		if definitionMatches(definition, value) {
			exact = append(exact, definition)
		} else {
			category = append(category, definition)
		}
	}
	result := append(exact, category...)
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func supportsCategory(definition Definition, category requirement.Category) bool {
	for _, value := range definition.Categories {
		if value == category {
			return true
		}
	}
	return false
}

func definitionMatches(definition Definition, value Requirement) bool {
	semantic := normalizeText(value.SemanticLabel)
	raw := normalizeText(value.RawText)
	for _, alias := range definition.Aliases {
		normalized := normalizeText(alias)
		if normalized != "" && (normalized == semantic || strings.Contains(raw, normalized)) {
			return true
		}
	}
	return false
}

func normalizeText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}
