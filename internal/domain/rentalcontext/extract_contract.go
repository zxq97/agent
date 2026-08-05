package rentalcontext

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/llmharness"
)

type extractResultEnvelope struct {
	LocationQuery *string                `json:"location_query"`
	PickupTime    *extractedTimeEnvelope `json:"pickup_time"`
	ReturnTime    *extractedTimeEnvelope `json:"return_time"`
	DomainMatched *bool                  `json:"domain_matched"`
}

type extractedTimeEnvelope struct {
	Status *ResolutionStatus `json:"status"`
	Raw    *string           `json:"raw"`
	Value  json.RawMessage   `json:"value"`
}

func decodeExtractResult(content string) (*ExtractResult, error) {
	result, err := decodeExtractResultStrict(content)
	if err != nil {
		return nil, err
	}
	if err := validateExtractResult(result); err != nil {
		return nil, err
	}
	return result, nil
}

func decodeExtractResultStrict(content string) (*ExtractResult, error) {
	var envelope extractResultEnvelope
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
	if envelope.LocationQuery == nil || envelope.PickupTime == nil || envelope.ReturnTime == nil || envelope.DomainMatched == nil {
		return nil, errors.New("location_query, pickup_time, return_time and domain_matched are required")
	}
	pickup, err := envelope.PickupTime.result("pickup_time")
	if err != nil {
		return nil, err
	}
	ret, err := envelope.ReturnTime.result("return_time")
	if err != nil {
		return nil, err
	}
	result := &ExtractResult{
		LocationQuery: *envelope.LocationQuery,
		PickupTime:    pickup,
		ReturnTime:    ret,
		DomainMatched: *envelope.DomainMatched,
	}
	return result, nil
}

func (e *extractedTimeEnvelope) result(field string) (ExtractedTime, error) {
	if e == nil || e.Status == nil || e.Raw == nil || len(e.Value) == 0 {
		return ExtractedTime{}, errors.Errorf("%s requires status, raw and value", field)
	}
	var value *string
	if !bytes.Equal(bytes.TrimSpace(e.Value), []byte("null")) {
		var parsed string
		if err := json.Unmarshal(e.Value, &parsed); err != nil {
			return ExtractedTime{}, err
		}
		if _, err := time.Parse(time.RFC3339, parsed); err != nil {
			return ExtractedTime{}, err
		}
		value = &parsed
	}
	return ExtractedTime{Status: *e.Status, Raw: *e.Raw, Value: value}, nil
}

func validateExtractResult(result *ExtractResult) error {
	if result == nil {
		return retryableRentalOutputError("extraction result is required", "missing_result")
	}
	if err := validateExtractedTime("pickup_time", result.PickupTime); err != nil {
		return err
	}
	if err := validateExtractedTime("return_time", result.ReturnTime); err != nil {
		return err
	}
	location := strings.TrimSpace(result.LocationQuery)
	if !result.DomainMatched {
		if location != "" || result.PickupTime.Status != ResolutionAbsent || result.ReturnTime.Status != ResolutionAbsent {
			return retryableRentalOutputError("domain_matched=false requires empty location and absent times", "domain_state_conflict")
		}
		return nil
	}
	if location == "" && result.PickupTime.Status == ResolutionAbsent && result.ReturnTime.Status == ResolutionAbsent {
		return retryableRentalOutputError("domain_matched=true requires at least one rental-context modification", "domain_state_conflict")
	}
	return nil
}

func validateExtractedTime(field string, value ExtractedTime) error {
	raw := strings.TrimSpace(value.Raw)
	switch value.Status {
	case ResolutionAbsent:
		if raw != "" || value.Value != nil {
			return retryableRentalOutputError(
				errors.Errorf("%s status=absent requires raw empty and value null", field).Error(),
				"time_state_conflict",
			)
		}
	case ResolutionAmbiguous:
		if raw == "" || value.Value != nil {
			return retryableRentalOutputError(
				errors.Errorf("%s status=ambiguous requires raw non-empty and value null", field).Error(),
				"time_state_conflict",
			)
		}
	case ResolutionResolved:
		if raw == "" || value.Value == nil {
			return retryableRentalOutputError(
				errors.Errorf("%s status=resolved requires raw and value", field).Error(),
				"time_state_conflict",
			)
		}
	default:
		return retryableRentalOutputError(
			errors.Errorf("%s has invalid status %q", field, value.Status).Error(),
			"time_state_conflict",
		)
	}
	return nil
}

func retryableRentalOutputError(message, repairCode string) error {
	return llmharness.NewOutputValidationError(
		message,
		llmharness.ValidationRetryableInvalid,
		repairCode,
	)
}
