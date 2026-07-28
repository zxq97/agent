package vehiclerequirement

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/progress"
	"github.com/zxq97/agent/internal/requirement"
	"github.com/zxq97/agent/internal/session"
	"github.com/zxq97/agent/internal/vehiclecatalog"
)

type Handler struct {
	extractor Extractor
	entities  vehiclecatalog.Resolver
}

func NewHandler(extractor Extractor, entities vehiclecatalog.Resolver) (*Handler, error) {
	if extractor == nil {
		return nil, errors.New("vehicle requirement: extractor is required")
	}
	if entities == nil {
		entities = vehiclecatalog.NewDefaultCatalog()
	}
	return &Handler{extractor: extractor, entities: entities}, nil
}

func (h *Handler) Handle(ctx context.Context, agentSession *session.AgentSession, input *UpdateInput) (*UpdateResult, error) {
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
		return nil, ErrDomainMismatch
	}
	deltas := make([]session.SearchRequirementStateItem, 0, len(extracted.Requirements))
	for _, requirement := range extracted.Requirements {
		deltas = append(deltas, h.normalize(requirement))
	}
	merged, changed := merge(agentSession.Search.Requirements, extracted.Requirements, deltas)
	result := &UpdateResult{Changed: changed, Requirements: append([]session.SearchRequirementStateItem(nil), merged...)}
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
		input.CurrentRequirements = append(input.CurrentRequirements, RequirementView{
			RawText:       requirement.RawText,
			SemanticLabel: requirement.SemanticLabel,
			Category:      requirement.Category,
			CanonicalType: requirement.CanonicalType,
			Value:         requirement.Value,
			Operator:      requirement.Operator,
			Importance:    requirement.Importance,
			Status:        requirement.Status,
		})
	}
	history := agentSession.Memory.RecentSearchCarTexts
	if len(history) > 2 {
		history = history[len(history)-2:]
	}
	input.RecentDomainHistory = append(input.RecentDomainHistory, history...)
	return input
}

func (h *Handler) normalize(requirement Requirement) session.SearchRequirementStateItem {
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
		Importance:       string(requirement.Importance),
		Status:           "active",
		EntityResolution: "",
		CatalogVersion:   h.entities.Version(),
	}
	entityType := entityTypeForFacet(facet)
	if entityType == "" || raw == "" {
		return item
	}
	resolution := h.entities.Resolve(&vehiclecatalog.ResolveInput{
		Name:       raw,
		Type:       entityType,
		BrandHint:  requirement.EntityContext.BrandHint,
		SeriesHint: requirement.EntityContext.SeriesHint,
	})
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
			left[index].Importance != right[index].Importance ||
			left[index].EntityID != right[index].EntityID {
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
	return normalizeOpenText(left.RawText) == normalizeOpenText(right.RawText)
}

func normalizeOpenText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}
