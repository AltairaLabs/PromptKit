package agui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// thinking is a mock turn that reasons in chunks before it replies.
func thinking(turn mock.Turn, reasoning ...string) mock.Turn {
	turn.ReasoningChunks = reasoning
	return turn
}

// sendOnlySender runs a real conversation through Send alone, as any Sender
// that does not stream does.
type sendOnlySender struct{ conv *sdk.Conversation }

func (s sendOnlySender) Send(ctx context.Context, msg any, opts ...sdk.SendOption) (*sdk.Response, error) {
	return s.conv.Send(ctx, msg, opts...)
}

func (s sendOnlySender) SendToolResult(ctx context.Context, callID string, result any) error {
	return s.conv.SendToolResult(ctx, callID, result)
}

func (s sendOnlySender) RejectClientTool(ctx context.Context, callID, reason string) {
	s.conv.RejectClientTool(ctx, callID, reason)
}

func (s sendOnlySender) Resume(ctx context.Context) (*sdk.Response, error) { return s.conv.Resume(ctx) }

func (s sendOnlySender) EventBus() events.Bus { return s.conv.EventBus() }

// A conversation that streams its reasoning (sdk.WithStreamReasoning) sends
// it as it arrives: a reasoning span holding one reasoning message, closed
// when the reply's text starts.
func TestRunSend_StreamsReasoningBeforeTheText(t *testing.T) {
	conv := openConv(t, streamingProvider(thinking(streamedText("Hi", " there."), "Think", "ing.")),
		sdk.WithStreamReasoning())

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"REASONING_START m1",
		"REASONING_MESSAGE_START m2 reasoning",
		"REASONING_MESSAGE_CONTENT m2 Think",
		"REASONING_MESSAGE_CONTENT m2 ing.",
		"REASONING_MESSAGE_END m2",
		"REASONING_END m1",
		"TEXT_MESSAGE_START m3",
		"TEXT_MESSAGE_CONTENT m3 Hi",
		"TEXT_MESSAGE_CONTENT m3  there.",
		"TEXT_MESSAGE_END m3",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// Each round of a tool loop reasons on its own: the round's reasoning closes
// before its calls, and the next round's opens a new span.
func TestRunSend_StreamsEachRoundsReasoning(t *testing.T) {
	conv := openConv(t, streamingProvider(
		thinking(streamedCall("lookup", map[string]any{"id": "42"}), "Need the order."),
		thinking(streamedText("Shipped."), "Got it."),
	), sdk.WithStreamReasoning())
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "shipped", nil })

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "where is 42?")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"REASONING_START m1",
		"REASONING_MESSAGE_START m2 reasoning",
		"REASONING_MESSAGE_CONTENT m2 Need the order.",
		"REASONING_MESSAGE_END m2",
		"REASONING_END m1",
		"TOOL_CALL_START call_0_lookup lookup parent=m3",
		`TOOL_CALL_ARGS call_0_lookup {"id":"42"}`,
		"TOOL_CALL_END call_0_lookup",
		"TOOL_CALL_RESULT call_0_lookup shipped",
		"REASONING_START m4",
		"REASONING_MESSAGE_START m5 reasoning",
		"REASONING_MESSAGE_CONTENT m5 Got it.",
		"REASONING_MESSAGE_END m5",
		"REASONING_END m4",
		"TEXT_MESSAGE_START m6",
		"TEXT_MESSAGE_CONTENT m6 Shipped.",
		"TEXT_MESSAGE_END m6",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// A conversation that does not stream its reasoning still reports it, whole,
// once the round has run: after the text that streamed.
func TestRunSend_ReasoningNotStreamedArrivesWithTheRound(t *testing.T) {
	conv := openConv(t, streamingProvider(thinking(streamedText("Hi."), "Think", "ing.")))

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Hi.",
		"TEXT_MESSAGE_END m1",
		"REASONING_START m2",
		"REASONING_MESSAGE_START m3 reasoning",
		"REASONING_MESSAGE_CONTENT m3 Thinking.",
		"REASONING_MESSAGE_END m3",
		"REASONING_END m2",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// A conversation run through Send reports each round's reasoning before the
// round's text and calls.
func TestRunSend_SendReportsReasoningBeforeEachRound(t *testing.T) {
	conv := openConv(t, streamingProvider(
		thinking(streamedCall("lookup", map[string]any{"id": "42"}, "Checking."), "Need the order."),
		thinking(streamedText("Shipped."), "Got it."),
	))
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "shipped", nil })

	evts, err := sendAndCollect(t, newAdapter(sendOnlySender{conv}, sendOnlySender{conv}), "where is 42?")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"REASONING_START m1",
		"REASONING_MESSAGE_START m2 reasoning",
		"REASONING_MESSAGE_CONTENT m2 Need the order.",
		"REASONING_MESSAGE_END m2",
		"REASONING_END m1",
		"TEXT_MESSAGE_START m3",
		"TEXT_MESSAGE_CONTENT m3 Checking.",
		"TEXT_MESSAGE_END m3",
		"TOOL_CALL_START call_0_lookup lookup parent=m3",
		`TOOL_CALL_ARGS call_0_lookup {"id":"42"}`,
		"TOOL_CALL_END call_0_lookup",
		"TOOL_CALL_RESULT call_0_lookup shipped",
		"REASONING_START m4",
		"REASONING_MESSAGE_START m5 reasoning",
		"REASONING_MESSAGE_CONTENT m5 Got it.",
		"REASONING_MESSAGE_END m5",
		"REASONING_END m4",
		"TEXT_MESSAGE_START m6",
		"TEXT_MESSAGE_CONTENT m6 Shipped.",
		"TEXT_MESSAGE_END m6",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// WithReasoning(false) keeps the model's reasoning from the client, streamed
// or not.
func TestRunSend_WithReasoningFalseSendsNoReasoning(t *testing.T) {
	want := []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Hi.",
		"TEXT_MESSAGE_END m1",
		"RUN_FINISHED",
	}

	streamed := openConv(t, streamingProvider(thinking(streamedText("Hi."), "Secret.")), sdk.WithStreamReasoning())
	evts, err := sendAndCollect(t, NewEventAdapter(streamed, WithReasoning(false)), "hi")
	require.NoError(t, err)
	assert.Equal(t, want, describeRun(evts))

	settled := openConv(t, streamingProvider(thinking(streamedText("Hi."), "Secret.")))
	evts, err = sendAndCollect(t, NewEventAdapter(settled, WithReasoning(false)), "hi")
	require.NoError(t, err)
	assert.Equal(t, want, describeRun(evts))
}

// Reasoning the provider returned only as opaque tokens (signatures,
// encrypted or redacted blocks) has no text to show and sends nothing.
func TestRunSend_OpaqueReasoningIsNotSent(t *testing.T) {
	msg := assistantMsg("Hi.")
	msg.Reasoning = &types.ReasoningTrace{
		Opaque:   []types.OpaqueReasoning{{Provider: "claude", Kind: "redacted_thinking", Data: "c2VjcmV0"}},
		Redacted: true,
	}
	resp := sdk.NewResponseForTest("Hi.", nil, sdk.WithTurnMessagesForTest([]types.Message{msg}))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Hi.",
		"TEXT_MESSAGE_END m1",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// In a streamed turn whose reasoning did not stream, every round's reasoning
// is reported once the turn's text is complete, in round order, so none of it
// lands inside a later round's open text message.
func TestRunSend_ReasoningNotStreamedFollowsTheTurnsText(t *testing.T) {
	conv := openConv(t, streamingProvider(
		thinking(streamedCall("lookup", map[string]any{"id": "42"}, "Checking."), "Need the order."),
		thinking(streamedText("Shipped."), "Got it."),
	))
	conv.OnTool("lookup", func(map[string]any) (any, error) { return "shipped", nil })

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "where is 42?")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Checking.",
		"TEXT_MESSAGE_END m1",
		"TOOL_CALL_START call_0_lookup lookup parent=m1",
		`TOOL_CALL_ARGS call_0_lookup {"id":"42"}`,
		"TOOL_CALL_END call_0_lookup",
		"TOOL_CALL_RESULT call_0_lookup shipped",
		"TEXT_MESSAGE_START m2",
		"TEXT_MESSAGE_CONTENT m2 Shipped.",
		"TEXT_MESSAGE_END m2",
		"REASONING_START m3",
		"REASONING_MESSAGE_START m4 reasoning",
		"REASONING_MESSAGE_CONTENT m4 Need the order.",
		"REASONING_MESSAGE_END m4",
		"REASONING_END m3",
		"REASONING_START m5",
		"REASONING_MESSAGE_START m6 reasoning",
		"REASONING_MESSAGE_CONTENT m6 Got it.",
		"REASONING_MESSAGE_END m6",
		"REASONING_END m5",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// A response that carries no turn messages reports the reasoning on its
// reply, before the reply's text.
func TestRunSend_ReasoningOfAReplyWithoutTurnMessages(t *testing.T) {
	resp := sdk.NewResponseForTest("Hi.", nil)
	resp.Message().Reasoning = &types.ReasoningTrace{Text: "Greet them."}
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"REASONING_START m1",
		"REASONING_MESSAGE_START m2 reasoning",
		"REASONING_MESSAGE_CONTENT m2 Greet them.",
		"REASONING_MESSAGE_END m2",
		"REASONING_END m1",
		"TEXT_MESSAGE_START m3",
		"TEXT_MESSAGE_CONTENT m3 Hi.",
		"TEXT_MESSAGE_END m3",
		"RUN_FINISHED",
	}, describeRun(evts))
}
