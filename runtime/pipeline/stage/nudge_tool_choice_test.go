package stage

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// choiceProvider plays a script of tool-call rounds, then answers, recording
// the tool choice it was given on each round.
type choiceProvider struct {
	*mock.ToolProvider
	mu      sync.Mutex
	script  [][]types.MessageToolCall
	choices []string
}

func (p *choiceProvider) SupportsStreaming() bool { return false }

func (p *choiceProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.choices = append(p.choices, choice)
	if len(p.script) == 0 {
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
	calls := p.script[0]
	p.script = p.script[1:]
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

func mockToolRegistry(t *testing.T, names ...string) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	for _, n := range names {
		require.NoError(t, reg.Register(&tools.ToolDescriptor{
			Name: n, Description: n, InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "mock",
		}))
	}
	return reg
}

// A forced tool choice kept by a nudged round is checked against the round's
// final tools array: a workflow handoff after the round that drops the forced
// tool makes the choice fall back to auto.
func TestNudgedRound_ForcedChoiceDroppedByAHandoffFallsBackToAuto(t *testing.T) {
	same := json.RawMessage(`{"id":"A"}`)
	provider := &choiceProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
		script: [][]types.MessageToolCall{{
			{ID: "c1", Name: "lookup_order", Args: same}, {ID: "c2", Name: "lookup_order", Args: same},
		}},
	}
	turnState := NewTurnState()
	turnState.SystemPrompt = "ORIGIN"
	turnState.AllowedTools = []string{"lookup_order", "cancel_order"}
	stg := NewProviderStageWithTurnState(provider, mockToolRegistry(t, "lookup_order", "cancel_order"),
		&pipeline.ToolPolicy{MaxIdenticalToolCalls: 2, ToolChoice: "cancel_order"},
		&ProviderConfig{MaxTokens: 100}, nil, nil, turnState)
	stg.SetWorkflowStateResolver(&fakeResolver{sequence: []Handoff{
		{Valid: true, SystemPrompt: "ORIGIN", AllowedTools: []string{"lookup_order", "cancel_order"}},
		{Valid: true, SystemPrompt: "DESTINATION", AllowedTools: []string{"lookup_order"}},
	}})

	runHandoffTurn(t, stg)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Equal(t, []string{"cancel_order", "auto"}, provider.choices)
}

// A round the breaker answers entirely runs nothing, so it needs no registry.
func TestNudgedRound_NudgeOnlyRoundWithoutARegistry(t *testing.T) {
	provider := mock.NewToolProvider("test", "model", false, nil)
	stg := NewProviderStage(provider, nil, &pipeline.ToolPolicy{MaxIdenticalToolCalls: 1}, &ProviderConfig{})
	loop, err := stg.newToolLoop(&providerInput{messages: []types.Message{{Role: "user", Content: "go"}}})
	require.NoError(t, err)

	resp := &types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{
		{ID: "c1", Name: "lookup_order", Args: json.RawMessage(`{}`)},
	}}
	done, _, loopErr := loop.afterRound(context.Background(), nil, resp, true, roundRef{round: 1})
	require.NoError(t, loopErr)
	assert.False(t, done)
	last := loop.messages[len(loop.messages)-1]
	require.NotNil(t, last.ToolResult)
	assert.Equal(t, "Repeated call ignored: \"lookup_order\" has been called 1 times with identical arguments and "+
		"was NOT executed again — the result will not change. Stop repeating this exact call: change the "+
		"arguments, use a different tool, or finish the task.", last.ToolResult.GetTextContent())
}

// A forced tool choice is checked wherever the tools array is rebuilt: a
// handoff before the first round that drops the forced tool sends auto.
func TestForcedChoice_DroppedByTheTurnsFirstHandoffFallsBackToAuto(t *testing.T) {
	provider := &choiceProvider{ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil)}
	turnState := NewTurnState()
	turnState.SystemPrompt = "ORIGIN"
	turnState.AllowedTools = []string{"lookup_order", "cancel_order"}
	stg := NewProviderStageWithTurnState(provider, mockToolRegistry(t, "lookup_order", "cancel_order"),
		&pipeline.ToolPolicy{ToolChoice: "cancel_order"}, &ProviderConfig{MaxTokens: 100}, nil, nil, turnState)
	stg.SetWorkflowStateResolver(&fakeResolver{sequence: []Handoff{
		{Valid: true, SystemPrompt: "DESTINATION", AllowedTools: []string{"lookup_order"}},
	}})

	runHandoffTurn(t, stg)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Equal(t, []string{"auto"}, provider.choices)
}

// A handoff that keeps the forced tool keeps the choice.
func TestForcedChoice_KeptByAHandoffThatKeepsTheTool(t *testing.T) {
	provider := &choiceProvider{ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil)}
	turnState := NewTurnState()
	turnState.SystemPrompt = "ORIGIN"
	turnState.AllowedTools = []string{"lookup_order", "cancel_order"}
	stg := NewProviderStageWithTurnState(provider, mockToolRegistry(t, "lookup_order", "cancel_order"),
		&pipeline.ToolPolicy{ToolChoice: "cancel_order"}, &ProviderConfig{MaxTokens: 100}, nil, nil, turnState)
	stg.SetWorkflowStateResolver(&fakeResolver{sequence: []Handoff{
		{Valid: true, SystemPrompt: "DESTINATION", AllowedTools: []string{"cancel_order"}},
	}})

	runHandoffTurn(t, stg)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Equal(t, []string{"cancel_order"}, provider.choices)
}
