package integration

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// recordingToolProvider is the mock tool provider, recording the system
// prompt of every round it is asked for.
type recordingToolProvider struct {
	*mock.ToolProvider
	mu      sync.Mutex
	systems []string
}

func (r *recordingToolProvider) record(req providers.PredictionRequest) {
	r.mu.Lock()
	r.systems = append(r.systems, req.System)
	r.mu.Unlock()
}

func (r *recordingToolProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	r.record(req)
	return r.ToolProvider.PredictWithTools(ctx, req, tools, choice)
}

func (r *recordingToolProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, choice string,
) (<-chan providers.StreamChunk, error) {
	r.record(req)
	return r.ToolProvider.PredictStreamWithTools(ctx, req, tools, choice)
}

func (r *recordingToolProvider) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.systems...)
}

const handoffVarsPackJSON = `{
	"id": "handoff-vars", "version": "1.0.0", "description": "handoff with variables",
	"workflow": {
		"version": 1, "entry": "intake",
		"states": {
			"intake": {"prompt_task": "intake", "description": "intake", "on_event": {"Escalate": "specialist"}},
			"specialist": {"prompt_task": "specialist", "description": "specialist"}
		}
	},
	"prompts": {
		"intake": {"id": "intake", "name": "Intake", "system_template": "Intake for {{customer}}."},
		"specialist": {"id": "specialist", "name": "Specialist", "system_template": "Specialist for {{customer}}."}
	}
}`

// TestWorkflowHandoff_DestinationPromptSeesTheConversationVariables lets the
// model move to the next state mid-turn. The round after the handoff runs on
// the destination prompt, rendered with the same variables the conversation
// renders with, never the raw template.
func TestWorkflowHandoff_DestinationPromptSeesTheConversationVariables(t *testing.T) {
	sources := map[string]sdk.Option{
		"with_variables":    sdk.WithVariables(map[string]string{"customer": "Acme"}),
		"variable_provider": sdk.WithVariableProvider(staticVars{"customer": "Acme"}),
	}
	for name, varOpt := range sources {
		t.Run(name, func(t *testing.T) {
			seen := runHandoffVarsCase(t, varOpt)
			require.GreaterOrEqual(t, len(seen), 2, "the turn has a round after the handoff")
			assert.Equal(t, "Intake for Acme.", seen[0])
			assert.Contains(t, seen[1], "Specialist for Acme.",
				"the handoff round did not render the destination prompt with the conversation's variables")
			assert.NotContains(t, seen[1], "{{", "a raw template reached the model")
		})
	}
}

// runHandoffVarsCase runs one turn in which the model moves the workflow to
// the next state, and returns the system prompt of every round.
func runHandoffVarsCase(t *testing.T, varOpt sdk.Option) []string {
	t.Helper()
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type: "tool_calls",
		ToolCalls: []mock.ToolCall{{Name: "workflow__transition",
			Arguments: map[string]interface{}{"event": "Escalate", "context": "needs a specialist"}}},
	})
	repo.addTurn("default", 2, mock.Turn{Type: "text", Content: "specialist here"})
	rec := &recordingToolProvider{ToolProvider: mock.NewToolProviderWithRepository("mock", "m", false, repo)}

	packPath := writePackFile(t, handoffVarsPackJSON)
	wc, err := sdk.OpenWorkflow(packPath,
		sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(),
		varOpt,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })

	_, err = wc.Send(context.Background(), "I need more help")
	require.NoError(t, err)
	require.Equal(t, "specialist", wc.CurrentState())

	return rec.seen()
}
