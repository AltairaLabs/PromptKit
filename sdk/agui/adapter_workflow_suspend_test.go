package agui

import (
	"context"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// A workflow turn in which the model transitions and, in the same response,
// calls a tool the turn must suspend on. The transition must wait for the
// suspended turn: committing it at the end of Send swapped in a new
// conversation for the destination state, so the answer went to a
// conversation that never made the call.

const suspendWorkflowPackJSON = `{
	"id": "agui-workflow-suspend", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {
		"gather_info": {"id": "gather_info", "name": "Gather", "version": "1.0.0",
			"system_template": "You gather.", "tools": ["get_location", "send_message"]},
		"process": {"id": "process", "name": "Process", "version": "1.0.0", "system_template": "You process."}
	},
	"tools": {
		"get_location": {"name": "get_location", "description": "The user's location",
			"parameters": {"type": "object", "properties": {}}},
		"send_message": {"name": "send_message", "description": "Send a message",
			"parameters": {"type": "object", "properties": {"body": {"type": "string"}}}}
	},
	"workflow": {
		"version": 1,
		"entry": "intake",
		"states": {
			"intake": {"prompt_task": "gather_info", "on_event": {"InfoComplete": "processing"}},
			"processing": {"prompt_task": "process"}
		}
	}
}`

func openSuspendWorkflow(t *testing.T, provider *scriptedProvider) *sdk.WorkflowConversation {
	t.Helper()
	wc, err := sdk.OpenWorkflow(writePack(t, suspendWorkflowPackJSON), sdk.WithProvider(provider), sdk.WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	return wc
}

func transitionCall() types.MessageToolCall {
	return call("c-tr", "workflow__transition", `{"event":"InfoComplete","context":"located"}`)
}

// assertAnsweredOnce checks that the conversation that made callID holds
// exactly one result for it, and that the model was given exactly one.
func assertAnsweredOnce(t *testing.T, conv *sdk.Conversation, provider *scriptedProvider, callID string) {
	t.Helper()
	assert.Equal(t, 1, toolResultCounts(conv.Messages(context.Background()))[callID],
		"the answer reaches the conversation that made the call")
	last := provider.seen[len(provider.seen)-1]
	assert.Equal(t, 1, toolResultCounts(last)[callID], "the model sees the answer once")
}

func TestE2E_WorkflowTransitionWaitsForFrontendTool(t *testing.T) {
	provider := newScriptedProvider(
		say("Moving on.", transitionCall(), call("c-fe", "get_location", `{}`)),
		say("Processing in Paris."),
	)
	wc := openSuspendWorkflow(t, provider)
	caller := wc.ActiveConversation()
	desc, err := caller.ToolRegistry().GetTool("get_location")
	require.NoError(t, err)
	desc.Mode = toolModeClient

	run1, err := sendAndCollect(t, NewWorkflowEventAdapter(wc), "hi")
	require.NoError(t, err)
	assert.Equal(t, "intake", wc.CurrentState(), "the transition waits for the suspended turn")
	assert.True(t, caller == wc.ActiveConversation(), "the conversation holding the call stays active")

	b := NewWorkflowEventAdapter(wc)
	evts, err := continueAndCollect(t, b, run1, func(ctx context.Context) error {
		return b.RunResume(ctx, []ToolResult{{CallID: "c-fe", Result: "Paris"}})
	})
	require.NoError(t, err)

	assertAnsweredOnce(t, caller, provider, "c-fe")
	texts := eventsOf[*aguievents.TextMessageContentEvent](evts)
	require.NotEmpty(t, texts)
	assert.Equal(t, "Processing in Paris.", texts[len(texts)-1].Delta, "the suspended turn completes")
	assert.Equal(t, "processing", wc.CurrentState(), "then the transition applies")
	assert.Equal(t, []stepEvent{
		{aguievents.EventTypeStepStarted, "intake"},
		{aguievents.EventTypeStepFinished, "intake"},
		{aguievents.EventTypeStepStarted, "processing"},
		{aguievents.EventTypeStepFinished, "processing"},
	}, stepEvents(evts))
}

func TestE2E_WorkflowTransitionWaitsForApprovalHold(t *testing.T) {
	provider := newScriptedProvider(
		say("Sending, then moving on.", transitionCall(), call("h1", "send_message", `{"body":"hi"}`)),
		say("Sent; processing now."),
	)
	wc := openSuspendWorkflow(t, provider)
	caller := wc.ActiveConversation()
	caller.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval"}
		},
		func(map[string]any) (any, error) { return map[string]any{"sent": true}, nil },
	)

	run1, err := sendAndCollect(t, NewWorkflowEventAdapter(wc), "send hi")
	require.NoError(t, err)
	finished := run1[len(run1)-1].(*aguievents.RunFinishedEvent)
	require.NotNil(t, finished.Outcome)
	assert.Equal(t, aguievents.RunFinishedOutcomeTypeInterrupt, finished.Outcome.Type)
	assert.Equal(t, "intake", wc.CurrentState())
	require.True(t, caller == wc.ActiveConversation(), "the conversation holding the call stays active")

	_, err = wc.ActiveConversation().ResolveTool(context.Background(), "h1")
	require.NoError(t, err)

	b := NewWorkflowEventAdapter(wc)
	evts, err := continueAndCollect(t, b, run1, b.RunContinue)
	require.NoError(t, err)

	assertAnsweredOnce(t, caller, provider, "h1")
	assert.Equal(t, "h1", eventsOf[*aguievents.ToolCallResultEvent](evts)[0].ToolCallID)
	texts := eventsOf[*aguievents.TextMessageContentEvent](evts)
	require.NotEmpty(t, texts)
	assert.Equal(t, "Sent; processing now.", texts[len(texts)-1].Delta)
	assert.Equal(t, "processing", wc.CurrentState())
	assert.Equal(t, []stepEvent{
		{aguievents.EventTypeStepStarted, "intake"},
		{aguievents.EventTypeStepFinished, "intake"},
		{aguievents.EventTypeStepStarted, "processing"},
		{aguievents.EventTypeStepFinished, "processing"},
	}, stepEvents(evts))
}
