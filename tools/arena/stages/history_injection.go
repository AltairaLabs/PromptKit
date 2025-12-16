// Package stages provides arena-specific pipeline stages for test execution.
package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// HistoryInjectionStage prepends conversation history to the pipeline stream.
// This is useful for self-play scenarios where history needs to be explicitly provided
// before the LLM generates the next user message.
//
// The stage emits all history messages first, then forwards any incoming elements.
type HistoryInjectionStage struct {
	stage.BaseStage
	history []types.Message
}

// NewHistoryInjectionStage creates a new history injection stage.
func NewHistoryInjectionStage(history []types.Message) *HistoryInjectionStage {
	return &HistoryInjectionStage{
		BaseStage: stage.NewBaseStage("history_injection", stage.StageTypeTransform),
		history:   history,
	}
}

// Process emits history messages first, then forwards all incoming elements.
//
//nolint:lll // Channel signature cannot be shortened
func (s *HistoryInjectionStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	// First, emit all history messages as StreamElements
	for i := range s.history {
		msg := s.history[i] // avoid copying in loop
		elem := stage.StreamElement{
			Message: &msg,
			Metadata: map[string]interface{}{
				"source": "history_injection",
			},
		}

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Then forward all incoming elements
	for elem := range input {
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}
