package sdk

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

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
	mw.wait()
	mw.close()
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

	mw.turnIndex.Store(3)
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
	mw.wait() // ensure goroutine completes before test exits
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

func TestEvalMiddleware_WaitBlocksUntilGoroutinesComplete(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	started := make(chan struct{})
	registry.Register(&blockingEvalHandler{
		typeName: "blocking",
		started:  started,
		result:   &evals.EvalResult{Passed: true},
	})
	runner := evals.NewEvalRunner(registry)

	conv := &Conversation{
		config: &config{evalRunner: runner},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "blocking", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	mw.dispatchTurnEvals(context.Background())

	// Wait for the goroutine to start
	<-started

	// wait() should block until the goroutine completes
	done := make(chan struct{})
	go func() {
		mw.wait()
		close(done)
	}()

	select {
	case <-done:
		// wait() returned — goroutine completed
	case <-time.After(2 * time.Second):
		t.Fatal("wait() did not return in time")
	}
}

func TestEvalMiddleware_CloseStopsInFlightEvals(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	started := make(chan struct{})
	registry.Register(&cancellableEvalHandler{
		typeName: "cancellable",
		started:  started,
	})
	runner := evals.NewEvalRunner(registry)

	conv := &Conversation{
		config: &config{evalRunner: runner},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "cancellable", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	mw.dispatchTurnEvals(context.Background())

	// Wait for the goroutine to start
	<-started

	// close() cancels context and waits for goroutines
	done := make(chan struct{})
	go func() {
		mw.close()
		close(done)
	}()

	select {
	case <-done:
		// close() returned — goroutine was cancelled and completed
	case <-time.After(2 * time.Second):
		t.Fatal("close() did not return in time")
	}
}

func TestEvalMiddleware_MultipleDispatchesAllTracked(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	var count int32
	var mu sync.Mutex
	registry.Register(&countingEvalHandler{
		typeName: "counting",
		count:    &count,
		mu:       &mu,
	})
	runner := evals.NewEvalRunner(registry)

	conv := &Conversation{
		config: &config{evalRunner: runner},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "counting", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Dispatch 5 turns
	for range 5 {
		mw.dispatchTurnEvals(context.Background())
	}

	// wait() should ensure all 5 goroutines complete
	mw.wait()

	mu.Lock()
	defer mu.Unlock()
	if count != 5 {
		t.Errorf("expected 5 eval runs, got %d", count)
	}
}

// blockingEvalHandler signals when started and completes quickly.
type blockingEvalHandler struct {
	typeName string
	started  chan struct{}
	result   *evals.EvalResult
}

func (h *blockingEvalHandler) Type() string { return h.typeName }

func (h *blockingEvalHandler) Eval(
	_ context.Context, _ *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	close(h.started)
	return h.result, nil
}

// cancellableEvalHandler blocks until the context is cancelled.
type cancellableEvalHandler struct {
	typeName string
	started  chan struct{}
}

func (h *cancellableEvalHandler) Type() string { return h.typeName }

func (h *cancellableEvalHandler) Eval(
	ctx context.Context, _ *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	close(h.started)
	<-ctx.Done()
	return &evals.EvalResult{Passed: false, Error: "cancelled"}, nil
}

// countingEvalHandler increments a counter on each eval call.
type countingEvalHandler struct {
	typeName string
	count    *int32
	mu       *sync.Mutex
}

func (h *countingEvalHandler) Type() string { return h.typeName }

func (h *countingEvalHandler) Eval(
	_ context.Context, _ *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	h.mu.Lock()
	*h.count++
	h.mu.Unlock()
	return &evals.EvalResult{Passed: true}, nil
}

func TestEvalMiddleware_SemaphoreSkipsWhenAtCapacity(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	started := make(chan struct{}, 5)
	unblock := make(chan struct{})
	registry.Register(&gatedEvalHandler{
		typeName: "gated",
		started:  started,
		unblock:  unblock,
	})
	runner := evals.NewEvalRunner(registry)

	conv := &Conversation{
		config: &config{
			evalRunner:         runner,
			maxConcurrentEvals: 2, // Only allow 2 concurrent evals
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "gated", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
	if cap(mw.sem) != 2 {
		t.Fatalf("expected semaphore capacity 2, got %d", cap(mw.sem))
	}

	// Dispatch 2 evals — both should be accepted (fill the semaphore)
	mw.dispatchTurnEvals(context.Background())
	mw.dispatchTurnEvals(context.Background())

	// Wait for both goroutines to start running
	<-started
	<-started

	// Dispatch a 3rd — should be skipped because semaphore is full
	turnBefore := mw.turnIndex.Load()
	mw.dispatchTurnEvals(context.Background())
	// turnIndex still increments, but no goroutine was launched
	if mw.turnIndex.Load() != turnBefore+1 {
		t.Errorf("expected turnIndex to increment, got %d", mw.turnIndex.Load())
	}

	// Unblock all goroutines and wait
	close(unblock)
	mw.wait()

	// Verify only 2 evals actually ran (the 3rd was skipped)
	if len(started) != 0 {
		t.Errorf("expected started channel to be drained, got %d remaining", len(started))
	}
}

func TestEvalMiddleware_SemaphoreDefaultCapacity(t *testing.T) {
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
	if cap(mw.sem) != DefaultMaxConcurrentEvals {
		t.Errorf("expected default semaphore capacity %d, got %d",
			DefaultMaxConcurrentEvals, cap(mw.sem))
	}
}

func TestEvalMiddleware_SemaphoreReleasedAfterCompletion(t *testing.T) {
	registry := evals.NewEmptyEvalTypeRegistry()
	var count int32
	var mu sync.Mutex
	registry.Register(&countingEvalHandler{
		typeName: "counting",
		count:    &count,
		mu:       &mu,
	})
	runner := evals.NewEvalRunner(registry)

	conv := &Conversation{
		config: &config{
			evalRunner:         runner,
			maxConcurrentEvals: 1, // Only 1 at a time
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "counting", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Dispatch 3 evals sequentially, waiting between each so the semaphore is released
	for range 3 {
		mw.dispatchTurnEvals(context.Background())
		mw.wait()
	}

	mu.Lock()
	defer mu.Unlock()
	if count != 3 {
		t.Errorf("expected 3 eval runs (semaphore released after each), got %d", count)
	}
}

func TestEvalMiddleware_CustomMaxConcurrentEvals(t *testing.T) {
	conv := &Conversation{
		config: &config{maxConcurrentEvals: 5},
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
	if cap(mw.sem) != 5 {
		t.Errorf("expected semaphore capacity 5, got %d", cap(mw.sem))
	}
}

func TestEvalMiddleware_BuildEvalContext_CachesMessages(t *testing.T) {
	conv := &Conversation{
		config: &config{},
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

	// First call at turn 0 — cache should be populated (no session so messages are nil)
	ctx1 := mw.buildEvalContext(context.Background())
	if ctx1.TurnIndex != 0 {
		t.Errorf("expected TurnIndex 0, got %d", ctx1.TurnIndex)
	}

	// Same turn index — should return cached result
	ctx2 := mw.buildEvalContext(context.Background())
	if ctx2.TurnIndex != 0 {
		t.Errorf("expected cached TurnIndex 0, got %d", ctx2.TurnIndex)
	}

	// Increment turn — cache should be invalidated
	mw.turnIndex.Store(1)
	ctx3 := mw.buildEvalContext(context.Background())
	if ctx3.TurnIndex != 1 {
		t.Errorf("expected TurnIndex 1, got %d", ctx3.TurnIndex)
	}
}

func TestEvalMiddleware_TurnIndexAtomic(t *testing.T) {
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

	// Verify atomic operations on turnIndex
	if mw.turnIndex.Load() != 0 {
		t.Errorf("expected initial turnIndex 0, got %d", mw.turnIndex.Load())
	}

	mw.turnIndex.Add(1)
	if mw.turnIndex.Load() != 1 {
		t.Errorf("expected turnIndex 1 after Add, got %d", mw.turnIndex.Load())
	}

	mw.turnIndex.Store(5)
	if mw.turnIndex.Load() != 5 {
		t.Errorf("expected turnIndex 5 after Store, got %d", mw.turnIndex.Load())
	}
}

// gatedEvalHandler signals when started and blocks until unblock is closed.
type gatedEvalHandler struct {
	typeName string
	started  chan struct{}
	unblock  chan struct{}
}

func (h *gatedEvalHandler) Type() string { return h.typeName }

func (h *gatedEvalHandler) Eval(
	_ context.Context, _ *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	h.started <- struct{}{}
	<-h.unblock
	return &evals.EvalResult{Passed: true}, nil
}

func TestEvalMiddleware_EmitResults_WithBus(t *testing.T) {
	// Use a single worker to guarantee ordered dispatch of events.
	bus := events.NewEventBus(events.WithWorkerPoolSize(1))
	defer bus.Close()

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

	// Collect 2 events (order may vary with multiple workers, so use maps).
	var got []*events.Event
	for range 2 {
		select {
		case e := <-received:
			got = append(got, e)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for eval events")
		}
	}

	// Verify we received one completed and one failed event.
	typeCount := map[events.EventType]int{}
	for _, e := range got {
		typeCount[e.Type]++
	}

	if typeCount[events.EventEvalCompleted] != 1 {
		t.Errorf("expected 1 eval.completed, got %d", typeCount[events.EventEvalCompleted])
	}
	if typeCount[events.EventEvalFailed] != 1 {
		t.Errorf("expected 1 eval.failed, got %d", typeCount[events.EventEvalFailed])
	}

	// Verify eval IDs are present.
	for _, e := range got {
		if e.Type == events.EventEvalCompleted {
			data, ok := e.Data.(*events.EvalCompletedData)
			if !ok {
				t.Fatal("expected *EvalCompletedData")
			}
			if data.EvalID != "e1" {
				t.Errorf("expected eval ID e1, got %q", data.EvalID)
			}
		}
	}
}

func TestNewEvalMiddleware_WithEvalGroups(t *testing.T) {
	conv := &Conversation{
		config: &config{evalGroups: []string{"safety"}},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "a", Type: "contains", Trigger: evals.TriggerEveryTurn, Groups: []string{"safety"}},
				{ID: "b", Type: "contains", Trigger: evals.TriggerEveryTurn, Groups: []string{"quality"}},
				{ID: "c", Type: "contains", Trigger: evals.TriggerEveryTurn, Groups: []string{"safety", "quality"}},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
	if len(mw.defs) != 2 {
		t.Fatalf("expected 2 defs (a,c), got %d", len(mw.defs))
	}
	if mw.defs[0].ID != "a" || mw.defs[1].ID != "c" {
		t.Errorf("expected defs [a,c], got [%s,%s]", mw.defs[0].ID, mw.defs[1].ID)
	}
}

func TestNewEvalMiddleware_WithEvalGroupsNoMatch(t *testing.T) {
	conv := &Conversation{
		config: &config{evalGroups: []string{"latency"}},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "a", Type: "contains", Trigger: evals.TriggerEveryTurn, Groups: []string{"safety"}},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw != nil {
		t.Error("expected nil middleware when no defs match group filter")
	}
}

func TestNewEvalMiddleware_WithEvalGroupsDefaultGroup(t *testing.T) {
	conv := &Conversation{
		config: &config{evalGroups: []string{evals.DefaultEvalGroup}},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "a", Type: "contains", Trigger: evals.TriggerEveryTurn},                             // no groups → default
				{ID: "b", Type: "contains", Trigger: evals.TriggerEveryTurn, Groups: []string{"safety"}}, // explicit
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
	if len(mw.defs) != 1 || mw.defs[0].ID != "a" {
		t.Errorf("expected only def 'a' (default group), got %v", mw.defs)
	}
}

func TestNewEvalMiddleware_NilEvalGroupsRunsAll(t *testing.T) {
	conv := &Conversation{
		config: &config{},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "a", Type: "contains", Trigger: evals.TriggerEveryTurn},
				{ID: "b", Type: "contains", Trigger: evals.TriggerEveryTurn, Groups: []string{"safety"}},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
	if len(mw.defs) != 2 {
		t.Errorf("expected all 2 defs when evalGroups is nil, got %d", len(mw.defs))
	}
}

func TestEvalMiddleware_WithMetricRecorder(t *testing.T) {
	collector := evals.NewMetricCollector(evals.WithNamespace("test"))

	conv := &Conversation{
		config: &config{metricRecorder: collector},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{
					ID:      "e1",
					Type:    "contains",
					Trigger: evals.TriggerEveryTurn,
					Metric: &evals.MetricDef{
						Name: "greeting",
						Type: evals.MetricBoolean,
					},
				},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
	if mw.metricWriter == nil {
		t.Fatal("expected non-nil metricWriter when MetricRecorder is configured")
	}

	// Simulate emitting a result — metric should be recorded.
	mw.emitResults([]evals.EvalResult{
		{EvalID: "e1", Type: "contains", Passed: true},
	})

	var buf strings.Builder
	if err := collector.WritePrometheus(&buf); err != nil {
		t.Fatalf("WritePrometheus failed: %v", err)
	}
	if !strings.Contains(buf.String(), "test_greeting") {
		t.Errorf("expected metric 'test_greeting' in output, got: %s", buf.String())
	}
}

func TestEvalMiddleware_WithoutMetricRecorder(t *testing.T) {
	conv := &Conversation{
		config: &config{},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{
					ID:      "e1",
					Type:    "contains",
					Trigger: evals.TriggerEveryTurn,
					Metric: &evals.MetricDef{
						Name: "greeting",
						Type: evals.MetricBoolean,
					},
				},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
	if mw.metricWriter != nil {
		t.Error("expected nil metricWriter when no MetricRecorder configured")
	}

	// Should not panic with nil metricWriter
	mw.emitResults([]evals.EvalResult{
		{EvalID: "e1", Type: "contains", Passed: true},
	})
}

func TestEvalMiddleware_EmitResults_IncludesSessionID(t *testing.T) {
	bus := events.NewEventBus(events.WithWorkerPoolSize(1))
	defer bus.Close()

	received := make(chan *events.Event, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		received <- e
	})

	// Create a conversation with a real unary session that has a known ID.
	conv := newTestConversation()
	conv.config.eventBus = bus
	conv.pack.Evals = []evals.EvalDef{
		{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	mw.emitResults([]evals.EvalResult{
		{EvalID: "e1", Type: "contains", Passed: true},
	})

	select {
	case e := <-received:
		// The session ID should match the conversation's session ID.
		expectedID := conv.ID()
		if e.SessionID != expectedID {
			t.Errorf("expected SessionID %q on eval event, got %q", expectedID, e.SessionID)
		}
		if e.SessionID == "" {
			t.Error("eval event SessionID must not be empty when session is available")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for eval event")
	}
}
