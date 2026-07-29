package vehiclecatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/agenthub"
	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/internal/llmharness"
)

const CandidateSelectorTaskID = "vehicle_entity.select_candidate"

type CandidateSelectionInput struct {
	Query      string                     `json:"query"`
	EntityType string                     `json:"entity_type"`
	BrandHint  string                     `json:"brand_hint,omitempty"`
	SeriesHint string                     `json:"series_hint,omitempty"`
	Candidates []agenthub.RecallCandidate `json:"candidates"`
}

type candidateSelectionOutput struct {
	CandidateID string  `json:"candidate_id"`
	Confidence  float64 `json:"confidence"`
}

type LLMCandidateSelector struct {
	harness *llmharness.Harness[CandidateSelectionInput, candidateSelectionOutput]
}

func NewLLMCandidateSelector(client llm.Client, policies ...llmharness.Policy) (*LLMCandidateSelector, error) {
	if client == nil {
		return nil, errors.New("vehicle catalog selector: llm client is required")
	}
	policy, err := llmharness.ResolvePolicy(policies)
	if err != nil {
		return nil, err
	}
	harness, err := llmharness.New(client, candidateSelectorTask(), policy)
	if err != nil {
		return nil, err
	}
	return &LLMCandidateSelector{harness: harness}, nil
}

func (s *LLMCandidateSelector) SelectCandidate(
	ctx context.Context,
	input *ResolveInput,
	candidates []agenthub.RecallCandidate,
) (string, error) {
	request := &CandidateSelectionInput{
		Query: input.Name, EntityType: string(input.Type),
		BrandHint: input.BrandHint, SeriesHint: input.SeriesHint,
		Candidates: append([]agenthub.RecallCandidate(nil), candidates...),
	}
	result, err := s.harness.Run(ctx, &llmharness.RunRequest[CandidateSelectionInput]{Input: request})
	if err != nil {
		return "", err
	}
	return result.Value.CandidateID, nil
}

func candidateSelectorTask() llmharness.Task[CandidateSelectionInput, candidateSelectionOutput] {
	return llmharness.Task[CandidateSelectionInput, candidateSelectionOutput]{
		ID:               CandidateSelectorTaskID,
		PromptVersion:    "1.0.0",
		SchemaVersion:    "vehicle-candidate-selection/1",
		ValidatorVersion: "1.0.0",
		ValidateInput:    validateCandidateSelectionInput,
		BuildRequest:     buildCandidateSelectionRequest,
		DecodeStrict:     decodeCandidateSelection,
		ValidateOutput:   validateCandidateSelectionOutput,
		RepairHint: func(string) string {
			return "只返回 candidate_id 和 confidence；candidate_id 必须来自输入 candidates。"
		},
	}
}

func validateCandidateSelectionInput(input *CandidateSelectionInput) error {
	if input == nil || strings.TrimSpace(input.Query) == "" ||
		strings.TrimSpace(input.EntityType) == "" ||
		len(input.Candidates) < 2 || len(input.Candidates) > 10 {
		return errors.New("vehicle catalog selector: query, entity_type and 2 to 10 candidates are required")
	}
	return nil
}

func buildCandidateSelectionRequest(input *CandidateSelectionInput) (*llm.ChatRequest, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return &llm.ChatRequest{
		System: candidateSelectorPrompt,
		Messages: []llm.Message{{
			Role: llm.RoleUser, Content: string(data),
		}},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}, nil
}

func decodeCandidateSelection(content string) (*candidateSelectionOutput, error) {
	type envelope struct {
		CandidateID *string  `json:"candidate_id"`
		Confidence  *float64 `json:"confidence"`
	}
	var decoded envelope
	decoder := json.NewDecoder(bytes.NewBufferString(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, errors.New("vehicle catalog selector: multiple JSON values are not allowed")
		}
		return nil, err
	}
	if decoded.CandidateID == nil || decoded.Confidence == nil ||
		strings.TrimSpace(*decoded.CandidateID) == "" ||
		*decoded.Confidence < 0 || *decoded.Confidence > 1 {
		return nil, errors.New("vehicle catalog selector: invalid output")
	}
	return &candidateSelectionOutput{
		CandidateID: strings.TrimSpace(*decoded.CandidateID),
		Confidence:  *decoded.Confidence,
	}, nil
}

func validateCandidateSelectionOutput(input *CandidateSelectionInput, output *candidateSelectionOutput) error {
	if output.Confidence < 0.8 {
		return llmharness.NewOutputValidationError(
			"vehicle catalog selector: confidence is below the execution threshold",
			llmharness.ValidationFinalFailure,
			"confidence_too_low",
		)
	}
	for _, candidate := range input.Candidates {
		if output.CandidateID == candidate.CandidateID {
			return nil
		}
	}
	return llmharness.NewOutputValidationError(
		"vehicle catalog selector: candidate_id must come from candidates",
		llmharness.ValidationRetryableInvalid,
		"candidate_not_allowed",
	)
}

const candidateSelectorPrompt = `你是受限的车型召回候选选择器。输入中的 candidates 来自 AgentHub，你只能在候选内选择最符合 query、entity_type、brand_hint 和 series_hint 的一个。

只返回严格 JSON：
{"candidate_id":"必须从 candidates 原样选择","confidence":0.0}

禁止生成车型名、Catalog ID、Provider ID、FilterCode 或候选列表之外的 ID。`

var _ CandidateSelector = (*LLMCandidateSelector)(nil)
