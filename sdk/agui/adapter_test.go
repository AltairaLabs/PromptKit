package agui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
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

func TestNewEventAdapter_DefaultIDs(t *testing.T) {
	sender := &mockSender{}
	ebp := &mockEventBusProvider{}
	a := newTestAdapter(sender, ebp)

	assert.NotEmpty(t, a.ThreadID())
	assert.NotEmpty(t, a.RunID())
}

func TestNewEventAdapter_CustomIDs(t *testing.T) {
	sender := &mockSender{}
	ebp := &mockEventBusProvider{}
	a := newTestAdapter(sender, ebp,
		WithThreadID("thread-123"),
		WithRunID("run-456"),
	)

	assert.Equal(t, "thread-123", a.ThreadID())
	assert.Equal(t, "run-456", a.RunID())
}

func TestNewEventAdapter_WithWorkflowSteps(t *testing.T) {
	sender := &mockSender{}
	ebp := &mockEventBusProvider{}
	a := newTestAdapter(sender, ebp, WithWorkflowSteps(true))

	assert.True(t, a.cfg.workflowSteps)
}

func TestRunSend_Success_EventSequence(t *testing.T) {
	resp := newTestResponse("Hello from assistant", nil)
	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{}

	a := newTestAdapter(sender, ebp,
		WithThreadID("t1"),
		WithRunID("r1"),
	)

	// Collect events in background
	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.NoError(t, err)

	// Wait for collector
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Verify event sequence
	require.GreaterOrEqual(t, len(evts), 4, "expected at least 4 events")

	assert.Equal(t, aguievents.EventTypeRunStarted, evts[0].Type())
	assert.Equal(t, aguievents.EventTypeTextMessageStart, evts[1].Type())
	assert.Equal(t, aguievents.EventTypeTextMessageContent, evts[2].Type())
	assert.Equal(t, aguievents.EventTypeTextMessageEnd, evts[3].Type())
	assert.Equal(t, aguievents.EventTypeRunFinished, evts[4].Type())

	// Verify RunStarted has correct thread/run IDs
	runStarted, ok := evts[0].(*aguievents.RunStartedEvent)
	require.True(t, ok)
	assert.Equal(t, "t1", runStarted.ThreadID())
	assert.Equal(t, "r1", runStarted.RunID())

	// Verify text content delta
	contentEvt, ok := evts[2].(*aguievents.TextMessageContentEvent)
	require.True(t, ok)
	assert.Equal(t, "Hello from assistant", contentEvt.Delta)

	// Verify RunFinished has correct thread/run IDs
	runFinished, ok := evts[4].(*aguievents.RunFinishedEvent)
	require.True(t, ok)
	assert.Equal(t, "t1", runFinished.ThreadID())
	assert.Equal(t, "r1", runFinished.RunID())
}

func TestRunSend_Error_EmitsRunError(t *testing.T) {
	sender := &mockSender{err: errors.New("provider error")}
	ebp := &mockEventBusProvider{}

	a := newTestAdapter(sender, ebp,
		WithThreadID("t1"),
		WithRunID("r1"),
	)

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.Error(t, err)
	assert.Equal(t, "provider error", err.Error())

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Should have RunStarted then RunError
	require.GreaterOrEqual(t, len(evts), 2)
	assert.Equal(t, aguievents.EventTypeRunStarted, evts[0].Type())
	assert.Equal(t, aguievents.EventTypeRunError, evts[1].Type())

	runErr, ok := evts[1].(*aguievents.RunErrorEvent)
	require.True(t, ok)
	assert.Equal(t, "provider error", runErr.Message)
}

func TestRunSend_ChannelClosed(t *testing.T) {
	resp := newTestResponse("ok", nil)
	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{}
	a := newTestAdapter(sender, ebp)

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.NoError(t, err)

	// Drain all buffered events first
	for range a.Events() {
		// drain until channel closes
	}

	// After draining, a second receive should return zero value and false
	_, ok := <-a.Events()
	assert.False(t, ok, "channel should be closed after RunSend and drain")
}

func TestRunSend_ChannelClosedOnError(t *testing.T) {
	sender := &mockSender{err: errors.New("fail")}
	ebp := &mockEventBusProvider{}
	a := newTestAdapter(sender, ebp)

	// Drain events first
	go func() {
		for range a.Events() {
			// drain
		}
	}()

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.Error(t, err)

	// Give time for channel close
	time.Sleep(50 * time.Millisecond)

	// Channel should be closed
	_, ok := <-a.Events()
	assert.False(t, ok, "channel should be closed after RunSend error")
}

func TestRunSend_WithToolCalls(t *testing.T) {
	toolCalls := []types.MessageToolCall{
		{
			ID:   "tc-1",
			Name: "get_weather",
			Args: json.RawMessage(`{"city":"NYC"}`),
		},
	}
	resp := newTestResponse("checking weather", toolCalls)
	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{}

	a := newTestAdapter(sender, ebp)

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("weather?"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Find tool call events
	var toolCallTypes []aguievents.EventType
	for _, ev := range evts {
		switch ev.Type() {
		case aguievents.EventTypeToolCallStart,
			aguievents.EventTypeToolCallArgs,
			aguievents.EventTypeToolCallEnd:
			toolCallTypes = append(toolCallTypes, ev.Type())
		}
	}

	require.Len(t, toolCallTypes, 3)
	assert.Equal(t, aguievents.EventTypeToolCallStart, toolCallTypes[0])
	assert.Equal(t, aguievents.EventTypeToolCallArgs, toolCallTypes[1])
	assert.Equal(t, aguievents.EventTypeToolCallEnd, toolCallTypes[2])

	// Verify ToolCallStart details
	for _, ev := range evts {
		if ev.Type() == aguievents.EventTypeToolCallStart {
			tcStart, ok := ev.(*aguievents.ToolCallStartEvent)
			require.True(t, ok)
			assert.Equal(t, "tc-1", tcStart.ToolCallID)
			assert.Equal(t, "get_weather", tcStart.ToolCallName)
			break
		}
	}

	// Verify ToolCallArgs delta
	for _, ev := range evts {
		if ev.Type() == aguievents.EventTypeToolCallArgs {
			tcArgs, ok := ev.(*aguievents.ToolCallArgsEvent)
			require.True(t, ok)
			assert.Equal(t, "tc-1", tcArgs.ToolCallID)
			assert.Equal(t, `{"city":"NYC"}`, tcArgs.Delta)
			break
		}
	}
}

func TestRunSend_WithStateProvider(t *testing.T) {
	resp := newTestResponse("ok", nil)
	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{}

	sp := &mockStateProvider{
		state: map[string]any{"count": 42},
	}

	a := newTestAdapter(sender, ebp,
		WithStateProvider(sp),
	)

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Should have StateSnapshot after RunStarted
	require.GreaterOrEqual(t, len(evts), 2)
	assert.Equal(t, aguievents.EventTypeRunStarted, evts[0].Type())
	assert.Equal(t, aguievents.EventTypeStateSnapshot, evts[1].Type())

	snapshot, ok := evts[1].(*aguievents.StateSnapshotEvent)
	require.True(t, ok)
	stateMap, ok := snapshot.Snapshot.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 42, stateMap["count"])
}

func TestRunSend_StateProviderError_Skipped(t *testing.T) {
	resp := newTestResponse("ok", nil)
	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{}

	sp := &mockStateProvider{err: errors.New("snapshot failed")}

	a := newTestAdapter(sender, ebp,
		WithStateProvider(sp),
	)

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// StateSnapshot should be skipped on error
	for _, ev := range evts {
		assert.NotEqual(t, aguievents.EventTypeStateSnapshot, ev.Type(),
			"StateSnapshot should not be emitted when provider returns error")
	}
}

func TestRunSend_EmptyResponseText(t *testing.T) {
	resp := newTestResponse("", nil)
	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{}
	a := newTestAdapter(sender, ebp)

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Should still have start/end but no content event
	var evtTypes []aguievents.EventType
	for _, ev := range evts {
		evtTypes = append(evtTypes, ev.Type())
	}

	assert.Contains(t, evtTypes, aguievents.EventTypeRunStarted)
	assert.Contains(t, evtTypes, aguievents.EventTypeTextMessageStart)
	assert.Contains(t, evtTypes, aguievents.EventTypeTextMessageEnd)
	assert.Contains(t, evtTypes, aguievents.EventTypeRunFinished)
	assert.NotContains(t, evtTypes, aguievents.EventTypeTextMessageContent)
}

func TestRunSend_WithEventBusToolResults(t *testing.T) {
	toolCalls := []types.MessageToolCall{
		{ID: "tc-1", Name: "lookup", Args: json.RawMessage(`{}`)},
	}
	resp := newTestResponse("done", toolCalls)

	bus := events.NewEventBus()
	defer bus.Close()

	// The sender will publish a tool call completed event during Send
	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{bus: bus}

	a := newTestAdapter(sender, ebp)

	// Simulate a tool result being published before Send returns
	// In real usage, the pipeline publishes these during execution
	go func() {
		// Small delay to ensure subscription is active
		time.Sleep(10 * time.Millisecond)
		bus.Publish(&events.Event{
			Type: events.EventToolCallCompleted,
			Data: &events.ToolCallCompletedData{
				CallID:   "tc-1",
				ToolName: "lookup",
				Status:   "success",
			},
		})
	}()

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	// Add small delay so the event bus publish happens before Send returns
	origSender := a.sender
	a.sender = &delaySender{inner: origSender, delay: 50 * time.Millisecond}

	err := a.RunSend(context.Background(), userMsg("lookup"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Look for ToolCallResult event
	var hasResult bool
	for _, ev := range evts {
		if ev.Type() == aguievents.EventTypeToolCallResult {
			hasResult = true
			tcResult, ok := ev.(*aguievents.ToolCallResultEvent)
			require.True(t, ok)
			assert.Equal(t, "tc-1", tcResult.ToolCallID)
			assert.Equal(t, "success", tcResult.Content)
		}
	}
	assert.True(t, hasResult, "expected a ToolCallResult event")
}

func TestRunSend_NilEventBus(t *testing.T) {
	resp := newTestResponse("ok", nil)
	sender := &mockSender{resp: resp}
	// nil event bus provider
	ebp := &mockEventBusProvider{bus: nil}

	a := newTestAdapter(sender, ebp)

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Should still work without event bus
	assert.GreaterOrEqual(t, len(evts), 4)
}

func TestCollectEvents(t *testing.T) {
	ch := make(chan aguievents.Event, 3)
	ch <- aguievents.NewRunStartedEvent("t", "r")
	ch <- aguievents.NewRunFinishedEvent("t", "r")
	close(ch)

	evts := collectEvents(ch)
	require.Len(t, evts, 2)
	assert.Equal(t, aguievents.EventTypeRunStarted, evts[0].Type())
	assert.Equal(t, aguievents.EventTypeRunFinished, evts[1].Type())
}

func TestRunSend_MultipleToolCalls(t *testing.T) {
	toolCalls := []types.MessageToolCall{
		{ID: "tc-1", Name: "tool_a", Args: json.RawMessage(`{"a":1}`)},
		{ID: "tc-2", Name: "tool_b", Args: json.RawMessage(`{"b":2}`)},
	}
	resp := newTestResponse("results", toolCalls)
	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{}

	a := newTestAdapter(sender, ebp)

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("do both"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Count tool call start events
	var startCount int
	for _, ev := range evts {
		if ev.Type() == aguievents.EventTypeToolCallStart {
			startCount++
		}
	}
	assert.Equal(t, 2, startCount, "expected 2 ToolCallStart events for 2 tool calls")
}

func TestRunSend_WorkflowSteps_EmitsStepEvents(t *testing.T) {
	resp := newTestResponse("transitioning", nil)
	bus := events.NewEventBus()
	defer bus.Close()

	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{bus: bus}

	a := newTestAdapter(sender, ebp, WithWorkflowSteps(true))

	// Simulate a workflow transition during Send
	go func() {
		time.Sleep(10 * time.Millisecond)
		bus.Publish(&events.Event{
			Type: events.EventWorkflowTransitioned,
			Data: &events.WorkflowTransitionedData{
				FromState: "greeting",
				ToState:   "triage",
			},
		})
	}()

	// Use delay sender so transition fires before Send returns
	a.sender = &delaySender{inner: sender, delay: 50 * time.Millisecond}

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("help"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Find step events
	var stepTypes []aguievents.EventType
	var stepNames []string
	for _, ev := range evts {
		switch ev.Type() {
		case aguievents.EventTypeStepStarted:
			stepTypes = append(stepTypes, ev.Type())
			stepNames = append(stepNames, ev.(*aguievents.StepStartedEvent).StepName)
		case aguievents.EventTypeStepFinished:
			stepTypes = append(stepTypes, ev.Type())
			stepNames = append(stepNames, ev.(*aguievents.StepFinishedEvent).StepName)
		}
	}

	require.Len(t, stepTypes, 2, "expected StepFinished + StepStarted")
	assert.Equal(t, aguievents.EventTypeStepFinished, stepTypes[0])
	assert.Equal(t, "greeting", stepNames[0])
	assert.Equal(t, aguievents.EventTypeStepStarted, stepTypes[1])
	assert.Equal(t, "triage", stepNames[1])
}

func TestRunSend_WorkflowSteps_InitialState_NoFinished(t *testing.T) {
	resp := newTestResponse("starting", nil)
	bus := events.NewEventBus()
	defer bus.Close()

	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{bus: bus}

	a := newTestAdapter(sender, ebp, WithWorkflowSteps(true))

	// Simulate initial workflow entry (empty FromState)
	go func() {
		time.Sleep(10 * time.Millisecond)
		bus.Publish(&events.Event{
			Type: events.EventWorkflowTransitioned,
			Data: &events.WorkflowTransitionedData{
				FromState: "",
				ToState:   "greeting",
			},
		})
	}()

	a.sender = &delaySender{inner: sender, delay: 50 * time.Millisecond}

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	var stepTypes []aguievents.EventType
	for _, ev := range evts {
		if ev.Type() == aguievents.EventTypeStepStarted || ev.Type() == aguievents.EventTypeStepFinished {
			stepTypes = append(stepTypes, ev.Type())
		}
	}

	// Only StepStarted, no StepFinished since FromState was empty
	require.Len(t, stepTypes, 1)
	assert.Equal(t, aguievents.EventTypeStepStarted, stepTypes[0])
}

func TestRunSend_WorkflowSteps_Disabled_NoStepEvents(t *testing.T) {
	resp := newTestResponse("ok", nil)
	bus := events.NewEventBus()
	defer bus.Close()

	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{bus: bus}

	// workflowSteps NOT enabled (default)
	a := newTestAdapter(sender, ebp)

	go func() {
		time.Sleep(10 * time.Millisecond)
		bus.Publish(&events.Event{
			Type: events.EventWorkflowTransitioned,
			Data: &events.WorkflowTransitionedData{
				FromState: "a",
				ToState:   "b",
			},
		})
	}()

	a.sender = &delaySender{inner: sender, delay: 50 * time.Millisecond}

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// No step events should be emitted
	for _, ev := range evts {
		assert.NotEqual(t, aguievents.EventTypeStepStarted, ev.Type())
		assert.NotEqual(t, aguievents.EventTypeStepFinished, ev.Type())
	}
}

func TestRunSend_WorkflowSteps_MultipleTransitions(t *testing.T) {
	resp := newTestResponse("processing", nil)
	bus := events.NewEventBus()
	defer bus.Close()

	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{bus: bus}

	a := newTestAdapter(sender, ebp, WithWorkflowSteps(true))

	go func() {
		time.Sleep(10 * time.Millisecond)
		bus.Publish(&events.Event{
			Type: events.EventWorkflowTransitioned,
			Data: &events.WorkflowTransitionedData{
				FromState: "intake",
				ToState:   "processing",
			},
		})
		time.Sleep(5 * time.Millisecond)
		bus.Publish(&events.Event{
			Type: events.EventWorkflowTransitioned,
			Data: &events.WorkflowTransitionedData{
				FromState: "processing",
				ToState:   "resolution",
			},
		})
	}()

	a.sender = &delaySender{inner: sender, delay: 80 * time.Millisecond}

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("go"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	var stepEvents []struct {
		typ  aguievents.EventType
		name string
	}
	for _, ev := range evts {
		switch ev.Type() {
		case aguievents.EventTypeStepFinished:
			stepEvents = append(stepEvents, struct {
				typ  aguievents.EventType
				name string
			}{ev.Type(), ev.(*aguievents.StepFinishedEvent).StepName})
		case aguievents.EventTypeStepStarted:
			stepEvents = append(stepEvents, struct {
				typ  aguievents.EventType
				name string
			}{ev.Type(), ev.(*aguievents.StepStartedEvent).StepName})
		}
	}

	// Two transitions = 4 step events
	require.Len(t, stepEvents, 4)
	assert.Equal(t, aguievents.EventTypeStepFinished, stepEvents[0].typ)
	assert.Equal(t, "intake", stepEvents[0].name)
	assert.Equal(t, aguievents.EventTypeStepStarted, stepEvents[1].typ)
	assert.Equal(t, "processing", stepEvents[1].name)
	assert.Equal(t, aguievents.EventTypeStepFinished, stepEvents[2].typ)
	assert.Equal(t, "processing", stepEvents[2].name)
	assert.Equal(t, aguievents.EventTypeStepStarted, stepEvents[3].typ)
	assert.Equal(t, "resolution", stepEvents[3].name)
}

// delaySender wraps a Sender and adds a delay before returning.
type delaySender struct {
	inner Sender
	delay time.Duration
}

func (d *delaySender) Send(ctx context.Context, msg any, opts ...sdk.SendOption) (*sdk.Response, error) {
	time.Sleep(d.delay)
	return d.inner.Send(ctx, msg, opts...)
}

func (d *delaySender) SendToolResult(ctx context.Context, callID string, result any) error {
	return d.inner.SendToolResult(ctx, callID, result)
}

func (d *delaySender) RejectClientTool(ctx context.Context, callID, reason string) {
	d.inner.RejectClientTool(ctx, callID, reason)
}

func (d *delaySender) Resume(ctx context.Context) (*sdk.Response, error) {
	return d.inner.Resume(ctx)
}

// --- Client tool tests ---

func TestRunSend_ClientTools_WithProvider(t *testing.T) {
	pendingTools := []sdk.PendingClientTool{
		{CallID: "ct-1", ToolName: "get_location", Args: map[string]any{"city": "NYC"}},
	}
	initialResp := sdk.NewResponseForTest("need location", nil,
		sdk.WithClientToolsForTest(pendingTools),
	)
	finalResp := sdk.NewResponseForTest("location is NYC", nil)

	sender := &mockSender{
		resp:            initialResp,
		resumeResponses: []*sdk.Response{finalResp},
	}
	ebp := &mockEventBusProvider{}

	providerCalled := false
	provider := func(_ context.Context, tools []sdk.PendingClientTool) ([]ToolResult, error) {
		providerCalled = true
		require.Len(t, tools, 1)
		assert.Equal(t, "ct-1", tools[0].CallID)
		return []ToolResult{
			{CallID: "ct-1", Result: map[string]any{"lat": 40.7}},
		}, nil
	}

	a := newTestAdapter(sender, ebp, WithToolResultProvider(provider))

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("where am I?"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	assert.True(t, providerCalled)

	// Verify sender was called correctly
	require.Len(t, sender.sendToolResultCalls, 1)
	assert.Equal(t, "ct-1", sender.sendToolResultCalls[0].callID)

	// Verify event types include: RunStarted, TextMessageStart,
	// TextMessageContent("need location"), ToolCallStart(ct-1), ToolCallArgs,
	// ToolCallEnd, Custom, ToolCallResult, TextMessageContent("location is NYC"),
	// TextMessageEnd, RunFinished
	var evtTypes []aguievents.EventType
	for _, ev := range evts {
		evtTypes = append(evtTypes, ev.Type())
	}

	assert.Equal(t, aguievents.EventTypeRunStarted, evtTypes[0])
	assert.Equal(t, aguievents.EventTypeTextMessageStart, evtTypes[1])
	assert.Contains(t, evtTypes, aguievents.EventTypeCustom)
	assert.Contains(t, evtTypes, aguievents.EventTypeToolCallResult)
	assert.Equal(t, aguievents.EventTypeRunFinished, evtTypes[len(evtTypes)-1])

	// Verify custom event content
	for _, ev := range evts {
		if ev.Type() == aguievents.EventTypeCustom {
			ce, ok := ev.(*aguievents.CustomEvent)
			require.True(t, ok)
			assert.Equal(t, "promptkit.client_tools_pending", ce.Name)
			break
		}
	}
}

func TestRunSend_ClientTools_NoProvider(t *testing.T) {
	pendingTools := []sdk.PendingClientTool{
		{CallID: "ct-1", ToolName: "get_location"},
	}
	resp := sdk.NewResponseForTest("pending", nil,
		sdk.WithClientToolsForTest(pendingTools),
	)

	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{}

	// No provider configured
	a := newTestAdapter(sender, ebp)

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Should emit tool call events for the pending tool + custom event, then finish normally
	var evtTypes []aguievents.EventType
	for _, ev := range evts {
		evtTypes = append(evtTypes, ev.Type())
	}

	assert.Contains(t, evtTypes, aguievents.EventTypeToolCallStart)
	assert.Contains(t, evtTypes, aguievents.EventTypeCustom)
	assert.Equal(t, aguievents.EventTypeRunFinished, evtTypes[len(evtTypes)-1])

	// Resume should NOT have been called
	assert.Empty(t, sender.sendToolResultCalls)
}

func TestRunSend_ClientTools_ProviderError(t *testing.T) {
	pendingTools := []sdk.PendingClientTool{
		{CallID: "ct-1", ToolName: "get_location"},
	}
	resp := sdk.NewResponseForTest("pending", nil,
		sdk.WithClientToolsForTest(pendingTools),
	)

	sender := &mockSender{resp: resp}
	ebp := &mockEventBusProvider{}

	provider := func(_ context.Context, _ []sdk.PendingClientTool) ([]ToolResult, error) {
		return nil, errors.New("provider failed")
	}

	a := newTestAdapter(sender, ebp, WithToolResultProvider(provider))

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("hi"))
	require.Error(t, err)
	assert.Equal(t, "provider failed", err.Error())

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Should have RunError event
	var hasRunError bool
	for _, ev := range evts {
		if ev.Type() == aguievents.EventTypeRunError {
			hasRunError = true
			re, ok := ev.(*aguievents.RunErrorEvent)
			require.True(t, ok)
			assert.Equal(t, "provider failed", re.Message)
		}
	}
	assert.True(t, hasRunError)
}

func TestRunSend_ClientTools_MultipleRounds(t *testing.T) {
	// First response: pending tool ct-1
	pendingTools1 := []sdk.PendingClientTool{
		{CallID: "ct-1", ToolName: "get_location"},
	}
	initialResp := sdk.NewResponseForTest("round 1", nil,
		sdk.WithClientToolsForTest(pendingTools1),
	)

	// Second response (after Resume): pending tool ct-2
	pendingTools2 := []sdk.PendingClientTool{
		{CallID: "ct-2", ToolName: "confirm_action"},
	}
	secondResp := sdk.NewResponseForTest("round 2", nil,
		sdk.WithClientToolsForTest(pendingTools2),
	)

	// Final response (after second Resume): no pending tools
	finalResp := sdk.NewResponseForTest("done", nil)

	sender := &mockSender{
		resp:            initialResp,
		resumeResponses: []*sdk.Response{secondResp, finalResp},
	}
	ebp := &mockEventBusProvider{}

	providerCallCount := 0
	provider := func(_ context.Context, tools []sdk.PendingClientTool) ([]ToolResult, error) {
		providerCallCount++
		return []ToolResult{
			{CallID: tools[0].CallID, Result: "ok"},
		}, nil
	}

	a := newTestAdapter(sender, ebp, WithToolResultProvider(provider))

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("go"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// Provider should have been called twice
	assert.Equal(t, 2, providerCallCount)

	// Should have 2 SendToolResult calls
	require.Len(t, sender.sendToolResultCalls, 2)
	assert.Equal(t, "ct-1", sender.sendToolResultCalls[0].callID)
	assert.Equal(t, "ct-2", sender.sendToolResultCalls[1].callID)

	// Should end with RunFinished
	var evtTypes []aguievents.EventType
	for _, ev := range evts {
		evtTypes = append(evtTypes, ev.Type())
	}
	assert.Equal(t, aguievents.EventTypeRunFinished, evtTypes[len(evtTypes)-1])

	// Should have 2 CustomEvents (one per round)
	var customCount int
	for _, ev := range evts {
		if ev.Type() == aguievents.EventTypeCustom {
			customCount++
		}
	}
	assert.Equal(t, 2, customCount)
}

func TestRunSend_ClientTools_Rejection(t *testing.T) {
	pendingTools := []sdk.PendingClientTool{
		{CallID: "ct-1", ToolName: "get_location"},
		{CallID: "ct-2", ToolName: "send_email"},
	}
	initialResp := sdk.NewResponseForTest("need tools", nil,
		sdk.WithClientToolsForTest(pendingTools),
	)
	finalResp := sdk.NewResponseForTest("handled rejection", nil)

	sender := &mockSender{
		resp:            initialResp,
		resumeResponses: []*sdk.Response{finalResp},
	}
	ebp := &mockEventBusProvider{}

	provider := func(_ context.Context, tools []sdk.PendingClientTool) ([]ToolResult, error) {
		return []ToolResult{
			{CallID: "ct-1", Result: map[string]any{"lat": 40.7}},
			{CallID: "ct-2", Rejected: true, Reason: "user declined"},
		}, nil
	}

	a := newTestAdapter(sender, ebp, WithToolResultProvider(provider))

	var evts []aguievents.Event
	done := make(chan struct{})
	go func() {
		evts = collectEvents(a.Events())
		close(done)
	}()

	err := a.RunSend(context.Background(), userMsg("do things"))
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	// ct-1 should be sent as tool result
	require.Len(t, sender.sendToolResultCalls, 1)
	assert.Equal(t, "ct-1", sender.sendToolResultCalls[0].callID)

	// ct-2 should be rejected
	require.Len(t, sender.rejectCalls, 1)
	assert.Equal(t, "ct-2", sender.rejectCalls[0].callID)
	assert.Equal(t, "user declined", sender.rejectCalls[0].reason)

	// Verify ToolCallResult events — one "completed", one "rejected: user declined"
	var resultContents []string
	for _, ev := range evts {
		if ev.Type() == aguievents.EventTypeToolCallResult {
			tcr, ok := ev.(*aguievents.ToolCallResultEvent)
			require.True(t, ok)
			resultContents = append(resultContents, tcr.Content)
		}
	}
	require.Len(t, resultContents, 2)
	assert.Equal(t, "completed", resultContents[0])
	assert.Equal(t, "rejected: user declined", resultContents[1])
}
