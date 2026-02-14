package evals

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

// mockPublisher captures published events.
type mockPublisher struct {
	mu       sync.Mutex
	events   []publishedEvent
	publishErr error
}

type publishedEvent struct {
	Subject string
	Data    []byte
}

func (m *mockPublisher) Publish(
	_ context.Context, subject string, data []byte,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.publishErr != nil {
		return m.publishErr
	}
	m.events = append(m.events, publishedEvent{
		Subject: subject,
		Data:    data,
	})
	return nil
}

// recordingWriter captures results written.
type recordingWriter struct {
	mu      sync.Mutex
	batches [][]EvalResult
	err     error
}

func (w *recordingWriter) WriteResults(
	_ context.Context, results []EvalResult,
) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.batches = append(w.batches, results)
	return w.err
}

func (w *recordingWriter) getBatches() [][]EvalResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	cp := make([][]EvalResult, len(w.batches))
	copy(cp, w.batches)
	return cp
}

func TestInProcDispatcher_TurnEvals(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)
	writer := &recordingWriter{}
	disp := NewInProcDispatcher(runner, writer)

	defs := []EvalDef{
		{ID: "e1", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1", TurnIndex: 0}

	results, err := disp.DispatchTurnEvals(
		context.Background(), defs, evalCtx,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if !results[0].Passed {
		t.Error("expected Passed=true")
	}
	if len(writer.batches) != 1 {
		t.Errorf("expected 1 write batch, got %d", len(writer.batches))
	}
}

func TestInProcDispatcher_SessionEvals(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)
	writer := &recordingWriter{}
	disp := NewInProcDispatcher(runner, writer)

	defs := []EvalDef{
		{
			ID:      "e1",
			Type:    "test",
			Trigger: TriggerOnSessionComplete,
		},
	}
	evalCtx := &EvalContext{SessionID: "s1", TurnIndex: 3}

	results, err := disp.DispatchSessionEvals(
		context.Background(), defs, evalCtx,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

func TestInProcDispatcher_NilWriter(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)
	disp := NewInProcDispatcher(runner, nil)

	defs := []EvalDef{
		{ID: "e1", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results, err := disp.DispatchTurnEvals(
		context.Background(), defs, evalCtx,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

func TestInProcDispatcher_WriterError(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)
	writer := &recordingWriter{err: errors.New("write failed")}
	disp := NewInProcDispatcher(runner, writer)

	defs := []EvalDef{
		{ID: "e1", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results, err := disp.DispatchTurnEvals(
		context.Background(), defs, evalCtx,
	)
	if err == nil {
		t.Fatal("expected error from writer")
	}
	// Results should still be returned even with writer error.
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

func TestInProcDispatcher_NoResults_SkipsWrite(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)
	writer := &recordingWriter{}
	disp := NewInProcDispatcher(runner, writer)

	// No matching triggers → no results → no write.
	defs := []EvalDef{
		{
			ID:      "e1",
			Type:    "test",
			Trigger: TriggerOnSessionComplete,
		},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	_, err := disp.DispatchTurnEvals(
		context.Background(), defs, evalCtx,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(writer.batches) != 0 {
		t.Errorf("expected no writes, got %d", len(writer.batches))
	}
}

func TestEventDispatcher_TurnEvals(t *testing.T) {
	pub := &mockPublisher{}
	disp := NewEventDispatcher(pub)

	defs := []EvalDef{
		{ID: "e1", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "session-123"}

	results, err := disp.DispatchTurnEvals(
		context.Background(), defs, evalCtx,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Error("event dispatcher should return nil results")
	}
	if len(pub.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.events))
	}
	if pub.events[0].Subject != "eval.turn.session-123" {
		t.Errorf(
			"subject = %q, want %q",
			pub.events[0].Subject, "eval.turn.session-123",
		)
	}

	// Verify payload is valid JSON with expected structure.
	var payload evalEventPayload
	if err := json.Unmarshal(pub.events[0].Data, &payload); err != nil {
		t.Fatalf("invalid payload JSON: %v", err)
	}
	if len(payload.Defs) != 1 {
		t.Errorf("payload defs count = %d, want 1", len(payload.Defs))
	}
	if payload.EvalCtx.SessionID != "session-123" {
		t.Errorf(
			"payload session = %q, want %q",
			payload.EvalCtx.SessionID, "session-123",
		)
	}
}

func TestEventDispatcher_SessionEvals(t *testing.T) {
	pub := &mockPublisher{}
	disp := NewEventDispatcher(pub)

	defs := []EvalDef{
		{
			ID:      "e1",
			Type:    "test",
			Trigger: TriggerOnSessionComplete,
		},
	}
	evalCtx := &EvalContext{SessionID: "sess-456"}

	results, err := disp.DispatchSessionEvals(
		context.Background(), defs, evalCtx,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Error("event dispatcher should return nil results")
	}
	if pub.events[0].Subject != "eval.session.sess-456" {
		t.Errorf(
			"subject = %q, want %q",
			pub.events[0].Subject, "eval.session.sess-456",
		)
	}
}

func TestEventDispatcher_PublishError(t *testing.T) {
	pub := &mockPublisher{publishErr: errors.New("bus down")}
	disp := NewEventDispatcher(pub)

	defs := []EvalDef{
		{ID: "e1", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	_, err := disp.DispatchTurnEvals(
		context.Background(), defs, evalCtx,
	)
	if err == nil {
		t.Fatal("expected publish error")
	}
}

func TestNoOpDispatcher_Turn(t *testing.T) {
	disp := &NoOpDispatcher{}
	results, err := disp.DispatchTurnEvals(
		context.Background(), nil, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Error("expected nil results")
	}
}

func TestNoOpDispatcher_Session(t *testing.T) {
	disp := &NoOpDispatcher{}
	results, err := disp.DispatchSessionEvals(
		context.Background(), nil, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Error("expected nil results")
	}
}
