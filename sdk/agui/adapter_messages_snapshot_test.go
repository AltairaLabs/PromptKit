package agui

import (
	"context"
	"encoding/json"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// snapshotJSON is the run's MESSAGES_SNAPSHOT as a client decodes it.
func snapshotJSON(t *testing.T, evts []aguievents.Event) []map[string]any {
	t.Helper()
	snaps := eventsOf[*aguievents.MessagesSnapshotEvent](evts)
	require.Len(t, snaps, 1, describeRun(evts))
	require.Equal(t, aguievents.EventTypeMessagesSnapshot, evts[len(evts)-2].Type(),
		"the snapshot comes right before RUN_FINISHED")
	data, err := json.Marshal(snaps[0].Messages)
	require.NoError(t, err)
	var out []map[string]any
	require.NoError(t, json.Unmarshal(data, &out))
	return out
}

// With WithMessagesSnapshot, each run ends by restating the conversation as
// the agent holds it: every message of every turn, in order, each keyed by
// the thread and its position, so a later snapshot replaces it in place.
func TestRunSend_MessagesSnapshotRestatesTheConversation(t *testing.T) {
	conv := openConv(t, streamingProvider(streamedText("Hello."), streamedText("Bye.")))

	_, err := sendAndCollect(t, NewEventAdapter(conv, WithThreadID("th"), WithMessagesSnapshot(true)), "hi")
	require.NoError(t, err)
	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithThreadID("th"), WithMessagesSnapshot(true)), "bye")
	require.NoError(t, err)

	assert.Equal(t, []map[string]any{
		{"id": "th-0", "role": "user", "content": "hi"},
		{"id": "th-1", "role": "assistant", "content": "Hello."},
		{"id": "th-2", "role": "user", "content": "bye"},
		{"id": "th-3", "role": "assistant", "content": "Bye."},
	}, snapshotJSON(t, evts))
}

// A tool round is restated with the assistant's calls and the tool's result;
// the turn's reasoning is restated as a reasoning message before the
// assistant message it led to.
func TestRunSend_MessagesSnapshotCarriesToolsAndReasoning(t *testing.T) {
	conv := openConv(t, streamingProvider(
		thinking(streamedCall("lookup", map[string]any{"id": "42"}, "Checking."), "Need the order."),
		streamedText("Shipped."),
	), sdk.WithStreamReasoning())
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "shipped", nil })

	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithThreadID("th"), WithMessagesSnapshot(true)), "ship 42")
	require.NoError(t, err)

	assert.Equal(t, []map[string]any{
		{"id": "th-0", "role": "user", "content": "ship 42"},
		{"id": "th-1-reasoning", "role": "reasoning", "content": "Need the order."},
		{"id": "th-1", "role": "assistant", "content": "Checking.", "toolCalls": []any{map[string]any{
			"id": "call_0_lookup", "type": "function",
			"function": map[string]any{"name": "lookup", "arguments": `{"id":"42"}`},
		}}},
		{"id": "th-2", "role": "tool", "content": "shipped", "toolCallId": "call_0_lookup"},
		{"id": "th-3", "role": "assistant", "content": "Shipped."},
	}, snapshotJSON(t, evts))
}

// Reasoning kept from the client (WithReasoning(false)) stays out of the
// snapshot too.
func TestRunSend_MessagesSnapshotLeavesOutHiddenReasoning(t *testing.T) {
	conv := openConv(t, streamingProvider(thinking(streamedText("Hi."), "Secret.")))

	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithThreadID("th"),
		WithMessagesSnapshot(true), WithReasoning(false)), "hi")
	require.NoError(t, err)

	assert.Equal(t, []map[string]any{
		{"id": "th-0", "role": "user", "content": "hi"},
		{"id": "th-1", "role": "assistant", "content": "Hi."},
	}, snapshotJSON(t, evts))
}

// By default a run sends no MESSAGES_SNAPSHOT.
func TestRunSend_NoMessagesSnapshotByDefault(t *testing.T) {
	conv := openConv(t, streamingProvider(streamedText("Hi.")))

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "hi")
	require.NoError(t, err)

	assert.Empty(t, eventsOf[*aguievents.MessagesSnapshotEvent](evts))
}

// historySender is a Sender whose history the test sets.
type historySender struct {
	mockSender
	history []types.Message
}

func (h *historySender) Messages(context.Context) []types.Message { return h.history }

// The run's reasoning is restated only before the assistant message it led
// to: a history whose tail is not the run's turn gets none.
func TestRunSend_MessagesSnapshotPlacesReasoningOnlyOnItsMessage(t *testing.T) {
	reply := assistantMsg("Hi.")
	reply.Reasoning = &types.ReasoningTrace{Text: "Greet them."}
	resp := sdk.NewResponseForTest("Hi.", nil, sdk.WithTurnMessagesForTest([]types.Message{reply}))
	aligned := &historySender{mockSender: mockSender{resp: resp}, history: []types.Message{
		types.NewUserMessage("hi"), assistantMsg("Hi."),
	}}
	shifted := &historySender{mockSender: mockSender{resp: resp}, history: []types.Message{
		assistantMsg("Hi."), types.NewUserMessage("hi"),
	}}

	evts, err := sendAndCollect(t, newTestAdapter(aligned, &mockEventBusProvider{},
		WithThreadID("th"), WithMessagesSnapshot(true)), "hi")
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{
		{"id": "th-0", "role": "user", "content": "hi"},
		{"id": "th-1-reasoning", "role": "reasoning", "content": "Greet them."},
		{"id": "th-1", "role": "assistant", "content": "Hi."},
	}, snapshotJSON(t, evts))

	evts, err = sendAndCollect(t, newTestAdapter(shifted, &mockEventBusProvider{},
		WithThreadID("th"), WithMessagesSnapshot(true)), "hi")
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{
		{"id": "th-0", "role": "assistant", "content": "Hi."},
		{"id": "th-1", "role": "user", "content": "hi"},
	}, snapshotJSON(t, evts))
}

// A conversation that does not expose its history sends no snapshot.
func TestRunSend_NoMessagesSnapshotWithoutHistory(t *testing.T) {
	a := newTestAdapter(&mockSender{resp: newTestResponse("Hi.", nil)}, &mockEventBusProvider{},
		WithMessagesSnapshot(true))

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)

	assert.Empty(t, eventsOf[*aguievents.MessagesSnapshotEvent](evts))
}
