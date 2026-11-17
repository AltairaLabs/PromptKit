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

// "any" (Claude's name for a required call) is a forced choice like
// "required": it is kept across a rebuild that still offers a tool.
func TestForcedChoice_AnyIsKeptWhileAToolIsOffered(t *testing.T) {
	provider := &choiceProvider{ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil)}
	turnState := NewTurnState()
	turnState.SystemPrompt = "ORIGIN"
	turnState.AllowedTools = []string{"lookup_order", "cancel_order"}
	stg := NewProviderStageWithTurnState(provider, mockToolRegistry(t, "lookup_order", "cancel_order"),
		&pipeline.ToolPolicy{ToolChoice: "any"}, &ProviderConfig{MaxTokens: 100}, nil, nil, turnState)
	stg.SetWorkflowStateResolver(&fakeResolver{sequence: []Handoff{
		{Valid: true, SystemPrompt: "DESTINATION", AllowedTools: []string{"lookup_order"}},
	}})

	runHandoffTurn(t, stg)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Equal(t, []string{"any"}, provider.choices)
}

// "any" falls back to auto when a rebuild leaves no tool to call.
func TestForcedChoice_AnyWithNoToolsLeftFallsBackToAuto(t *testing.T) {
	stg := NewProviderStage(mock.NewToolProvider("mock", "mock-model", false, nil),
		mockToolRegistry(t, "lookup_order"), &pipeline.ToolPolicy{ToolChoice: "any"}, &ProviderConfig{})
	loop, err := stg.newToolLoop(&providerInput{
		allowedTools: []string{"lookup_order"}, messages: []types.Message{{Role: "user", Content: "go"}},
	})
	require.NoError(t, err)
	require.Equal(t, "any", loop.toolChoice)
	rebuilt, _, err := stg.buildProviderTools(nil, nil)
	require.NoError(t, err)
	loop.useTools(rebuilt)
	assert.Equal(t, "auto", loop.toolChoice)
}

// A tools array built for a provider that cannot take tools is not sent, so
// it offers no tool a forced choice could name.
func TestForcedChoice_NothingIsBuiltForAProviderWithoutTools(t *testing.T) {
	stg := NewProviderStage(mock.NewToolProvider("mock", "mock-model", false, nil),
		mockToolRegistry(t, "lookup_order"), nil, &ProviderConfig{})
	r := stg.runner()
	offer := r.collectDescriptors([]string{"lookup_order"}, nil)
	_, _, err := stg.buildProviderToolsFrom(offer, nil)
	require.NoError(t, err)
	assert.False(t, r.choiceFits("lookup_order"))
	assert.False(t, r.choiceFits("required"))
}

// A forced choice is the host's policy: a nudged round's retry keeps it, also
// when it names the tool that looped (changing the arguments is the
// correction).
func TestNudgedRound_ForcedChoiceIsKeptForTheRetry(t *testing.T) {
	same := json.RawMessage(`{"id":"A"}`)
	for name, choice := range map[string]string{"named": "lookup_order", "required": "required", "any": "any"} {
		t.Run(name, func(t *testing.T) {
			provider := &choiceProvider{
				ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
				script: [][]types.MessageToolCall{{
					{ID: "c1", Name: "lookup_order", Args: same}, {ID: "c2", Name: "lookup_order", Args: same},
				}},
			}
			stg := NewProviderStage(provider, mockToolRegistry(t, "lookup_order"),
				&pipeline.ToolPolicy{MaxIdenticalToolCalls: 2, ToolChoice: choice}, &ProviderConfig{MaxTokens: 100})
			turnState := NewTurnState()
			turnState.AllowedTools = []string{"lookup_order"}
			stg.turnState = turnState

			runHandoffTurn(t, stg)

			provider.mu.Lock()
			defer provider.mu.Unlock()
			assert.Equal(t, []string{choice, choice}, provider.choices)
		})
	}
}

// A forced choice of a tool that did not loop is kept for the retry.
func TestNudgedRound_ForcedChoiceOfAnotherToolIsKept(t *testing.T) {
	same := json.RawMessage(`{"id":"A"}`)
	provider := &choiceProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
		script: [][]types.MessageToolCall{{
			{ID: "c1", Name: "lookup_order", Args: same}, {ID: "c2", Name: "lookup_order", Args: same},
		}},
	}
	stg := NewProviderStage(provider, mockToolRegistry(t, "lookup_order", "cancel_order"),
		&pipeline.ToolPolicy{MaxIdenticalToolCalls: 2, ToolChoice: "cancel_order"}, &ProviderConfig{MaxTokens: 100})
	turnState := NewTurnState()
	turnState.AllowedTools = []string{"lookup_order", "cancel_order"}
	stg.turnState = turnState

	runHandoffTurn(t, stg)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Equal(t, []string{"cancel_order", "cancel_order"}, provider.choices)
}

// A round's results are placed by each call's position, also when calls share
// an ID or have none.
func TestRunRound_ResultsByCallPositionWithSharedIDs(t *testing.T) {
	stg := NewProviderStage(mock.NewToolProvider("mock", "mock-model", false, nil),
		mockToolRegistry(t, "lookup_order", "cancel_order"), &pipeline.ToolPolicy{MaxIdenticalToolCalls: 2},
		&ProviderConfig{})
	stg.offerState().setAllowed([]string{"lookup_order", "cancel_order"})
	a := json.RawMessage(`{"id":"A"}`)
	ctx := context.Background()

	for name, id := range map[string]string{"shared": "x", "empty": ""} {
		t.Run(name, func(t *testing.T) {
			turn := stg.runner().newTurn(nil, 10)
			turn.RunRound(ctx, []types.MessageToolCall{{ID: id, Name: "lookup_order", Args: a}}, roundRef{round: 1})
			out := turn.RunRound(ctx, []types.MessageToolCall{
				{ID: id, Name: "lookup_order", Args: a}, {ID: id, Name: "cancel_order", Args: a},
			}, roundRef{round: 2})
			require.Len(t, out.Results, 2)
			assert.Equal(t, "lookup_order", out.Results[0].ToolResult.Name)
			assert.Contains(t, out.Results[0].ToolResult.GetTextContent(), "Repeated call ignored")
			assert.Equal(t, "cancel_order", out.Results[1].ToolResult.Name)
		})
	}
}

// "required" is kept for a nudged round's retry while a tool that did not
// loop is offered.
func TestNudgedRound_RequiredIsKeptWhileAnotherToolIsOffered(t *testing.T) {
	same := json.RawMessage(`{"id":"A"}`)
	provider := &choiceProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
		script: [][]types.MessageToolCall{{
			{ID: "c1", Name: "lookup_order", Args: same}, {ID: "c2", Name: "lookup_order", Args: same},
		}},
	}
	stg := NewProviderStage(provider, mockToolRegistry(t, "lookup_order", "cancel_order"),
		&pipeline.ToolPolicy{MaxIdenticalToolCalls: 2, ToolChoice: "required"}, &ProviderConfig{MaxTokens: 100})
	turnState := NewTurnState()
	turnState.AllowedTools = []string{"lookup_order", "cancel_order"}
	stg.turnState = turnState

	runHandoffTurn(t, stg)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	assert.Equal(t, []string{"required", "required"}, provider.choices)
}
