package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// testEvalHandler is a simple handler for integration tests.
type testEvalHandler struct {
	typeName string
	result   *evals.EvalResult
}

func (h *testEvalHandler) Type() string { return h.typeName }

func (h *testEvalHandler) Eval(
	_ context.Context, _ *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	return h.result, nil
}

func TestE2E_EvalMiddleware_DispatchesTurnEvalsAndEmitsEvents(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(&testEvalHandler{
		typeName: "contains",
		result:   &evals.EvalResult{Passed: true, Score: func() *float64 { v := 0.9; return &v }()},
	})
	runner := evals.NewEvalRunner(registry)
	bus := events.NewEventBus()

	received := make(chan *events.Event, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		received <- e
	})

	conv := &Conversation{
		config: &config{
			evalRunner: runner,
			eventBus:   bus,
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt:     &pack.Prompt{},
		promptName: "test-prompt",
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Simulate turn dispatch
	mw.dispatchTurnEvals(context.Background())
	mw.wait() // ensure goroutine completes

	select {
	case evt := <-received:
		data, ok := evt.Data.(*events.EvalCompletedData)
		if !ok {
			t.Fatal("expected *EvalCompletedData")
		}
		if data.EvalID != "e1" {
			t.Errorf("expected eval ID e1, got %q", data.EvalID)
		}
		if !data.Passed {
			t.Error("expected passed=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for eval completed event")
	}
}

func TestE2E_EvalMiddleware_DispatchesSessionEvalsOnClose(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(&testEvalHandler{
		typeName: "summary",
		result:   &evals.EvalResult{Passed: true},
	})
	runner := evals.NewEvalRunner(registry)
	bus := events.NewEventBus()

	received := make(chan *events.Event, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		received <- e
	})

	conv := &Conversation{
		config: &config{
			evalRunner: runner,
			eventBus:   bus,
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e2", Type: "summary", Trigger: evals.TriggerOnSessionComplete},
			},
		},
		prompt:     &pack.Prompt{},
		promptName: "test-prompt",
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Simulate session close dispatch (synchronous)
	mw.dispatchSessionEvals(context.Background())

	select {
	case evt := <-received:
		data, ok := evt.Data.(*events.EvalCompletedData)
		if !ok {
			t.Fatal("expected *EvalCompletedData")
		}
		if data.EvalID != "e2" {
			t.Errorf("expected eval ID e2, got %q", data.EvalID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for eval completed event")
	}
}

func TestE2E_EvalMiddleware_TurnIndexIncrements(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	registry.Register(&testEvalHandler{
		typeName: "contains",
		result:   &evals.EvalResult{Passed: true},
	})
	runner := evals.NewEvalRunner(registry)
	bus := events.NewEventBus()

	var mu sync.Mutex
	var received []*events.Event
	done := make(chan struct{}, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
		done <- struct{}{}
	})

	conv := &Conversation{
		config: &config{
			evalRunner: runner,
			eventBus:   bus,
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt:     &pack.Prompt{},
		promptName: "test-prompt",
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Dispatch 3 turns
	for range 3 {
		mw.dispatchTurnEvals(context.Background())
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	}

	if mw.turnIndex.Load() != 3 {
		t.Errorf("expected turnIndex 3, got %d", mw.turnIndex.Load())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 3 {
		t.Fatalf("expected 3 events, got %d", len(received))
	}
}
