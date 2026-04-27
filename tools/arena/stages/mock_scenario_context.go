package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MockScenarioContextStage adds scenario context to TurnState's
// ProviderRequestMetadata so MockProvider can select scenario-specific
// canned responses.
//
// This stage should be placed before ProviderStage in the pipeline
// when using MockProvider to ensure scenario context is available.
type MockScenarioContextStage struct {
	stage.BaseStage
	scenario  *config.Scenario
	turnState *stage.TurnState
}

// NewMockScenarioContextStageWithTurnState creates a stage that writes
// scenario context into the shared *TurnState's ProviderRequestMetadata.
func NewMockScenarioContextStageWithTurnState(
	scenario *config.Scenario, turnState *stage.TurnState,
) *MockScenarioContextStage {
	return &MockScenarioContextStage{
		BaseStage: stage.NewBaseStage("mock_scenario_context", stage.StageTypeTransform),
		scenario:  scenario,
		turnState: turnState,
	}
}

// Process writes scenario context into TurnState and forwards all elements
// unchanged.
//
//nolint:lll // Channel signature cannot be shortened
func (s *MockScenarioContextStage) Process(ctx context.Context, input <-chan stage.StreamElement, output chan<- stage.StreamElement) error {
	defer close(output)

	// Collect all elements and messages
	var elements []stage.StreamElement
	var messages []types.Message

	for elem := range input {
		elements = append(elements, elem)
		if elem.Message != nil {
			messages = append(messages, *elem.Message)
		}
	}

	if s.shouldAddScenarioContext() {
		turnNumber := s.determineTurnNumber(messages)
		s.writeScenarioContext(turnNumber)

		logger.Debug("MockScenarioContextStage set provider metadata",
			"mock_scenario_id", s.scenario.ID,
			"mock_turn_number", turnNumber)
	}

	for i := range elements {
		select {
		case output <- elements[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// shouldAddScenarioContext checks if scenario context should be added.
func (s *MockScenarioContextStage) shouldAddScenarioContext() bool {
	return s.turnState != nil && s.scenario != nil && s.scenario.ID != ""
}

// writeScenarioContext writes scenario id and turn number into TurnState's
// provider request metadata bag.
func (s *MockScenarioContextStage) writeScenarioContext(turnNumber int) {
	if s.turnState.ProviderRequestMetadata == nil {
		s.turnState.ProviderRequestMetadata = map[string]interface{}{}
	}
	s.turnState.ProviderRequestMetadata["mock_scenario_id"] = s.scenario.ID
	s.turnState.ProviderRequestMetadata["mock_turn_number"] = turnNumber
}

// determineTurnNumber gets the turn number from existing TurnState metadata
// or by counting messages.
func (s *MockScenarioContextStage) determineTurnNumber(messages []types.Message) int {
	// Try to read an existing arena_user_completed_turns from TurnState first
	// (set by SelfPlayUserTurnContextStage when present in the pipeline).
	if s.turnState != nil {
		if v, ok := s.turnState.ProviderRequestMetadata["arena_user_completed_turns"].(int); ok && v > 0 {
			return v
		}
	}

	// Next prefer assistant message count to advance after tool results.
	if turnNumber := s.countAssistantMessages(messages); turnNumber > 0 {
		return turnNumber
	}

	// Fallback to counting user messages.
	return s.countUserMessages(messages)
}

// countAssistantMessages counts assistant messages and returns next turn index.
func (s *MockScenarioContextStage) countAssistantMessages(messages []types.Message) int {
	const roleAssistant = "assistant"
	count := 0
	for i := range messages {
		if messages[i].Role == roleAssistant {
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return count + 1
}

// countUserMessages counts the number of user messages.
func (s *MockScenarioContextStage) countUserMessages(messages []types.Message) int {
	count := 0
	for i := range messages {
		if messages[i].Role == roleUser {
			count++
		}
	}
	return count
}
