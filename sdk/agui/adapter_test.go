package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// mockSender implements Sender for testing.
type mockSender struct {
	resp *sdk.Response
	err  error

	// Client tool tracking
	sendToolResultCalls []sendToolResultCall
	sendToolResultErr   error
	rejectCalls         []rejectCall
	resumeResponses     []*sdk.Response // shifted off in order
	resumeErr           error
	resumeCalls         int
}

type sendToolResultCall struct {
	callID string
	result any
}

type rejectCall struct {
	callID string
	reason string
}

func (m *mockSender) Send(_ context.Context, _ any, _ ...sdk.SendOption) (*sdk.Response, error) {
	return m.resp, m.err
}

func (m *mockSender) SendToolResult(_ context.Context, callID string, result any) error {
	m.sendToolResultCalls = append(m.sendToolResultCalls, sendToolResultCall{callID, result})
	return m.sendToolResultErr
}

func (m *mockSender) RejectClientTool(_ context.Context, callID, reason string) {
	m.rejectCalls = append(m.rejectCalls, rejectCall{callID, reason})
}

func (m *mockSender) Resume(_ context.Context) (*sdk.Response, error) {
	m.resumeCalls++
	if m.resumeErr != nil {
		return nil, m.resumeErr
	}
	if len(m.resumeResponses) == 0 {
		return sdk.NewResponseForTest("resumed", nil), nil
	}
	r := m.resumeResponses[0]
	m.resumeResponses = m.resumeResponses[1:]
	return r, nil
}

// mockEventBusProvider implements EventBusProvider for testing.
type mockEventBusProvider struct {
	bus events.Bus
}

func (m *mockEventBusProvider) EventBus() events.Bus {
	return m.bus
}

// mockStateProvider implements StateProvider for testing.
type mockStateProvider struct {
	state any
	err   error
}

func (m *mockStateProvider) Snapshot(_ Sender) (any, error) {
	return m.state, m.err
}

// newTestResponse creates a Response with text content for testing.
func newTestResponse(text string, toolCalls []types.MessageToolCall) *sdk.Response {
	return sdk.NewResponseForTest(text, toolCalls)
}

// userMsg creates a pointer to a user message for passing to RunSend.
func userMsg(text string) *types.Message {
	msg := types.NewUserMessage(text)
	return &msg
}

func assistantMsg(text string, calls ...types.MessageToolCall) types.Message {
	return types.Message{Role: roleAssistant, Content: text, ToolCalls: calls}
}

func toolMsg(callID, content string) types.Message {
	r := types.NewTextToolResult(callID, "", content)
	return types.Message{Role: roleTool, Content: content, ToolResult: &r}
}

func call(id, name, args string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
}

func TestNewEventAdapter_DefaultIDs(t *testing.T) {
	a := newTestAdapter(&mockSender{}, &mockEventBusProvider{})

	assert.NotEmpty(t, a.ThreadID())
	assert.NotEmpty(t, a.RunID())
}

func TestNewEventAdapter_CustomIDs(t *testing.T) {
	a := newTestAdapter(&mockSender{}, &mockEventBusProvider{},
		WithThreadID("thread-123"),
		WithRunID("run-456"),
	)

	assert.Equal(t, "thread-123", a.ThreadID())
	assert.Equal(t, "run-456", a.RunID())
}

func TestNewEventAdapter_WithWorkflowSteps(t *testing.T) {
	a := newTestAdapter(&mockSender{}, &mockEventBusProvider{}, WithWorkflowSteps(true))

	assert.True(t, a.cfg.workflowSteps)
}

func TestRunSend_Success_EventSequence(t *testing.T) {
	sender := &mockSender{resp: newTestResponse("Hello from assistant", nil)}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithThreadID("t1"), WithRunID("r1"))

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)

	assert.Equal(t, []aguievents.EventType{
		aguievents.EventTypeRunStarted,
		aguievents.EventTypeTextMessageStart,
		aguievents.EventTypeTextMessageContent,
		aguievents.EventTypeTextMessageEnd,
		aguievents.EventTypeRunFinished,
	}, eventTypes(evts))

	runStarted := evts[0].(*aguievents.RunStartedEvent)
	assert.Equal(t, "t1", runStarted.ThreadID())
	assert.Equal(t, "r1", runStarted.RunID())
	assert.Equal(t, "Hello from assistant", evts[2].(*aguievents.TextMessageContentEvent).Delta)
	runFinished := evts[4].(*aguievents.RunFinishedEvent)
	assert.Equal(t, "t1", runFinished.ThreadID())
	assert.Equal(t, "r1", runFinished.RunID())
	assert.Nil(t, runFinished.Outcome, "a completed run carries no outcome, which means success")
}

func TestRunSend_Error_EmitsRunError(t *testing.T) {
	sender := &mockSender{err: errors.New("provider error")}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithThreadID("t1"), WithRunID("r1"))

	evts, err := sendAndCollect(t, a, "hi")
	require.Error(t, err)
	assert.Equal(t, "provider error", err.Error())

	assert.Equal(t, []aguievents.EventType{aguievents.EventTypeRunStarted, aguievents.EventTypeRunError}, eventTypes(evts))
	assert.Equal(t, "provider error", evts[1].(*aguievents.RunErrorEvent).Message)
}

func TestRunSend_ChannelClosed(t *testing.T) {
	a := newTestAdapter(&mockSender{resp: newTestResponse("ok", nil)}, &mockEventBusProvider{})

	// A short run fits in the buffer, so it completes before anyone reads.
	require.NoError(t, a.RunSend(context.Background(), userMsg("hi")))

	requireValidSequence(t, collectEvents(a.Events()))
	_, ok := <-a.Events()
	assert.False(t, ok, "channel should be closed after RunSend and drain")
}

func TestRunSend_ChannelClosedOnError(t *testing.T) {
	a := newTestAdapter(&mockSender{err: errors.New("fail")}, &mockEventBusProvider{})

	_, err := sendAndCollect(t, a, "hi")
	require.Error(t, err)

	_, ok := <-a.Events()
	assert.False(t, ok, "channel should be closed after RunSend error")
}

func TestRunSend_WithToolCalls(t *testing.T) {
	resp := newTestResponse("checking weather", []types.MessageToolCall{call("tc-1", "get_weather", `{"city":"NYC"}`)})
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "weather?")
	require.NoError(t, err)

	starts := eventsOf[*aguievents.ToolCallStartEvent](evts)
	require.Len(t, starts, 1)
	assert.Equal(t, "tc-1", starts[0].ToolCallID)
	assert.Equal(t, "get_weather", starts[0].ToolCallName)
	args := eventsOf[*aguievents.ToolCallArgsEvent](evts)
	require.Len(t, args, 1)
	assert.Equal(t, `{"city":"NYC"}`, args[0].Delta)
	assert.Len(t, eventsOf[*aguievents.ToolCallEndEvent](evts), 1)
}

func TestRunSend_WithStateProvider(t *testing.T) {
	sp := &mockStateProvider{state: map[string]any{"count": 42}}
	a := newTestAdapter(&mockSender{resp: newTestResponse("ok", nil)}, &mockEventBusProvider{}, WithStateProvider(sp))

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(evts), 2)
	assert.Equal(t, aguievents.EventTypeRunStarted, evts[0].Type())
	snapshot, ok := evts[1].(*aguievents.StateSnapshotEvent)
	require.True(t, ok)
	assert.Equal(t, 42, snapshot.Snapshot.(map[string]any)["count"])
}

func TestRunSend_StateProviderError_Skipped(t *testing.T) {
	sp := &mockStateProvider{err: errors.New("snapshot failed")}
	a := newTestAdapter(&mockSender{resp: newTestResponse("ok", nil)}, &mockEventBusProvider{}, WithStateProvider(sp))

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)
	assert.NotContains(t, eventTypes(evts), aguievents.EventTypeStateSnapshot)
}

func TestRunSend_EmptyResponseText(t *testing.T) {
	a := newTestAdapter(&mockSender{resp: newTestResponse("", nil)}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)

	got := eventTypes(evts)
	assert.Contains(t, got, aguievents.EventTypeTextMessageStart)
	assert.Contains(t, got, aguievents.EventTypeTextMessageEnd)
	assert.NotContains(t, got, aguievents.EventTypeTextMessageContent)
}

func TestRunSend_NilEventBus(t *testing.T) {
	a := newTestAdapter(&mockSender{resp: newTestResponse("ok", nil)}, &mockEventBusProvider{bus: nil})

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)
	assert.Len(t, evts, 5)
}

func TestCollectEvents(t *testing.T) {
	ch := make(chan aguievents.Event, 3)
	ch <- aguievents.NewRunStartedEvent("t", "r")
	ch <- aguievents.NewRunFinishedEvent("t", "r")
	close(ch)

	evts := collectEvents(ch)
	assert.Equal(t, []aguievents.EventType{aguievents.EventTypeRunStarted, aguievents.EventTypeRunFinished}, eventTypes(evts))
}

func TestRunSend_MultipleToolCalls(t *testing.T) {
	resp := newTestResponse("results", []types.MessageToolCall{
		call("tc-1", "tool_a", `{"a":1}`),
		call("tc-2", "tool_b", `{"b":2}`),
	})
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "do both")
	require.NoError(t, err)
	assert.Len(t, eventsOf[*aguievents.ToolCallStartEvent](evts), 2)
}

// --- Server-side tool calls come from the whole turn, not the last message ---

// A turn that called a tool and then answered holds two assistant messages.
// Response.ToolCalls() describes only the last one, which made no calls, so an
// adapter reading it emitted no TOOL_CALL_* events and lost the first text.
func TestRunSend_ServerToolCallsFromEarlierRound(t *testing.T) {
	turn := []types.Message{
		types.NewUserMessage("where is my order?"),
		assistantMsg("Let me look.", call("call-1", "lookup", `{"id":"42"}`)),
		toolMsg("call-1", `{"status":"shipped"}`),
		assistantMsg("Found it."),
	}
	resp := sdk.NewResponseForTest("Found it.", nil, sdk.WithTurnMessagesForTest(turn))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "where is my order?")
	require.NoError(t, err)

	assert.Equal(t, []aguievents.EventType{
		aguievents.EventTypeRunStarted,
		aguievents.EventTypeTextMessageStart,
		aguievents.EventTypeTextMessageContent,
		aguievents.EventTypeTextMessageEnd,
		aguievents.EventTypeToolCallStart,
		aguievents.EventTypeToolCallArgs,
		aguievents.EventTypeToolCallEnd,
		aguievents.EventTypeToolCallResult,
		aguievents.EventTypeTextMessageStart,
		aguievents.EventTypeTextMessageContent,
		aguievents.EventTypeTextMessageEnd,
		aguievents.EventTypeRunFinished,
	}, eventTypes(evts))

	texts := eventsOf[*aguievents.TextMessageContentEvent](evts)
	assert.Equal(t, "Let me look.", texts[0].Delta)
	assert.Equal(t, "Found it.", texts[1].Delta)
	start := eventsOf[*aguievents.ToolCallStartEvent](evts)[0]
	assert.Equal(t, texts[0].MessageID, *start.ParentMessageID, "the call belongs to the message that made it")
	result := eventsOf[*aguievents.ToolCallResultEvent](evts)[0]
	assert.Equal(t, "call-1", result.ToolCallID)
	assert.Equal(t, `{"status":"shipped"}`, result.Content, "the result is what the tool returned, not a status")
}

// A tool message with an error and no text reports the error as its result.
func TestRunSend_FailedServerToolReportsError(t *testing.T) {
	failed := types.Message{Role: roleTool, ToolResult: &types.MessageToolResult{ID: "call-1", Error: "timeout"}}
	turn := []types.Message{assistantMsg("", call("call-1", "lookup", `{}`)), failed, assistantMsg("Sorry.")}
	resp := sdk.NewResponseForTest("Sorry.", nil, sdk.WithTurnMessagesForTest(turn))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)

	results := eventsOf[*aguievents.ToolCallResultEvent](evts)
	require.Len(t, results, 1)
	assert.Equal(t, "timeout", results[0].Content)
	assert.Len(t, eventsOf[*aguievents.TextMessageStartEvent](evts), 1,
		"a message that only calls tools emits no empty text message")
}

// --- A slow consumer loses nothing ---

// 25 tool calls and their results overflow the 64-slot buffer. The adapter
// used to drop what did not fit, including TEXT_MESSAGE_END and RUN_FINISHED,
// so a consumer that paused saw a run that never ended. It must wait instead.
func TestRunSend_SlowConsumerLosesNoEvents(t *testing.T) {
	const n = 25
	turn := []types.Message{}
	calls := make([]types.MessageToolCall, n)
	for i := range calls {
		calls[i] = call(fmt.Sprintf("call-%d", i), "lookup", `{"q":"x"}`)
	}
	turn = append(turn, assistantMsg("Looking.", calls...))
	for i := range calls {
		turn = append(turn, toolMsg(calls[i].ID, "ok"))
	}
	turn = append(turn, assistantMsg("Done."))
	resp := sdk.NewResponseForTest("Done.", nil, sdk.WithTurnMessagesForTest(turn))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	runDone := make(chan error, 1)
	go func() { runDone <- a.RunSend(context.Background(), userMsg("go")) }()

	// Read nothing until the buffer is full (or the run has ended).
	require.Eventually(t, func() bool {
		select {
		case err := <-runDone:
			runDone <- err
			return true
		default:
			return len(a.events) == cap(a.events)
		}
	}, collectTimeout, time.Millisecond)

	evts := collectEvents(a.Events())
	require.NoError(t, <-runDone)
	requireValidSequence(t, evts)
	assert.Len(t, evts, 1+3+n*3+n+3+1, "every event is delivered")
	assert.Equal(t, aguievents.EventTypeRunFinished, evts[len(evts)-1].Type())
}

// A consumer that goes away cancels the run's context; the adapter stops
// waiting for it and RunSend returns.
func TestRunSend_CanceledContextReleasesBlockedRun(t *testing.T) {
	turn := []types.Message{}
	for i := 0; i < eventChannelBufferSize; i++ {
		turn = append(turn, assistantMsg(fmt.Sprintf("part %d", i)))
	}
	resp := sdk.NewResponseForTest("", nil, sdk.WithTurnMessagesForTest(turn))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- a.RunSend(ctx, userMsg("go")) }()

	require.Eventually(t, func() bool { return len(a.events) == cap(a.events) }, collectTimeout, time.Millisecond)
	cancel()

	select {
	case err := <-runDone:
		assert.NoError(t, err)
	case <-time.After(collectTimeout):
		t.Fatal("RunSend stayed blocked after its context was canceled")
	}
}

// --- Client (frontend) tools ---

// A suspended client call appears both in the last assistant message's tool
// calls and in ClientTools(). Emitting both reopened the call and doubled its
// arguments: {"q":"a"}{"q":"a"}.
func TestRunSend_ClientToolEmittedOnce(t *testing.T) {
	pending := []sdk.PendingClientTool{{CallID: "ct-1", ToolName: "get_location", Args: map[string]any{"q": "a"}}}
	resp := sdk.NewResponseForTest("need location", []types.MessageToolCall{call("ct-1", "get_location", `{"q":"a"}`)},
		sdk.WithClientToolsForTest(pending))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "where am I?")
	require.NoError(t, err)

	require.Len(t, eventsOf[*aguievents.ToolCallStartEvent](evts), 1)
	args := eventsOf[*aguievents.ToolCallArgsEvent](evts)
	require.Len(t, args, 1)
	assert.Equal(t, `{"q":"a"}`, args[0].Delta)
}

// AG-UI: a producer MUST NOT answer a frontend tool. The run finishes with the
// call unanswered, with no result and no proprietary signal, and the answer
// arrives in the next run's input.
func TestRunSend_ClientTools_NoProvider_LeavesCallUnanswered(t *testing.T) {
	pending := []sdk.PendingClientTool{{CallID: "ct-1", ToolName: "get_location"}}
	sender := &mockSender{resp: sdk.NewResponseForTest("pending", nil, sdk.WithClientToolsForTest(pending))}
	a := newTestAdapter(sender, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)

	got := eventTypes(evts)
	assert.Contains(t, got, aguievents.EventTypeToolCallStart)
	assert.NotContains(t, got, aguievents.EventTypeToolCallResult)
	assert.NotContains(t, got, aguievents.EventTypeCustom)
	assert.Equal(t, aguievents.EventTypeRunFinished, got[len(got)-1])
	assert.Nil(t, evts[len(evts)-1].(*aguievents.RunFinishedEvent).Outcome,
		"a run that stops on a frontend tool is a completed run, not an interrupted one")
	assert.Zero(t, sender.resumeCalls)
	assert.Empty(t, sender.sendToolResultCalls)
}

// The next run carries the application's answer. RunResume hands it to the
// conversation, resumes, and does not echo the answer back.
func TestRunResume_AnswersPendingCallAndContinues(t *testing.T) {
	resumed := sdk.NewResponseForTest("You are in NYC.", nil, sdk.WithTurnMessagesForTest([]types.Message{
		toolMsg("ct-1", `{"city":"NYC"}`),
		assistantMsg("You are in NYC."),
	}))
	sender := &mockSender{resumeResponses: []*sdk.Response{resumed}}
	a := newTestAdapter(sender, &mockEventBusProvider{})

	evts, err := runAndCollect(t, a, func(ctx context.Context) error {
		return a.RunResume(ctx, []ToolResult{{CallID: "ct-1", Result: json.RawMessage(`{"city":"NYC"}`)}})
	})
	require.NoError(t, err)

	require.Len(t, sender.sendToolResultCalls, 1)
	assert.Equal(t, "ct-1", sender.sendToolResultCalls[0].callID)
	assert.Equal(t, 1, sender.resumeCalls)
	assert.NotContains(t, eventTypes(evts), aguievents.EventTypeToolCallResult,
		"the answer came from the application; echoing it would duplicate its tool message")
	texts := eventsOf[*aguievents.TextMessageContentEvent](evts)
	require.Len(t, texts, 1)
	assert.Equal(t, "You are in NYC.", texts[0].Delta)
}

func TestRunResume_ErrorAndRejection(t *testing.T) {
	sender := &mockSender{}
	a := newTestAdapter(sender, &mockEventBusProvider{})

	_, err := runAndCollect(t, a, func(ctx context.Context) error {
		return a.RunResume(ctx, []ToolResult{
			{CallID: "ct-1", Error: "permission denied"},
			{CallID: "ct-2", Rejected: true, Reason: "user declined"},
		})
	})
	require.NoError(t, err)

	require.Len(t, sender.sendToolResultCalls, 1)
	assert.Equal(t, "Tool error: permission denied", sender.sendToolResultCalls[0].result)
	require.Len(t, sender.rejectCalls, 1)
	assert.Equal(t, rejectCall{"ct-2", "user declined"}, sender.rejectCalls[0])
}

func TestRunResume_ResolveFailureIsRunError(t *testing.T) {
	sender := &mockSender{sendToolResultErr: errors.New("closed")}
	a := newTestAdapter(sender, &mockEventBusProvider{})

	evts, err := runAndCollect(t, a, func(ctx context.Context) error {
		return a.RunResume(ctx, []ToolResult{{CallID: "ct-1", Result: "x"}})
	})
	require.Error(t, err)
	assert.Equal(t, aguievents.EventTypeRunError, evts[len(evts)-1].Type())
	assert.Zero(t, sender.resumeCalls)
}

// multimodalSender also accepts results made of content parts.
type multimodalSender struct {
	mockSender
	parts map[string][]types.ContentPart
}

func (m *multimodalSender) SendToolResultMultimodal(_ context.Context, callID string, parts []types.ContentPart) error {
	if m.parts == nil {
		m.parts = map[string][]types.ContentPart{}
	}
	m.parts[callID] = parts
	return nil
}

func TestRunResume_PartsResult(t *testing.T) {
	text := "a screenshot"
	parts := []types.ContentPart{{Type: types.ContentTypeText, Text: &text}}

	mm := &multimodalSender{}
	a := newTestAdapter(mm, &mockEventBusProvider{})
	_, err := runAndCollect(t, a, func(ctx context.Context) error {
		return a.RunResume(ctx, []ToolResult{{CallID: "ct-1", Result: parts}})
	})
	require.NoError(t, err)
	assert.Equal(t, parts, mm.parts["ct-1"], "parts go to the multimodal path when the conversation has one")

	plain := &mockSender{}
	b := newTestAdapter(plain, &mockEventBusProvider{})
	_, err = runAndCollect(t, b, func(ctx context.Context) error {
		return b.RunResume(ctx, []ToolResult{{CallID: "ct-1", Result: parts}})
	})
	require.NoError(t, err)
	require.Len(t, plain.sendToolResultCalls, 1)
	assert.Equal(t, "a screenshot", plain.sendToolResultCalls[0].result, "otherwise their text is sent")
}

// --- Client tools answered on the server by a ToolResultProvider ---

func TestRunSend_ClientTools_WithProvider(t *testing.T) {
	pending := []sdk.PendingClientTool{{CallID: "ct-1", ToolName: "get_location", Args: map[string]any{"city": "NYC"}}}
	sender := &mockSender{
		resp:            sdk.NewResponseForTest("need location", nil, sdk.WithClientToolsForTest(pending)),
		resumeResponses: []*sdk.Response{sdk.NewResponseForTest("location is NYC", nil)},
	}
	providerCalled := false
	provider := func(_ context.Context, tools []sdk.PendingClientTool) ([]ToolResult, error) {
		providerCalled = true
		require.Len(t, tools, 1)
		assert.Equal(t, "ct-1", tools[0].CallID)
		return []ToolResult{{CallID: "ct-1", Result: map[string]any{"lat": 40.7}}}, nil
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "where am I?")
	require.NoError(t, err)

	assert.True(t, providerCalled)
	require.Len(t, sender.sendToolResultCalls, 1)
	assert.Equal(t, "ct-1", sender.sendToolResultCalls[0].callID)

	assert.NotContains(t, eventTypes(evts), aguievents.EventTypeCustom, "no proprietary mid-run signal")
	results := eventsOf[*aguievents.ToolCallResultEvent](evts)
	require.Len(t, results, 1)
	assert.Equal(t, `{"lat":40.7}`, results[0].Content, "the result itself, not the word \"completed\"")
	texts := eventsOf[*aguievents.TextMessageContentEvent](evts)
	require.Len(t, texts, 2)
	assert.Equal(t, "location is NYC", texts[1].Delta)
}

// The resumed turn's tool message is what the model saw; the result event
// carries that, once.
func TestRunSend_ClientTools_WithProvider_PrefersResumedToolMessage(t *testing.T) {
	pending := []sdk.PendingClientTool{{CallID: "ct-1", ToolName: "get_location"}}
	resumed := sdk.NewResponseForTest("ok", nil, sdk.WithTurnMessagesForTest([]types.Message{
		toolMsg("ct-1", `"as the model saw it"`),
		assistantMsg("ok"),
	}))
	sender := &mockSender{
		resp:            sdk.NewResponseForTest("", nil, sdk.WithClientToolsForTest(pending)),
		resumeResponses: []*sdk.Response{resumed},
	}
	provider := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "ct-1", Result: "as the model saw it"}}, nil
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)

	results := eventsOf[*aguievents.ToolCallResultEvent](evts)
	require.Len(t, results, 1)
	assert.Equal(t, `"as the model saw it"`, results[0].Content)
}

func TestRunSend_ClientTools_ProviderError(t *testing.T) {
	pending := []sdk.PendingClientTool{{CallID: "ct-1", ToolName: "get_location"}}
	sender := &mockSender{resp: sdk.NewResponseForTest("pending", nil, sdk.WithClientToolsForTest(pending))}
	provider := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return nil, errors.New("provider failed")
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "hi")
	require.Error(t, err)
	assert.Equal(t, "provider failed", err.Error())

	last, ok := evts[len(evts)-1].(*aguievents.RunErrorEvent)
	require.True(t, ok)
	assert.Equal(t, "provider failed", last.Message)
}

func TestRunSend_ClientTools_ResumeError(t *testing.T) {
	pending := []sdk.PendingClientTool{{CallID: "ct-1", ToolName: "get_location"}}
	sender := &mockSender{
		resp:      sdk.NewResponseForTest("pending", nil, sdk.WithClientToolsForTest(pending)),
		resumeErr: errors.New("resume failed"),
	}
	provider := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "ct-1", Result: "x"}}, nil
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "hi")
	require.Error(t, err)
	assert.Equal(t, aguievents.EventTypeRunError, evts[len(evts)-1].Type())
}

// A provider that answers only some calls leaves the rest for the
// application: the run ends without resuming.
func TestRunSend_ClientTools_ProviderAnswersSome(t *testing.T) {
	pending := []sdk.PendingClientTool{
		{CallID: "ct-1", ToolName: "server_side"},
		{CallID: "ct-2", ToolName: "frontend"},
	}
	sender := &mockSender{resp: sdk.NewResponseForTest("", nil, sdk.WithClientToolsForTest(pending))}
	provider := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{{CallID: "ct-1", Result: "done"}}, nil
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)

	assert.Zero(t, sender.resumeCalls, "ct-2 is unanswered, so the turn cannot resume")
	results := eventsOf[*aguievents.ToolCallResultEvent](evts)
	require.Len(t, results, 1)
	assert.Equal(t, "ct-1", results[0].ToolCallID)
	assert.Equal(t, "done", results[0].Content)
}

func TestRunSend_ClientTools_MultipleRounds(t *testing.T) {
	round1 := sdk.NewResponseForTest("round 1", nil,
		sdk.WithClientToolsForTest([]sdk.PendingClientTool{{CallID: "ct-1", ToolName: "get_location"}}))
	round2 := sdk.NewResponseForTest("round 2", nil,
		sdk.WithClientToolsForTest([]sdk.PendingClientTool{{CallID: "ct-2", ToolName: "confirm_action"}}))
	sender := &mockSender{resp: round1, resumeResponses: []*sdk.Response{round2, sdk.NewResponseForTest("done", nil)}}

	providerCallCount := 0
	provider := func(_ context.Context, tools []sdk.PendingClientTool) ([]ToolResult, error) {
		providerCallCount++
		return []ToolResult{{CallID: tools[0].CallID, Result: "ok"}}, nil
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)

	assert.Equal(t, 2, providerCallCount)
	require.Len(t, sender.sendToolResultCalls, 2)
	assert.Equal(t, "ct-1", sender.sendToolResultCalls[0].callID)
	assert.Equal(t, "ct-2", sender.sendToolResultCalls[1].callID)
	assert.Len(t, eventsOf[*aguievents.ToolCallResultEvent](evts), 2)
	assert.Equal(t, aguievents.EventTypeRunFinished, evts[len(evts)-1].Type())
}

func TestRunSend_ClientTools_Rejection(t *testing.T) {
	pending := []sdk.PendingClientTool{
		{CallID: "ct-1", ToolName: "get_location"},
		{CallID: "ct-2", ToolName: "send_email"},
	}
	sender := &mockSender{
		resp:            sdk.NewResponseForTest("need tools", nil, sdk.WithClientToolsForTest(pending)),
		resumeResponses: []*sdk.Response{sdk.NewResponseForTest("handled rejection", nil)},
	}
	provider := func(context.Context, []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{
			{CallID: "ct-1", Result: map[string]any{"lat": 40.7}},
			{CallID: "ct-2", Rejected: true, Reason: "user declined"},
		}, nil
	}
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithToolResultProvider(provider))

	evts, err := sendAndCollect(t, a, "do things")
	require.NoError(t, err)

	require.Len(t, sender.sendToolResultCalls, 1)
	assert.Equal(t, "ct-1", sender.sendToolResultCalls[0].callID)
	require.Len(t, sender.rejectCalls, 1)
	assert.Equal(t, rejectCall{"ct-2", "user declined"}, sender.rejectCalls[0])

	var contents []string
	for _, r := range eventsOf[*aguievents.ToolCallResultEvent](evts) {
		contents = append(contents, r.Content)
	}
	assert.Equal(t, []string{`{"lat":40.7}`, "Tool rejected: user declined"}, contents,
		"each result says what the model was told")
}

// --- Approval-held tools (OnToolAsync) ---

// A held call is waiting for approval: it has no result yet. The adapter used
// to fabricate TOOL_CALL_RESULT{"pending"}. AG-UI ends a run that stops to ask
// with the interrupt outcome, naming what it waits for.
func TestRunSend_ApprovalHoldEndsWithInterrupt(t *testing.T) {
	held := []sdk.PendingTool{{ID: "call-1", Name: "send_message", Reason: "requires_approval", Message: "Approve send?"}}
	turn := []types.Message{assistantMsg("Sending.", call("call-1", "send_message", `{"body":"hi"}`))}
	resp := sdk.NewResponseForTest("Sending.", []types.MessageToolCall{call("call-1", "send_message", `{"body":"hi"}`)},
		sdk.WithTurnMessagesForTest(turn), sdk.WithPendingToolsForTest(held))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "send it")
	require.NoError(t, err)

	assert.NotContains(t, eventTypes(evts), aguievents.EventTypeToolCallResult, "a held call has no result yet")
	finished := evts[len(evts)-1].(*aguievents.RunFinishedEvent)
	require.NotNil(t, finished.Outcome)
	assert.Equal(t, aguievents.RunFinishedOutcomeTypeInterrupt, finished.Outcome.Type)
	require.Len(t, finished.Outcome.Interrupts, 1)
	in := finished.Outcome.Interrupts[0]
	assert.Equal(t, "call-1", in.ID)
	assert.Equal(t, "call-1", in.ToolCallID)
	assert.Equal(t, "requires_approval", in.Reason)
	assert.Equal(t, "Approve send?", in.Message)
}

func TestRunSend_ApprovalHoldWithoutReason(t *testing.T) {
	resp := sdk.NewResponseForTest("", []types.MessageToolCall{call("call-1", "x", `{}`)},
		sdk.WithPendingToolsForTest([]sdk.PendingTool{{ID: "call-1", Name: "x"}}))
	a := newTestAdapter(&mockSender{resp: resp}, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "go")
	require.NoError(t, err)
	finished := evts[len(evts)-1].(*aguievents.RunFinishedEvent)
	require.NotNil(t, finished.Outcome)
	assert.Equal(t, interruptReasonToolCall, finished.Outcome.Interrupts[0].Reason)
}

// continuingSender also continues after approval holds.
type continuingSender struct {
	mockSender
	continueResp *sdk.Response
	continued    bool
}

func (c *continuingSender) Continue(context.Context) (*sdk.Response, error) {
	c.continued = true
	return c.continueResp, nil
}

// After the application resolves the hold, the continued turn reports the
// approved tool's result: only the agent knows it.
func TestRunContinue_EmitsApprovedResult(t *testing.T) {
	sender := &continuingSender{continueResp: sdk.NewResponseForTest("Sent.", nil, sdk.WithTurnMessagesForTest(
		[]types.Message{toolMsg("call-1", `{"sent":true}`), assistantMsg("Sent.")}))}
	a := newTestAdapter(sender, &mockEventBusProvider{})

	held := []aguievents.Event{
		aguievents.NewRunStartedEvent("t", "r0"),
		aguievents.NewToolCallStartEvent("call-1", "send_message"),
		aguievents.NewToolCallEndEvent("call-1"),
		aguievents.NewRunFinishedEvent("t", "r0"),
	}
	evts, err := continueAndCollect(t, a, held, a.RunContinue)
	require.NoError(t, err)

	assert.True(t, sender.continued)
	results := eventsOf[*aguievents.ToolCallResultEvent](evts)
	require.Len(t, results, 1)
	assert.Equal(t, `{"sent":true}`, results[0].Content)
}

func TestRunContinue_Unsupported(t *testing.T) {
	a := newTestAdapter(&mockSender{}, &mockEventBusProvider{})

	evts, err := runAndCollect(t, a, a.RunContinue)
	require.ErrorIs(t, err, ErrContinueUnsupported)
	assert.Equal(t, aguievents.EventTypeRunError, evts[len(evts)-1].Type())
}

// --- Workflow steps ---

// workflowMockSender reports a workflow state that a Send can change.
type workflowMockSender struct {
	mockSender
	mu        sync.Mutex
	state     string
	nextState string
}

func (w *workflowMockSender) Send(ctx context.Context, msg any, opts ...sdk.SendOption) (*sdk.Response, error) {
	resp, err := w.mockSender.Send(ctx, msg, opts...)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.nextState != "" {
		w.state = w.nextState
	}
	return resp, err
}

func (w *workflowMockSender) CurrentState() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

type stepEvent struct {
	typ  aguievents.EventType
	name string
}

func stepEvents(evts []aguievents.Event) []stepEvent {
	var out []stepEvent
	for _, ev := range evts {
		switch e := ev.(type) {
		case *aguievents.StepStartedEvent:
			out = append(out, stepEvent{e.Type(), e.StepName})
		case *aguievents.StepFinishedEvent:
			out = append(out, stepEvent{e.Type(), e.StepName})
		}
	}
	return out
}

// The step a run works in is opened at the start and closed before
// RUN_FINISHED. The old adapter finished a step it never started, never
// closed the one it started, and raced the event bus.
func TestRunSend_WorkflowSteps_Transition(t *testing.T) {
	sender := &workflowMockSender{state: "greeting", nextState: "triage"}
	sender.resp = newTestResponse("transitioning", nil)
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithWorkflowSteps(true))

	evts, err := sendAndCollect(t, a, "help")
	require.NoError(t, err)

	assert.Equal(t, []stepEvent{
		{aguievents.EventTypeStepStarted, "greeting"},
		{aguievents.EventTypeStepFinished, "greeting"},
		{aguievents.EventTypeStepStarted, "triage"},
		{aguievents.EventTypeStepFinished, "triage"},
	}, stepEvents(evts))
	assert.Equal(t, aguievents.EventTypeRunFinished, evts[len(evts)-1].Type(), "no step event after the run closes")
}

func TestRunSend_WorkflowSteps_NoTransition(t *testing.T) {
	sender := &workflowMockSender{state: "intake"}
	sender.resp = newTestResponse("ok", nil)
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithWorkflowSteps(true))

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)
	assert.Equal(t, []stepEvent{
		{aguievents.EventTypeStepStarted, "intake"},
		{aguievents.EventTypeStepFinished, "intake"},
	}, stepEvents(evts))
}

func TestRunSend_WorkflowSteps_ErrorClosesRun(t *testing.T) {
	sender := &workflowMockSender{state: "intake"}
	sender.err = errors.New("boom")
	a := newTestAdapter(sender, &mockEventBusProvider{}, WithWorkflowSteps(true))

	evts, err := sendAndCollect(t, a, "hi")
	require.Error(t, err)
	assert.Equal(t, aguievents.EventTypeRunError, evts[len(evts)-1].Type(), "RUN_ERROR ends the open step with the run")
}

func TestRunSend_WorkflowSteps_Disabled_NoStepEvents(t *testing.T) {
	sender := &workflowMockSender{state: "a", nextState: "b"}
	sender.resp = newTestResponse("ok", nil)
	a := newTestAdapter(sender, &mockEventBusProvider{})

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)
	assert.Empty(t, stepEvents(evts))
}

func TestRunSend_WorkflowSteps_NotAWorkflow(t *testing.T) {
	a := newTestAdapter(&mockSender{resp: newTestResponse("ok", nil)}, &mockEventBusProvider{}, WithWorkflowSteps(true))

	evts, err := sendAndCollect(t, a, "hi")
	require.NoError(t, err)
	assert.Empty(t, stepEvents(evts))
}
