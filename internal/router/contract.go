package router

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/pkg/errors"
)

type routeResultEnvelope struct {
	Candidates     *[]routeCandidateEnvelope `json:"candidates"`
	UnassignedText *string                   `json:"unassigned_text"`
}

type routeCandidateEnvelope struct {
	Action       *ActionType `json:"action"`
	EvidenceText *string     `json:"evidence_text"`
	Confidence   *float64    `json:"confidence"`
}

func decodeRouteResult(content, sourceText string) (*RouteResult, error) {
	var envelope routeResultEnvelope
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
	if envelope.Candidates == nil || envelope.UnassignedText == nil {
		return nil, errors.New("candidates and unassigned_text are required")
	}
	if len(*envelope.Candidates) == 0 {
		return nil, errors.New("at least one route candidate is required")
	}
	result := &RouteResult{Candidates: make([]RouteCandidate, 0, len(*envelope.Candidates)), UnassignedText: *envelope.UnassignedText}
	seen := make(map[ActionType]struct{})
	for _, item := range *envelope.Candidates {
		candidate, err := item.result(sourceText)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[candidate.Action]; exists {
			return nil, errors.Errorf("duplicate action %q", candidate.Action)
		}
		seen[candidate.Action] = struct{}{}
		result.Candidates = append(result.Candidates, candidate)
	}
	if text := strings.TrimSpace(result.UnassignedText); text != "" && !strings.Contains(sourceText, text) {
		return nil, errors.New("unassigned_text must quote source_text")
	}
	return result, nil
}

func (e routeCandidateEnvelope) result(sourceText string) (RouteCandidate, error) {
	if e.Action == nil || e.EvidenceText == nil || e.Confidence == nil {
		return RouteCandidate{}, errors.New("action, evidence_text and confidence are required")
	}
	switch *e.Action {
	case ActionModifyRentalContext, ActionUpdateVehicleRequirements, ActionRequestVehicleSearch, ActionGeneralReply:
	default:
		return RouteCandidate{}, errors.Errorf("invalid action %q", *e.Action)
	}
	evidence := strings.TrimSpace(*e.EvidenceText)
	if evidence == "" || !strings.Contains(sourceText, evidence) {
		return RouteCandidate{}, errors.New("evidence_text must be a non-empty quote from source_text")
	}
	if *e.Confidence < 0 || *e.Confidence > 1 {
		return RouteCandidate{}, errors.New("confidence must be between 0 and 1")
	}
	return RouteCandidate{Action: *e.Action, EvidenceText: evidence, Confidence: *e.Confidence}, nil
}
