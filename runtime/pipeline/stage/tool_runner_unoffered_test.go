package stage

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// unofferedLoop returns a text turn's tool loop over a registry holding
// "offered" and "unlisted", whose prompt allows only "offered", with every
// call to "offered" denied by a tool hook.
func unofferedLoop(t *testing.T) *toolLoop {
	t.Helper()
	registry := tools.NewRegistry()
	for _, name := range []string{"offered", "unlisted"} {
		require.NoError(t, registry.Register(&tools.ToolDescriptor{
			Name: name, Description: name, InputSchema: json.RawMessage(`{"type":"object"}`), Mode: "mock",
		}))
	}
	hookReg := hooks.NewRegistry(hooks.WithToolHook(&denyToolHook{blockedTool: "offered", reason: "denied"}))
	stage := NewProviderStageWithHooks(mock.NewToolProvider("test", "model", false, nil), registry, nil,
		&ProviderConfig{}, nil, hookReg)
	loop, err := stage.newToolLoop(&providerInput{
		allowedTools: []string{"offered"},
		messages:     []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	loop.maxRounds = 10
	return loop
}

// playCallRound plays one provider round of calls to name through the loop and
// returns the round's tool results.
func playCallRound(t *testing.T, loop *toolLoop, round int, id, name string) []types.Message {
	t.Helper()
	before := len(loop.messages)
	response := types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{
		{ID: id, Name: name, Args: json.RawMessage(`{"n":"` + id + `"}`)}, // distinct: no repeated-call nudge
	}}
	done, _, err := loop.afterRound(context.Background(), []string{"offered"}, &response, true, roundRef{round: round})
	require.NoError(t, err)
	require.False(t, done)
	return loop.messages[before+1:]
}

// A registered tool the prompt does not allow is refused on a text turn: it
// is not in the tools array the model was given.
func TestToolLoop_UnofferedRegisteredToolIsRefused(t *testing.T) {
	loop := unofferedLoop(t)

	got := playCallRound(t, loop, 1, "c1", "unlisted")

	assert.Equal(t, []types.Message{
		policyRefusal("c1", "unlisted", "Tool unlisted was not run: it is not available this turn"),
	}, got)
}

// A tool excluded for the turn after repeated rejections was offered: a
// further call to it takes its usual path (here the hook denies it again),
// not the refusal of a tool never offered.
func TestToolLoop_ExcludedToolKeepsItsPath(t *testing.T) {
	loop := unofferedLoop(t)
	playCallRound(t, loop, 1, "c1", "offered")
	playCallRound(t, loop, 2, "c2", "offered")
	require.True(t, loop.excluded["offered"])

	got := playCallRound(t, loop, 3, "c3", "offered")

	assert.Equal(t, []types.Message{
		policyRefusal("c3", "offered", "Tool offered blocked by hook: denied"),
	}, got)
}
