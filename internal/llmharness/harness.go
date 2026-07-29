// Package llmharness provides a typed execution boundary for LLM tasks.
//
// Domain packages own request construction, strict decoding, and output
// validation. Harness executes that contract with a bounded model policy and
// never exposes raw model content or partially validated values to callers.
package llmharness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/llm"
	"github.com/zxq97/agent/pkg/http"
	"github.com/zxq97/agent/pkg/log"
)

// ValidationDisposition controls whether an output validation failure may use
// another model attempt.
type ValidationDisposition string

const (
	ValidationRetryableInvalid ValidationDisposition = "retryable_invalid"
	ValidationFinalFailure     ValidationDisposition = "final_failure"
)

// OutputValidationError is implemented by domain validation failures that
// explicitly declare their retry behavior.
type OutputValidationError interface {
	error
	Disposition() ValidationDisposition
	RepairCode() string
}

type outputValidationError struct {
	message     string
	disposition ValidationDisposition
	repairCode  string
}

func (e *outputValidationError) Error() string {
	return e.message
}

func (e *outputValidationError) Disposition() ValidationDisposition {
	return e.disposition
}

func (e *outputValidationError) RepairCode() string {
	return e.repairCode
}

// NewOutputValidationError creates a domain output error with an explicit
// retry disposition. Callers must use a stable repair code rather than model-
// generated or raw error text.
func NewOutputValidationError(message string, disposition ValidationDisposition, repairCode string) error {
	return &outputValidationError{
		message:     message,
		disposition: disposition,
		repairCode:  repairCode,
	}
}

// Policy bounds model selection, retries, and total logical-call duration.
type Policy struct {
	PrimaryModel   string
	FallbackModel  string
	RetryOnInvalid bool
	RetryOnEmpty   bool
	RetryTransient bool
	MaxAttempts    int
	TotalTimeout   time.Duration
}

// DefaultPolicy returns the production-safe first-release policy. It permits
// one same-model repair and keeps fallback disabled until separately evaluated.
func DefaultPolicy() Policy {
	return Policy{
		PrimaryModel:   llm.ModelConversation,
		RetryOnInvalid: true,
		RetryOnEmpty:   true,
		RetryTransient: true,
		MaxAttempts:    2,
		TotalTimeout:   60 * time.Second,
	}
}

// ResolvePolicy returns the default policy or the single supplied override.
// It keeps compatibility constructors concise while rejecting ambiguous input.
func ResolvePolicy(overrides []Policy) (Policy, error) {
	if len(overrides) == 0 {
		return DefaultPolicy(), nil
	}
	if len(overrides) > 1 {
		return Policy{}, errors.New("llm harness: at most one policy override is allowed")
	}
	return overrides[0], nil
}

// Task is the domain-owned contract executed by Harness.
type Task[I, O any] struct {
	ID               string
	PromptVersion    string
	SchemaVersion    string
	ValidatorVersion string

	ValidateInput  func(*I) error
	BuildRequest   func(*I) (*llm.ChatRequest, error)
	DecodeStrict   func(string) (*O, error)
	ValidateOutput func(*I, *O) error
	RepairHint     func(string) string
}

// RunRequest contains the typed input for one logical call.
type RunRequest[I any] struct {
	Input *I
}

// RunResult contains only a fully decoded and validated domain result.
type RunResult[O any] struct {
	Value        *O
	Model        string
	Attempts     int
	FallbackUsed bool
	Usage        llm.Usage
}

// Harness executes one typed Task using one immutable Policy.
type Harness[I, O any] struct {
	client llm.Client
	task   Task[I, O]
	policy Policy
}

// New validates and constructs a task-specific Harness.
func New[I, O any](client llm.Client, task Task[I, O], policy Policy) (*Harness[I, O], error) {
	if client == nil {
		return nil, errors.New("llm harness: client is required")
	}
	if strings.TrimSpace(task.ID) == "" ||
		strings.TrimSpace(task.PromptVersion) == "" ||
		strings.TrimSpace(task.SchemaVersion) == "" ||
		strings.TrimSpace(task.ValidatorVersion) == "" {
		return nil, errors.New("llm harness: task id and versions are required")
	}
	if task.ValidateInput == nil || task.BuildRequest == nil ||
		task.DecodeStrict == nil || task.ValidateOutput == nil {
		return nil, errors.New("llm harness: task callbacks are required")
	}
	if strings.TrimSpace(policy.PrimaryModel) == "" {
		return nil, errors.New("llm harness: primary model is required")
	}
	if policy.MaxAttempts < 1 || policy.MaxAttempts > 2 {
		return nil, errors.New("llm harness: max attempts must be 1 or 2")
	}
	if policy.TotalTimeout <= 0 {
		return nil, errors.New("llm harness: total timeout must be positive")
	}
	return &Harness[I, O]{client: client, task: task, policy: policy}, nil
}

// Run executes ValidateInput, BuildRequest, Chat, DecodeStrict, and
// ValidateOutput. A caller receives either a valid typed value or the original
// error from the stage that ultimately failed.
func (h *Harness[I, O]) Run(ctx context.Context, request *RunRequest[I]) (*RunResult[O], error) {
	if request == nil || request.Input == nil {
		return nil, errors.New("llm harness: run input is required")
	}
	if err := h.task.ValidateInput(request.Input); err != nil {
		return nil, err
	}
	baseRequest, err := h.task.BuildRequest(request.Input)
	if err != nil {
		return nil, err
	}
	if baseRequest == nil {
		return nil, errors.New("llm harness: task returned nil request")
	}

	logicalContext, cancel := context.WithTimeout(ctx, h.policy.TotalTimeout)
	defer cancel()

	var usage llm.Usage
	var lastError error
	var repairCode string
	promptContentHash := contentHash(baseRequest.System)
	for attempt := 1; attempt <= h.policy.MaxAttempts; attempt++ {
		model := h.modelForAttempt(attempt)
		attemptRequest := cloneRequest(baseRequest)
		attemptRequest.Model = model
		if attempt > 1 && repairCode != "" {
			applyRepairHint(attemptRequest, h.task.RepairHint, repairCode)
			temperature := 0.0
			attemptRequest.Temperature = &temperature
		}

		startedAt := time.Now()
		response, callErr := h.client.Chat(logicalContext, attemptRequest)
		if callErr != nil {
			lastError = callErr
			h.recordAttempt(logicalContext, attempt, promptContentHash, attemptRequest, startedAt, providerFailureKind(logicalContext, callErr), callErr, nil)
			if !h.shouldRetryProviderError(logicalContext, callErr, attempt) {
				return nil, callErr
			}
			repairCode = ""
			continue
		}
		if response != nil {
			addUsage(&usage, response.Usage)
		}
		if response == nil || strings.TrimSpace(response.Content) == "" {
			lastError = errors.Errorf("llm harness %s: empty output", h.task.ID)
			h.recordAttempt(logicalContext, attempt, promptContentHash, attemptRequest, startedAt, "empty_output", lastError, response)
			if !h.policy.RetryOnEmpty || !h.hasNextAttempt(attempt) {
				return nil, lastError
			}
			repairCode = "empty_output"
			continue
		}

		value, decodeErr := h.task.DecodeStrict(response.Content)
		if decodeErr != nil {
			lastError = decodeErr
			h.recordAttempt(logicalContext, attempt, promptContentHash, attemptRequest, startedAt, decodeFailureKind(response), decodeErr, response)
			if !h.policy.RetryOnInvalid || !h.hasNextAttempt(attempt) {
				return nil, decodeErr
			}
			repairCode = "invalid_structure"
			continue
		}
		if value == nil {
			lastError = errors.Errorf("llm harness %s: decoder returned nil output", h.task.ID)
			h.recordAttempt(logicalContext, attempt, promptContentHash, attemptRequest, startedAt, "output_parse", lastError, response)
			if !h.policy.RetryOnInvalid || !h.hasNextAttempt(attempt) {
				return nil, lastError
			}
			repairCode = "invalid_structure"
			continue
		}

		if validateErr := h.task.ValidateOutput(request.Input, value); validateErr != nil {
			lastError = validateErr
			h.recordAttempt(logicalContext, attempt, promptContentHash, attemptRequest, startedAt, validationFailureKind(validateErr), validateErr, response)
			code, retryable := retryableValidation(validateErr)
			if !retryable || !h.policy.RetryOnInvalid || !h.hasNextAttempt(attempt) {
				return nil, validateErr
			}
			repairCode = code
			continue
		}

		h.recordAttempt(logicalContext, attempt, promptContentHash, attemptRequest, startedAt, "", nil, response)
		return &RunResult[O]{
			Value:        value,
			Model:        responseModel(response, model),
			Attempts:     attempt,
			FallbackUsed: h.usesFallback(attempt),
			Usage:        usage,
		}, nil
	}
	return nil, lastError
}

func (h *Harness[I, O]) hasNextAttempt(attempt int) bool {
	return attempt < h.policy.MaxAttempts
}

func (h *Harness[I, O]) modelForAttempt(attempt int) string {
	if h.usesFallback(attempt) {
		return h.policy.FallbackModel
	}
	return h.policy.PrimaryModel
}

func (h *Harness[I, O]) usesFallback(attempt int) bool {
	return attempt > 1 &&
		strings.TrimSpace(h.policy.FallbackModel) != "" &&
		h.policy.FallbackModel != h.policy.PrimaryModel
}

func (h *Harness[I, O]) shouldRetryProviderError(ctx context.Context, err error, attempt int) bool {
	if !h.policy.RetryTransient || !h.hasNextAttempt(attempt) || ctx.Err() != nil {
		return false
	}
	if networkError, ok := err.(net.Error); ok {
		return networkError.Timeout() || networkError.Temporary()
	}
	statusCode, ok := httpclient.ErrorStatusCode(err)
	return ok && (statusCode == 429 || statusCode >= 500)
}

func (h *Harness[I, O]) recordAttempt(
	ctx context.Context,
	attempt int,
	promptContentHash string,
	request *llm.ChatRequest,
	startedAt time.Time,
	failureKind string,
	err error,
	response *llm.ChatResponse,
) {
	responseFields := map[string]any{
		"success":      err == nil,
		"failure_kind": failureKind,
	}
	if response != nil {
		responseFields["finish_reason"] = response.FinishReason
		responseFields["usage"] = response.Usage
	}
	entry := log.Entry{
		Component: "llm_harness",
		Operation: h.task.ID,
		Request: map[string]any{
			"task_id":                h.task.ID,
			"prompt_version":         h.task.PromptVersion,
			"prompt_content_hash":    promptContentHash,
			"schema_version":         h.task.SchemaVersion,
			"validator_version":      h.task.ValidatorVersion,
			"assembled_request_hash": requestHash(request),
			"model":                  request.Model,
			"attempt":                attempt,
			"fallback":               h.usesFallback(attempt),
		},
		Response:   responseFields,
		DurationMS: time.Since(startedAt).Milliseconds(),
	}
	if err != nil {
		entry.Error = err.Error()
	}
	log.Write(ctx, entry)
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func requestHash(request *llm.ChatRequest) string {
	if request == nil {
		return ""
	}
	payload := struct {
		Model          string              `json:"model"`
		System         string              `json:"system"`
		Messages       []llm.Message       `json:"messages"`
		Tools          []llm.Tool          `json:"tools,omitempty"`
		ToolChoice     string              `json:"tool_choice,omitempty"`
		ResponseFormat *llm.ResponseFormat `json:"response_format,omitempty"`
		Temperature    *float64            `json:"temperature,omitempty"`
		MaxTokens      int                 `json:"max_tokens,omitempty"`
	}{
		Model:          request.Model,
		System:         request.System,
		Messages:       request.Messages,
		Tools:          request.Tools,
		ToolChoice:     request.ToolChoice,
		ResponseFormat: request.ResponseFormat,
		Temperature:    request.Temperature,
		MaxTokens:      request.MaxTokens,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return contentHash(string(data))
}

func cloneRequest(input *llm.ChatRequest) *llm.ChatRequest {
	result := *input
	result.Messages = append([]llm.Message(nil), input.Messages...)
	result.Tools = append([]llm.Tool(nil), input.Tools...)
	if input.ResponseFormat != nil {
		format := *input.ResponseFormat
		result.ResponseFormat = &format
	}
	if input.Temperature != nil {
		temperature := *input.Temperature
		result.Temperature = &temperature
	}
	return &result
}

func applyRepairHint(request *llm.ChatRequest, hintBuilder func(string) string, repairCode string) {
	if hintBuilder == nil {
		return
	}
	hint := strings.TrimSpace(hintBuilder(repairCode))
	if hint == "" {
		return
	}
	request.System = strings.TrimSpace(request.System) + "\n\n输出纠错要求：" + hint
}

func retryableValidation(err error) (string, bool) {
	validationError, ok := err.(OutputValidationError)
	if !ok || validationError.Disposition() != ValidationRetryableInvalid {
		return "", false
	}
	return validationError.RepairCode(), true
}

func validationFailureKind(err error) string {
	_, retryable := retryableValidation(err)
	if retryable {
		return "output_validation_invalid"
	}
	return "final_validation"
}

func providerFailureKind(ctx context.Context, err error) string {
	if err == context.Canceled {
		return "cancelled"
	}
	if err == context.DeadlineExceeded || ctx.Err() == context.DeadlineExceeded {
		return "deadline_exceeded"
	}
	if statusCode, ok := httpclient.ErrorStatusCode(err); ok {
		if statusCode == 429 {
			return "rate_limited"
		}
		if statusCode >= 500 {
			return "provider_unavailable"
		}
		if statusCode == 401 || statusCode == 403 {
			return "provider_auth"
		}
		if statusCode >= 400 {
			return "invalid_request"
		}
	}
	if _, ok := err.(net.Error); ok {
		return "transport"
	}
	return "provider_call"
}

func decodeFailureKind(response *llm.ChatResponse) string {
	if response != nil && response.FinishReason == "length" {
		return "truncated_output"
	}
	return "output_parse"
}

func responseModel(response *llm.ChatResponse, fallback string) string {
	if response != nil && strings.TrimSpace(response.Model) != "" {
		return response.Model
	}
	return fallback
}

func addUsage(total *llm.Usage, value llm.Usage) {
	total.PromptTokens += value.PromptTokens
	total.CompletionTokens += value.CompletionTokens
	total.TotalTokens += value.TotalTokens
	total.CacheHitTokens += value.CacheHitTokens
}
