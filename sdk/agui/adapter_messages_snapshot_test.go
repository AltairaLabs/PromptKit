package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
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

	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithThreadID("th"), WithMessagesSnapshot(true),
		WithReasoning(true)), "ship 42")
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

// The run's reasoning is restated before the assistant message it led to,
// found by what it says rather than by position, so a history a store trimmed
// (no user message, a summary first) or appended to keeps it on that message.
// A reply the history does not hold gets none.
func TestRunSend_MessagesSnapshotPlacesReasoningOnItsMessage(t *testing.T) {
	reply := assistantMsg("Hi.")
	reply.Reasoning = &types.ReasoningTrace{Text: "Greet them."}
	turn := []types.Message{types.NewUserMessage("hi"), reply}
	resp := sdk.NewResponseForTest("Hi.", nil, sdk.WithTurnMessagesForTest(turn))
	cases := map[string]struct {
		history []types.Message
		want    []map[string]any
	}{
		"trimmed and appended": {
			history: []types.Message{
				{Role: "system", Content: "Summary."}, assistantMsg("Hi."), assistantMsg("Anything else?"),
			},
			want: []map[string]any{
				{"id": "th-0", "role": "system", "content": "Summary."},
				{"id": "th-1-reasoning", "role": "reasoning", "content": "Greet them."},
				{"id": "th-1", "role": "assistant", "content": "Hi."},
				{"id": "th-2", "role": "assistant", "content": "Anything else?"},
			},
		},
		"same text in another role": {
			history: []types.Message{assistantMsg("Hi."), types.NewUserMessage("Hi.")},
			want: []map[string]any{
				{"id": "th-0-reasoning", "role": "reasoning", "content": "Greet them."},
				{"id": "th-0", "role": "assistant", "content": "Hi."},
				{"id": "th-1", "role": "user", "content": "Hi."},
			},
		},
		"reply not in the history": {
			history: []types.Message{types.NewUserMessage("hi"), assistantMsg("Hello.")},
			want: []map[string]any{
				{"id": "th-0", "role": "user", "content": "hi"},
				{"id": "th-1", "role": "assistant", "content": "Hello."},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sender := &historySender{mockSender: mockSender{resp: resp}, history: tc.history}
			evts, err := sendAndCollect(t, newTestAdapter(sender, &mockEventBusProvider{},
				WithThreadID("th"), WithMessagesSnapshot(true), WithReasoning(true)), "hi")
			require.NoError(t, err)
			assert.Equal(t, tc.want, snapshotJSON(t, evts))
		})
	}
}

// A history that reads back empty or shorter than the run's own turn (a
// store that failed) sends no snapshot, which would clear the client.
func TestRunSend_NoMessagesSnapshotOfAMissingHistory(t *testing.T) {
	turn := []types.Message{types.NewUserMessage("hi"), assistantMsg("Hi.")}
	withTurn := sdk.NewResponseForTest("Hi.", nil, sdk.WithTurnMessagesForTest(turn))
	cases := map[string]struct {
		resp    *sdk.Response
		history []types.Message
	}{
		"empty":                   {withTurn, nil},
		"shorter":                 {withTurn, []types.Message{assistantMsg("Hi.")}},
		"empty, no turn reported": {sdk.NewResponseForTest("Hi.", nil), nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			sender := &historySender{mockSender: mockSender{resp: tc.resp}, history: tc.history}
			evts, err := sendAndCollect(t, newTestAdapter(sender, &mockEventBusProvider{},
				WithMessagesSnapshot(true)), "hi")
			require.NoError(t, err)
			assert.Equal(t, []string{
				"RUN_STARTED", "TEXT_MESSAGE_START m1", "TEXT_MESSAGE_CONTENT m1 Hi.", "TEXT_MESSAGE_END m1", "RUN_FINISHED",
			}, describeRun(evts))
		})
	}
}

// Rounds that said the same thing keep their own reasoning; a message with
// the same text but other tool calls (none, or another tool) is not taken for
// them, a turn message the history lost does not stop the others being found,
// and a tool message with no result is left out.
func TestRunSend_MessagesSnapshotKeepsEachRoundsReasoning(t *testing.T) {
	first := assistantMsg("Checking.", call("call_0", "lookup", `{}`))
	first.Reasoning = &types.ReasoningTrace{Text: "First look."}
	second := assistantMsg("Checking.", call("call_0", "lookup", `{}`))
	second.Reasoning = &types.ReasoningTrace{Text: "Second look."}
	final := assistantMsg("Done.")
	final.Reasoning = &types.ReasoningTrace{Text: "Wrap up."}
	turn := []types.Message{first, toolMsg("call_0", "one"), second, toolMsg("call_0", "one"), final}
	resp := sdk.NewResponseForTest("Done.", nil, sdk.WithTurnMessagesForTest(turn))
	plain := func(m types.Message) types.Message { m.Reasoning = nil; return m }
	history := []types.Message{
		plain(first), toolMsg("call_0", "one"), plain(second), assistantMsg("Checking."),
		assistantMsg("Checking.", call("call_0", "search", `{}`)), {Role: roleTool},
		toolMsg("call_0", "one"), types.NewUserMessage("Later."),
	}
	sender := &historySender{mockSender: mockSender{resp: resp}, history: history}

	evts, err := sendAndCollect(t, newTestAdapter(sender, &mockEventBusProvider{},
		WithThreadID("th"), WithMessagesSnapshot(true), WithReasoning(true)), "go")
	require.NoError(t, err)

	var got []string
	for _, m := range snapshotJSON(t, evts) {
		got = append(got, fmt.Sprint(m["id"], " ", m["role"], " ", m["content"]))
	}
	assert.Equal(t, []string{
		"th-0-reasoning reasoning First look.",
		"th-0 assistant Checking.",
		"th-1 tool one",
		"th-2-reasoning reasoning Second look.",
		"th-2 assistant Checking.",
		"th-3 assistant Checking.",
		"th-4 assistant Checking.",
		"th-6 tool one",
		"th-7 user Later.",
	}, got)
}

// A conversation that does not expose its history sends no snapshot.
func TestRunSend_NoMessagesSnapshotWithoutHistory(t *testing.T) {
	a := newTestAdapter(&mockSender{resp: newTestResponse("Hi.", nil)}, &mockEventBusProvider{},
		WithMessagesSnapshot(true))

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)

	assert.Empty(t, eventsOf[*aguievents.MessagesSnapshotEvent](evts))
}

// unreadableStore keeps the conversation, and fails every whole-state read
// once failing is set, as a store whose backend went down for reads does.
type unreadableStore struct {
	*statestore.MemoryStore
	failing atomic.Bool
}

func (s *unreadableStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
	if s.failing.Load() {
		return nil, errors.New("store unavailable")
	}
	return s.MemoryStore.Load(ctx, id)
}

// A conversation whose history cannot be read once the turn has run sends no
// snapshot: an empty one would clear the client.
func TestRunSend_NoMessagesSnapshotWhenTheStoreFails(t *testing.T) {
	store := &unreadableStore{MemoryStore: statestore.NewMemoryStore()}
	conv := openConv(t, streamingProvider(
		streamedCall("lookup", map[string]any{"id": "42"}),
		streamedText("Shipped."),
	), sdk.WithStateStore(store))
	conv.OnTool("lookup", func(map[string]any) (any, error) {
		store.failing.Store(true)
		return "shipped", nil
	})

	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithMessagesSnapshot(true)), "ship 42")
	require.NoError(t, err)

	assert.Empty(t, eventsOf[*aguievents.MessagesSnapshotEvent](evts))
	assert.Equal(t, "RUN_FINISHED", describeRun(evts)[len(evts)-1])
	assert.Nil(t, conv.Messages(context.Background()), "the history read fails")
}
