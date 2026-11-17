package stage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A DescribeTool that renames a tool offers the model the new name only: a
// call to the registry name it no longer advertises is refused. The
// advertised name is not a registered tool, so a call to it keeps the
// registry's not-found error.
func TestToolLoop_RenamedToolsOriginalNameIsRefused(t *testing.T) {
	reg := registryWithTools(t, "fine", "kb_search")
	exec := &grantingExec{}
	reg.RegisterExecutor(exec)
	allowed := []string{"fine", "kb_search"}
	provider := &scriptedRoundsProvider{ToolProvider: mock.NewToolProvider("test", "model", false, nil)}
	stage := NewProviderStage(provider, reg, nil, &ProviderConfig{
		DescribeTool: func(d *tools.ToolDescriptor) *tools.ToolDescriptor {
			if d.Name != "kb_search" {
				return d
			}
			renamed := *d
			renamed.Name = "kb_search_v2"
			return &renamed
		},
	})
	loop, err := stage.newToolLoop(&providerInput{
		allowedTools: allowed, messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	loop.maxRounds = 10

	_, got := play(t, loop, allowed, 1,
		unofferedCall("c1", "kb_search"), unofferedCall("c2", "kb_search_v2"), unofferedCall("c3", "fine"))

	require.Equal(t, [][]string{{"fine", "kb_search_v2"}}, provider.builds, "the model is offered the new name")
	require.Len(t, got, 3)
	assert.Equal(t, policyRefusal("c1", "kb_search", "Tool kb_search was not run: it is not available this turn"),
		got[0])
	assert.Equal(t, "Error: tool not found: kb_search_v2", got[1].ToolResult.GetTextContent())
	assert.Equal(t, okText("c3", "fine", "ok"), got[2])
	assert.Equal(t, []string{"fine"}, exec.calls(), "the unadvertised name never runs")
}

// A call the round refuses that is also a repeated identical call gets its
// refusal, not the repeated-call nudge saying its result would not change:
// it never ran. It still counts toward the repeated-call breaker.
func TestToolLoop_RepeatedRefusedCallGetsItsRefusal(t *testing.T) {
	f := newUnofferedFixture(t, mock.NewToolProvider("test", "model", false, nil), "")
	same := func(id string) types.MessageToolCall {
		return types.MessageToolCall{ID: id, Name: "unlisted", Args: json.RawMessage(`{"q":1}`)}
	}
	f.playRound(t, 1, same("c1"))
	f.playRound(t, 2, same("c2"))

	got := f.playRound(t, 3, same("c3"))

	assert.Equal(t, []types.Message{policyRefusal("c3", "unlisted", unofferedRefusalText)}, got)
	response := types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{same("c4")}}
	_, _, err := f.loop.afterRound(context.Background(), unofferedAllowed, &response, true, roundRef{round: 4})
	require.Error(t, err)
	assert.Equal(t, `provider stage: tool loop persisted after a self-correction nudge: "unlisted" called with `+
		`identical arguments`, err.Error())
}

// A workflow state whose allowed tools are the turn's in another order, or
// none where the turn has none, is the same set of tools: the tools array is
// not rebuilt for it.
func TestToolLoop_HandoffWithTheSameToolSetDoesNotRebuild(t *testing.T) {
	for name, tc := range map[string]struct {
		turn, state []string
		call        string
	}{
		"reordered": {
			turn:  []string{"workflow__transition", "origin_tool"},
			state: []string{"origin_tool", "workflow__transition"},
			call:  "origin_tool",
		},
		"nil versus empty": {turn: nil, state: []string{}, call: "workflow__transition"},
	} {
		t.Run(name, func(t *testing.T) {
			registry := registryWithTools(t, "workflow__transition", "origin_tool")
			registry.RegisterExecutor(&grantingExec{})
			provider := &scriptedRoundsProvider{
				ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
				script:       [][]types.MessageToolCall{{unofferedCall("c1", tc.call)}},
			}
			turnState := NewTurnState()
			turnState.SystemPrompt = "ORIGIN PROMPT"
			turnState.AllowedTools = tc.turn
			stage := NewProviderStageWithTurnState(provider, registry, nil, &ProviderConfig{MaxTokens: 100}, nil, nil,
				turnState)
			stage.SetWorkflowStateResolver(&fakeResolver{sequence: []Handoff{
				{Valid: true, SystemPrompt: "ORIGIN PROMPT", AllowedTools: tc.state},
			}})

			runHandoffTurn(t, stage)

			require.Equal(t, 2, provider.rounds)
			assert.Len(t, provider.builds, 1, "the tools array was rebuilt for the same tools")
		})
	}
}

// The stage's executeToolCalls decides the round's refusals as a round does:
// a call to a tool the turn does not offer is refused through it too.
func TestProviderStage_ExecuteToolCallsRefusesAnUnofferedTool(t *testing.T) {
	f := newUnofferedFixture(t, mock.NewToolProvider("test", "model", false, nil), "")

	got, err := f.loop.stage.executeToolCalls(context.Background(),
		[]types.MessageToolCall{unofferedCall("c1", "unlisted")}, roundRef{round: 1})

	require.NoError(t, err)
	assert.Equal(t, []types.Message{policyRefusal("c1", "unlisted", unofferedRefusalText)}, got)
}
