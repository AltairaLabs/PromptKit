package stages

import (
	"context"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// MockScenarioContextStage adds scenario context to the stream elements
// for MockProvider to use scenario-specific responses.
//
// This stage should be placed before ProviderStage in the pipeline
// when using MockProvider to ensure scenario context is available.
type MockScenarioContextStage struct {
	stage.BaseStage
	scenario *config.Scenario
}

// NewMockScenarioContextStage creates a stage that adds scenario context
// to stream elements for MockProvider scenario-specific responses.
func NewMockScenarioContextStage(scenario *config.Scenario) *MockScenarioContextStage {
	return &MockScenarioContextStage{
		BaseStage: stage.NewBaseStage("mock_scenario_context", stage.StageTypeTransform),
		scenario:  scenario,
	}
}

// Process adds scenario context to all elements.
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

	// Determine if we should add scenario context
	if !s.shouldAddScenarioContext() {
		// Just forward elements without modification
		for i := range elements {
			select {
			case output <- elements[i]:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}

	logger.Debug("MockScenarioContextStage adding scenario context",
		"scenario_id", s.scenario.ID,
		"messages", len(messages))

	// Calculate turn number from messages
	turnNumber := s.determineTurnNumber(elements, messages)

	// Forward elements with enriched metadata
	for i := range elements {
		elem := &elements[i]
		s.addScenarioContextToMetadata(elem, turnNumber)

		select {
		case output <- *elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	logger.Debug("MockScenarioContextStage set metadata",
		"mock_scenario_id", s.scenario.ID,
		"mock_turn_number", turnNumber)

	return nil
}

// shouldAddScenarioContext checks if scenario context should be added
func (s *MockScenarioContextStage) shouldAddScenarioContext() bool {
	return s.scenario != nil && s.scenario.ID != ""
}

// addScenarioContextToMetadata adds scenario ID and turn number to element metadata
func (s *MockScenarioContextStage) addScenarioContextToMetadata(elem *stage.StreamElement, turnNumber int) {
	if elem.Metadata == nil {
		elem.Metadata = make(map[string]interface{})
	}

	// Add scenario context to metadata for MockProvider
	elem.Metadata["mock_scenario_id"] = s.scenario.ID
	elem.Metadata["mock_turn_number"] = turnNumber
}

// determineTurnNumber gets the turn number from metadata or counts messages
func (s *MockScenarioContextStage) determineTurnNumber(elements []stage.StreamElement, messages []types.Message) int {
	// Try to get from authoritative metadata first
	if turnNumber := s.getTurnNumberFromMetadata(elements); turnNumber > 0 {
		return turnNumber
	}

	// Next prefer assistant message count to advance after tool results
	if turnNumber := s.countAssistantMessages(messages); turnNumber > 0 {
		return turnNumber
	}

	// Fallback to counting user messages
	return s.countUserMessages(messages)
}

// getTurnNumberFromMetadata extracts turn number from authoritative metadata
func (s *MockScenarioContextStage) getTurnNumberFromMetadata(elements []stage.StreamElement) int {
	for i := range elements {
		if elements[i].Metadata != nil {
			if v, ok := elements[i].Metadata["arena_user_completed_turns"].(int); ok {
				return v
			}
		}
	}
	return 0
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

// countUserMessages counts the number of user messages
func (s *MockScenarioContextStage) countUserMessages(messages []types.Message) int {
	count := 0
	for i := range messages {
		if messages[i].Role == roleUser {
			count++
		}
	}
	return count
}
