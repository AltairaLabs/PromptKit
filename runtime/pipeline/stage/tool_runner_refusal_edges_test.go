package stage

import (
	"context"
	"encoding/json"
	"fmt"
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

// A refused call repeated past the repeated-call threshold is answered with
// its refusal every time: it never ran, so it neither gets the nudge nor uses
// it up. A later genuine loop of a tool that runs still gets its one nudge.
func TestToolLoop_RepeatedRefusedCallLeavesTheNudge(t *testing.T) {
	f := newUnofferedFixture(t, mock.NewToolProvider("test", "model", false, nil), "")
	call := func(id, name string) types.MessageToolCall {
		return types.MessageToolCall{ID: id, Name: name, Args: json.RawMessage(`{"q":1}`)}
	}
	for round := 1; round <= 4; round++ {
		id := fmt.Sprintf("u%d", round)
		assert.Equal(t, []types.Message{policyRefusal(id, "unlisted", unofferedRefusalText)},
			f.playRound(t, round, call(id, "unlisted")), "round %d", round)
	}
	f.playRound(t, 5, call("f1", "fine"))
	f.playRound(t, 6, call("f2", "fine"))

	got := f.playRound(t, 7, call("f3", "fine"))

	assert.Equal(t, []types.Message{types.NewToolResultMessage(types.NewTextToolResult("f3", "fine",
		`Repeated call ignored: "fine" has been called 3 times with identical arguments and was NOT executed `+
			"again — the result will not change. Stop repeating this exact call: change the arguments, use a "+
			"different tool, or finish the task."))}, got, "the genuine loop is nudged")
}

// A workflow state whose allowed tools select the turn's tools (the same
// names in another order, none where the turn has none, or the names a
// wildcard of the turn's matches) is offered the same tools: the tools array
// is not rebuilt for it.
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
		"wildcard versus its names": {
			turn:  []string{"mcp__kb__*"},
			state: []string{"mcp__kb__get", "mcp__kb__search"},
			call:  "mcp__kb__get",
		},
	} {
		t.Run(name, func(t *testing.T) {
			registry := registryWithTools(t, "workflow__transition", "origin_tool", "mcp__kb__get", "mcp__kb__search")
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
