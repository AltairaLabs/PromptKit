package agui

import (
	"context"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// Some providers number call ids per response (Gemini: call_0, call_1, ...),
// so two rounds of one turn can both make a call_0. Each is its own call and
// gets its own result; a result is only a duplicate within its round.

func resultIDs(evts []aguievents.Event) []string {
	var ids []string
	for _, r := range eventsOf[*aguievents.ToolCallResultEvent](evts) {
		ids = append(ids, r.ToolCallID+"="+r.Content)
	}
	return ids
}

func TestRunSend_CallIDReusedAcrossRounds(t *testing.T) {
	turn := []types.Message{
		assistantMsg("First.", call("call_0", "lookup", `{"id":"1"}`)),
		toolMsg("call_0", "one"),
		assistantMsg("Second.", call("call_0", "lookup", `{"id":"2"}`)),
		toolMsg("call_0", "two"),
		assistantMsg("Done."),
	}
	resp := sdk.NewResponseForTest("Done.", nil, sdk.WithTurnMessagesForTest(turn))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)
	assert.Equal(t, []string{"call_0=one", "call_0=two"}, resultIDs(evts),
		"the second round's call_0 is a server tool with its own result")
}

// The input answers the previous run's frontend call_0; the resumed turn then
// makes a server call that reuses call_0. Only the answer from the input is
// left out.
func TestRunResume_CallIDReusedInResumedTurn(t *testing.T) {
	resumed := sdk.NewResponseForTest("Done.", nil, sdk.WithTurnMessagesForTest([]types.Message{
		toolMsg("call_0", `{"city":"Paris"}`),
		assistantMsg("Looking it up.", call("call_0", "lookup", `{"city":"Paris"}`)),
		toolMsg("call_0", "sunny"),
		assistantMsg("Done."),
	}))
	a := newTestAdapter(&mockSender{resumeResponses: []*sdk.Response{resumed}}, &mockEventBusProvider{})

	evts, err := runAndCollect(t, a, func(ctx context.Context) error {
		return a.RunResume(ctx, []ToolResult{{CallID: "call_0", Result: "Paris"}})
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"call_0=sunny"}, resultIDs(evts))
}
