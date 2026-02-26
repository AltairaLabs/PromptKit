package evals

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
)

// mockEvalLoader implements PackEvalLoader for testing.
type mockEvalLoader struct {
	defs []EvalDef
	err  error
}

func (m *mockEvalLoader) LoadEvals(_ string) ([]EvalDef, error) {
	return m.defs, m.err
}

// recordingDispatcher records dispatch calls for verification.
type recordingDispatcher struct {
	mu             sync.Mutex
	turnCalls      int
	sessionCalls   int
	turnResults    []EvalResult
	sessionResults []EvalResult
	turnCh         chan struct{} // signaled on each turn dispatch
	sessionCh      chan struct{} // signaled on each session dispatch
}

func newRecordingDispatcher() *recordingDispatcher {
	return &recordingDispatcher{
		turnCh:    make(chan struct{}, 100),
		sessionCh: make(chan struct{}, 100),
	}
}

func (d *recordingDispatcher) DispatchTurnEvals(
	_ context.Context, _ []EvalDef, _ *EvalContext,
) ([]EvalResult, error) {
	d.mu.Lock()
	d.turnCalls++
	results := d.turnResults
	d.mu.Unlock()
	d.turnCh <- struct{}{}
	return results, nil
}

func (d *recordingDispatcher) DispatchSessionEvals(
	_ context.Context, _ []EvalDef, _ *EvalContext,
) ([]EvalResult, error) {
	d.mu.Lock()
	d.sessionCalls++
	results := d.sessionResults
	d.mu.Unlock()
	d.sessionCh <- struct{}{}
	return results, nil
}

func (d *recordingDispatcher) DispatchConversationEvals(
	_ context.Context, _ []EvalDef, _ *EvalContext,
) ([]EvalResult, error) {
	return nil, nil
}

func (d *recordingDispatcher) TurnCalls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.turnCalls
}

func (d *recordingDispatcher) SessionCalls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessionCalls
}

// recordingResultWriter records written results.
type recordingResultWriter struct {
	mu      sync.Mutex
	results []EvalResult
	calls   int
}

func (w *recordingResultWriter) WriteResults(_ context.Context, results []EvalResult) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	w.results = append(w.results, results...)
	return nil
}

func (w *recordingResultWriter) Calls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

func TestEventBusEvalListener_AssistantTriggersTurnEvals(t *testing.T) {
	bus := events.NewEventBus()
	dispatcher := newRecordingDispatcher()
	loader := &mockEvalLoader{
		defs: []EvalDef{{ID: "e1", Type: "contains", Trigger: TriggerEveryTurn}},
	}

	listener := NewEventBusEvalListener(bus, dispatcher, loader, nil)
	defer listener.Close()

	// Set a prompt ID so loader returns evals
	listener.accumulator.AddMessage("s1", "prompt1", "user", "hello")

	// Publish assistant message — should trigger turn evals
	bus.Publish(&events.Event{
		Type:      events.EventMessageCreated,
		SessionID: "s1",
		Data: &events.MessageCreatedData{
			Role:    "assistant",
			Content: "hi there",
		},
	})

	// Wait for async dispatch
	select {
	case <-dispatcher.turnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for turn eval dispatch")
	}

	if got := dispatcher.TurnCalls(); got != 1 {
		t.Errorf("expected 1 turn dispatch, got %d", got)
	}
}

func TestEventBusEvalListener_UserMessageNoTurnEvals(t *testing.T) {
	bus := events.NewEventBus()
	dispatcher := newRecordingDispatcher()
	loader := &mockEvalLoader{
		defs: []EvalDef{{ID: "e1", Type: "contains", Trigger: TriggerEveryTurn}},
	}

	listener := NewEventBusEvalListener(bus, dispatcher, loader, nil)
	defer listener.Close()

	// Publish user message — should NOT trigger turn evals
	bus.Publish(&events.Event{
		Type:      events.EventMessageCreated,
		SessionID: "s1",
		Data: &events.MessageCreatedData{
			Role:    "user",
			Content: "hello",
		},
	})

	// Give time for any dispatch to happen
	time.Sleep(100 * time.Millisecond)

	if got := dispatcher.TurnCalls(); got != 0 {
		t.Errorf("expected 0 turn dispatches for user message, got %d", got)
	}
}

func TestEventBusEvalListener_SessionAccumulation(t *testing.T) {
	acc := NewSessionAccumulator()

	acc.AddMessage("s1", "p1", "user", "hello")
	acc.AddMessage("s1", "p1", "assistant", "hi")
	acc.AddMessage("s1", "p1", "user", "how are you?")
	acc.AddMessage("s1", "p1", "assistant", "I'm well")

	ctx := acc.BuildEvalContext("s1")
	if len(ctx.Messages) != 4 {
		t.Errorf("expected 4 messages, got %d", len(ctx.Messages))
	}
	if ctx.TurnIndex != 2 {
		t.Errorf("expected turn index 2, got %d", ctx.TurnIndex)
	}
	if ctx.CurrentOutput != "I'm well" {
		t.Errorf("expected current output 'I'm well', got %q", ctx.CurrentOutput)
	}
	if ctx.SessionID != "s1" {
		t.Errorf("expected session ID s1, got %q", ctx.SessionID)
	}
}

func TestEventBusEvalListener_CloseSessionFiresSessionEvals(t *testing.T) {
	bus := events.NewEventBus()
	dispatcher := newRecordingDispatcher()
	loader := &mockEvalLoader{
		defs: []EvalDef{{ID: "e1", Type: "contains", Trigger: TriggerOnSessionComplete}},
	}
	writer := &recordingResultWriter{}

	listener := NewEventBusEvalListener(bus, dispatcher, loader, writer)
	defer listener.Close()

	// Seed session with a prompt ID
	listener.accumulator.AddMessage("s1", "prompt1", "user", "hello")
	listener.accumulator.AddMessage("s1", "prompt1", "assistant", "hi")

	dispatcher.mu.Lock()
	dispatcher.sessionResults = []EvalResult{{EvalID: "e1", Passed: true}}
	dispatcher.mu.Unlock()

	listener.CloseSession(context.Background(), "s1")

	select {
	case <-dispatcher.sessionCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for session eval dispatch")
	}

	if got := dispatcher.SessionCalls(); got != 1 {
		t.Errorf("expected 1 session dispatch, got %d", got)
	}

	if got := writer.Calls(); got != 1 {
		t.Errorf("expected 1 result write call, got %d", got)
	}
}

func TestEventBusEvalListener_TTLCleanup(t *testing.T) {
	acc := NewSessionAccumulator()

	acc.AddMessage("old", "p1", "user", "hello")

	// Manually set lastSeen in the past
	acc.mu.Lock()
	acc.sessions["old"].mu.Lock()
	acc.sessions["old"].lastSeen = time.Now().Add(-1 * time.Hour)
	acc.sessions["old"].mu.Unlock()
	acc.mu.Unlock()

	acc.AddMessage("recent", "p1", "user", "hello")

	removed := acc.CleanupBefore(time.Now().Add(-30 * time.Minute))
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	// recent should still be there
	ctx := acc.BuildEvalContext("recent")
	if len(ctx.Messages) != 1 {
		t.Errorf("recent session should still have 1 message")
	}

	// old should be gone
	ctx = acc.BuildEvalContext("old")
	if len(ctx.Messages) != 0 {
		t.Errorf("old session should be removed")
	}
}

func TestEventBusEvalListener_ConcurrentMessages(t *testing.T) {
	bus := events.NewEventBus()
	dispatcher := newRecordingDispatcher()
	loader := &mockEvalLoader{
		defs: []EvalDef{{ID: "e1", Type: "contains", Trigger: TriggerEveryTurn}},
	}

	listener := NewEventBusEvalListener(bus, dispatcher, loader, nil)
	defer listener.Close()

	// Seed with prompt ID
	listener.accumulator.AddMessage("s1", "prompt1", "user", "init")

	var dispatched atomic.Int32
	go func() {
		for range dispatcher.turnCh {
			dispatched.Add(1)
		}
	}()

	// Send many concurrent assistant messages
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish(&events.Event{
				Type:      events.EventMessageCreated,
				SessionID: "s1",
				Data: &events.MessageCreatedData{
					Role:    "assistant",
					Content: "response",
				},
			})
		}()
	}
	wg.Wait()

	// Wait for all dispatches
	time.Sleep(500 * time.Millisecond)

	// Should have dispatched for each assistant message (event bus dispatches async)
	if got := dispatched.Load(); got == 0 {
		t.Error("expected at least some turn dispatches")
	}
}

func TestEventBusEvalListener_NilResultWriterNoPanic(t *testing.T) {
	bus := events.NewEventBus()
	dispatcher := newRecordingDispatcher()
	dispatcher.mu.Lock()
	dispatcher.turnResults = []EvalResult{{EvalID: "e1", Passed: true}}
	dispatcher.mu.Unlock()

	loader := &mockEvalLoader{
		defs: []EvalDef{{ID: "e1", Type: "contains", Trigger: TriggerEveryTurn}},
	}

	// nil resultWriter should not panic
	listener := NewEventBusEvalListener(bus, dispatcher, loader, nil)
	defer listener.Close()

	listener.accumulator.AddMessage("s1", "prompt1", "user", "hello")

	bus.Publish(&events.Event{
		Type:      events.EventMessageCreated,
		SessionID: "s1",
		Data: &events.MessageCreatedData{
			Role:    "assistant",
			Content: "hi",
		},
	})

	select {
	case <-dispatcher.turnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	// If we get here without panic, test passes
}

func TestSessionAccumulator_EmptySession(t *testing.T) {
	acc := NewSessionAccumulator()
	ctx := acc.BuildEvalContext("nonexistent")
	if ctx.SessionID != "nonexistent" {
		t.Errorf("expected session ID preserved")
	}
	if len(ctx.Messages) != 0 {
		t.Errorf("expected no messages for missing session")
	}
}
