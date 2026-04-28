package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// TurnIndexStage computes role-specific turn counters from accumulated messages.
// It writes the counters into TurnState's ProviderRequestMetadata so downstream
// stages (e.g. mock provider context) and persisted state.Metadata can read
// them. Keys written:
//   - arena_user_completed_turns: number of completed user messages
//   - arena_user_next_turn: completed user messages + 1 (next user turn to generate)
//   - arena_assistant_completed_turns: number of completed assistant messages
//   - arena_assistant_next_turn: completed assistant messages + 1
type TurnIndexStage struct {
	stage.BaseStage
	turnState *stage.TurnState
}

// NewTurnIndexStage creates a new turn index stage that does not publish into
// any TurnState. Useful for tests that only need passthrough behavior.
func NewTurnIndexStage() *TurnIndexStage {
	return &TurnIndexStage{
		BaseStage: stage.NewBaseStage("arena_turn_index", stage.StageTypeTransform),
	}
}

// NewTurnIndexStageWithTurnState creates a turn index stage that publishes
// the computed counters onto the supplied TurnState's ProviderRequestMetadata.
func NewTurnIndexStageWithTurnState(turnState *stage.TurnState) *TurnIndexStage {
	return &TurnIndexStage{
		BaseStage: stage.NewBaseStage("arena_turn_index", stage.StageTypeTransform),
		turnState: turnState,
	}
}

// Process computes role-specific turn counters and forwards elements unchanged.
//
//nolint:lll // Channel signature cannot be shortened
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

	// Publish counters onto TurnState if provided. Use idempotent semantics:
	// don't overwrite values an upstream stage may have set explicitly.
	if s.turnState != nil {
		if s.turnState.ProviderRequestMetadata == nil {
			s.turnState.ProviderRequestMetadata = map[string]interface{}{}
		}
		m := s.turnState.ProviderRequestMetadata
		setIfAbsent(m, "arena_user_completed_turns", userCount)
		setIfAbsent(m, "arena_user_next_turn", userCount+1)
		setIfAbsent(m, "arena_assistant_completed_turns", assistantCount)
		setIfAbsent(m, "arena_assistant_next_turn", assistantCount+1)
	}

	// Second pass: forward elements unchanged.
	for i := range elements {
		select {
		case output <- elements[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func setIfAbsent(m map[string]interface{}, key string, value interface{}) {
	if _, ok := m[key]; ok {
		return
	}
	m[key] = value
}
