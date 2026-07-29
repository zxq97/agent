package vehiclecatalog

import (
	"context"
	"strings"
	"time"

	"github.com/zxq97/agent/api/agenthub"
)

type EntityType string

const (
	EntityBrand  EntityType = "brand"
	EntitySeries EntityType = "series"
	EntityModel  EntityType = "model"
)

type Entity struct {
	ID               string
	Type             EntityType
	CanonicalName    string
	Aliases          []string
	ParentID         string
	BrandID          string
	ProviderBindings []ProviderBinding
	Facts            VehicleCatalogFacts
	DataVersion      string
}

type ProviderBinding struct {
	Provider     string
	ProviderID   string
	ProviderName string
	ValidFrom    time.Time
	ValidTo      *time.Time
}

// VehicleCatalogFacts contains optional, versioned facts unavailable in the
// Guide quote. Nil means unknown and must never be converted to a zero value.
type VehicleCatalogFacts struct {
	TrunkVolumeL  *float64
	RearLegroomMM *float64
	ISOFIXCount   *int
	SeatFoldable  *bool
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
	Source     string
	Reason     string
}

type Resolver interface {
	Resolve(*ResolveInput) Resolution
	Version() string
}

// ContextResolver supports optional bounded external recall without changing
// the synchronous local catalog contract used by deterministic callers.
type ContextResolver interface {
	ResolveContext(context.Context, *ResolveInput) Resolution
}

type CandidateSelector interface {
	SelectCandidate(context.Context, *ResolveInput, []agenthub.RecallCandidate) (string, error)
}

// RecallResolver uses AgentHub only after a local not_found, then requires
// every candidate to resolve back to one authoritative catalog entity.
type RecallResolver struct {
	local    *StaticCatalog
	recall   agenthub.Client
	selector CandidateSelector
}

func NewRecallResolver(local *StaticCatalog, recall agenthub.Client, selector CandidateSelector) *RecallResolver {
	if local == nil {
		local = NewDefaultCatalog()
	}
	return &RecallResolver{local: local, recall: recall, selector: selector}
}

func (r *RecallResolver) Version() string {
	if r == nil || r.local == nil {
		return ""
	}
	return r.local.Version()
}

func (r *RecallResolver) Resolve(input *ResolveInput) Resolution {
	if r == nil || r.local == nil {
		return Resolution{Status: ResolveNotFound}
	}
	return r.local.Resolve(input)
}

func (r *RecallResolver) ResolveContext(ctx context.Context, input *ResolveInput) Resolution {
	local := r.Resolve(input)
	if local.Status != ResolveNotFound || r == nil || r.recall == nil || input == nil ||
		strings.TrimSpace(input.Name) == "" {
		return local
	}
	response, err := r.recall.RecallVehicles(ctx, &agenthub.RecallRequest{
		Query:          input.Name,
		EntityType:     string(input.Type),
		BrandHint:      input.BrandHint,
		SeriesHint:     input.SeriesHint,
		CatalogVersion: r.local.Version(),
		TopK:           8,
	})
	if err != nil {
		return Resolution{Status: ResolveNotFound, Source: "agenthub", Reason: "agenthub_error"}
	}
	if response == nil || len(response.Candidates) == 0 {
		return Resolution{Status: ResolveNotFound, Source: "agenthub", Reason: "agenthub_empty"}
	}
	validated := make(map[string]Resolution)
	candidateEntity := make(map[string]string)
	for _, candidate := range response.Candidates {
		if candidate.EntityType != string(input.Type) {
			continue
		}
		value := r.local.Resolve(&ResolveInput{
			Name:       candidate.Name,
			Type:       input.Type,
			BrandHint:  firstNonEmpty(candidate.BrandHint, input.BrandHint),
			SeriesHint: firstNonEmpty(candidate.SeriesHint, input.SeriesHint),
		})
		if value.Entity == nil || (value.Status != ResolveExact && value.Status != ResolveAlias) {
			continue
		}
		validated[value.Entity.ID] = value
		candidateEntity[candidate.CandidateID] = value.Entity.ID
	}
	if len(validated) == 0 {
		return Resolution{Status: ResolveNotFound, Source: "agenthub", Reason: "agenthub_candidate_unverified"}
	}
	if len(validated) == 1 {
		for _, value := range validated {
			value.Source = "agenthub_revalidated"
			return value
		}
	}
	if r.selector == nil {
		return Resolution{Status: ResolveAmbiguous, Source: "agenthub", Reason: "agenthub_candidates_ambiguous"}
	}
	candidateID, err := r.selector.SelectCandidate(ctx, input, response.Candidates)
	if err != nil {
		return Resolution{Status: ResolveAmbiguous, Source: "agenthub", Reason: "agenthub_candidate_selection_failed"}
	}
	entityID, allowed := candidateEntity[candidateID]
	if !allowed {
		return Resolution{Status: ResolveNotFound, Source: "agenthub", Reason: "agenthub_candidate_outside_whitelist"}
	}
	value := validated[entityID]
	value.Source = "agenthub_revalidated"
	return value
}

func firstNonEmpty(left, right string) string {
	if strings.TrimSpace(left) != "" {
		return left
	}
	return right
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
	return NewVersionedStaticCatalog("vehicle-catalog-v2", []Entity{
		{ID: "brand:tesla", Type: EntityBrand, CanonicalName: "特斯拉", Aliases: []string{"tesla"}, ProviderBindings: guideBindings("特斯拉")},
		{ID: "model:tesla:model-y", Type: EntityModel, CanonicalName: "Model Y", Aliases: []string{"modely", "model-y", "特斯拉modely", "特斯拉model y"}, BrandID: "brand:tesla", ProviderBindings: guideBindings("特斯拉Model Y")},
		{ID: "model:tesla:model-3", Type: EntityModel, CanonicalName: "Model 3", Aliases: []string{"model3", "model-3", "特斯拉model3", "特斯拉model 3"}, BrandID: "brand:tesla", ProviderBindings: guideBindings("特斯拉Model 3")},
		{ID: "brand:toyota", Type: EntityBrand, CanonicalName: "丰田", Aliases: []string{"toyota"}, ProviderBindings: guideBindings("丰田")},
		{ID: "series:toyota:camry", Type: EntitySeries, CanonicalName: "凯美瑞", Aliases: []string{"camry", "丰田凯美瑞"}, BrandID: "brand:toyota"},
		{ID: "model:toyota:camry", Type: EntityModel, CanonicalName: "凯美瑞", Aliases: []string{"camry", "丰田凯美瑞"}, ParentID: "series:toyota:camry", BrandID: "brand:toyota", ProviderBindings: guideBindings("丰田凯美瑞")},
		{ID: "brand:xiaomi", Type: EntityBrand, CanonicalName: "小米", Aliases: []string{"xiaomi", "小米汽车"}, ProviderBindings: guideBindings("小米")},
		{ID: "model:xiaomi:su7", Type: EntityModel, CanonicalName: "小米SU7", Aliases: []string{"su7", "小米su 7", "xiaomisu7"}, BrandID: "brand:xiaomi", ProviderBindings: guideBindings("小米SU7")},
		{ID: "brand:byd", Type: EntityBrand, CanonicalName: "比亚迪", Aliases: []string{"byd"}, ProviderBindings: guideBindings("比亚迪")},
		{ID: "brand:bmw", Type: EntityBrand, CanonicalName: "宝马", Aliases: []string{"bmw"}, ProviderBindings: guideBindings("宝马")},
		{ID: "series:bmw:3", Type: EntitySeries, CanonicalName: "宝马3系", Aliases: []string{"bmw3系", "bmw 3 series", "3系"}, BrandID: "brand:bmw"},
		{ID: "model:bmw:3:325li", Type: EntityModel, CanonicalName: "宝马325Li", Aliases: []string{"325li", "宝马325 li"}, ParentID: "series:bmw:3", BrandID: "brand:bmw", ProviderBindings: guideBindings("宝马325Li")},
	})
}

func guideBindings(name string) []ProviderBinding {
	return []ProviderBinding{{Provider: "guide", ProviderName: name}}
}

// ProviderNames returns only names explicitly bound to the requested provider.
func (c *StaticCatalog) ProviderNames(entityID, provider string) []string {
	entity, ok := c.EntityByID(entityID)
	if !ok {
		return nil
	}
	var result []string
	now := time.Now()
	for _, binding := range entity.ProviderBindings {
		if !binding.ValidFrom.IsZero() && now.Before(binding.ValidFrom) {
			continue
		}
		if binding.ValidTo != nil && !now.Before(*binding.ValidTo) {
			continue
		}
		if binding.Provider == provider && strings.TrimSpace(binding.ProviderName) != "" {
			result = append(result, binding.ProviderName)
		}
	}
	return result
}

func (c *StaticCatalog) Version() string {
	if c == nil {
		return ""
	}
	return c.version
}

// EntityByID returns a catalog entity by its stable identifier.
func (c *StaticCatalog) EntityByID(id string) (Entity, bool) {
	if c == nil || strings.TrimSpace(id) == "" {
		return Entity{}, false
	}
	for _, entity := range c.entities {
		if entity.ID == id {
			return entity, true
		}
	}
	return Entity{}, false
}

// ModelsBySeries returns the known models belonging to a series.
func (c *StaticCatalog) ModelsBySeries(seriesID string) []Entity {
	if c == nil || strings.TrimSpace(seriesID) == "" {
		return nil
	}
	var result []Entity
	for _, entity := range c.entities {
		if entity.Type == EntityModel && entity.ParentID == seriesID {
			result = append(result, entity)
		}
	}
	return result
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
