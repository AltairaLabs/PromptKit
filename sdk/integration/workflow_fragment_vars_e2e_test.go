package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

const handoffFragmentPackJSON = `{
	"id": "handoff-fragment", "version": "1.0.0", "description": "handoff with fragments",
	"fragments": {"policy": "Policy for {{customer}}."},
	"workflow": {
		"version": 1, "entry": "intake",
		"states": {
			"intake": {"prompt_task": "intake", "description": "intake", "on_event": {"Escalate": "specialist"}},
			"specialist": {"prompt_task": "specialist", "description": "specialist"}
		}
	},
	"prompts": {
		"intake": {"id": "intake", "name": "Intake", "system_template": "Intake {{policy}}",
			"variables": [{"name": "customer", "type": "string", "required": true}]},
		"specialist": {"id": "specialist", "name": "Specialist", "system_template": "Specialist {{policy}}",
			"variables": [{"name": "customer", "type": "string", "required": true}]}
	}
}`

func runFragmentHandoffTurn(t *testing.T, setup func(wc *sdk.WorkflowConversation), sendOpts ...sdk.SendOption) []string {
	t.Helper()
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type: "tool_calls",
		ToolCalls: []mock.ToolCall{{Name: "workflow__transition",
			Arguments: map[string]interface{}{"event": "Escalate", "context": "x"}}},
	})
	repo.addTurn("default", 2, mock.Turn{Type: "text", Content: "specialist here"})
	repo.addTurn("default", 3, mock.Turn{Type: "text", Content: "again"})
	rec := &recordingToolProvider{ToolProvider: mock.NewToolProviderWithRepository("mock", "m", false, repo)}

	wc, err := sdk.OpenWorkflow(writePackFile(t, handoffFragmentPackJSON),
		sdk.WithProvider(rec), sdk.WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	setup(wc)

	_, err = wc.Send(context.Background(), "help", sendOpts...)
	require.NoError(t, err)
	require.Equal(t, "specialist", wc.CurrentState())
	return rec.seen()
}

func TestWorkflowHandoff_FragmentRendersWithSetVar(t *testing.T) {
	seen := runFragmentHandoffTurn(t, func(wc *sdk.WorkflowConversation) {
		wc.ActiveConversation().SetVar("customer", "Acme")
	})
	require.GreaterOrEqual(t, len(seen), 2)
	assert.Equal(t, "Intake Policy for Acme.", seen[0])
	assert.Equal(t, "Specialist Policy for Acme.", seen[1])
}

func TestWorkflowHandoff_FragmentRendersWithJSONInput(t *testing.T) {
	seen := runFragmentHandoffTurn(t, func(*sdk.WorkflowConversation) {},
		sdk.WithJSONInput(map[string]any{"customer": "Acme"}))
	require.GreaterOrEqual(t, len(seen), 2)
	assert.Equal(t, "Intake Policy for Acme.", seen[0])
	assert.Equal(t, "Specialist Policy for Acme.", seen[1])
}

const duplexFragmentPackJSON = `{
	"id": "duplex-fragment", "version": "1.0.0", "description": "duplex",
	"fragments": {"policy": "Policy for {{customer}}."},
	"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
		"system_template": "Chat {{policy}}",
		"variables": [{"name": "customer", "type": "string", "required": true}]}}
}`

func TestDuplex_FragmentRendersWithRequiredSetVar(t *testing.T) {
	rec := newRecordingStreamingProvider()
	conv, err := sdk.OpenDuplex(writePackFile(t, duplexFragmentPackJSON), "chat",
		sdk.WithProvider(rec), sdk.WithSkipSchemaValidation(),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	conv.SetVar("customer", "Acme")
	responseCh, err := conv.Response()
	require.NoError(t, err)
	sendDuplexTurn(t, conv, responseCh, "hello")
	instructions := rec.sessionInstructions()
	require.Len(t, instructions, 1)
	assert.Equal(t, "Chat Policy for Acme.", instructions[0])
}

func TestWorkflowHandoff_NextTurnKeepsSetVar(t *testing.T) {
	repo := newTestTurnRepository()
	repo.addTurn("default", 1, mock.Turn{
		Type: "tool_calls",
		ToolCalls: []mock.ToolCall{{Name: "workflow__transition",
			Arguments: map[string]interface{}{"event": "Escalate", "context": "x"}}},
	})
	repo.addTurn("default", 2, mock.Turn{Type: "text", Content: "specialist here"})
	repo.addTurn("default", 3, mock.Turn{Type: "text", Content: "again"})
	rec := &recordingToolProvider{ToolProvider: mock.NewToolProviderWithRepository("mock", "m", false, repo)}
	wc, err := sdk.OpenWorkflow(writePackFile(t, handoffFragmentPackJSON),
		sdk.WithProvider(rec), sdk.WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	wc.ActiveConversation().SetVar("customer", "Acme")
	_, err = wc.Send(context.Background(), "help")
	require.NoError(t, err)
	_, err = wc.Send(context.Background(), "more")
	require.NoError(t, err)
	seen := rec.seen()
	assert.Equal(t, "Specialist Policy for Acme.", seen[len(seen)-1])
}

// TestWorkflowTransition_NextStateKeepsSetVar moves the workflow with
// Transition, as a host does. The next state's conversation keeps the
// variables the application set, and its own WithVariables-style bindings
// (the carried context) are not overwritten by the earlier state's.
func TestWorkflowTransition_NextStateKeepsSetVar(t *testing.T) {
	rec := newRecordingProvider()
	wc, err := sdk.OpenWorkflow(writePackFile(t, handoffFragmentPackJSON),
		sdk.WithProvider(rec), sdk.WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	ctx := context.Background()

	wc.ActiveConversation().SetVar("customer", "Acme")
	_, err = wc.Send(ctx, "hello")
	require.NoError(t, err)
	assert.Equal(t, "Intake Policy for Acme.", rec.system())

	state, err := wc.Transition("Escalate")
	require.NoError(t, err)
	require.Equal(t, "specialist", state)
	_, err = wc.Send(ctx, "more")
	require.NoError(t, err, "the SetVar value was lost on the transition")
	assert.Equal(t, "Specialist Policy for Acme.", rec.system())

	wc.ActiveConversation().SetVar("customer", "Globex")
	_, err = wc.Send(ctx, "again")
	require.NoError(t, err)
	assert.Equal(t, "Specialist Policy for Globex.", rec.system(), "a later SetVar still wins")
}
