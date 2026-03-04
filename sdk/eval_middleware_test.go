package sdk

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

func TestNewEvalMiddleware_DisabledReturnsNil(t *testing.T) {
	conv := &Conversation{
		config: &config{evalsDisabled: true},
		pack:   &pack.Pack{},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw != nil {
		t.Error("expected nil middleware when evals disabled")
	}
}

func TestNewEvalMiddleware_NoDefsReturnsNil(t *testing.T) {
	conv := &Conversation{
		config: &config{},
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
		config: &config{},
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

func TestNewEvalMiddleware_WithExplicitRunner(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	runner := evals.NewEvalRunner(registry)

	conv := &Conversation{
		config: &config{evalRunner: runner},
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
	if mw.runner != runner {
		t.Error("expected explicit runner to be used")
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
		config: &config{},
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

func TestEvalMiddleware_EmitterFromEventBus(t *testing.T) {
	bus := events.NewEventBus()

	conv := &Conversation{
		config: &config{eventBus: bus},
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
	if mw.emitter == nil {
		t.Error("expected non-nil emitter when event bus is configured")
	}
}

func TestEvalMiddleware_NoEventBusNilEmitter(t *testing.T) {
	conv := &Conversation{
		config: &config{},
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
	if mw.emitter != nil {
		t.Error("expected nil emitter when no event bus")
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
		config: &config{},
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
		config: &config{},
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

func TestEvalMiddleware_DispatchTurnEvalsDoesNotPanic(t *testing.T) {
	conv := &Conversation{
		config: &config{},
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

	// Should not panic — runs async, handler may not be found but that's ok
	mw.dispatchTurnEvals(context.Background())
}

func TestEvalMiddleware_DispatchSessionEvalsDoesNotPanic(t *testing.T) {
	conv := &Conversation{
		config: &config{},
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

	// Should not panic — runs synchronously
	mw.dispatchSessionEvals(context.Background())
}

func TestEvalMiddleware_EmitResults_NilEmitter(t *testing.T) {
	conv := &Conversation{
		config: &config{},
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

	// Should not panic with nil emitter
	mw.emitResults([]evals.EvalResult{{EvalID: "e1", Passed: true}})
}

func TestEvalMiddleware_EmitResults_WithBus(t *testing.T) {
	bus := events.NewEventBus()
	received := make(chan *events.Event, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		received <- e
	})
	bus.Subscribe(events.EventEvalFailed, func(e *events.Event) {
		received <- e
	})

	conv := &Conversation{
		config: &config{eventBus: bus},
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

	mw.emitResults([]evals.EvalResult{
		{EvalID: "e1", Type: "contains", Passed: true},
		{EvalID: "e2", Type: "regex", Passed: false},
	})

	// Check we got 2 events
	e1 := <-received
	e2 := <-received

	if e1.Type != events.EventEvalCompleted {
		t.Errorf("expected eval.completed, got %s", e1.Type)
	}
	if e2.Type != events.EventEvalFailed {
		t.Errorf("expected eval.failed, got %s", e2.Type)
	}

	data1, ok := e1.Data.(*events.EvalCompletedData)
	if !ok {
		t.Fatal("expected *EvalCompletedData")
	}
	if data1.EvalID != "e1" {
		t.Errorf("expected eval ID e1, got %q", data1.EvalID)
	}
}
