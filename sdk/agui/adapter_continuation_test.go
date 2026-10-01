package agui

import (
	"context"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// These cover runs that continue a thread: the next run's input carries every
// tool message the thread has, not only the answers the turn is waiting for.

// toolResultCounts counts the tool results per call id in what a model call
// was given.
func toolResultCounts(msgs []types.Message) map[string]int {
	counts := map[string]int{}
	for i := range msgs {
		if msgs[i].ToolResult != nil {
			counts[msgs[i].ToolResult.ID]++
		}
	}
	return counts
}

// resultContent is the content of the TOOL_CALL_RESULT for callID in evts.
func resultContent(t *testing.T, evts []aguievents.Event, callID string) string {
	t.Helper()
	for _, r := range eventsOf[*aguievents.ToolCallResultEvent](evts) {
		if r.ToolCallID == callID {
			return r.Content
		}
	}
	t.Fatalf("no TOOL_CALL_RESULT for %s", callID)
	return ""
}

func assistantWithCalls(ids ...string) aguitypes.Message {
	msg := aguitypes.Message{ID: "a1", Role: aguitypes.RoleAssistant}
	for _, id := range ids {
		msg.ToolCalls = append(msg.ToolCalls, aguitypes.ToolCall{
			ID: id, Type: "function", Function: aguitypes.FunctionCall{Name: id, Arguments: "{}"},
		})
	}
	return msg
}

// The model calls a server tool and a frontend tool in one round. The server
// tool's result was emitted in run 1, so the consumer's next input carries a
// tool message for it as well as its own answer. Resolving both handed the
// model the server tool's result twice.
func TestE2E_ResumeIgnoresServerToolResultInInput(t *testing.T) {
	provider := newScriptedProvider(
		say("Checking.", call("c-srv", "lookup", `{"id":"1"}`), call("c-fe", "get_location", `{}`)),
		say("Your order is near Paris."),
	)
	conv := openConv(t, provider)
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "shipped", nil })
	bindClientTool(t, conv, "get_location")

	run1, err := sendAndCollect(t, NewEventAdapter(conv), "where is it?")
	require.NoError(t, err)

	input := []aguitypes.Message{
		{ID: "u1", Role: aguitypes.RoleUser, Content: "where is it?"},
		assistantWithCalls("c-srv", "c-fe"),
		{ID: "t1", Role: aguitypes.RoleTool, ToolCallID: "c-srv", Content: resultContent(t, run1, "c-srv")},
		{ID: "t2", Role: aguitypes.RoleTool, ToolCallID: "c-fe", Content: `{"city":"Paris"}`},
	}
	b := NewEventAdapter(conv)
	_, err = continueAndCollect(t, b, run1, func(ctx context.Context) error {
		return b.RunResume(ctx, ToolResultsFromAGUI(input))
	})
	require.NoError(t, err)

	last := provider.seen[len(provider.seen)-1]
	assert.Equal(t, map[string]int{"c-srv": 1, "c-fe": 1}, toolResultCounts(last),
		"each call is answered exactly once in what the model sees")
}

// A ToolResultProvider answered one of two pending calls in run 1; the input
// of run 2 carries that answer again alongside the application's.
func TestE2E_ResumeIgnoresProviderAnsweredCallInInput(t *testing.T) {
	provider := newScriptedProvider(
		say("Two things.", call("c1", "get_location", `{}`), call("c2", "send_message", `{"body":"hi"}`)),
		say("All done."),
	)
	conv := openConv(t, provider)
	bindClientTool(t, conv, "get_location")
	bindClientTool(t, conv, "send_message")

	answerC1 := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "c1", Result: "Paris"}}, nil
	}
	run1, err := sendAndCollect(t, NewEventAdapter(conv, WithToolResultProvider(answerC1)), "go")
	require.NoError(t, err)

	input := []aguitypes.Message{
		{ID: "u1", Role: aguitypes.RoleUser, Content: "go"},
		assistantWithCalls("c1", "c2"),
		{ID: "t1", Role: aguitypes.RoleTool, ToolCallID: "c1", Content: resultContent(t, run1, "c1")},
		{ID: "t2", Role: aguitypes.RoleTool, ToolCallID: "c2", Content: "sent"},
	}
	b := NewEventAdapter(conv)
	_, err = continueAndCollect(t, b, run1, func(ctx context.Context) error {
		return b.RunResume(ctx, ToolResultsFromAGUI(input))
	})
	require.NoError(t, err)

	last := provider.seen[len(provider.seen)-1]
	assert.Equal(t, map[string]int{"c1": 1, "c2": 1}, toolResultCounts(last))
}

// Two calls held for approval: Continue feeds each result to the model in
// turn, and every one of them is reported, with the replies between them.
func TestE2E_ContinueReportsEveryHeldResult(t *testing.T) {
	provider := newScriptedProvider(
		say("Sending both.", call("h1", "send_message", `{"body":"a"}`), call("h2", "send_message", `{"body":"b"}`)),
		say("First sent."),
		say("Both sent."),
	)
	conv := openConv(t, provider)
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval"}
		},
		func(args map[string]any) (any, error) { return map[string]any{"sent": args["body"]}, nil },
	)

	run1, err := sendAndCollect(t, NewEventAdapter(conv), "send both")
	require.NoError(t, err)
	finished := run1[len(run1)-1].(*aguievents.RunFinishedEvent)
	require.NotNil(t, finished.Outcome)
	require.Len(t, finished.Outcome.Interrupts, 2)
	for _, in := range finished.Outcome.Interrupts {
		_, err = conv.ResolveTool(context.Background(), in.ID)
		require.NoError(t, err)
	}

	b := NewEventAdapter(conv)
	evts, err := continueAndCollect(t, b, run1, b.RunContinue)
	require.NoError(t, err)

	var ids []string
	for _, r := range eventsOf[*aguievents.ToolCallResultEvent](evts) {
		ids = append(ids, r.ToolCallID)
	}
	assert.ElementsMatch(t, []string{"h1", "h2"}, ids, "every held call's result is reported")
	texts := eventsOf[*aguievents.TextMessageContentEvent](evts)
	require.NotEmpty(t, texts)
	assert.Equal(t, "Both sent.", texts[len(texts)-1].Delta)
}

const clientWorkflowPackJSON = `{
	"id": "agui-workflow-client", "version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {
		"gather_info": {"id": "gather_info", "name": "Gather", "version": "1.0.0",
			"system_template": "You gather.", "tools": ["get_location"]},
		"process": {"id": "process", "name": "Process", "version": "1.0.0", "system_template": "You process."}
	},
	"tools": {"get_location": {"name": "get_location", "description": "The user's location",
		"parameters": {"type": "object", "properties": {}}}},
	"workflow": {
		"version": 1,
		"entry": "intake",
		"states": {
			"intake": {"prompt_task": "gather_info", "on_event": {"InfoComplete": "processing"}},
			"processing": {"prompt_task": "process", "control": "user"}
		}
	}
}`

// A transition the model makes in a resumed turn is applied, as it is after
// Send, and the resumed run shows the step change.
func TestE2E_WorkflowTransitionAfterResumeIsCommitted(t *testing.T) {
	provider := newScriptedProvider(
		say("Where are you?", call("c-fe", "get_location", `{}`)),
		say("Thanks.", call("c-tr", "workflow__transition", `{"event":"InfoComplete","context":"located"}`)),
		say("Processing."),
	)
	wc, err := sdk.OpenWorkflow(writePack(t, clientWorkflowPackJSON), sdk.WithProvider(provider), sdk.WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })
	desc, err := wc.ActiveConversation().ToolRegistry().GetTool("get_location")
	require.NoError(t, err)
	desc.Mode = toolModeClient

	run1, err := sendAndCollect(t, NewWorkflowEventAdapter(wc), "hi")
	require.NoError(t, err)

	b := NewWorkflowEventAdapter(wc)
	evts, err := continueAndCollect(t, b, run1, func(ctx context.Context) error {
		return b.RunResume(ctx, []ToolResult{{CallID: "c-fe", Result: "Paris"}})
	})
	require.NoError(t, err)

	assert.Equal(t, "processing", wc.CurrentState(), "the transition is applied")
	assert.Equal(t, []stepEvent{
		{aguievents.EventTypeStepStarted, "intake"},
		{aguievents.EventTypeStepFinished, "intake"},
		{aguievents.EventTypeStepStarted, "processing"},
		{aguievents.EventTypeStepFinished, "processing"},
	}, stepEvents(evts))
}
