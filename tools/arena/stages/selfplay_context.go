package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// SelfPlayUserTurnContextStage adds scenario context for the NEXT user turn
// (completed user turns + 1). Intended only for self-play user generation.
//
// This stage enriches elements with metadata that MockProvider uses to select
// the appropriate mock response based on scenario and turn number.
type SelfPlayUserTurnContextStage struct {
	stage.BaseStage
	scenario *config.Scenario
}

// NewSelfPlayUserTurnContextStage creates a new self-play context stage.
func NewSelfPlayUserTurnContextStage(scenario *config.Scenario) *SelfPlayUserTurnContextStage {
	return &SelfPlayUserTurnContextStage{
		BaseStage: stage.NewBaseStage("selfplay_user_context", stage.StageTypeTransform),
		scenario:  scenario,
	}
}

// Process adds next-turn self-play context metadata to all elements.
//
//nolint:lll // Channel signature cannot be shortened
func (s *SelfPlayUserTurnContextStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	// Accumulate elements and count user turns
	elements, userCount := s.accumulateAndCount(input)

	// Forward elements with enriched metadata
	return s.forwardWithMetadata(ctx, elements, userCount, output)
}

// accumulateAndCount collects all input elements and counts user messages.
func (s *SelfPlayUserTurnContextStage) accumulateAndCount(
	input <-chan stage.StreamElement,
) (elements []stage.StreamElement, userCount int) {
	for elem := range input {
		elements = append(elements, elem)
		userCount = s.countUserTurn(&elem, userCount)
	}
	return elements, userCount
}

// countUserTurn updates the user count based on an element.
func (s *SelfPlayUserTurnContextStage) countUserTurn(elem *stage.StreamElement, currentCount int) int {
	// Count user messages
	if elem.Message != nil && elem.Message.Role == "user" {
		currentCount++
	}

	// Also check if turn count is already in metadata (from TurnIndexStage)
	if elem.Metadata != nil {
		if count, ok := elem.Metadata["arena_user_completed_turns"].(int); ok && count > currentCount {
			return count
		}
	}

	return currentCount
}

// forwardWithMetadata enriches elements with context and forwards them.
func (s *SelfPlayUserTurnContextStage) forwardWithMetadata(
	ctx context.Context,
	elements []stage.StreamElement,
	userCount int,
	output chan<- stage.StreamElement,
) error {
	nextUserTurn := userCount + 1

	for i := range elements {
		elem := &elements[i]
		s.enrichElement(elem, userCount, nextUserTurn)

		select {
		case output <- *elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// enrichElement adds self-play context metadata to an element.
func (s *SelfPlayUserTurnContextStage) enrichElement(elem *stage.StreamElement, userCount, nextUserTurn int) {
	if s.scenario == nil || s.scenario.ID == "" {
		return
	}

	if elem.Metadata == nil {
		elem.Metadata = make(map[string]interface{})
	}

	elem.Metadata["arena_user_completed_turns"] = userCount
	elem.Metadata["arena_user_next_turn"] = nextUserTurn
	elem.Metadata["arena_role"] = "self_play_user"
	elem.Metadata["mock_scenario_id"] = s.scenario.ID
	elem.Metadata["mock_turn_number"] = nextUserTurn // backward-compat
}
