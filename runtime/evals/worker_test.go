package evals

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// mockSubscriber calls handlers with pre-configured events.
type mockSubscriber struct {
	turnEvents    [][]byte
	sessionEvents [][]byte
	subErr        error
}

func (m *mockSubscriber) Subscribe(
	ctx context.Context,
	subject string,
	handler func(event []byte) error,
) error {
	if m.subErr != nil {
		return m.subErr
	}

	var events [][]byte
	switch subject {
	case "eval.turn.*":
		events = m.turnEvents
	case "eval.session.*":
		events = m.sessionEvents
	}

	for _, e := range events {
		if err := handler(e); err != nil {
			return err
		}
	}

	// Block until context is canceled to simulate long-running sub.
	<-ctx.Done()
	return ctx.Err()
}

// testLogger captures log output.
type testLogger struct {
	messages []string
}

func (l *testLogger) Printf(format string, _ ...any) {
	l.messages = append(l.messages, format)
}

func makePayload(
	t *testing.T, defs []EvalDef, evalCtx *EvalContext,
) []byte {
	t.Helper()
	data, err := json.Marshal(evalEventPayload{
		Defs:    defs,
		EvalCtx: evalCtx,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return data
}

func TestEvalWorker_ProcessesTurnEvents(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)
	writer := &recordingWriter{}

	payload := makePayload(t,
		[]EvalDef{{ID: "e1", Type: "test", Trigger: TriggerEveryTurn}},
		&EvalContext{SessionID: "s1", TurnIndex: 0},
	)

	sub := &mockSubscriber{turnEvents: [][]byte{payload}}
	worker := NewEvalWorker(runner, sub, writer)

	ctx, cancel := context.WithTimeout(
		context.Background(), 500*time.Millisecond,
	)
	defer cancel()

	_ = worker.Start(ctx)

	batches := writer.getBatches()
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}
	if len(batches[0]) != 1 {
		t.Fatalf(
			"expected 1 result in batch, got %d", len(batches[0]),
		)
	}
	if !batches[0][0].Passed {
		t.Error("expected Passed=true")
	}
}

func TestEvalWorker_ProcessesSessionEvents(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)
	writer := &recordingWriter{}

	payload := makePayload(t,
		[]EvalDef{
			{
				ID:      "e1",
				Type:    "test",
				Trigger: TriggerOnSessionComplete,
			},
		},
		&EvalContext{SessionID: "s1", TurnIndex: 5},
	)

	sub := &mockSubscriber{sessionEvents: [][]byte{payload}}
	worker := NewEvalWorker(runner, sub, writer)

	ctx, cancel := context.WithTimeout(
		context.Background(), 500*time.Millisecond,
	)
	defer cancel()

	_ = worker.Start(ctx)

	batches := writer.getBatches()
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}
}

func TestEvalWorker_InvalidPayload(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)
	writer := &recordingWriter{}
	logger := &testLogger{}

	sub := &mockSubscriber{
		turnEvents: [][]byte{[]byte("not json")},
	}
	worker := NewEvalWorker(runner, sub, writer, WithLogger(logger))

	ctx, cancel := context.WithTimeout(
		context.Background(), 500*time.Millisecond,
	)
	defer cancel()

	err := worker.Start(ctx)
	// Should get an error from the failed decode propagating through
	// the subscription.
	if err == nil {
		t.Fatal("expected error from invalid payload")
	}
	if len(logger.messages) == 0 {
		t.Error("expected log message for decode failure")
	}
}

func TestEvalWorker_SubscriptionError(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	sub := &mockSubscriber{subErr: errors.New("sub failed")}
	worker := NewEvalWorker(runner, sub, nil)

	ctx, cancel := context.WithTimeout(
		context.Background(), 500*time.Millisecond,
	)
	defer cancel()

	err := worker.Start(ctx)
	if err == nil {
		t.Fatal("expected subscription error")
	}
}

func TestEvalWorker_NilResultWriter(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	payload := makePayload(t,
		[]EvalDef{{ID: "e1", Type: "test", Trigger: TriggerEveryTurn}},
		&EvalContext{SessionID: "s1"},
	)

	sub := &mockSubscriber{turnEvents: [][]byte{payload}}
	worker := NewEvalWorker(runner, sub, nil)

	ctx, cancel := context.WithTimeout(
		context.Background(), 500*time.Millisecond,
	)
	defer cancel()

	// Should not panic with nil writer.
	_ = worker.Start(ctx)
}

func TestEvalWorker_WriterError(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)
	writer := &recordingWriter{err: errors.New("write failed")}
	logger := &testLogger{}

	payload := makePayload(t,
		[]EvalDef{{ID: "e1", Type: "test", Trigger: TriggerEveryTurn}},
		&EvalContext{SessionID: "s1"},
	)

	sub := &mockSubscriber{turnEvents: [][]byte{payload}}
	worker := NewEvalWorker(runner, sub, writer, WithLogger(logger))

	ctx, cancel := context.WithTimeout(
		context.Background(), 500*time.Millisecond,
	)
	defer cancel()

	err := worker.Start(ctx)
	if err == nil {
		t.Fatal("expected error from writer failure")
	}
}
