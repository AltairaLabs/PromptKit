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
	if m.scenario != nil && m.scenario.ID != "" {
		// Determine assistant turn number (user completed turns) from authoritative metadata if present
		turnNumber := 0
		if execCtx.Metadata != nil {
			if v, ok := execCtx.Metadata["arena_user_completed_turns"].(int); ok {
				turnNumber = v
			}
		}
		// Fallback to counting user messages in execCtx if metadata not present
		if turnNumber == 0 {
			const roleUser = "user"
			for i := range execCtx.Messages {
				if execCtx.Messages[i].Role == roleUser {
					turnNumber++
				}
			}
		}

		// Initialize metadata map if not exists
		if execCtx.Metadata == nil {
			execCtx.Metadata = make(map[string]interface{})
		}

		// Add scenario context to metadata for MockProvider
		execCtx.Metadata["mock_scenario_id"] = m.scenario.ID
		execCtx.Metadata["mock_turn_number"] = turnNumber
	}

	return next()
}

func (m *mockScenarioContextMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Mock scenario context middleware doesn't process chunks
	return nil
}
