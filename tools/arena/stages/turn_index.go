package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// TurnIndexStage computes role-specific turn counters from accumulated messages.
// It sets clear, role-specific metadata keys that other stages can consume:
// - arena_user_completed_turns: number of completed user messages
// - arena_user_next_turn: completed user messages + 1 (next user turn to generate)
// - arena_assistant_completed_turns: number of completed assistant messages
// - arena_assistant_next_turn: completed assistant messages + 1
//
// This stage enriches all elements with turn count metadata.
type TurnIndexStage struct {
	stage.BaseStage
}

// NewTurnIndexStage creates a new turn index stage.
func NewTurnIndexStage() *TurnIndexStage {
	return &TurnIndexStage{
		BaseStage: stage.NewBaseStage("arena_turn_index", stage.StageTypeTransform),
	}
}

// Process computes role-specific turn counters and enriches elements with metadata.
//
//nolint:gocognit,lll // Turn index calculation with multiple role types is complex
func (s *TurnIndexStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	const roleUser = "user"
	const roleAssistant = "assistant"

	// First pass: accumulate all elements and count turns
	var elements []stage.StreamElement
	userCount := 0
	assistantCount := 0

	for elem := range input {
		elements = append(elements, elem)

		// Count completed turns from Message elements
		if elem.Message != nil {
			switch elem.Message.Role {
			case roleUser:
				userCount++
			case roleAssistant:
				assistantCount++
			}
		}
	}

	// Compute turn metadata
	turnMetadata := map[string]interface{}{
		"arena_user_completed_turns":      userCount,
		"arena_user_next_turn":            userCount + 1,
		"arena_assistant_completed_turns": assistantCount,
		"arena_assistant_next_turn":       assistantCount + 1,
	}

	// Second pass: enrich all elements with turn metadata and forward
	for i := range elements {
		if elements[i].Metadata == nil {
			elements[i].Metadata = make(map[string]interface{})
		}

		// Add turn metadata to element (idempotent - won't overwrite if already set)
		for key, value := range turnMetadata {
			if _, exists := elements[i].Metadata[key]; !exists {
				elements[i].Metadata[key] = value
			}
		}

		// Forward element
		select {
		case output <- elements[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}
