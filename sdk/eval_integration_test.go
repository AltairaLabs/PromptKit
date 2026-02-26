package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/testutil"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// integrationDispatcher captures all dispatch calls with sync support.
type integrationDispatcher struct {
	mu             sync.Mutex
	turnCalls      int
	sessionCalls   int
	turnContexts   []*evals.EvalContext
	sessionContexts []*evals.EvalContext
	turnCh         chan struct{}
	sessionCh      chan struct{}
}

func newIntegrationDispatcher() *integrationDispatcher {
	return &integrationDispatcher{
		turnCh:    make(chan struct{}, 100),
		sessionCh: make(chan struct{}, 100),
	}
}

func (d *integrationDispatcher) DispatchTurnEvals(
	_ context.Context, _ []evals.EvalDef, evalCtx *evals.EvalContext,
) ([]evals.EvalResult, error) {
	d.mu.Lock()
	d.turnCalls++
	d.turnContexts = append(d.turnContexts, evalCtx)
	d.mu.Unlock()
	d.turnCh <- struct{}{}
	return []evals.EvalResult{
		{EvalID: "e1", Passed: true, Score: testutil.Ptr(0.9)},
	}, nil
}

func (d *integrationDispatcher) DispatchSessionEvals(
	_ context.Context, _ []evals.EvalDef, evalCtx *evals.EvalContext,
) ([]evals.EvalResult, error) {
	d.mu.Lock()
	d.sessionCalls++
	d.sessionContexts = append(d.sessionContexts, evalCtx)
	d.mu.Unlock()
	d.sessionCh <- struct{}{}
	return []evals.EvalResult{
		{EvalID: "e2", Passed: true},
	}, nil
}

func (d *integrationDispatcher) DispatchConversationEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return nil, nil
}


// integrationResultWriter records results for assertion.
type integrationResultWriter struct {
	mu      sync.Mutex
	results []evals.EvalResult
	calls   int
	written chan struct{}
}

func newIntegrationResultWriter() *integrationResultWriter {
	return &integrationResultWriter{written: make(chan struct{}, 100)}
}

func (w *integrationResultWriter) WriteResults(_ context.Context, results []evals.EvalResult) error {
	w.mu.Lock()
	w.calls++
	w.results = append(w.results, results...)
	w.mu.Unlock()
	w.written <- struct{}{}
	return nil
}

func (w *integrationResultWriter) Results() []evals.EvalResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := make([]evals.EvalResult, len(w.results))
	copy(r, w.results)
	return r
}

func (w *integrationResultWriter) Calls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

func TestE2E_EvalMiddleware_DispatchesTurnEvals(t *testing.T) {
	dispatcher := newIntegrationDispatcher()
	writer := newIntegrationResultWriter()

	conv := &Conversation{
		config: &config{
			evalDispatcher:    dispatcher,
			evalResultWriters: []evals.ResultWriter{writer},
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

	// Wait for async dispatch
	select {
	case <-dispatcher.turnCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for turn dispatch")
	}

	// Wait for result write (which happens in the same goroutine after dispatch)
	select {
	case <-writer.written:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result write")
	}

	if dispatcher.turnCalls != 1 {
		t.Errorf("expected 1 turn dispatch, got %d", dispatcher.turnCalls)
	}

	results := writer.Results()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].EvalID != "e1" {
		t.Errorf("expected eval ID e1, got %q", results[0].EvalID)
	}
}

func TestE2E_EvalMiddleware_DispatchesSessionEvalsOnClose(t *testing.T) {
	dispatcher := newIntegrationDispatcher()
	writer := newIntegrationResultWriter()

	conv := &Conversation{
		config: &config{
			evalDispatcher:    dispatcher,
			evalResultWriters: []evals.ResultWriter{writer},
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
	case <-dispatcher.sessionCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for session dispatch")
	}

	if dispatcher.sessionCalls != 1 {
		t.Errorf("expected 1 session dispatch, got %d", dispatcher.sessionCalls)
	}

	select {
	case <-writer.written:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result write")
	}

	results := writer.Results()
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].EvalID != "e2" {
		t.Errorf("expected eval ID e2, got %q", results[0].EvalID)
	}
}

func TestE2E_EvalMiddleware_EventDispatcher(t *testing.T) {
	// Test that EventDispatcher publishes events rather than running evals
	published := make(chan []byte, 10)
	publisher := &mockEventPublisher{published: published}
	dispatcher := evals.NewEventDispatcher(publisher)

	conv := &Conversation{
		config: &config{
			evalDispatcher: dispatcher,
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

	// Dispatch turn evals — should publish to event bus, not run in-proc
	mw.dispatchTurnEvals(context.Background())

	// Wait for async publish
	select {
	case data := <-published:
		if len(data) == 0 {
			t.Error("expected non-empty published data")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event publish")
	}
}

// mockEventPublisher records published events.
type mockEventPublisher struct {
	published chan []byte
}

func (p *mockEventPublisher) Publish(_ context.Context, _ string, data []byte) error {
	p.published <- data
	return nil
}

func TestE2E_EvalMiddleware_TurnIndexIncrements(t *testing.T) {
	dispatcher := newIntegrationDispatcher()

	conv := &Conversation{
		config: &config{
			evalDispatcher: dispatcher,
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
		case <-dispatcher.turnCh:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout")
		}
	}

	dispatcher.mu.Lock()
	defer dispatcher.mu.Unlock()

	if len(dispatcher.turnContexts) != 3 {
		t.Fatalf("expected 3 contexts, got %d", len(dispatcher.turnContexts))
	}

	// Turn indices should increment: 1, 2, 3
	for i, ctx := range dispatcher.turnContexts {
		expected := i + 1
		if ctx.TurnIndex != expected {
			t.Errorf("turn %d: expected TurnIndex %d, got %d", i, expected, ctx.TurnIndex)
		}
	}
}
