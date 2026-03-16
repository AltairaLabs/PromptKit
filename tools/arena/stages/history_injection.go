// Package stages provides arena-specific pipeline stages for test execution.
package stages

import (
	"context"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// HistoryInjectionStage prepends conversation history to the pipeline stream.
// This is useful for self-play scenarios where history needs to be explicitly provided
// before the LLM generates the next user message.
//
// The stage emits all history messages first, then forwards any incoming elements.
//
// When swapRoles is true, user↔assistant roles are swapped so that the self-play
// LLM sees its own prior outputs as "assistant" and the target's responses as "user".
// This prevents the self-play LLM from confusing itself with the target assistant.
type HistoryInjectionStage struct {
	stage.BaseStage
	history   []types.Message
	swapRoles bool
}

// NewHistoryInjectionStage creates a new history injection stage.
func NewHistoryInjectionStage(history []types.Message) *HistoryInjectionStage {
	return &HistoryInjectionStage{
		BaseStage: stage.NewBaseStage("history_injection", stage.StageTypeTransform),
		history:   history,
	}
}

// NewHistoryInjectionStageSwapped creates a history injection stage that swaps
// user↔assistant roles. Use this for self-play generation so the LLM sees the
// conversation from its own perspective (its prior outputs as "assistant").
func NewHistoryInjectionStageSwapped(history []types.Message) *HistoryInjectionStage {
	return &HistoryInjectionStage{
		BaseStage: stage.NewBaseStage("history_injection", stage.StageTypeTransform),
		history:   history,
		swapRoles: true,
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
		if s.swapRoles {
			msg = swapMessageRole(&s.history[i])
		}
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

// swapMessageRole returns a shallow copy of the message with user↔assistant swapped.
// System messages are left unchanged.
func swapMessageRole(msg *types.Message) types.Message {
	swapped := *msg
	switch strings.ToLower(msg.Role) {
	case roleUser:
		swapped.Role = "assistant"
	case "assistant":
		swapped.Role = roleUser
	}
	return swapped
}
