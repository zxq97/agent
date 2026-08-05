package vehiclerequirement

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/llmharness"
	"github.com/zxq97/agent/internal/requirement"
)

type extractEnvelope struct {
	Requirements  *[]requirementEnvelope `json:"requirements"`
	DomainMatched *bool                  `json:"domain_matched"`
}

type requirementEnvelope struct {
	RawText       *string                `json:"raw_text"`
	SemanticLabel *string                `json:"semantic_label"`
	Category      *requirement.Category  `json:"category"`
	CanonicalType json.RawMessage        `json:"canonical_type"`
	Value         json.RawMessage        `json:"value"`
	Operation     *Operation             `json:"operation"`
	Relation      *ConstraintRelation    `json:"relation"`
	Alternatives  []alternativeEnvelope  `json:"alternatives"`
	Importance    *Importance            `json:"importance"`
	Confidence    *float64               `json:"confidence"`
	EntityContext *entityContextEnvelope `json:"entity_context"`
}

type alternativeEnvelope struct {
	CanonicalType json.RawMessage        `json:"canonical_type"`
	Value         json.RawMessage        `json:"value"`
	EntityContext *entityContextEnvelope `json:"entity_context"`
}

type valueEnvelope struct {
	Kind   *requirement.ValueKind `json:"kind"`
	Text   *string                `json:"text"`
	Number *float64               `json:"number"`
	Range  *rangeEnvelope         `json:"range"`
	Unit   *string                `json:"unit"`
}

type rangeEnvelope struct {
	Min *float64 `json:"min"`
	Max *float64 `json:"max"`
}

type entityContextEnvelope struct {
	BrandHint  *string `json:"brand_hint"`
	SeriesHint *string `json:"series_hint"`
}

func decodeExtractResult(content string) (*ExtractResult, error) {
	result, err := decodeExtractResultStrict(content)
	if err != nil {
		return nil, err
	}
	if err := validateExtractResultState(result); err != nil {
		return nil, err
	}
	return result, nil
}

func decodeExtractResultStrict(content string) (*ExtractResult, error) {
	var envelope extractEnvelope
	decoder := json.NewDecoder(bytes.NewBufferString(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple JSON values are not allowed")
		}
		return nil, err
	}
	if envelope.Requirements == nil || envelope.DomainMatched == nil {
		return nil, errors.New("requirements and domain_matched are required")
	}
	result := &ExtractResult{Requirements: make([]Requirement, 0, len(*envelope.Requirements)), DomainMatched: *envelope.DomainMatched}
	for _, item := range *envelope.Requirements {
		requirementValue, err := item.result()
		if err != nil {
			return nil, err
		}
		result.Requirements = append(result.Requirements, requirementValue)
	}
	return result, nil
}

func validateExtractResultState(result *ExtractResult) error {
	if !result.DomainMatched && len(result.Requirements) > 0 {
		return llmharness.NewOutputValidationError(
			"domain_matched=false requires an empty requirements array",
			llmharness.ValidationRetryableInvalid,
			"domain_state_conflict",
		)
	}
	if result.DomainMatched && len(result.Requirements) == 0 {
		return llmharness.NewOutputValidationError(
			"domain_matched=true requires at least one requirement",
			llmharness.ValidationRetryableInvalid,
			"domain_state_conflict",
		)
	}
	return nil
}

func (e requirementEnvelope) result() (Requirement, error) {
	if e.RawText == nil || e.SemanticLabel == nil || e.Category == nil ||
		len(e.CanonicalType) == 0 || len(e.Value) == 0 || e.Operation == nil ||
		e.Relation == nil || e.Importance == nil || e.Confidence == nil ||
		e.EntityContext == nil {
		return Requirement{}, errors.New("raw_text, semantic_label, category, canonical_type, value, operation, relation, importance, confidence and entity_context are required")
	}
	if e.EntityContext.BrandHint == nil || e.EntityContext.SeriesHint == nil {
		return Requirement{}, errors.New("entity_context.brand_hint and entity_context.series_hint are required")
	}
	canonicalType, err := decodeCanonicalType(e.CanonicalType)
	if err != nil {
		return Requirement{}, err
	}
	value, err := decodeRequirementValue(e.Value)
	if err != nil {
		return Requirement{}, err
	}
	result := Requirement{
		RawText:       strings.TrimSpace(*e.RawText),
		SemanticLabel: strings.TrimSpace(*e.SemanticLabel),
		Category:      *e.Category,
		CanonicalType: canonicalType,
		Value:         value,
		Operation:     *e.Operation,
		Relation:      *e.Relation,
		Operator:      operatorForRelation(*e.Relation),
		Importance:    *e.Importance,
		Confidence:    *e.Confidence,
		EntityContext: EntityContext{
			BrandHint:  strings.TrimSpace(*e.EntityContext.BrandHint),
			SeriesHint: strings.TrimSpace(*e.EntityContext.SeriesHint),
		},
	}
	for _, alternative := range e.Alternatives {
		value, err := alternative.result()
		if err != nil {
			return Requirement{}, err
		}
		result.Alternatives = append(result.Alternatives, value)
	}
	result.Facet = result.CanonicalType
	result.RawValue = rawValue(result.Value)
	if err := validateRequirement(result); err != nil {
		return Requirement{}, err
	}
	return result, nil
}

func (e alternativeEnvelope) result() (ConstraintAlternative, error) {
	if len(e.CanonicalType) == 0 || len(e.Value) == 0 || e.EntityContext == nil ||
		e.EntityContext.BrandHint == nil || e.EntityContext.SeriesHint == nil {
		return ConstraintAlternative{}, errors.New("alternative canonical_type, value and entity_context are required")
	}
	canonicalType, err := decodeCanonicalType(e.CanonicalType)
	if err != nil {
		return ConstraintAlternative{}, err
	}
	value, err := decodeRequirementValue(e.Value)
	if err != nil {
		return ConstraintAlternative{}, err
	}
	return ConstraintAlternative{
		CanonicalType: canonicalType,
		Value:         value,
		EntityContext: EntityContext{
			BrandHint:  strings.TrimSpace(*e.EntityContext.BrandHint),
			SeriesHint: strings.TrimSpace(*e.EntityContext.SeriesHint),
		},
	}, nil
}

func decodeCanonicalType(data json.RawMessage) (Facet, error) {
	value := strings.TrimSpace(string(data))
	if value == "null" {
		return "", nil
	}
	var decoded string
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", err
	}
	return Facet(strings.TrimSpace(decoded)), nil
}

func decodeRequirementValue(data json.RawMessage) (requirement.Value, error) {
	if strings.TrimSpace(string(data)) == "null" {
		return requirement.Value{Kind: requirement.ValueNone}, nil
	}
	var envelope valueEnvelope
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return requirement.Value{}, err
	}
	if envelope.Kind == nil {
		return requirement.Value{}, errors.New("requirement value kind is required")
	}
	value := requirement.Value{Kind: *envelope.Kind}
	if envelope.Text != nil {
		value.Text = strings.TrimSpace(*envelope.Text)
	}
	value.Number = envelope.Number
	if envelope.Range != nil {
		value.Range = &requirement.NumberRange{Min: envelope.Range.Min, Max: envelope.Range.Max}
	}
	if envelope.Unit != nil {
		value.Unit = strings.TrimSpace(*envelope.Unit)
	}
	if err := validateRequirementValue(value); err != nil {
		return requirement.Value{}, err
	}
	return value, nil
}

func validateRequirementValue(value requirement.Value) error {
	switch value.Kind {
	case requirement.ValueNone:
		if value.Text != "" || value.Number != nil || value.Range != nil || value.Unit != "" {
			return errors.New("none requirement value cannot contain data")
		}
	case requirement.ValueText:
		if value.Text == "" || value.Number != nil || value.Range != nil {
			return errors.New("text requirement value must contain only text")
		}
	case requirement.ValueNumber:
		if value.Number == nil || value.Text != "" || value.Range != nil {
			return errors.New("number requirement value must contain only number and optional unit")
		}
	case requirement.ValueRange:
		if value.Range == nil || (value.Range.Min == nil && value.Range.Max == nil) || value.Text != "" || value.Number != nil {
			return errors.New("range requirement value must contain a minimum or maximum")
		}
		if value.Range.Min != nil && value.Range.Max != nil && *value.Range.Min > *value.Range.Max {
			return errors.New("requirement value range minimum exceeds maximum")
		}
	case requirement.ValueEntity:
		return errors.New("entity requirement values are not accepted from the extractor")
	default:
		return errors.Errorf("invalid requirement value kind %q", value.Kind)
	}
	return nil
}

func validateRequirement(value Requirement) error {
	if value.CanonicalType != "" {
		switch value.CanonicalType {
		case FacetSeatNum, FacetVehicleType, FacetPricePreference, FacetCarAge,
			FacetComfortPreference, FacetEnergyType, FacetTransmission, FacetBrand,
			FacetVehicleSeries, FacetVehicleModel, FacetCustom:
		default:
			return errors.Errorf("invalid canonical_type %q", value.CanonicalType)
		}
		if !categorySupportsCanonicalType(value.Category, value.CanonicalType) {
			return errors.Errorf("category %q does not support canonical_type %q", value.Category, value.CanonicalType)
		}
	} else if value.SemanticLabel == "" {
		return errors.New("semantic_label is required when canonical_type is null")
	}
	switch value.Category {
	case requirement.CategoryVehicle, requirement.CategoryPrice, requirement.CategoryConfiguration,
		requirement.CategoryPreference, requirement.CategoryUsageScenario, requirement.CategoryUnknown:
	default:
		return errors.Errorf("invalid requirement category %q", value.Category)
	}
	switch value.Operation {
	case OperationAdd, OperationReplace, OperationRemove:
	default:
		return errors.Errorf("invalid operation %q", value.Operation)
	}
	switch value.Relation {
	case RelationExact, RelationAtLeast, RelationAtMost, RelationRange,
		RelationExclude, RelationAnyOf:
	default:
		return errors.Errorf("invalid relation %q", value.Relation)
	}
	switch value.Importance {
	case ImportanceHard, ImportanceSoft:
	default:
		return errors.Errorf("invalid importance %q", value.Importance)
	}
	if value.RawText == "" {
		return errors.New("raw_text is required")
	}
	if value.Operation != OperationRemove && value.Value.Kind == requirement.ValueNone && value.CanonicalType != "" {
		return errors.New("known requirement value is required unless operation=remove")
	}
	if value.Operation != OperationRemove {
		if err := validateCanonicalValueKind(value.CanonicalType, value.Value.Kind); err != nil {
			return err
		}
	}
	if value.Relation == RelationRange && value.Value.Kind != requirement.ValueRange {
		return errors.New("range relation requires a range value")
	}
	if value.Relation == RelationAnyOf {
		if len(value.Alternatives) < 2 || value.CanonicalType != "" || value.Value.Kind != requirement.ValueNone {
			return errors.New("any_of requires at least two alternatives and no top-level canonical value")
		}
		for _, alternative := range value.Alternatives {
			switch alternative.CanonicalType {
			case FacetBrand, FacetVehicleSeries, FacetVehicleModel:
			default:
				return errors.New("any_of currently supports only brand, vehicle_series and vehicle_model alternatives")
			}
			if alternative.Value.Kind != requirement.ValueText {
				return errors.New("vehicle any_of alternatives require text values")
			}
		}
	} else if len(value.Alternatives) > 0 {
		return errors.New("alternatives are allowed only with relation=any_of")
	}
	if value.Confidence < 0 || value.Confidence > 1 {
		return errors.New("confidence must be between 0 and 1")
	}
	return nil
}

func operatorForRelation(value ConstraintRelation) Operator {
	switch value {
	case RelationAtLeast:
		return OperatorGTE
	case RelationAtMost:
		return OperatorLTE
	case RelationRange:
		return OperatorIN
	case RelationExclude:
		return OperatorNotEQ
	case RelationAnyOf:
		return OperatorIN
	default:
		return OperatorEQ
	}
}

func validateCanonicalValueKind(canonicalType Facet, kind requirement.ValueKind) error {
	var allowed map[requirement.ValueKind]struct{}
	switch canonicalType {
	case "":
		return nil
	case FacetSeatNum:
		allowed = map[requirement.ValueKind]struct{}{requirement.ValueNumber: {}}
	case FacetPricePreference:
		allowed = map[requirement.ValueKind]struct{}{
			requirement.ValueText: {}, requirement.ValueNumber: {}, requirement.ValueRange: {},
		}
	case FacetCarAge:
		allowed = map[requirement.ValueKind]struct{}{requirement.ValueText: {}, requirement.ValueNumber: {}}
	default:
		allowed = map[requirement.ValueKind]struct{}{requirement.ValueText: {}}
	}
	if _, exists := allowed[kind]; !exists {
		return errors.Errorf("canonical_type %q does not support value kind %q", canonicalType, kind)
	}
	return nil
}

func categorySupportsCanonicalType(category requirement.Category, canonicalType Facet) bool {
	switch canonicalType {
	case FacetBrand, FacetVehicleSeries, FacetVehicleModel, FacetVehicleType:
		return category == requirement.CategoryVehicle
	case FacetPricePreference:
		return category == requirement.CategoryPrice
	case FacetSeatNum, FacetCarAge, FacetEnergyType, FacetTransmission:
		return category == requirement.CategoryConfiguration
	case FacetComfortPreference:
		return category == requirement.CategoryPreference
	case FacetCustom:
		return category == requirement.CategoryPreference || category == requirement.CategoryUsageScenario || category == requirement.CategoryUnknown
	default:
		return false
	}
}

func rawValue(value requirement.Value) string {
	switch value.Kind {
	case requirement.ValueText:
		return value.Text
	case requirement.ValueNumber:
		if value.Number == nil {
			return ""
		}
		return strconv.FormatFloat(*value.Number, 'f', -1, 64)
	case requirement.ValueRange:
		if value.Range == nil {
			return ""
		}
		if value.Range.Min != nil && value.Range.Max != nil {
			return strconv.FormatFloat(*value.Range.Min, 'f', -1, 64) + ".." + strconv.FormatFloat(*value.Range.Max, 'f', -1, 64)
		}
		if value.Range.Min != nil {
			return strconv.FormatFloat(*value.Range.Min, 'f', -1, 64)
		}
		if value.Range.Max != nil {
			return strconv.FormatFloat(*value.Range.Max, 'f', -1, 64)
		}
	}
	return ""
}
