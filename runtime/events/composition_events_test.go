package events

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestEmitter_CompositionEvents verifies that the Emitter publishes all six
// composition.* events in order and that the payloads carry the expected values.
func TestEmitter_CompositionEvents(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	em := NewEmitter(bus, "exec1", "sess1", "conv1")

	var captured []*Event
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(6)

	bus.SubscribeAll(func(e *Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
		wg.Done()
	})

	em.CompositionStarted("flow", json.RawMessage(`{"x":1}`))
	em.CompositionStepStarted("classify", "prompt", json.RawMessage(`"in"`))
	em.CompositionStepCompleted("classify", "prompt", json.RawMessage(`"in"`), json.RawMessage(`{"type":"paper"}`), 1, nil)
	em.CompositionBranchEvaluated("route", "extract_paper")
	em.CompositionParallelCompleted("meta", []CompositionParallelBranch{{ID: "a", Status: "complete"}})
	em.CompositionCompleted("flow", json.RawMessage(`{"ok":true}`), nil, 5)

	if !waitForWG(&wg, 500*time.Millisecond) {
		t.Fatalf("timed out: expected 6 composition events, got %d", len(captured))
	}

	mu.Lock()
	defer mu.Unlock()

	if len(captured) != 6 {
		t.Fatalf("expected 6 events, got %d", len(captured))
	}

	// Assert event types in order.
	wantTypes := []EventType{
		EventCompositionStarted,
		EventCompositionStepStarted,
		EventCompositionStepCompleted,
		EventCompositionBranchEvaluated,
		EventCompositionParallelCompleted,
		EventCompositionCompleted,
	}

	// Sort by sequence to assert order.
	// Events are appended under a mutex but goroutine scheduling may vary;
	// sort by Sequence (stamped monotonically by the bus) for a stable check.
	sortBySequence(captured)

	for i, want := range wantTypes {
		if captured[i].Type != want {
			t.Errorf("event[%d]: got type %q, want %q", i, captured[i].Type, want)
		}
	}

	// Spot-check CompositionStepCompleted payload (index 2).
	stepCompleted, ok := captured[2].Data.(*CompositionStepCompletedData)
	if !ok {
		t.Fatalf("event[2] data: got %T, want *CompositionStepCompletedData", captured[2].Data)
	}
	if stepCompleted.StepID != "classify" {
		t.Errorf("StepID = %q, want %q", stepCompleted.StepID, "classify")
	}
	if stepCompleted.Kind != "prompt" {
		t.Errorf("Kind = %q, want %q", stepCompleted.Kind, "prompt")
	}
	if string(stepCompleted.Output) != `{"type":"paper"}` {
		t.Errorf("Output = %q, want %q", string(stepCompleted.Output), `{"type":"paper"}`)
	}
	if stepCompleted.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", stepCompleted.Attempt)
	}
	if stepCompleted.Error != "" {
		t.Errorf("Error = %q, want empty (no error)", stepCompleted.Error)
	}

	// Spot-check CompositionCompleted payload (index 5).
	completed, ok := captured[5].Data.(*CompositionCompletedData)
	if !ok {
		t.Fatalf("event[5] data: got %T, want *CompositionCompletedData", captured[5].Data)
	}
	if completed.DurationMs != 5 {
		t.Errorf("DurationMs = %d, want 5", completed.DurationMs)
	}
	if completed.Error != "" {
		t.Errorf("Error = %q, want empty", completed.Error)
	}
}

// TestEmitter_CompositionEvents_ErrorString verifies that CompositionCompleted
// and CompositionStepCompleted carry error strings from non-nil errors.
func TestEmitter_CompositionEvents_ErrorString(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	em := NewEmitter(bus, "exec-err", "sess-err", "conv-err")

	var gotStep, gotComp *Event
	var wg sync.WaitGroup
	wg.Add(2)

	bus.Subscribe(EventCompositionStepCompleted, func(e *Event) {
		gotStep = e
		wg.Done()
	})
	bus.Subscribe(EventCompositionCompleted, func(e *Event) {
		gotComp = e
		wg.Done()
	})

	em.CompositionStepCompleted("s1", "prompt", nil, nil, 2, errors.New("step failed"))
	em.CompositionCompleted("flow", nil, errors.New("flow failed"), 10)

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for events")
	}

	stepData := gotStep.Data.(*CompositionStepCompletedData)
	if stepData.Error != "step failed" {
		t.Errorf("step Error = %q, want %q", stepData.Error, "step failed")
	}

	compData := gotComp.Data.(*CompositionCompletedData)
	if compData.Error != "flow failed" {
		t.Errorf("comp Error = %q, want %q", compData.Error, "flow failed")
	}
}

// TestEmitter_CompositionStartedCtx verifies the Ctx variant stamps a SpanContext.
func TestEmitter_CompositionStartedCtx(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	em := NewEmitter(bus, "exec-ctx", "sess-ctx", "conv-ctx")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)
	bus.Subscribe(EventCompositionStarted, func(e *Event) {
		got = e
		wg.Done()
	})

	// nil ctx is safe.
	em.CompositionStartedCtx(nil, "flow", json.RawMessage(`{}`))

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for composition.started event")
	}

	if got.Type != EventCompositionStarted {
		t.Errorf("Type = %q, want %q", got.Type, EventCompositionStarted)
	}
	data, ok := got.Data.(*CompositionStartedData)
	if !ok {
		t.Fatalf("Data: got %T, want *CompositionStartedData", got.Data)
	}
	if data.Composition != "flow" {
		t.Errorf("Composition = %q, want %q", data.Composition, "flow")
	}
}

// TestEmitter_CompositionStepStartedCtx verifies the Ctx variant for step.started.
func TestEmitter_CompositionStepStartedCtx(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	em := NewEmitter(bus, "exec-sctx", "sess-sctx", "conv-sctx")

	var got *Event
	var wg sync.WaitGroup
	wg.Add(1)
	bus.Subscribe(EventCompositionStepStarted, func(e *Event) {
		got = e
		wg.Done()
	})

	em.CompositionStepStartedCtx(nil, "classify", "prompt", json.RawMessage(`"input"`))

	if !waitForWG(&wg, 200*time.Millisecond) {
		t.Fatal("timed out waiting for composition.step.started event")
	}

	data, ok := got.Data.(*CompositionStepStartedData)
	if !ok {
		t.Fatalf("Data: got %T, want *CompositionStepStartedData", got.Data)
	}
	if data.StepID != "classify" || data.Kind != "prompt" {
		t.Errorf("StepID=%q Kind=%q, want classify/prompt", data.StepID, data.Kind)
	}
}

// sortBySequence is a minimal sort used by tests (avoids importing sort for a small slice).
func sortBySequence(events []*Event) {
	n := len(events)
	for i := 1; i < n; i++ {
		for j := i; j > 0 && events[j].Sequence < events[j-1].Sequence; j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
	}
}
