package llmharness

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/api/llm"
)

type scriptedClient struct {
	responses []*llm.ChatResponse
	errors    []error
	requests  []*llm.ChatRequest
}

func (c *scriptedClient) Chat(_ context.Context, request *llm.ChatRequest) (*llm.ChatResponse, error) {
	c.requests = append(c.requests, request)
	index := len(c.requests) - 1
	if index < len(c.errors) && c.errors[index] != nil {
		return nil, c.errors[index]
	}
	if index >= len(c.responses) {
		return nil, errors.New("scripted client: response is missing")
	}
	return c.responses[index], nil
}

func (c *scriptedClient) ChatStream(context.Context, *llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	return nil, errors.New("scripted client: streaming is not supported")
}

func TestHarnessRunsFullContractAndAggregatesUsage(t *testing.T) {
	client := &scriptedClient{responses: []*llm.ChatResponse{
		{Model: "primary", Content: "invalid", Usage: llm.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}},
		{Model: "primary", Content: "42", Usage: llm.Usage{PromptTokens: 3, CompletionTokens: 1, TotalTokens: 4}},
	}}
	task := integerTask()
	harness, err := New(client, task, testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	input := 42
	result, err := harness.Run(context.Background(), &RunRequest[int]{Input: &input})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value == nil || *result.Value != 42 || result.Attempts != 2 || result.FallbackUsed {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Usage.TotalTokens != 7 || len(client.requests) != 2 {
		t.Fatalf("usage=%#v requests=%d", result.Usage, len(client.requests))
	}
	if client.requests[0].Model != "primary" || client.requests[1].Model != "primary" {
		t.Fatalf("models=%q,%q", client.requests[0].Model, client.requests[1].Model)
	}
	if !strings.Contains(client.requests[1].System, "repair invalid_structure") {
		t.Fatalf("repair hint missing from %q", client.requests[1].System)
	}
	if client.requests[1].Temperature == nil || *client.requests[1].Temperature != 0 {
		t.Fatalf("repair temperature=%v", client.requests[1].Temperature)
	}
}

func TestHarnessUsesFallbackForSecondAttempt(t *testing.T) {
	client := &scriptedClient{responses: []*llm.ChatResponse{
		{Content: "bad"},
		{Model: "fallback", Content: "7"},
	}}
	policy := testPolicy()
	policy.FallbackModel = "fallback"
	harness, err := New(client, integerTask(), policy)
	if err != nil {
		t.Fatal(err)
	}
	input := 7
	result, err := harness.Run(context.Background(), &RunRequest[int]{Input: &input})
	if err != nil {
		t.Fatal(err)
	}
	if !result.FallbackUsed || result.Model != "fallback" || client.requests[1].Model != "fallback" {
		t.Fatalf("result=%#v second_request=%#v", result, client.requests[1])
	}
}

func TestHarnessRetriesOnlyExplicitRetryableValidation(t *testing.T) {
	t.Run("retryable", func(t *testing.T) {
		client := &scriptedClient{responses: []*llm.ChatResponse{{Content: "1"}, {Content: "2"}}}
		task := integerTask()
		task.ValidateOutput = func(_ *int, value *int) error {
			if *value == 1 {
				return NewOutputValidationError("wrong evidence", ValidationRetryableInvalid, "evidence")
			}
			return nil
		}
		harness, err := New(client, task, testPolicy())
		if err != nil {
			t.Fatal(err)
		}
		input := 2
		result, err := harness.Run(context.Background(), &RunRequest[int]{Input: &input})
		if err != nil {
			t.Fatal(err)
		}
		if result.Attempts != 2 || *result.Value != 2 {
			t.Fatalf("unexpected result: %#v", result)
		}
	})

	t.Run("unclassified final error keeps identity", func(t *testing.T) {
		finalError := errors.New("domain validator failed")
		client := &scriptedClient{responses: []*llm.ChatResponse{{Content: "1"}, {Content: "2"}}}
		task := integerTask()
		task.ValidateOutput = func(_ *int, _ *int) error {
			return finalError
		}
		harness, err := New(client, task, testPolicy())
		if err != nil {
			t.Fatal(err)
		}
		input := 1
		_, runErr := harness.Run(context.Background(), &RunRequest[int]{Input: &input})
		if runErr != finalError || len(client.requests) != 1 {
			t.Fatalf("error=%v requests=%d", runErr, len(client.requests))
		}
	})
}

func TestHarnessRetriesTransientProviderErrorAndPreservesFinalError(t *testing.T) {
	transient := timeoutError{}
	client := &scriptedClient{
		responses: []*llm.ChatResponse{nil, nil},
		errors:    []error{transient, errors.New("provider rejected request")},
	}
	harness, err := New(client, integerTask(), testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	input := 1
	_, runErr := harness.Run(context.Background(), &RunRequest[int]{Input: &input})
	if runErr != client.errors[1] || len(client.requests) != 2 {
		t.Fatalf("error=%v requests=%d", runErr, len(client.requests))
	}
	if client.requests[1].System != "base" || client.requests[1].Temperature != nil {
		t.Fatalf("transport retry was changed into repair: %#v", client.requests[1])
	}
}

func TestHarnessDoesNotMutateTaskRequestOrCallModelForInvalidInput(t *testing.T) {
	client := &scriptedClient{}
	baseRequest := &llm.ChatRequest{Model: "domain-selected-model", System: "base"}
	task := integerTask()
	task.BuildRequest = func(*int) (*llm.ChatRequest, error) {
		return baseRequest, nil
	}
	harness, err := New(client, task, testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	invalid := -1
	if _, runErr := harness.Run(context.Background(), &RunRequest[int]{Input: &invalid}); runErr == nil {
		t.Fatal("expected input validation error")
	}
	if len(client.requests) != 0 {
		t.Fatalf("model calls=%d", len(client.requests))
	}

	valid := 1
	client.responses = []*llm.ChatResponse{{Content: "1"}}
	if _, runErr := harness.Run(context.Background(), &RunRequest[int]{Input: &valid}); runErr != nil {
		t.Fatal(runErr)
	}
	if baseRequest.Model != "domain-selected-model" || baseRequest.System != "base" {
		t.Fatalf("base request mutated: %#v", baseRequest)
	}
}

func integerTask() Task[int, int] {
	return Task[int, int]{
		ID:               "test.integer",
		PromptVersion:    "1.0.0",
		SchemaVersion:    "integer/1",
		ValidatorVersion: "1.0.0",
		ValidateInput: func(input *int) error {
			if input == nil || *input < 0 {
				return errors.New("test integer: non-negative input is required")
			}
			return nil
		},
		BuildRequest: func(*int) (*llm.ChatRequest, error) {
			return &llm.ChatRequest{System: "base"}, nil
		},
		DecodeStrict: func(content string) (*int, error) {
			value, err := strconv.Atoi(content)
			if err != nil {
				return nil, err
			}
			return &value, nil
		},
		ValidateOutput: func(_ *int, _ *int) error {
			return nil
		},
		RepairHint: func(code string) string {
			return "repair " + code
		},
	}
}

func testPolicy() Policy {
	return Policy{
		PrimaryModel:   "primary",
		RetryOnInvalid: true,
		RetryOnEmpty:   true,
		RetryTransient: true,
		MaxAttempts:    2,
		TotalTimeout:   time.Second,
	}
}

type timeoutError struct{}

func (timeoutError) Error() string {
	return "timeout"
}

func (timeoutError) Timeout() bool {
	return true
}

func (timeoutError) Temporary() bool {
	return true
}
