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
	// Add scenario context to the execution context if we have scenario metadata
	if m.scenario != nil && m.scenario.ID != "" {
		// For turn numbering, we'll use the number of user messages in the current conversation
		// as a simple proxy for turn number (since each user message represents a turn)
		turnNumber := 0
		for _, msg := range execCtx.Messages {
			if msg.Role == "user" {
				turnNumber++
			}
		}

		execCtx.Context = providers.WithMockScenarioContext(execCtx.Context, m.scenario.ID, turnNumber)
	}

	return next()
}

func (m *mockScenarioContextMiddleware) StreamChunk(execCtx *pipeline.ExecutionContext, chunk *providers.StreamChunk) error {
	// Mock scenario context middleware doesn't process chunks
	return nil
}
