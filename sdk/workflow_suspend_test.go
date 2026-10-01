package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const suspendWorkflowPack = `{
	"id": "workflow-suspend", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {
		"gather_info": {"id": "gather_info", "name": "Gather", "version": "1.0.0",
			"system_template": "You gather.", "tools": ["locate"]},
		"process": {"id": "process", "name": "Process", "version": "1.0.0", "system_template": "You process."}
	},
	"tools": {"locate": {"name": "locate", "description": "Client tool",
		"parameters": {"type": "object", "properties": {}}}},
	"workflow": {
		"version": 1,
		"entry": "intake",
		"states": {
			"intake": {"prompt_task": "gather_info", "on_event": {"InfoComplete": "processing"}},
			"processing": {"prompt_task": "process"}
		}
	}
}`

// The model transitions and calls a client tool in one response. The turn
// suspends on the client call, so the transition must wait: the answer goes to
// the conversation that made the call, and the resumed turn applies the
// transition.
func TestWorkflowSend_SuspendedTurnDefersTransition(t *testing.T) {
	provider := &roundsProvider{
		ToolProvider: mock.NewToolProvider("x", "m", false, nil),
		rounds: []providers.PredictionResponse{
			{Content: "Moving on.", ToolCalls: []types.MessageToolCall{
				{ID: "c-tr", Name: "workflow__transition", Args: []byte(`{"event":"InfoComplete","context":"located"}`)},
				{ID: "c-cli", Name: "locate", Args: []byte(`{}`)},
			}},
			{Content: "Processing."},
		},
	}
	wc, err := OpenWorkflow(writeWorkflowTestPack(t, suspendWorkflowPack), WithProvider(provider), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	caller := wc.ActiveConversation()
	desc, err := caller.ToolRegistry().GetTool("locate")
	require.NoError(t, err)
	desc.Mode = "client"
	ctx := context.Background()

	resp, err := wc.Send(ctx, "hi")
	require.NoError(t, err)
	require.True(t, resp.HasPendingClientTools())
	assert.Equal(t, "intake", wc.CurrentState())
	require.True(t, caller == wc.ActiveConversation(), "the conversation holding the call stays active")

	require.NoError(t, caller.SendToolResult(ctx, "c-cli", "Paris"))
	resp, err = caller.Resume(ctx)
	require.NoError(t, err)

	assert.Equal(t, "Processing.", resp.Text())
	assert.Equal(t, "processing", wc.CurrentState())
	assert.Equal(t, 1, resultCounts(provider.seen[len(provider.seen)-1])["c-cli"])
}
