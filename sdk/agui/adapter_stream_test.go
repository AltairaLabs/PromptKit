package agui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// These drive RunSend over real conversations whose mock provider streams its
// replies in several chunks, and assert the exact event sequence a client
// receives: text arrives as it streams, one TEXT_MESSAGE_CONTENT per chunk.

// streamScript answers each model call with the next scripted mock turn.
type streamScript struct {
	mu    sync.Mutex
	turns []mock.Turn
	next  int
}

func (s *streamScript) GetTurn(context.Context, mock.ResponseParams) (*mock.Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.next >= len(s.turns) {
		return &mock.Turn{Type: "text", Content: "(no more script)"}, nil
	}
	t := s.turns[s.next]
	s.next++
	return &t, nil
}

func (s *streamScript) GetResponse(ctx context.Context, p mock.ResponseParams) (string, error) {
	t, err := s.GetTurn(ctx, p)
	return t.Content, err
}

// streamingProvider is the mock tool provider streaming the scripted turns.
func streamingProvider(turns ...mock.Turn) *mock.ToolProvider {
	return mock.NewToolProviderWithRepository("mock", "mock-model", false, &streamScript{turns: turns})
}

func streamedText(chunks ...string) mock.Turn {
	return mock.Turn{Type: "text", StreamChunks: chunks}
}

func streamedCall(name string, args map[string]any, chunks ...string) mock.Turn {
	return mock.Turn{Type: "tool_calls", StreamChunks: chunks, ToolCalls: []mock.ToolCall{{Name: name, Arguments: args}}}
}

// describeRun renders a run's events one per line, naming each generated
// message id m1, m2, ... in the order it first appears, so a test can assert
// the exact sequence and that the ids agree.
func describeRun(evts []aguievents.Event) []string {
	ids := map[string]string{}
	alias := func(id string) string {
		if a, ok := ids[id]; ok {
			return a
		}
		a := fmt.Sprintf("m%d", len(ids)+1)
		ids[id] = a
		return a
	}
	out := make([]string, 0, len(evts))
	for _, ev := range evts {
		out = append(out, describeEvent(ev, alias))
	}
	return out
}

func describeEvent(ev aguievents.Event, alias func(string) string) string {
	switch e := ev.(type) {
	case *aguievents.TextMessageStartEvent:
		return "TEXT_MESSAGE_START " + alias(e.MessageID)
	case *aguievents.TextMessageContentEvent:
		return "TEXT_MESSAGE_CONTENT " + alias(e.MessageID) + " " + e.Delta
	case *aguievents.TextMessageEndEvent:
		return "TEXT_MESSAGE_END " + alias(e.MessageID)
	case *aguievents.ToolCallStartEvent:
		parent := ""
		if e.ParentMessageID != nil {
			parent = " parent=" + alias(*e.ParentMessageID)
		}
		return "TOOL_CALL_START " + e.ToolCallID + " " + e.ToolCallName + parent
	case *aguievents.ToolCallArgsEvent:
		return "TOOL_CALL_ARGS " + e.ToolCallID + " " + e.Delta
	case *aguievents.ToolCallEndEvent:
		return "TOOL_CALL_END " + e.ToolCallID
	case *aguievents.ToolCallResultEvent:
		return "TOOL_CALL_RESULT " + e.ToolCallID + " " + e.Content
	case *aguievents.RunFinishedEvent:
		if e.Outcome == nil {
			return "RUN_FINISHED"
		}
		var held []string
		for _, in := range e.Outcome.Interrupts {
			held = append(held, in.ToolCallID+" "+in.Reason)
		}
		return "RUN_FINISHED " + string(e.Outcome.Type) + " " + strings.Join(held, ",")
	case *aguievents.RunErrorEvent:
		return "RUN_ERROR " + e.Message
	default:
		return string(ev.Type())
	}
}

// lastAssistantText is the text of the conversation's last assistant message.
func lastAssistantText(t *testing.T, conv *sdk.Conversation) string {
	t.Helper()
	msgs := conv.Messages(context.Background())
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == roleAssistant {
			return msgs[i].GetContent()
		}
	}
	t.Fatal("the conversation has no assistant message")
	return ""
}

// A streamed reply arrives one TEXT_MESSAGE_CONTENT per chunk, and the deltas
// make up the reply.
func TestRunSend_StreamsTextChunkByChunk(t *testing.T) {
	conv := openConv(t, streamingProvider(streamedText("Hel", "lo, ", "world.")))

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Hel",
		"TEXT_MESSAGE_CONTENT m1 lo, ",
		"TEXT_MESSAGE_CONTENT m1 world.",
		"TEXT_MESSAGE_END m1",
		"RUN_FINISHED",
	}, describeRun(evts))
	assert.Equal(t, "Hello, world.", lastAssistantText(t, conv))
}

// A tool round streams its text, then its call and the call's result, and the
// next round's text streams as a new message.
func TestRunSend_StreamsAToolRound(t *testing.T) {
	conv := openConv(t, streamingProvider(
		streamedCall("lookup", map[string]any{"id": "42"}, "Let me ", "look."),
		streamedText("Found ", "it."),
	))
	conv.OnTool("lookup", func(map[string]any) (any, error) { return map[string]any{"status": "shipped"}, nil })

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "where is order 42?")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Let me ",
		"TEXT_MESSAGE_CONTENT m1 look.",
		"TEXT_MESSAGE_END m1",
		"TOOL_CALL_START call_0_lookup lookup parent=m1",
		`TOOL_CALL_ARGS call_0_lookup {"id":"42"}`,
		"TOOL_CALL_END call_0_lookup",
		"TOOL_CALL_RESULT call_0_lookup {\n  \"status\": \"shipped\"\n}",
		"TEXT_MESSAGE_START m2",
		"TEXT_MESSAGE_CONTENT m2 Found ",
		"TEXT_MESSAGE_CONTENT m2 it.",
		"TEXT_MESSAGE_END m2",
		"RUN_FINISHED",
	}, describeRun(evts))
	assert.Equal(t, "Found it.", lastAssistantText(t, conv))
}

// Each round of a multi-round turn reports its own calls and results, once,
// though the mock numbers its call ids per response, so both rounds call
// call_0_lookup. A round that only calls a tool sends no text message; its
// call still names the message as its parent.
func TestRunSend_StreamsEachRoundOfAMultiRoundTurn(t *testing.T) {
	conv := openConv(t, streamingProvider(
		streamedCall("lookup", map[string]any{"id": "1"}, "Checking ", "one."),
		streamedCall("lookup", map[string]any{"id": "2"}),
		streamedText("Both ", "shipped."),
	))
	conv.OnTool("lookup", func(args map[string]any) (any, error) {
		return map[string]any{"id": args["id"], "status": "shipped"}, nil
	})

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "check 1 and 2")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Checking ",
		"TEXT_MESSAGE_CONTENT m1 one.",
		"TEXT_MESSAGE_END m1",
		"TOOL_CALL_START call_0_lookup lookup parent=m1",
		`TOOL_CALL_ARGS call_0_lookup {"id":"1"}`,
		"TOOL_CALL_END call_0_lookup",
		"TOOL_CALL_RESULT call_0_lookup {\n  \"id\": \"1\",\n  \"status\": \"shipped\"\n}",
		"TOOL_CALL_START call_0_lookup lookup parent=m2",
		`TOOL_CALL_ARGS call_0_lookup {"id":"2"}`,
		"TOOL_CALL_END call_0_lookup",
		"TOOL_CALL_RESULT call_0_lookup {\n  \"id\": \"2\",\n  \"status\": \"shipped\"\n}",
		"TEXT_MESSAGE_START m3",
		"TEXT_MESSAGE_CONTENT m3 Both ",
		"TEXT_MESSAGE_CONTENT m3 shipped.",
		"TEXT_MESSAGE_END m3",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// gatedStreamProvider streams its scripted turns, but holds every chunk after
// the first until the test releases it, so a test can see what the client has
// before the model has finished.
type gatedStreamProvider struct {
	*mock.ToolProvider
	release chan struct{}
}

func (p *gatedStreamProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools interface{}, choice string,
) (<-chan providers.StreamChunk, error) {
	in, err := p.ToolProvider.PredictStreamWithTools(ctx, req, tools, choice)
	if err != nil {
		return nil, err
	}
	out := make(chan providers.StreamChunk)
	go func() {
		defer close(out)
		first := true
		for c := range in {
			if !first {
				<-p.release
			}
			first = false
			out <- c
		}
	}()
	return out, nil
}

// The first delta reaches the client while the model is still streaming.
func TestRunSend_DeltaArrivesBeforeTheModelFinishes(t *testing.T) {
	provider := &gatedStreamProvider{
		ToolProvider: streamingProvider(streamedText("Once ", "upon ", "a time.")),
		release:      make(chan struct{}),
	}
	conv := openConv(t, provider)
	var once sync.Once
	release := func() { once.Do(func() { close(provider.release) }) }
	t.Cleanup(release)
	a := NewEventAdapter(conv)

	runDone := make(chan error, 1)
	go func() { runDone <- a.RunSend(context.Background(), userMsg("tell me a story")) }()

	var early []aguievents.Event
	timeout := time.After(collectTimeout)
	for len(early) == 0 || early[len(early)-1].Type() != aguievents.EventTypeTextMessageContent {
		select {
		case ev, ok := <-a.Events():
			require.True(t, ok, "the run ended before its first delta: %v", describeRun(early))
			early = append(early, ev)
		case <-timeout:
			release()
			t.Fatalf("no delta arrived while the model was streaming; got %v", describeRun(early))
		}
	}
	assert.Equal(t, []string{"RUN_STARTED", "TEXT_MESSAGE_START m1", "TEXT_MESSAGE_CONTENT m1 Once "},
		describeRun(early), "the first chunk arrives before the model is let finish")

	release()
	evts := append(early, collectEvents(a.Events())...)
	require.NoError(t, <-runDone)
	requireValidSequence(t, evts)
	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Once ",
		"TEXT_MESSAGE_CONTENT m1 upon ",
		"TEXT_MESSAGE_CONTENT m1 a time.",
		"TEXT_MESSAGE_END m1",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// failingStreamProvider streams some text, then fails the stream.
type failingStreamProvider struct {
	*mock.ToolProvider
	deltas []string
	err    error
}

func (p *failingStreamProvider) PredictStreamWithTools(
	context.Context, providers.PredictionRequest, interface{}, string,
) (<-chan providers.StreamChunk, error) {
	out := make(chan providers.StreamChunk, len(p.deltas)+1)
	sofar := ""
	for _, d := range p.deltas {
		sofar += d
		out <- providers.StreamChunk{Delta: d, Content: sofar}
	}
	reason := "error"
	out <- providers.StreamChunk{Error: p.err, FinishReason: &reason}
	close(out)
	return out, nil
}

// A stream that fails part-way has sent the text so far, closes its message,
// and ends the run with RUN_ERROR.
func TestRunSend_ErrorMidStreamEndsTheRun(t *testing.T) {
	provider := &failingStreamProvider{
		ToolProvider: streamingProvider(),
		deltas:       []string{"Partial ", "answer"},
		err:          errors.New("connection reset"),
	}
	conv := openConv(t, provider)

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection reset")

	got := describeRun(evts)
	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Partial ",
		"TEXT_MESSAGE_CONTENT m1 answer",
		"TEXT_MESSAGE_END m1",
	}, got[:len(got)-1], "the streamed message ends before the error")
	assert.True(t, strings.HasPrefix(got[len(got)-1], "RUN_ERROR "), got[len(got)-1])
	assert.Contains(t, got[len(got)-1], "connection reset")
}

// A frontend tool call streams with its round's text and is left unanswered;
// the next run resumes the turn, streams its reply and echoes no answer.
func TestRunSend_StreamsAFrontendToolRoundTrip(t *testing.T) {
	conv := openConv(t, streamingProvider(
		streamedCall("get_location", map[string]any{"q": "a"}, "Where ", "are you?"),
		streamedText("You are ", "in Paris."),
	))
	bindClientTool(t, conv, "get_location")

	first, err := sendAndCollect(t, NewEventAdapter(conv), "where am I?")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Where ",
		"TEXT_MESSAGE_CONTENT m1 are you?",
		"TEXT_MESSAGE_END m1",
		"TOOL_CALL_START call_0_get_location get_location parent=m1",
		`TOOL_CALL_ARGS call_0_get_location {"q":"a"}`,
		"TOOL_CALL_END call_0_get_location",
		"RUN_FINISHED",
	}, describeRun(first))

	input := []aguitypes.Message{
		{ID: "t1", Role: aguitypes.RoleTool, ToolCallID: "call_0_get_location", Content: `{"city":"Paris"}`},
	}
	b := NewEventAdapter(conv)
	second, err := continueAndCollect(t, b, first, func(ctx context.Context) error {
		return b.RunResume(ctx, ToolResultsFromAGUI(input))
	})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 You are ",
		"TEXT_MESSAGE_CONTENT m1 in Paris.",
		"TEXT_MESSAGE_END m1",
		"RUN_FINISHED",
	}, describeRun(second))
	assert.Equal(t, "You are in Paris.", lastAssistantText(t, conv))
}

// A streamed call held for approval ends the run with the interrupt outcome,
// as a sent one does, and the continuing run reports the approved result.
func TestRunSend_StreamedApprovalHoldInterrupts(t *testing.T) {
	conv := openConv(t, streamingProvider(
		streamedCall("send_message", map[string]any{"body": "hi"}, "Sending."),
		streamedText("Sent."),
	))
	conv.OnToolAsync("send_message",
		func(map[string]any) sdktools.PendingResult {
			return sdktools.PendingResult{Reason: "requires_approval", Message: "Approve send?"}
		},
		func(map[string]any) (any, error) { return map[string]any{"sent": true}, nil },
	)

	first, err := sendAndCollect(t, NewEventAdapter(conv), "send hi")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Sending.",
		"TEXT_MESSAGE_END m1",
		"TOOL_CALL_START call_0_send_message send_message parent=m1",
		`TOOL_CALL_ARGS call_0_send_message {"body":"hi"}`,
		"TOOL_CALL_END call_0_send_message",
		"RUN_FINISHED interrupt call_0_send_message requires_approval",
	}, describeRun(first))

	_, err = conv.ResolveTool(context.Background(), "call_0_send_message")
	require.NoError(t, err)
	b := NewEventAdapter(conv)
	second, err := continueAndCollect(t, b, first, b.RunContinue)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"RUN_STARTED",
		"TOOL_CALL_RESULT call_0_send_message {\n  \"sent\": true\n}",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Sent.",
		"TEXT_MESSAGE_END m1",
		"RUN_FINISHED",
	}, describeRun(second))
}

// A ToolResultProvider answers a streamed turn's client tool inside the run:
// the answer is reported once, and the resumed turn follows.
func TestRunSend_ToolResultProviderAnswersAStreamedCall(t *testing.T) {
	conv := openConv(t, streamingProvider(
		streamedCall("get_location", map[string]any{"q": "a"}, "Locating."),
		streamedText("Paris."),
	))
	bindClientTool(t, conv, "get_location")
	provider := func(_ context.Context, pending []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: pending[0].CallID, Result: map[string]any{"city": "Paris"}}}, nil
	}

	evts, err := sendAndCollect(t, NewEventAdapter(conv, WithToolResultProvider(provider)), "where am I?")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Locating.",
		"TEXT_MESSAGE_END m1",
		"TOOL_CALL_START call_0_get_location get_location parent=m1",
		`TOOL_CALL_ARGS call_0_get_location {"q":"a"}`,
		"TOOL_CALL_END call_0_get_location",
		`TOOL_CALL_RESULT call_0_get_location {"city":"Paris"}`,
		"TEXT_MESSAGE_START m2",
		"TEXT_MESSAGE_CONTENT m2 Paris.",
		"TEXT_MESSAGE_END m2",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// An output guardrail that cuts a streamed reply replaces the text already
// shown: the streamed message closes and the guardrail's message follows as a
// new one, since AG-UI cannot take text back.
func TestRunSend_GuardrailCutStartsANewMessage(t *testing.T) {
	const message = "Let's talk about something else."
	conv, err := sdk.Open(writePack(t, toolPackJSON), "chat",
		sdk.WithProvider(streamingProvider(streamedText("Say P", "ine", "apple, please."))),
		sdk.WithSkipSchemaValidation(),
		sdk.WithGuardrail(guardrails.Output("content_excludes",
			map[string]any{"patterns": []any{"pineapple"}}, guardrails.WithMessage(message))))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "what goes on pizza?")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Say ",
		"TEXT_MESSAGE_END m1",
		"TEXT_MESSAGE_START m2",
		"TEXT_MESSAGE_CONTENT m2 " + message,
		"TEXT_MESSAGE_END m2",
		"RUN_FINISHED",
	}, describeRun(evts))
	assert.Equal(t, message, lastAssistantText(t, conv))
}

// A reply the guardrail cuts before any of it was shown arrives as the
// guardrail's message alone.
func TestRunSend_GuardrailCutBeforeAnyTextIsOneMessage(t *testing.T) {
	const message = "Let's talk about something else."
	conv, err := sdk.Open(writePack(t, toolPackJSON), "chat",
		sdk.WithProvider(streamingProvider(streamedText("P", "ine", "apple is the best."))),
		sdk.WithSkipSchemaValidation(),
		sdk.WithGuardrail(guardrails.Output("content_excludes",
			map[string]any{"patterns": []any{"pineapple"}}, guardrails.WithMessage(message))))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "what goes on pizza?")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 " + message,
		"TEXT_MESSAGE_END m1",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// A reply whose text the turn completed past what streamed sends the rest
// before the message ends, so the deltas still make up the reply.
func TestRunSend_ReplyLongerThanItsStreamSendsTheRest(t *testing.T) {
	conv := openConv(t, streamingProvider(mock.Turn{Type: "text", Content: "Hello there", StreamChunks: []string{"Hel", "lo"}}))

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Hel",
		"TEXT_MESSAGE_CONTENT m1 lo",
		"TEXT_MESSAGE_CONTENT m1  there",
		"TEXT_MESSAGE_END m1",
		"RUN_FINISHED",
	}, describeRun(evts))
	assert.Equal(t, "Hello there", lastAssistantText(t, conv))
}

// A reply whose text is not what streamed is sent whole as a new message
// after the streamed one.
func TestRunSend_ReplyUnlikeItsStreamIsANewMessage(t *testing.T) {
	conv := openConv(t, streamingProvider(mock.Turn{Type: "text", Content: "Goodbye", StreamChunks: []string{"Hel", "lo"}}))

	evts, err := sendAndCollect(t, NewEventAdapter(conv), "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1",
		"TEXT_MESSAGE_CONTENT m1 Hel",
		"TEXT_MESSAGE_CONTENT m1 lo",
		"TEXT_MESSAGE_END m1",
		"TEXT_MESSAGE_START m2",
		"TEXT_MESSAGE_CONTENT m2 Goodbye",
		"TEXT_MESSAGE_END m2",
		"RUN_FINISHED",
	}, describeRun(evts))
	assert.Equal(t, "Goodbye", lastAssistantText(t, conv))
}

// chunkSender is a Sender whose Stream replays chunks, for streams a real
// conversation does not produce.
type chunkSender struct {
	*mockSender
	chunks []sdk.StreamChunk
}

func (s *chunkSender) Stream(context.Context, any, ...sdk.SendOption) <-chan sdk.StreamChunk {
	ch := make(chan sdk.StreamChunk, len(s.chunks))
	for _, c := range s.chunks {
		ch <- c
	}
	close(ch)
	return ch
}

// A stream that ends without its response or an error ends the run with
// RUN_ERROR, rather than a RUN_FINISHED for a turn that never finished.
func TestRunSend_StreamWithoutAResponseIsAnError(t *testing.T) {
	sender := &chunkSender{mockSender: &mockSender{}, chunks: []sdk.StreamChunk{{Type: sdk.ChunkText, Text: "Hi"}}}

	evts, err := sendAndCollect(t, newTestAdapter(sender, &mockEventBusProvider{}), "hi")

	require.ErrorIs(t, err, errStreamIncomplete)
	assert.Equal(t, []string{
		"RUN_STARTED", "TEXT_MESSAGE_START m1", "TEXT_MESSAGE_CONTENT m1 Hi", "TEXT_MESSAGE_END m1",
		"RUN_ERROR " + errStreamIncomplete.Error(),
	}, describeRun(evts))
}

// A stream cut short by the run's canceled context reports the cancellation.
func TestRunSend_StreamEndedByCancellationReportsIt(t *testing.T) {
	sender := &chunkSender{mockSender: &mockSender{}}
	a := newTestAdapter(sender, &mockEventBusProvider{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := a.RunSend(ctx, userMsg("hi"))

	require.ErrorIs(t, err, context.Canceled)
}

// A response that holds fewer rounds than streamed (its history compacted, say)
// still leaves no message open.
func TestRunSend_StreamedRoundsTheResponseLacksAreClosed(t *testing.T) {
	call := types.MessageToolCall{ID: "c1", Name: "lookup", Args: []byte(`{}`)}
	sender := &chunkSender{mockSender: &mockSender{}, chunks: []sdk.StreamChunk{
		{Type: sdk.ChunkText, Text: "A"},
		{Type: sdk.ChunkToolCall, ToolCall: &call},
		{Type: sdk.ChunkToolResult, ToolResult: &types.MessageToolResult{ID: "c1", Parts: []types.ContentPart{types.NewTextPart("ok")}}},
		{Type: sdk.ChunkText, Text: "B"},
		{Type: sdk.ChunkDone, Message: sdk.NewResponseForTest("B", nil,
			sdk.WithTurnMessagesForTest([]types.Message{assistantMsg("B")}))},
	}}

	evts, err := sendAndCollect(t, newTestAdapter(sender, &mockEventBusProvider{}), "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1", "TEXT_MESSAGE_CONTENT m1 A", "TEXT_MESSAGE_END m1",
		"TOOL_CALL_START c1 lookup parent=m1", "TOOL_CALL_ARGS c1 {}", "TOOL_CALL_END c1",
		"TOOL_CALL_RESULT c1 ok",
		"TEXT_MESSAGE_START m2", "TEXT_MESSAGE_CONTENT m2 B",
		"TEXT_MESSAGE_START m3", "TEXT_MESSAGE_CONTENT m3 B", "TEXT_MESSAGE_END m3",
		"TEXT_MESSAGE_END m2",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// A round that the turn goes on from without results (none came for its
// calls) still ends with its calls: the next round's calls name the next
// round's message, and each call is started once.
func TestRunSend_RoundWithoutResultsStillEndsAtTheNextRound(t *testing.T) {
	first := types.MessageToolCall{ID: "c1", Name: "lookup", Args: []byte(`{"id":"1"}`)}
	also := types.MessageToolCall{ID: "c1b", Name: "lookup", Args: []byte(`{"id":"1b"}`)}
	second := types.MessageToolCall{ID: "c2", Name: "lookup", Args: []byte(`{"id":"2"}`)}
	sender := &chunkSender{mockSender: &mockSender{}, chunks: []sdk.StreamChunk{
		{Type: sdk.ChunkText, Text: "A"},
		{Type: sdk.ChunkToolCall, ToolCall: &first, Round: 1},
		{Type: sdk.ChunkToolCall, ToolCall: &also, Round: 1},
		{Type: sdk.ChunkToolCall, ToolCall: &second, Round: 2},
		{Type: sdk.ChunkText, Text: "B"},
		{Type: sdk.ChunkDone, Message: sdk.NewResponseForTest("B", nil, sdk.WithTurnMessagesForTest([]types.Message{
			{Role: roleAssistant, Content: "A", ToolCalls: []types.MessageToolCall{first, also}},
			{Role: roleAssistant, ToolCalls: []types.MessageToolCall{second}},
			assistantMsg("B"),
		}))},
	}}

	evts, err := sendAndCollect(t, newTestAdapter(sender, &mockEventBusProvider{}), "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START m1", "TEXT_MESSAGE_CONTENT m1 A", "TEXT_MESSAGE_END m1",
		"TOOL_CALL_START c1 lookup parent=m1", `TOOL_CALL_ARGS c1 {"id":"1"}`, "TOOL_CALL_END c1",
		"TOOL_CALL_START c1b lookup parent=m1", `TOOL_CALL_ARGS c1b {"id":"1b"}`, "TOOL_CALL_END c1b",
		"TOOL_CALL_START c2 lookup parent=m2", `TOOL_CALL_ARGS c2 {"id":"2"}`, "TOOL_CALL_END c2",
		"TEXT_MESSAGE_START m3", "TEXT_MESSAGE_CONTENT m3 B", "TEXT_MESSAGE_END m3",
		"RUN_FINISHED",
	}, describeRun(evts))
}

// A stream that does not number its rounds (Round 0) still splits them where
// a round's results end.
func TestRunSend_UnnumberedRoundsSplitAtTheirResults(t *testing.T) {
	call := types.MessageToolCall{ID: "c1", Name: "lookup", Args: []byte(`{}`)}
	result := types.MessageToolResult{ID: "c1", Parts: []types.ContentPart{types.NewTextPart("ok")}}
	sender := &chunkSender{mockSender: &mockSender{}, chunks: []sdk.StreamChunk{
		{Type: sdk.ChunkToolCall, ToolCall: &call},
		{Type: sdk.ChunkToolResult, ToolResult: &result},
		{Type: sdk.ChunkToolCall, ToolCall: &call},
		{Type: sdk.ChunkToolResult, ToolResult: &result},
		{Type: sdk.ChunkDone, Message: sdk.NewResponseForTest("", nil, sdk.WithTurnMessagesForTest([]types.Message{
			{Role: roleAssistant, ToolCalls: []types.MessageToolCall{call}},
			{Role: roleTool, ToolResult: &result},
			{Role: roleAssistant, ToolCalls: []types.MessageToolCall{call}},
			{Role: roleTool, ToolResult: &result},
			assistantMsg("done"),
		}))},
	}}

	evts, err := sendAndCollect(t, newTestAdapter(sender, &mockEventBusProvider{}), "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"RUN_STARTED",
		"TOOL_CALL_START c1 lookup parent=m1", "TOOL_CALL_ARGS c1 {}", "TOOL_CALL_END c1", "TOOL_CALL_RESULT c1 ok",
		"TOOL_CALL_START c1 lookup parent=m2", "TOOL_CALL_ARGS c1 {}", "TOOL_CALL_END c1", "TOOL_CALL_RESULT c1 ok",
		"TEXT_MESSAGE_START m3", "TEXT_MESSAGE_CONTENT m3 done", "TEXT_MESSAGE_END m3",
		"RUN_FINISHED",
	}, describeRun(evts))
}
