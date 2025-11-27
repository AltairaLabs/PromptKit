package middleware

import (
	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// SelfPlayUserTurnContextMiddleware adds scenario context for the NEXT user turn
// (completed user turns + 1). Intended only for self-play user generation.
type selfPlayUserTurnContextMiddleware struct {
	scenario *config.Scenario
}

// SelfPlayUserTurnContextMiddleware creates middleware that adds scenario context
// with next-turn numbering to the execution context for MockProvider while also
// setting clear, role-specific metadata keys.
func SelfPlayUserTurnContextMiddleware(scenario *config.Scenario) pipeline.Middleware {
	return &selfPlayUserTurnContextMiddleware{scenario: scenario}
}

// Process implements pipeline.Middleware; adds next-turn self-play context metadata.
func (m *selfPlayUserTurnContextMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
	if m.scenario != nil && m.scenario.ID != "" {
		if execCtx.Metadata == nil {
			execCtx.Metadata = make(map[string]interface{})
		}

		completedUserTurns, nextUserTurn := computeUserTurnCounts(execCtx)

		execCtx.Metadata["arena_user_completed_turns"] = completedUserTurns
		execCtx.Metadata["arena_user_next_turn"] = nextUserTurn
		execCtx.Metadata["arena_role"] = "self_play_user"
		execCtx.Metadata["mock_scenario_id"] = m.scenario.ID

		// Backward-compat for mock provider repository selection
		execCtx.Metadata["mock_turn_number"] = nextUserTurn
	}
	return next()
}

// StreamChunk implements pipeline.Middleware; no-op for this middleware.
func (m *selfPlayUserTurnContextMiddleware) StreamChunk(
	execCtx *pipeline.ExecutionContext,
	chunk *providers.StreamChunk,
) error {
	return nil
}

func computeUserTurnCounts(execCtx *pipeline.ExecutionContext) (int, int) { //nolint: gocritic
	// Prefer counters from TurnIndexMiddleware if present
	var completedUserTurns, nextUserTurn int
	if v, ok := execCtx.Metadata["arena_user_completed_turns"].(int); ok {
		completedUserTurns = v
	}
	if v, ok := execCtx.Metadata["arena_user_next_turn"].(int); ok {
		nextUserTurn = v
	}

	// Fallback: compute from messages if missing
	if completedUserTurns == 0 && nextUserTurn == 0 {
		for i := range execCtx.Messages { // index-based to avoid copying values
			if execCtx.Messages[i].Role == "user" {
				completedUserTurns++
			}
		}
		nextUserTurn = completedUserTurns + 1
	}

	return completedUserTurns, nextUserTurn
}
