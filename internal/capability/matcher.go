package capability

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/internal/llmharness"
)

// LLMTaskID is the stable identifier for capability candidate matching.
const LLMTaskID = "capability.match"

type LLMMatcher struct {
	harness *llmharness.Harness[MatchRequest, []Match]
}

func NewLLMMatcher(client llm.Client, policies ...llmharness.Policy) (*LLMMatcher, error) {
	if client == nil {
		return nil, errors.New("capability matcher: llm client is required")
	}
	policy, err := llmharness.ResolvePolicy(policies)
	if err != nil {
		return nil, err
	}
	harness, err := llmharness.New(client, matcherTask(), policy)
	if err != nil {
		return nil, err
	}
	return &LLMMatcher{harness: harness}, nil
}

func (m *LLMMatcher) Match(ctx context.Context, input *MatchRequest) ([]Match, error) {
	result, err := m.harness.Run(ctx, &llmharness.RunRequest[MatchRequest]{Input: input})
	if err != nil {
		return nil, err
	}
	return *result.Value, nil
}

func matcherTask() llmharness.Task[MatchRequest, []Match] {
	return llmharness.Task[MatchRequest, []Match]{
		ID:               LLMTaskID,
		PromptVersion:    "1.0.0",
		SchemaVersion:    "capability-match-output/1",
		ValidatorVersion: "1.0.0",
		ValidateInput:    validateMatchInput,
		BuildRequest:     buildMatchRequest,
		DecodeStrict: func(content string) (*[]Match, error) {
			matches, err := decodeMatches(content)
			if err != nil {
				return nil, err
			}
			return &matches, nil
		},
		ValidateOutput: validateMatches,
		RepairHint: func(string) string {
			return "请重新返回完整 JSON；matches 最多一个，capability_id 只能取自输入 candidates。"
		},
	}
}

func validateMatchInput(input *MatchRequest) error {
	if input == nil || len(input.Candidates) < 2 || len(input.Candidates) > 10 {
		return errors.New("capability matcher: requirement and 2 to 10 candidates are required")
	}
	return nil
}

func buildMatchRequest(input *MatchRequest) (*llm.ChatRequest, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return &llm.ChatRequest{
		System:         matcherPrompt,
		Messages:       []llm.Message{{Role: llm.RoleUser, Content: string(data)}},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}, nil
}

func validateMatches(input *MatchRequest, matches *[]Match) error {
	allowed := make(map[string]struct{}, len(input.Candidates))
	for _, candidate := range input.Candidates {
		allowed[candidate.ID] = struct{}{}
	}
	for _, match := range *matches {
		if _, exists := allowed[match.CapabilityID]; !exists {
			return llmharness.NewOutputValidationError(
				"capability matcher: capability_id must come from candidates",
				llmharness.ValidationRetryableInvalid,
				"candidate_not_allowed",
			)
		}
	}
	return nil
}

type matchEnvelope struct {
	Matches *[]matchItemEnvelope `json:"matches"`
}

type matchItemEnvelope struct {
	CapabilityID *string  `json:"capability_id"`
	Relation     *string  `json:"relation"`
	Confidence   *float64 `json:"confidence"`
}

func decodeMatches(content string) ([]Match, error) {
	var envelope matchEnvelope
	decoder := json.NewDecoder(bytes.NewBufferString(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, errors.New("capability matcher: multiple JSON values are not allowed")
		}
		return nil, err
	}
	if envelope.Matches == nil {
		return nil, errors.New("capability matcher: matches is required")
	}
	if len(*envelope.Matches) > 1 {
		return nil, errors.New("capability matcher: at most one match is allowed")
	}
	result := make([]Match, 0, len(*envelope.Matches))
	for _, item := range *envelope.Matches {
		if item.CapabilityID == nil || item.Relation == nil || item.Confidence == nil {
			return nil, errors.New("capability matcher: capability_id, relation and confidence are required")
		}
		id := strings.TrimSpace(*item.CapabilityID)
		relation := strings.TrimSpace(*item.Relation)
		if id == "" || (relation != "exact" && relation != "relevant") ||
			*item.Confidence < 0 || *item.Confidence > 1 {
			return nil, errors.New("capability matcher: invalid match")
		}
		result = append(result, Match{CapabilityID: id, Relation: relation, Confidence: *item.Confidence})
	}
	return result, nil
}

const matcherPrompt = `你是受限的车辆能力候选匹配器。输入包含一个用户 Requirement 和 2～10 个由服务端提供的 Capability 候选。

你只能判断语义关系，不能决定车辆是否满足需求，也不能创建能力、字段、阈值、FilterCode 或执行参数。

只返回严格 JSON：
{"matches":[{"capability_id":"必须从 candidates 原样选择","relation":"exact | relevant","confidence":0.0}]}

规则：
1. 最多返回一个候选；没有可靠候选时返回空 matches。
2. exact 表示 Requirement 与 Capability 的定义基本等价。
3. relevant 只表示相关，不能据此执行过滤或排序。
4. capability_id 必须来自输入 candidates。
5. 不输出解释或额外字段。`
