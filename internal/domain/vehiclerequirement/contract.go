package vehiclerequirement

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/pkg/errors"
)

type extractEnvelope struct {
	Requirements  *[]requirementEnvelope `json:"requirements"`
	DomainMatched *bool                  `json:"domain_matched"`
}

type requirementEnvelope struct {
	Facet         *Facet                 `json:"facet"`
	RawText       *string                `json:"raw_text"`
	RawValue      *string                `json:"raw_value"`
	Operation     *Operation             `json:"operation"`
	Operator      *Operator              `json:"operator"`
	Importance    *Importance            `json:"importance"`
	Confidence    *float64               `json:"confidence"`
	EntityContext *entityContextEnvelope `json:"entity_context"`
}

type entityContextEnvelope struct {
	BrandHint  *string `json:"brand_hint"`
	SeriesHint *string `json:"series_hint"`
}

func decodeExtractResult(content string) (*ExtractResult, error) {
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
		requirement, err := item.result()
		if err != nil {
			return nil, err
		}
		result.Requirements = append(result.Requirements, requirement)
	}
	if !result.DomainMatched && len(result.Requirements) > 0 {
		return nil, errors.New("domain_matched=false requires an empty requirements array")
	}
	if result.DomainMatched && len(result.Requirements) == 0 {
		return nil, errors.New("domain_matched=true requires at least one requirement")
	}
	return result, nil
}

func (e requirementEnvelope) result() (Requirement, error) {
	if e.Facet == nil || e.RawText == nil || e.RawValue == nil || e.Operation == nil || e.Operator == nil || e.Importance == nil || e.Confidence == nil || e.EntityContext == nil {
		return Requirement{}, errors.New("facet, raw_text, raw_value, operation, operator, importance, confidence and entity_context are required")
	}
	if e.EntityContext.BrandHint == nil || e.EntityContext.SeriesHint == nil {
		return Requirement{}, errors.New("entity_context.brand_hint and entity_context.series_hint are required")
	}
	requirement := Requirement{
		Facet:      *e.Facet,
		RawText:    strings.TrimSpace(*e.RawText),
		RawValue:   strings.TrimSpace(*e.RawValue),
		Operation:  *e.Operation,
		Operator:   *e.Operator,
		Importance: *e.Importance,
		Confidence: *e.Confidence,
		EntityContext: EntityContext{
			BrandHint:  strings.TrimSpace(*e.EntityContext.BrandHint),
			SeriesHint: strings.TrimSpace(*e.EntityContext.SeriesHint),
		},
	}
	if err := validateRequirement(requirement); err != nil {
		return Requirement{}, err
	}
	return requirement, nil
}

func validateRequirement(requirement Requirement) error {
	switch requirement.Facet {
	case FacetSeatNum, FacetVehicleType, FacetPricePreference, FacetCarAge,
		FacetComfortPreference, FacetEnergyType, FacetTransmission, FacetBrand,
		FacetVehicleSeries, FacetVehicleModel, FacetCustom:
	default:
		return errors.Errorf("invalid facet %q", requirement.Facet)
	}
	switch requirement.Operation {
	case OperationAdd, OperationReplace, OperationRemove:
	default:
		return errors.Errorf("invalid operation %q", requirement.Operation)
	}
	switch requirement.Operator {
	case OperatorEQ, OperatorNotEQ, OperatorGT, OperatorGTE, OperatorLT,
		OperatorLTE, OperatorIN, OperatorNotIN, OperatorContains:
	default:
		return errors.Errorf("invalid operator %q", requirement.Operator)
	}
	switch requirement.Importance {
	case ImportanceHard, ImportanceSoft:
	default:
		return errors.Errorf("invalid importance %q", requirement.Importance)
	}
	if requirement.RawText == "" {
		return errors.New("raw_text is required")
	}
	if requirement.Operation != OperationRemove && requirement.RawValue == "" {
		return errors.New("raw_value is required unless operation=remove")
	}
	if requirement.Confidence < 0 || requirement.Confidence > 1 {
		return errors.New("confidence must be between 0 and 1")
	}
	return nil
}
