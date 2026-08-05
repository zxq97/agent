package vehiclerequirement

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/domain"
	"github.com/zxq97/agent/internal/progress"
	"github.com/zxq97/agent/internal/requirement"
	"github.com/zxq97/agent/internal/session"
	"github.com/zxq97/agent/internal/vehiclecatalog"
)

// Handler extracts and normalizes vehicle requirements into session deltas.
type Handler interface {
	Handle(context.Context, *session.AgentSession, *Input) (*Result, error)
}

type handler struct {
	extractor Extractor
	entities  vehiclecatalog.Resolver
}

func NewHandler(extractor Extractor, entities vehiclecatalog.Resolver) (Handler, error) {
	if extractor == nil {
		return nil, errors.New("vehicle requirement: extractor is required")
	}
	if entities == nil {
		return nil, errors.New("vehicle requirement: vehicle resolver is required")
	}
	return &handler{extractor: extractor, entities: entities}, nil
}

func (h *handler) Handle(ctx context.Context, agentSession *session.AgentSession, input *Input) (*Result, error) {
	if agentSession == nil {
		return nil, errors.New("vehicle requirement: session is required")
	}
	if input == nil || strings.TrimSpace(input.SourceText) == "" {
		return nil, errors.New("vehicle requirement: input source text is required")
	}
	progress.Emit(ctx, "requirement_matching", "正在提取并归一车辆条件")
	extracted, err := h.extractor.Extract(ctx, extractionInput(agentSession, input.SourceText))
	if err != nil {
		return nil, err
	}
	if !extracted.DomainMatched {
		return nil, domain.ErrDomainMismatch
	}
	deltas := make([]session.SearchRequirementStateItem, 0, len(extracted.Requirements))
	for _, requirement := range extracted.Requirements {
		deltas = append(deltas, h.normalizeContext(ctx, requirement))
	}
	merged, changed := merge(agentSession.Search.Requirements, extracted.Requirements, deltas)
	result := &Result{Changed: changed, Requirements: append([]session.SearchRequirementStateItem(nil), merged...)}
	if changed {
		result.Deltas = []session.StateDelta{&session.RequirementDelta{
			Requirements:      merged,
			IncrementVersion:  true,
			ActivateGoal:      true,
			ClearNoPreference: len(merged) > 0,
			MemoryText:        input.SourceText,
		}}
	}
	return result, nil
}

func extractionInput(agentSession *session.AgentSession, sourceText string) *ExtractionInput {
	input := &ExtractionInput{SourceText: sourceText, CurrentRequirements: make([]RequirementView, 0, len(agentSession.Search.Requirements))}
	for _, requirement := range agentSession.Search.Requirements {
		view := RequirementView{
			RawText:       requirement.RawText,
			SemanticLabel: requirement.SemanticLabel,
			Category:      requirement.Category,
			CanonicalType: requirement.CanonicalType,
			Value:         requirement.Value,
			Relation:      requirementRelation(requirement),
			Importance:    requirement.Importance,
		}
		for _, alternative := range requirement.Alternatives {
			view.Alternatives = append(view.Alternatives, RequirementAlternativeView{
				CanonicalType: alternative.Facet,
				Value:         alternative.CanonicalValue,
			})
		}
		input.CurrentRequirements = append(input.CurrentRequirements, view)
	}
	history := agentSession.Memory.RecentSearchCarTexts
	if len(history) > 2 {
		history = history[len(history)-2:]
	}
	input.RecentDomainHistory = append(input.RecentDomainHistory, history...)
	return input
}

func (h *handler) normalize(requirement Requirement) session.SearchRequirementStateItem {
	return h.normalizeContext(context.Background(), requirement)
}

func (h *handler) normalizeContext(ctx context.Context, requirement Requirement) session.SearchRequirementStateItem {
	if requirementRelationFromDelta(requirement) == RelationAnyOf && len(requirement.Alternatives) > 0 {
		return h.normalizeAnyOf(ctx, requirement)
	}
	facet := requirement.CanonicalType
	if facet == "" {
		facet = requirement.Facet
	}
	raw := requirement.RawValue
	if raw == "" {
		raw = rawValue(requirement.Value)
	}
	canonical := normalizeRequirementValue(facet, requirement)
	id := requirementID(facet, canonical, requirement.Operator)
	if facet == "" {
		id = semanticRequirementID(requirement)
	}
	item := session.SearchRequirementStateItem{
		ID:               id,
		Facet:            string(facet),
		RawText:          requirement.RawText,
		RawValue:         raw,
		CanonicalValue:   canonical,
		SemanticLabel:    requirement.SemanticLabel,
		Category:         requirement.Category,
		CanonicalType:    string(facet),
		Value:            requirement.Value,
		Operator:         string(requirement.Operator),
		Relation:         string(requirementRelationFromDelta(requirement)),
		Importance:       string(requirement.Importance),
		Status:           "active",
		EntityResolution: "",
		CatalogVersion:   h.entities.Version(),
	}
	entityType := entityTypeForFacet(facet)
	if entityType == "" || raw == "" {
		return item
	}
	resolveInput := &vehiclecatalog.ResolveInput{
		Name:       raw,
		Type:       entityType,
		BrandHint:  requirement.EntityContext.BrandHint,
		SeriesHint: requirement.EntityContext.SeriesHint,
	}
	resolution := h.entities.Resolve(resolveInput)
	if contextual, ok := h.entities.(vehiclecatalog.ContextResolver); ok {
		resolution = contextual.ResolveContext(ctx, resolveInput)
	}
	item.EntityResolution = string(resolution.Status)
	if resolution.Entity == nil {
		item.ResolutionReason = "vehicle entity catalog has no unique match"
		return item
	}
	item.EntityID = resolution.Entity.ID
	item.EntityType = string(resolution.Entity.Type)
	item.EntityBrandID = resolution.Entity.BrandID
	item.EntityParentID = resolution.Entity.ParentID
	item.CanonicalValue = resolution.Entity.CanonicalName
	item.ID = requirementID(facet, item.CanonicalValue, requirement.Operator)
	return item
}

func (h *handler) normalizeAnyOf(ctx context.Context, requirement Requirement) session.SearchRequirementStateItem {
	item := session.SearchRequirementStateItem{
		Facet:          "vehicle_entity_any_of",
		RawText:        requirement.RawText,
		SemanticLabel:  requirement.SemanticLabel,
		Category:       requirement.Category,
		Operator:       string(OperatorIN),
		Relation:       string(RelationAnyOf),
		Importance:     string(requirement.Importance),
		Status:         "active",
		CatalogVersion: h.entities.Version(),
	}
	for _, alternative := range requirement.Alternatives {
		raw := rawValue(alternative.Value)
		value := session.SearchRequirementAlternative{
			Facet:          string(alternative.CanonicalType),
			RawValue:       raw,
			CanonicalValue: normalizeScalar(alternative.CanonicalType, raw),
		}
		entityType := entityTypeForFacet(alternative.CanonicalType)
		if entityType != "" && raw != "" {
			input := &vehiclecatalog.ResolveInput{
				Name:       raw,
				Type:       entityType,
				BrandHint:  alternative.EntityContext.BrandHint,
				SeriesHint: alternative.EntityContext.SeriesHint,
			}
			resolution := h.entities.Resolve(input)
			if contextual, ok := h.entities.(vehiclecatalog.ContextResolver); ok {
				resolution = contextual.ResolveContext(ctx, input)
			}
			value.EntityResolution = string(resolution.Status)
			if resolution.Entity != nil {
				value.EntityID = resolution.Entity.ID
				value.EntityType = string(resolution.Entity.Type)
				value.EntityBrandID = resolution.Entity.BrandID
				value.EntityParentID = resolution.Entity.ParentID
				value.CanonicalValue = resolution.Entity.CanonicalName
			}
		}
		item.Alternatives = append(item.Alternatives, value)
	}
	item.CanonicalValue = alternativesDisplayValue(item.Alternatives)
	item.ID = anyOfRequirementID(item.Alternatives)
	return item
}

func alternativesDisplayValue(values []session.SearchRequirementAlternative) string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.CanonicalValue) != "" {
			result = append(result, value.CanonicalValue)
		}
	}
	return strings.Join(result, " OR ")
}

func anyOfRequirementID(values []session.SearchRequirementAlternative) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		identity := value.EntityID
		if identity == "" {
			identity = normalizeOpenText(value.CanonicalValue)
		}
		parts = append(parts, value.Facet+"="+identity)
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return fmt.Sprintf("vehicle_any_of:%x", sum[:6])
}

func requirementRelation(value session.SearchRequirementStateItem) string {
	if value.Relation != "" {
		return value.Relation
	}
	switch value.Operator {
	case string(OperatorGTE):
		return string(RelationAtLeast)
	case string(OperatorLTE):
		return string(RelationAtMost)
	case string(OperatorNotEQ), string(OperatorNotIN):
		return string(RelationExclude)
	case string(OperatorIN):
		return string(RelationAnyOf)
	default:
		return string(RelationExact)
	}
}

func requirementRelationFromDelta(value Requirement) ConstraintRelation {
	if value.Relation != "" {
		return value.Relation
	}
	switch value.Operator {
	case OperatorGTE:
		return RelationAtLeast
	case OperatorLTE:
		return RelationAtMost
	case OperatorNotEQ, OperatorNotIN:
		return RelationExclude
	case OperatorIN:
		return RelationAnyOf
	default:
		return RelationExact
	}
}

func normalizeRequirementValue(facet Facet, value Requirement) string {
	if facet != FacetPricePreference {
		return normalizeScalar(facet, value.RawValue)
	}
	unit := strings.ToLower(strings.TrimSpace(value.Value.Unit))
	scope := "total"
	if unit == "daily_cny" {
		scope = "daily"
	}
	switch value.Value.Kind {
	case requirement.ValueNumber:
		if value.Value.Number != nil {
			return scope + operatorSymbol(value.Operator) +
				fmt.Sprintf("%g", *value.Value.Number) + "CNY"
		}
	case requirement.ValueRange:
		if value.Value.Range != nil {
			if value.Value.Range.Min != nil && value.Value.Range.Max != nil {
				return fmt.Sprintf("%s=%g..%gCNY", scope, *value.Value.Range.Min, *value.Value.Range.Max)
			}
			if value.Value.Range.Min != nil {
				return fmt.Sprintf("%s>=%gCNY", scope, *value.Value.Range.Min)
			}
			if value.Value.Range.Max != nil {
				return fmt.Sprintf("%s<=%gCNY", scope, *value.Value.Range.Max)
			}
		}
	}
	return normalizeScalar(facet, value.RawValue)
}

func operatorSymbol(value Operator) string {
	switch value {
	case OperatorLT:
		return "<"
	case OperatorLTE:
		return "<="
	case OperatorGT:
		return ">"
	case OperatorGTE:
		return ">="
	default:
		return "="
	}
}

func entityTypeForFacet(facet Facet) vehiclecatalog.EntityType {
	switch facet {
	case FacetBrand:
		return vehiclecatalog.EntityBrand
	case FacetVehicleSeries:
		return vehiclecatalog.EntitySeries
	case FacetVehicleModel:
		return vehiclecatalog.EntityModel
	default:
		return ""
	}
}

func normalizeScalar(facet Facet, value string) string {
	value = strings.TrimSpace(value)
	switch facet {
	case FacetSeatNum:
		return strings.TrimSpace(strings.TrimSuffix(value, "座"))
	case FacetVehicleType:
		switch strings.ToLower(strings.ReplaceAll(value, " ", "")) {
		case "越野车":
			return "SUV"
		case "suv":
			return "SUV"
		case "mpv":
			return "MPV"
		}
	case FacetTransmission:
		switch value {
		case "自动波", "自动":
			return "自动挡"
		case "手动":
			return "手动挡"
		}
	case FacetEnergyType:
		switch value {
		case "电车", "纯电车", "纯电动":
			return "纯电"
		case "油车", "汽油车":
			return "汽油"
		case "插电混动":
			return "插混"
		}
	}
	return value
}

func merge(current []session.SearchRequirementStateItem, operations []Requirement, normalized []session.SearchRequirementStateItem) ([]session.SearchRequirementStateItem, bool) {
	result := append([]session.SearchRequirementStateItem(nil), current...)
	for index, operation := range operations {
		facet := operation.CanonicalType
		if facet == "" {
			facet = operation.Facet
		}
		switch operation.Operation {
		case OperationRemove:
			result = removeRequirement(result, operation, normalized[index])
		case OperationReplace:
			next := result
			if facet == "" {
				next = removeMatchingOpenRequirement(next, normalized[index])
			} else {
				next = removeFacet(next, string(facet))
				next = reconcileReplacedVehicleEntity(next, facet, normalized[index])
			}
			if !containsRequirement(next, normalized[index]) {
				next = append(next, normalized[index])
			}
			result = next
		case OperationAdd:
			if !containsRequirement(result, normalized[index]) {
				result = append(result, normalized[index])
			}
		}
	}
	return result, !sameRequirements(current, result)
}

// reconcileReplacedVehicleEntity prevents a newly replaced vehicle entity from
// being combined with a stale, incompatible entity from an earlier turn. It
// only applies to replace semantics; an explicit add remains visible for the
// compiler to validate instead of being silently discarded.
func reconcileReplacedVehicleEntity(
	values []session.SearchRequirementStateItem,
	facet Facet,
	replacement session.SearchRequirementStateItem,
) []session.SearchRequirementStateItem {
	result := make([]session.SearchRequirementStateItem, 0, len(values))
	for _, value := range values {
		switch facet {
		case FacetBrand:
			// Replacing a brand is a deliberate broad/new vehicle choice. Old
			// series and model constraints must not keep narrowing that choice.
			if value.Facet == string(FacetVehicleSeries) || value.Facet == string(FacetVehicleModel) {
				continue
			}
		case FacetVehicleSeries:
			if value.Facet == string(FacetVehicleModel) {
				continue
			}
			if value.Facet == string(FacetBrand) &&
				(replacement.EntityBrandID == "" || value.EntityID != replacement.EntityBrandID) {
				continue
			}
		case FacetVehicleModel:
			if value.Facet == string(FacetBrand) &&
				(replacement.EntityBrandID == "" || value.EntityID != replacement.EntityBrandID) {
				continue
			}
			if value.Facet == string(FacetVehicleSeries) &&
				(replacement.EntityParentID == "" || value.EntityID != replacement.EntityParentID) {
				continue
			}
		}
		result = append(result, value)
	}
	return result
}

func removeFacet(values []session.SearchRequirementStateItem, facet string) []session.SearchRequirementStateItem {
	result := make([]session.SearchRequirementStateItem, 0, len(values))
	for _, value := range values {
		if value.Facet != facet {
			result = append(result, value)
		}
	}
	return result
}

func removeRequirement(values []session.SearchRequirementStateItem, operation Requirement, normalized session.SearchRequirementStateItem) []session.SearchRequirementStateItem {
	canonical := normalized.CanonicalValue
	facet := operation.CanonicalType
	if facet == "" {
		facet = operation.Facet
	}
	result := make([]session.SearchRequirementStateItem, 0, len(values))
	for _, value := range values {
		if facet == "" {
			if value.Facet == "" && openRequirementMatches(value, normalized) {
				continue
			}
		} else if value.Facet == string(facet) && (canonical == "" || strings.EqualFold(value.CanonicalValue, canonical) || strings.EqualFold(value.RawValue, operation.RawValue)) {
			continue
		}
		result = append(result, value)
	}
	return result
}

func sameRequirements(left, right []session.SearchRequirementStateItem) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Facet != right[index].Facet ||
			left[index].SemanticLabel != right[index].SemanticLabel ||
			left[index].Category != right[index].Category ||
			left[index].CanonicalValue != right[index].CanonicalValue ||
			(left[index].Facet == "" &&
				(left[index].ID != right[index].ID || left[index].RawText != right[index].RawText ||
					left[index].RawValue != right[index].RawValue)) ||
			left[index].Operator != right[index].Operator ||
			requirementRelation(left[index]) != requirementRelation(right[index]) ||
			left[index].Importance != right[index].Importance ||
			left[index].EntityID != right[index].EntityID ||
			!reflect.DeepEqual(left[index].Alternatives, right[index].Alternatives) {
			return false
		}
	}
	return true
}

func containsRequirement(values []session.SearchRequirementStateItem, target session.SearchRequirementStateItem) bool {
	for _, value := range values {
		if value.ID == target.ID {
			return true
		}
		if target.Facet != "" &&
			(value.Facet == target.Facet &&
				value.CanonicalValue == target.CanonicalValue &&
				value.Operator == target.Operator &&
				value.Importance == target.Importance) {
			return true
		}
	}
	return false
}

func requirementID(facet Facet, value string, operator Operator) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s", facet, strings.ToLower(strings.TrimSpace(value)), operator)))
	return fmt.Sprintf("%s:%x", facet, sum[:6])
}

func semanticRequirementID(value Requirement) string {
	fingerprint := strings.Join([]string{
		string(value.Category),
		normalizeOpenText(value.RawText),
		string(value.Operator),
	}, "|")
	sum := sha256.Sum256([]byte(fingerprint))
	return fmt.Sprintf("semantic:%x", sum[:6])
}

func removeMatchingOpenRequirement(values []session.SearchRequirementStateItem, target session.SearchRequirementStateItem) []session.SearchRequirementStateItem {
	result := make([]session.SearchRequirementStateItem, 0, len(values))
	for _, value := range values {
		if value.Facet == "" && openRequirementMatches(value, target) {
			continue
		}
		result = append(result, value)
	}
	return result
}

func openRequirementMatches(left, right session.SearchRequirementStateItem) bool {
	if left.ID != "" && right.ID != "" && left.ID == right.ID {
		return true
	}
	if left.Category != right.Category {
		return false
	}
	leftLabel := strings.TrimSpace(left.SemanticLabel)
	rightLabel := strings.TrimSpace(right.SemanticLabel)
	if strings.Contains(leftLabel, "_") && strings.EqualFold(leftLabel, rightLabel) {
		return true
	}
	return normalizeOpenText(left.RawText) == normalizeOpenText(right.RawText)
}

func normalizeOpenText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}
