package llmharness

import (
	"context"

	"github.com/zxq97/agent/api/llm"
)

// Extractor exposes the common typed extraction shape used by domain tasks.
type Extractor[I, O any] interface {
	Extract(context.Context, *I) (*O, error)
}

type extractor[I, O any] struct {
	harness *Harness[I, O]
}

// NewExtractor creates a typed adapter that exposes only the validated value
// returned by Harness. The Task still owns all domain prompts and validation.
func NewExtractor[I, O any](client llm.Client, task Task[I, O], policy Policy) (Extractor[I, O], error) {
	harness, err := New(client, task, policy)
	if err != nil {
		return nil, err
	}
	return &extractor[I, O]{harness: harness}, nil
}

func (e *extractor[I, O]) Extract(ctx context.Context, input *I) (*O, error) {
	result, err := e.harness.Run(ctx, &RunRequest[I]{Input: input})
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}
