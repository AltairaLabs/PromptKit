package sdk

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// testDispatcher records dispatch calls.
type testDispatcher struct {
	mu           sync.Mutex
	turnCalls    int
	sessionCalls int
	turnCh       chan struct{}
	sessionCh    chan struct{}
}

func newTestDispatcher() *testDispatcher {
	return &testDispatcher{
		turnCh:    make(chan struct{}, 100),
		sessionCh: make(chan struct{}, 100),
	}
}

func (d *testDispatcher) DispatchTurnEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	d.mu.Lock()
	d.turnCalls++
	d.mu.Unlock()
	d.turnCh <- struct{}{}
	return nil, nil
}

func (d *testDispatcher) DispatchSessionEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	d.mu.Lock()
	d.sessionCalls++
	d.mu.Unlock()
	d.sessionCh <- struct{}{}
	return nil, nil
}

func (d *testDispatcher) DispatchConversationEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return nil, nil
}

func (d *testDispatcher) TurnCalls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.turnCalls
}

func (d *testDispatcher) SessionCalls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessionCalls
}

// testResultWriter records written results.
type testResultWriter struct {
	mu      sync.Mutex
	results []evals.EvalResult
	calls   int
}

func (w *testResultWriter) WriteResults(_ context.Context, results []evals.EvalResult) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	w.results = append(w.results, results...)
	return nil
}

func TestNewEvalMiddleware_NilDispatcherReturnsNil(t *testing.T) {
	conv := &Conversation{
		config: &config{},
		pack:   &pack.Pack{},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw != nil {
		t.Error("expected nil middleware when no dispatcher configured")
	}
}

func TestNewEvalMiddleware_NoDefsReturnsNil(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack:   &pack.Pack{}, // No evals
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw != nil {
		t.Error("expected nil middleware when no eval defs")
	}
}

func TestNewEvalMiddleware_WithDefs(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware when defs exist")
	}
	if len(mw.defs) != 1 {
		t.Errorf("expected 1 def, got %d", len(mw.defs))
	}
}

func TestEvalMiddleware_NilMiddlewareSafeNoOp(t *testing.T) {
	// Should not panic
	var mw *evalMiddleware
	mw.dispatchTurnEvals(context.Background())
	mw.dispatchSessionEvals(context.Background())
}

func TestEvalMiddleware_ResolvesPackAndPromptEvals(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "a", Type: "contains", Trigger: evals.TriggerEveryTurn},
				{ID: "b", Type: "regex", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{
			Evals: []evals.EvalDef{
				{ID: "b", Type: "regex_override", Trigger: evals.TriggerEveryTurn}, // Override
				{ID: "c", Type: "length", Trigger: evals.TriggerOnSessionComplete},
			},
		},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Should be: a (from pack), b_override (from prompt), c (from prompt)
	if len(mw.defs) != 3 {
		t.Fatalf("expected 3 resolved defs, got %d", len(mw.defs))
	}
	if mw.defs[0].ID != "a" {
		t.Errorf("expected first def ID 'a', got %q", mw.defs[0].ID)
	}
	if mw.defs[1].Type != "regex_override" {
		t.Errorf("expected second def type 'regex_override', got %q", mw.defs[1].Type)
	}
	if mw.defs[2].ID != "c" {
		t.Errorf("expected third def ID 'c', got %q", mw.defs[2].ID)
	}
}

func TestEvalMiddleware_MultipleResultWritersComposed(t *testing.T) {
	w1 := &testResultWriter{}
	w2 := &testResultWriter{}

	conv := &Conversation{
		config: &config{
			evalDispatcher:    &evals.NoOpDispatcher{},
			evalResultWriters: []evals.ResultWriter{w1, w2},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Verify it's a composite writer
	if _, ok := mw.resultWriter.(*evals.CompositeResultWriter); !ok {
		t.Error("expected CompositeResultWriter when multiple writers provided")
	}
}

func TestEvalMiddleware_SingleResultWriter(t *testing.T) {
	w := &testResultWriter{}

	conv := &Conversation{
		config: &config{
			evalDispatcher:    &evals.NoOpDispatcher{},
			evalResultWriters: []evals.ResultWriter{w},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Single writer should be used directly, not wrapped
	if mw.resultWriter != w {
		t.Error("expected single writer to be used directly")
	}
}

func TestEvalMiddleware_NoResultWriters(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
	if mw.resultWriter != nil {
		t.Error("expected nil result writer")
	}
}

func TestEvalMiddleware_NilConfig(t *testing.T) {
	conv := &Conversation{
		config: nil,
	}

	mw := newEvalMiddleware(conv)
	if mw != nil {
		t.Error("expected nil middleware when config is nil")
	}
}

func TestEvalMiddleware_NilPack(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack:   nil,
		prompt: nil,
	}

	mw := newEvalMiddleware(conv)
	if mw != nil {
		t.Error("expected nil middleware when pack and prompt are nil")
	}
}

func TestEvalMiddleware_BuildEvalContext_NoSession(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt:     &pack.Prompt{},
		promptName: "my-prompt",
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	mw.turnIndex = 3
	ctx := mw.buildEvalContext(context.Background())

	if ctx.TurnIndex != 3 {
		t.Errorf("expected TurnIndex 3, got %d", ctx.TurnIndex)
	}
	if ctx.PromptID != "my-prompt" {
		t.Errorf("expected PromptID 'my-prompt', got %q", ctx.PromptID)
	}
	if ctx.SessionID != "" {
		t.Errorf("expected empty SessionID, got %q", ctx.SessionID)
	}
	if len(ctx.Messages) != 0 {
		t.Errorf("expected no messages, got %d", len(ctx.Messages))
	}
}

// errorDispatcher returns errors on dispatch.
type errorDispatcher struct {
	turnErr    error
	sessionErr error
}

func (d *errorDispatcher) DispatchTurnEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return nil, d.turnErr
}

func (d *errorDispatcher) DispatchSessionEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return nil, d.sessionErr
}

func (d *errorDispatcher) DispatchConversationEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return nil, nil
}

func TestEvalMiddleware_DispatchTurnEvalsError(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &errorDispatcher{turnErr: errors.New("turn error")},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Should not panic on error — runs async so we can't easily check,
	// but at least verify it doesn't crash
	mw.dispatchTurnEvals(context.Background())
}

func TestEvalMiddleware_DispatchSessionEvalsError(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &errorDispatcher{sessionErr: errors.New("session error")},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Should not panic on error — runs synchronously
	mw.dispatchSessionEvals(context.Background())
}

// errorResultWriter returns an error on WriteResults.
type errorResultWriter struct{}

func (w *errorResultWriter) WriteResults(_ context.Context, _ []evals.EvalResult) error {
	return errors.New("write error")
}

// returningDispatcher always returns results.
type returningDispatcher struct {
	results []evals.EvalResult
}

func (d *returningDispatcher) DispatchTurnEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return d.results, nil
}

func (d *returningDispatcher) DispatchSessionEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return d.results, nil
}

func (d *returningDispatcher) DispatchConversationEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return d.results, nil
}

func TestEvalMiddleware_SessionEvalsResultWriterError(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &returningDispatcher{
				results: []evals.EvalResult{{EvalID: "e1", Passed: true}},
			},
			evalResultWriters: []evals.ResultWriter{&errorResultWriter{}},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Should not panic even when result writer errors
	mw.dispatchSessionEvals(context.Background())
}
