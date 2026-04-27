package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// SelfPlayUserTurnContextStage adds scenario context for the NEXT user turn
// (completed user turns + 1). Intended only for self-play user generation.
//
// This stage writes scenario/turn coordination keys into TurnState's
// ProviderRequestMetadata so the mock provider can select the appropriate
// canned response by scenario id, turn number, and persona.
type SelfPlayUserTurnContextStage struct {
	stage.BaseStage
	scenario      *config.Scenario
	turnIndexHint int // If > 0, use this instead of computing from history
	turnState     *stage.TurnState
	personaID     string
}

// NewSelfPlayUserTurnContextStageWithTurnState creates a self-play context
// stage that writes coordination metadata into the shared *TurnState.
func NewSelfPlayUserTurnContextStageWithTurnState(
	scenario *config.Scenario,
	personaID string,
	turnState *stage.TurnState,
) *SelfPlayUserTurnContextStage {
	return &SelfPlayUserTurnContextStage{
		BaseStage: stage.NewBaseStage("selfplay_user_context", stage.StageTypeTransform),
		scenario:  scenario,
		turnState: turnState,
		personaID: personaID,
	}
}

// NewSelfPlayUserTurnContextStageWithHintAndTurnState creates a self-play
// context stage with an explicit turn index. The turnIndexHint should be the
// 1-indexed selfplay turn number (first selfplay = 1). Used when the scenario
// has mixed file-based and selfplay turns.
func NewSelfPlayUserTurnContextStageWithHintAndTurnState(
	scenario *config.Scenario,
	turnIndexHint int,
	personaID string,
	turnState *stage.TurnState,
) *SelfPlayUserTurnContextStage {
	return &SelfPlayUserTurnContextStage{
		BaseStage:     stage.NewBaseStage("selfplay_user_context", stage.StageTypeTransform),
		scenario:      scenario,
		turnIndexHint: turnIndexHint,
		turnState:     turnState,
		personaID:     personaID,
	}
}

// Process writes next-turn self-play context into TurnState's provider
// request metadata, then forwards all input elements unchanged.
//
//nolint:lll // Channel signature cannot be shortened
func (s *SelfPlayUserTurnContextStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	// Accumulate elements and count user turns
	elements, userCount := s.accumulateAndCount(input)

	// If we have a turn index hint, use it directly instead of computing from history.
	// This is important for scenarios with mixed file-based and selfplay turns,
	// where the selfplay turn number should be relative to selfplay iterations only.
	nextUserTurn := userCount + 1
	if s.turnIndexHint > 0 {
		nextUserTurn = s.turnIndexHint
	}

	s.writeProviderMetadata(userCount, nextUserTurn)

	// Forward all elements unchanged.
	for i := range elements {
		select {
		case output <- elements[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// accumulateAndCount collects all input elements and counts user messages.
func (s *SelfPlayUserTurnContextStage) accumulateAndCount(
	input <-chan stage.StreamElement,
) (elements []stage.StreamElement, userCount int) {
	for elem := range input {
		elements = append(elements, elem)
		if elem.Message != nil && elem.Message.Role == "user" {
			userCount++
		}
	}
	return elements, userCount
}

// writeProviderMetadata writes self-play coordination keys into the
// per-Turn ProviderRequestMetadata bag on TurnState.
func (s *SelfPlayUserTurnContextStage) writeProviderMetadata(userCount, nextUserTurn int) {
	if s.turnState == nil || s.scenario == nil || s.scenario.ID == "" {
		return
	}
	if s.turnState.ProviderRequestMetadata == nil {
		s.turnState.ProviderRequestMetadata = map[string]interface{}{}
	}
	m := s.turnState.ProviderRequestMetadata
	m["arena_user_completed_turns"] = userCount
	m["arena_user_next_turn"] = nextUserTurn
	m["arena_role"] = "self_play_user"
	m["mock_scenario_id"] = s.scenario.ID
	m["mock_turn_number"] = nextUserTurn // backward-compat
	if s.personaID != "" {
		m["mock_persona_id"] = s.personaID
	}
}
