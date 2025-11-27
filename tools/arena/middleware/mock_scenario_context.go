package middleware

import (
	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// MockScenarioContextMiddleware adds scenario context to the execution context
// for MockProvider to use scenario-specific responses.
//
// This middleware should be placed before ProviderMiddleware in the pipeline
// when using MockProvider to ensure scenario context is available.
type mockScenarioContextMiddleware struct {
	scenario *config.Scenario
}

// MockScenarioContextMiddleware creates middleware that adds scenario context
// to the execution context for MockProvider scenario-specific responses.
func MockScenarioContextMiddleware(scenario *config.Scenario) pipeline.Middleware {
	return &mockScenarioContextMiddleware{scenario: scenario}
}

func (m *mockScenarioContextMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	// Add scenario context to the execution context metadata if we have scenario metadata
	if m.shouldAddScenarioContext() {
		m.addScenarioContextToMetadata(execCtx)
	}

	return next()
}

// shouldAddScenarioContext checks if scenario context should be added
func (m *mockScenarioContextMiddleware) shouldAddScenarioContext() bool {
	return m.scenario != nil && m.scenario.ID != ""
}

// addScenarioContextToMetadata adds scenario ID and turn number to execution context
func (m *mockScenarioContextMiddleware) addScenarioContextToMetadata(execCtx *pipeline.ExecutionContext) {
	turnNumber := m.determineTurnNumber(execCtx)

	// Initialize metadata map if not exists
	if execCtx.Metadata == nil {
		execCtx.Metadata = make(map[string]interface{})
	}

	// Add scenario context to metadata for MockProvider
	execCtx.Metadata["mock_scenario_id"] = m.scenario.ID
	execCtx.Metadata["mock_turn_number"] = turnNumber
}

// determineTurnNumber gets the turn number from metadata or counts user messages
func (m *mockScenarioContextMiddleware) determineTurnNumber(execCtx *pipeline.ExecutionContext) int {
	// Try to get from authoritative metadata first
	if turnNumber := m.getTurnNumberFromMetadata(execCtx); turnNumber > 0 {
		return turnNumber
	}

	// Fallback to counting user messages
	return m.countUserMessages(execCtx)
}

// getTurnNumberFromMetadata extracts turn number from authoritative metadata
func (m *mockScenarioContextMiddleware) getTurnNumberFromMetadata(execCtx *pipeline.ExecutionContext) int {
	if execCtx.Metadata == nil {
		return 0
	}

	if v, ok := execCtx.Metadata["arena_user_completed_turns"].(int); ok {
		return v
	}

	return 0
}

// countUserMessages counts the number of user messages in the execution context
func (m *mockScenarioContextMiddleware) countUserMessages(execCtx *pipeline.ExecutionContext) int {
	const roleUser = "user"
	count := 0
	for i := range execCtx.Messages {
		if execCtx.Messages[i].Role == roleUser {
			count++
		}
	}
	return count
}

func (m *mockScenarioContextMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Mock scenario context middleware doesn't process chunks
	return nil
}
