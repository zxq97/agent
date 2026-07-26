package vehiclecatalog

import "strings"

type EntityType string

const (
	EntityBrand  EntityType = "brand"
	EntitySeries EntityType = "series"
	EntityModel  EntityType = "model"
)

type Entity struct {
	ID            string
	Type          EntityType
	CanonicalName string
	Aliases       []string
	ParentID      string
	BrandID       string
}

type ResolveStatus string

const (
	ResolveExact     ResolveStatus = "exact"
	ResolveAlias     ResolveStatus = "alias"
	ResolveAmbiguous ResolveStatus = "ambiguous"
	ResolveNotFound  ResolveStatus = "not_found"
)

type ResolveInput struct {
	Name       string
	Type       EntityType
	BrandHint  string
	SeriesHint string
}

type Resolution struct {
	Status     ResolveStatus
	Entity     *Entity
	Candidates []Entity
}

type Resolver interface {
	Resolve(*ResolveInput) Resolution
	Version() string
}

type StaticCatalog struct {
	version  string
	entities []Entity
}

func NewStaticCatalog(entities []Entity) *StaticCatalog {
	return NewVersionedStaticCatalog("static-v1", entities)
}

func NewVersionedStaticCatalog(version string, entities []Entity) *StaticCatalog {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "static-v1"
	}
	return &StaticCatalog{version: version, entities: append([]Entity(nil), entities...)}
}

func NewDefaultCatalog() *StaticCatalog {
	return NewStaticCatalog([]Entity{
		{ID: "brand:tesla", Type: EntityBrand, CanonicalName: "特斯拉", Aliases: []string{"tesla"}},
		{ID: "model:tesla:model-y", Type: EntityModel, CanonicalName: "Model Y", Aliases: []string{"modely", "model-y", "特斯拉modely", "特斯拉model y"}, BrandID: "brand:tesla"},
		{ID: "model:tesla:model-3", Type: EntityModel, CanonicalName: "Model 3", Aliases: []string{"model3", "model-3", "特斯拉model3", "特斯拉model 3"}, BrandID: "brand:tesla"},
		{ID: "brand:toyota", Type: EntityBrand, CanonicalName: "丰田", Aliases: []string{"toyota"}},
		{ID: "series:toyota:camry", Type: EntitySeries, CanonicalName: "凯美瑞", Aliases: []string{"camry", "丰田凯美瑞"}, BrandID: "brand:toyota"},
		{ID: "brand:xiaomi", Type: EntityBrand, CanonicalName: "小米", Aliases: []string{"xiaomi", "小米汽车"}},
		{ID: "model:xiaomi:su7", Type: EntityModel, CanonicalName: "小米SU7", Aliases: []string{"su7", "小米su 7", "xiaomisu7"}, BrandID: "brand:xiaomi"},
		{ID: "brand:byd", Type: EntityBrand, CanonicalName: "比亚迪", Aliases: []string{"byd"}},
		{ID: "brand:bmw", Type: EntityBrand, CanonicalName: "宝马", Aliases: []string{"bmw"}},
		{ID: "series:bmw:3", Type: EntitySeries, CanonicalName: "宝马3系", Aliases: []string{"bmw3系", "bmw 3 series", "3系"}, BrandID: "brand:bmw"},
		{ID: "model:bmw:3:325li", Type: EntityModel, CanonicalName: "宝马325Li", Aliases: []string{"325li", "宝马325 li"}, ParentID: "series:bmw:3", BrandID: "brand:bmw"},
	})
}

func (c *StaticCatalog) Version() string {
	if c == nil {
		return ""
	}
	return c.version
}

func (c *StaticCatalog) Resolve(input *ResolveInput) Resolution {
	if c == nil || input == nil {
		return Resolution{Status: ResolveNotFound}
	}
	name := normalize(input.Name)
	if name == "" {
		return Resolution{Status: ResolveNotFound}
	}
	var exact []Entity
	var aliases []Entity
	for _, entity := range c.entities {
		if input.Type != "" && entity.Type != input.Type {
			continue
		}
		if normalize(entity.CanonicalName) == name {
			exact = append(exact, entity)
			continue
		}
		for _, alias := range entity.Aliases {
			if normalize(alias) == name {
				aliases = append(aliases, entity)
				break
			}
		}
	}
	if filtered, applied := c.filterByHints(exact, input); applied {
		exact = filtered
	}
	if len(exact) == 1 {
		entity := exact[0]
		return Resolution{Status: ResolveExact, Entity: &entity}
	}
	if len(exact) > 1 {
		return Resolution{Status: ResolveAmbiguous, Candidates: exact}
	}
	if filtered, applied := c.filterByHints(aliases, input); applied {
		aliases = filtered
	}
	if len(aliases) == 1 {
		entity := aliases[0]
		return Resolution{Status: ResolveAlias, Entity: &entity}
	}
	if len(aliases) > 1 {
		return Resolution{Status: ResolveAmbiguous, Candidates: aliases}
	}
	return Resolution{Status: ResolveNotFound}
}

func (c *StaticCatalog) filterByHints(values []Entity, input *ResolveInput) ([]Entity, bool) {
	brandHint := normalize(input.BrandHint)
	seriesHint := normalize(input.SeriesHint)
	if len(values) == 0 || (brandHint == "" && seriesHint == "") {
		return nil, false
	}
	filtered := append([]Entity(nil), values...)
	applied := false
	if brandIDs := c.matchingIDs(EntityBrand, brandHint); len(brandIDs) > 0 {
		applied = true
		next := make([]Entity, 0, len(filtered))
		for _, entity := range filtered {
			if _, exists := brandIDs[entity.BrandID]; exists {
				next = append(next, entity)
			}
		}
		filtered = next
	}
	if seriesIDs := c.matchingIDs(EntitySeries, seriesHint); len(seriesIDs) > 0 {
		applied = true
		next := make([]Entity, 0, len(filtered))
		for _, entity := range filtered {
			if _, exists := seriesIDs[entity.ParentID]; exists {
				next = append(next, entity)
			}
		}
		filtered = next
	}
	if !applied {
		return nil, false
	}
	return filtered, true
}

func (c *StaticCatalog) matchingIDs(entityType EntityType, hint string) map[string]struct{} {
	result := make(map[string]struct{})
	if hint == "" {
		return result
	}
	for _, entity := range c.entities {
		if entity.Type != entityType {
			continue
		}
		if normalize(entity.CanonicalName) == hint {
			result[entity.ID] = struct{}{}
			continue
		}
		for _, alias := range entity.Aliases {
			if normalize(alias) == hint {
				result[entity.ID] = struct{}{}
				break
			}
		}
	}
	return result
}

func normalize(value string) string {
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "", "车型", "", "车系", "")
	return replacer.Replace(strings.ToLower(strings.TrimSpace(value)))
}
