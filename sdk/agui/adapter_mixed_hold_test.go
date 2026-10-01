package agui

import (
	"context"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// One round leaves both an approval-held call and a client call pending. The
// ToolResultProvider can answer the client call, but the turn cannot resume
// while the held call is unanswered: the model would get a history with a tool
// call and no result. The run ends with the hold's interrupt instead.
func TestE2E_ProviderDoesNotResumePastAnApprovalHold(t *testing.T) {
	provider := newScriptedProvider(
		say("Two things.", call("c1", "get_location", `{}`), call("h1", "send_message", `{"body":"hi"}`)),
		say("Done."),
	)
	conv := openConv(t, provider)
	bindClientTool(t, conv, "get_location")
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval"}
		},
		func(args map[string]any) (any, error) { return args["body"], nil },
	)
	answer := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "c1", Result: "here"}}, nil
	}

	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithToolResultProvider(answer)), "do both")
	require.NoError(t, err)

	assert.NotContains(t, eventTypes(evts), aguievents.EventTypeRunError)
	assert.Len(t, provider.seen, 1, "the turn must not resume while a call is held for approval")
	finished, ok := evts[len(evts)-1].(*aguievents.RunFinishedEvent)
	require.True(t, ok, "the run ends with RUN_FINISHED")
	require.NotNil(t, finished.Outcome)
	assert.Equal(t, aguievents.RunFinishedOutcomeTypeInterrupt, finished.Outcome.Type)
	require.Len(t, finished.Outcome.Interrupts, 1)
	assert.Equal(t, "h1", finished.Outcome.Interrupts[0].ToolCallID)
}
