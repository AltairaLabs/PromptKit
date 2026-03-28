package workflow

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func linearSpec() *Spec {
	return &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "task_a", OnEvent: map[string]string{"Next": "b"}},
			"b": {PromptTask: "task_b", OnEvent: map[string]string{"Next": "c"}},
			"c": {PromptTask: "task_c"},
		},
	}
}

func branchingSpec() *Spec {
	return &Spec{
		Version: 1,
		Entry:   "start",
		States: map[string]*State{
			"start": {PromptTask: "triage", OnEvent: map[string]string{
				"ToBilling":   "billing",
				"ToTechnical": "technical",
			}},
			"billing":   {PromptTask: "billing_task"},
			"technical": {PromptTask: "tech_task"},
		},
	}
}

func loopingSpec() *Spec {
	return &Spec{
		Version: 1,
		Entry:   "draft",
		States: map[string]*State{
			"draft": {PromptTask: "write", OnEvent: map[string]string{
				"Review": "review",
			}},
			"review": {PromptTask: "check", OnEvent: map[string]string{
				"Revise":  "draft",
				"Approve": "done",
			}},
			"done": {PromptTask: "final"},
		},
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC)
}

func TestNewStateMachine(t *testing.T) {
	sm := NewStateMachine(linearSpec())
	if sm.CurrentState() != "a" {
		t.Errorf("CurrentState = %q, want %q", sm.CurrentState(), "a")
	}
	if sm.CurrentPromptTask() != "task_a" {
		t.Errorf("CurrentPromptTask = %q, want %q", sm.CurrentPromptTask(), "task_a")
	}
}

func TestLinearWorkflow(t *testing.T) {
	sm := NewStateMachine(linearSpec()).WithTimeFunc(func() time.Time { return fixedTime() })

	// a -> b
	if _, err := sm.ProcessEvent("Next"); err != nil {
		t.Fatalf("ProcessEvent(Next): %v", err)
	}
	if sm.CurrentState() != "b" {
		t.Errorf("CurrentState = %q, want %q", sm.CurrentState(), "b")
	}
	if sm.CurrentPromptTask() != "task_b" {
		t.Errorf("CurrentPromptTask = %q, want %q", sm.CurrentPromptTask(), "task_b")
	}
	if sm.IsTerminal() {
		t.Error("state b should not be terminal")
	}

	// b -> c
	if _, err := sm.ProcessEvent("Next"); err != nil {
		t.Fatalf("ProcessEvent(Next): %v", err)
	}
	if sm.CurrentState() != "c" {
		t.Errorf("CurrentState = %q, want %q", sm.CurrentState(), "c")
	}
	if !sm.IsTerminal() {
		t.Error("state c should be terminal")
	}

	// c is terminal — further events should fail
	_, err := sm.ProcessEvent("Next")
	if !errors.Is(err, ErrTerminalState) {
		t.Errorf("expected ErrTerminalState, got: %v", err)
	}
}

func TestBranchingWorkflow(t *testing.T) {
	sm := NewStateMachine(branchingSpec())

	if _, err := sm.ProcessEvent("ToBilling"); err != nil {
		t.Fatalf("ProcessEvent(ToBilling): %v", err)
	}
	if sm.CurrentState() != "billing" {
		t.Errorf("CurrentState = %q, want %q", sm.CurrentState(), "billing")
	}
	if !sm.IsTerminal() {
		t.Error("billing should be terminal")
	}

	// Start fresh and go the other way
	sm2 := NewStateMachine(branchingSpec())
	if _, err := sm2.ProcessEvent("ToTechnical"); err != nil {
		t.Fatalf("ProcessEvent(ToTechnical): %v", err)
	}
	if sm2.CurrentState() != "technical" {
		t.Errorf("CurrentState = %q, want %q", sm2.CurrentState(), "technical")
	}
}

func TestLoopingWorkflow(t *testing.T) {
	sm := NewStateMachine(loopingSpec())

	// draft -> review -> draft -> review -> done
	if _, err := sm.ProcessEvent("Review"); err != nil {
		t.Fatal(err)
	}
	if sm.CurrentState() != "review" {
		t.Fatalf("CurrentState = %q, want review", sm.CurrentState())
	}

	if _, err := sm.ProcessEvent("Revise"); err != nil {
		t.Fatal(err)
	}
	if sm.CurrentState() != "draft" {
		t.Fatalf("CurrentState = %q, want draft", sm.CurrentState())
	}

	if _, err := sm.ProcessEvent("Review"); err != nil {
		t.Fatal(err)
	}
	if _, err := sm.ProcessEvent("Approve"); err != nil {
		t.Fatal(err)
	}
	if sm.CurrentState() != "done" {
		t.Fatalf("CurrentState = %q, want done", sm.CurrentState())
	}
	if !sm.IsTerminal() {
		t.Error("done should be terminal")
	}

	ctx := sm.Context()
	if ctx.TransitionCount() != 4 {
		t.Errorf("TransitionCount = %d, want 4", ctx.TransitionCount())
	}
}

func TestInvalidEvent(t *testing.T) {
	sm := NewStateMachine(linearSpec())
	_, err := sm.ProcessEvent("Nonexistent")
	if !errors.Is(err, ErrInvalidEvent) {
		t.Errorf("expected ErrInvalidEvent, got: %v", err)
	}
	// State should not change on error
	if sm.CurrentState() != "a" {
		t.Errorf("state should not change on error, got %q", sm.CurrentState())
	}
}

func TestAvailableEvents(t *testing.T) {
	sm := NewStateMachine(branchingSpec())
	events := sm.AvailableEvents()
	if len(events) != 2 {
		t.Fatalf("AvailableEvents len = %d, want 2", len(events))
	}
	// Should be sorted
	if events[0] != "ToBilling" || events[1] != "ToTechnical" {
		t.Errorf("AvailableEvents = %v, want [ToBilling ToTechnical]", events)
	}

	// Terminal state has no events
	sm2 := NewStateMachine(linearSpec())
	_, _ = sm2.ProcessEvent("Next")
	_, _ = sm2.ProcessEvent("Next")
	if events := sm2.AvailableEvents(); events != nil {
		t.Errorf("terminal state AvailableEvents = %v, want nil", events)
	}
}

func TestContextSnapshot(t *testing.T) {
	sm := NewStateMachine(linearSpec()).WithTimeFunc(func() time.Time { return fixedTime() })
	_, _ = sm.ProcessEvent("Next")

	ctx := sm.Context()
	if ctx.CurrentState != "b" {
		t.Errorf("Context.CurrentState = %q, want %q", ctx.CurrentState, "b")
	}
	if ctx.TransitionCount() != 1 {
		t.Errorf("TransitionCount = %d, want 1", ctx.TransitionCount())
	}

	// Mutating the snapshot should not affect the machine
	ctx.CurrentState = "modified"
	if sm.CurrentState() != "b" {
		t.Error("Context snapshot should be independent of machine")
	}
}

func TestRestoreFromContext(t *testing.T) {
	spec := linearSpec()
	sm := NewStateMachine(spec).WithTimeFunc(func() time.Time { return fixedTime() })
	_, _ = sm.ProcessEvent("Next") // a -> b

	// Persist and restore
	savedCtx := sm.Context()
	sm2 := NewStateMachineFromContext(spec, savedCtx)

	if sm2.CurrentState() != "b" {
		t.Errorf("restored CurrentState = %q, want %q", sm2.CurrentState(), "b")
	}
	if sm2.CurrentPromptTask() != "task_b" {
		t.Errorf("restored CurrentPromptTask = %q, want %q", sm2.CurrentPromptTask(), "task_b")
	}

	// Continue from restored state
	if _, err := sm2.ProcessEvent("Next"); err != nil {
		t.Fatalf("ProcessEvent after restore: %v", err)
	}
	if sm2.CurrentState() != "c" {
		t.Errorf("state after continue = %q, want %q", sm2.CurrentState(), "c")
	}

	ctx2 := sm2.Context()
	if ctx2.TransitionCount() != 2 {
		t.Errorf("TransitionCount = %d, want 2", ctx2.TransitionCount())
	}
}

func TestTransitionHistoryTimestamps(t *testing.T) {
	callCount := 0
	times := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	sm := NewStateMachine(linearSpec()).WithTimeFunc(func() time.Time {
		t := times[callCount]
		callCount++
		return t
	})

	_, _ = sm.ProcessEvent("Next")
	_, _ = sm.ProcessEvent("Next")

	ctx := sm.Context()
	if !ctx.History[0].Timestamp.Equal(times[0]) {
		t.Errorf("History[0].Timestamp = %v, want %v", ctx.History[0].Timestamp, times[0])
	}
	if !ctx.History[1].Timestamp.Equal(times[1]) {
		t.Errorf("History[1].Timestamp = %v, want %v", ctx.History[1].Timestamp, times[1])
	}
}

// longChainSpec creates a linear spec with many states for concurrency tests.
func longChainSpec() *Spec {
	states := map[string]*State{}
	for i := 0; i < 100; i++ {
		name := stateNameForIndex(i)
		next := stateNameForIndex(i + 1)
		if i == 99 {
			states[name] = &State{PromptTask: "task_" + name}
		} else {
			states[name] = &State{PromptTask: "task_" + name, OnEvent: map[string]string{"Next": next}}
		}
	}
	return &Spec{Version: 1, Entry: "s0", States: states}
}

func stateNameForIndex(i int) string {
	return "s" + string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func TestConcurrentReads(t *testing.T) {
	sm := NewStateMachine(loopingSpec())
	_, _ = sm.ProcessEvent("Review")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sm.CurrentState()
			_ = sm.CurrentPromptTask()
			_ = sm.IsTerminal()
			_ = sm.AvailableEvents()
			_ = sm.Context()
		}()
	}
	wg.Wait()
}

func TestConcurrentProcessEvent(t *testing.T) {
	// Use the looping spec so events keep cycling: draft -> review -> draft -> ...
	spec := loopingSpec()
	sm := NewStateMachine(spec)

	var wg sync.WaitGroup
	const goroutines = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				// Try all possible events; ignore errors from invalid state combos
				_, _ = sm.ProcessEvent("Review")
				_, _ = sm.ProcessEvent("Revise")
				_, _ = sm.ProcessEvent("Approve")
			}
		}()
	}
	wg.Wait()

	// After all goroutines, the state machine should be in a valid state.
	state := sm.CurrentState()
	if state != "draft" && state != "review" && state != "done" {
		t.Errorf("unexpected state %q after concurrent processing", state)
	}
}

func TestConcurrentReadsDuringWrites(t *testing.T) {
	spec := loopingSpec()
	sm := NewStateMachine(spec)

	var wg sync.WaitGroup

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_, _ = sm.ProcessEvent("Review")
				_, _ = sm.ProcessEvent("Revise")
			}
		}()
	}

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = sm.CurrentState()
				_ = sm.CurrentPromptTask()
				_ = sm.IsTerminal()
				_ = sm.AvailableEvents()
				_ = sm.Context()
			}
		}()
	}

	wg.Wait()
}

func TestConcurrentContextSnapshot(t *testing.T) {
	spec := loopingSpec()
	sm := NewStateMachine(spec)

	var wg sync.WaitGroup
	snapshots := make([]*Context, 20)

	// Take snapshots concurrently while events are being processed.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = sm.ProcessEvent("Review")
			_, _ = sm.ProcessEvent("Revise")
		}()
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			snapshots[idx] = sm.Context()
		}(i)
	}
	wg.Wait()

	// All snapshots should be valid contexts with a known state.
	for i, snap := range snapshots {
		if snap == nil {
			t.Fatalf("snapshot %d is nil", i)
		}
		if snap.CurrentState != "draft" && snap.CurrentState != "review" && snap.CurrentState != "done" {
			t.Errorf("snapshot %d has invalid state %q", i, snap.CurrentState)
		}
	}
}

func TestTerminal_ExplicitField(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "task_a", OnEvent: map[string]string{"Next": "b"}},
			"b": {PromptTask: "task_b", Terminal: true},
		},
	}
	sm := NewStateMachine(spec)
	if _, err := sm.ProcessEvent("Next"); err != nil {
		t.Fatalf("ProcessEvent(Next): %v", err)
	}
	if !sm.IsTerminal() {
		t.Error("state b with Terminal:true should be terminal")
	}
}

func TestTerminal_BackwardCompat(t *testing.T) {
	// A state with empty OnEvent (no Terminal field set) is still terminal.
	spec := &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "task_a", OnEvent: map[string]string{"Next": "b"}},
			"b": {PromptTask: "task_b"},
		},
	}
	sm := NewStateMachine(spec)
	if _, err := sm.ProcessEvent("Next"); err != nil {
		t.Fatalf("ProcessEvent(Next): %v", err)
	}
	if !sm.IsTerminal() {
		t.Error("state b with empty OnEvent should be terminal (backward compat)")
	}
}

func TestTerminal_WithOnEvent_BlocksTransitions(t *testing.T) {
	// A state with Terminal: true blocks ProcessEvent even if OnEvent is set.
	// This ensures consistency between IsTerminal() and ProcessEvent().
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {
				PromptTask: "task_a",
				Terminal:   true,
				OnEvent:    map[string]string{"Next": "b"},
			},
			"b": {PromptTask: "task_b"},
		},
	}
	sm := NewStateMachine(spec)
	if !sm.IsTerminal() {
		t.Error("state a with Terminal: true should be terminal")
	}
	_, err := sm.ProcessEvent("Next")
	if !errors.Is(err, ErrTerminalState) {
		t.Errorf("expected ErrTerminalState, got: %v", err)
	}
}

func TestMaxVisits_RedirectOnExceeded(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "task_a", OnEvent: map[string]string{"Loop": "b"}},
			"b": {
				PromptTask:  "task_b",
				MaxVisits:   2,
				OnMaxVisits: "c",
				OnEvent:     map[string]string{"Back": "a"},
			},
			"c": {PromptTask: "task_c"},
		},
	}
	sm := NewStateMachine(spec)

	// Visit b twice (MaxVisits=2)
	if _, err := sm.ProcessEvent("Loop"); err != nil { // a -> b (visit 1)
		t.Fatal(err)
	}
	if sm.CurrentState() != "b" {
		t.Fatalf("expected b, got %q", sm.CurrentState())
	}
	if _, err := sm.ProcessEvent("Back"); err != nil { // b -> a
		t.Fatal(err)
	}
	if _, err := sm.ProcessEvent("Loop"); err != nil { // a -> b (visit 2)
		t.Fatal(err)
	}
	if sm.CurrentState() != "b" {
		t.Fatalf("expected b, got %q", sm.CurrentState())
	}
	if _, err := sm.ProcessEvent("Back"); err != nil { // b -> a
		t.Fatal(err)
	}

	// Third attempt should redirect to c
	result, err := sm.ProcessEvent("Loop") // a -> redirect to c
	if err != nil {
		t.Fatalf("expected redirect, got error: %v", err)
	}
	if sm.CurrentState() != "c" {
		t.Errorf("expected redirect to c, got %q", sm.CurrentState())
	}
	if !result.Redirected {
		t.Error("expected Redirected=true")
	}
	if result.OriginalTarget != "b" {
		t.Errorf("OriginalTarget = %q, want %q", result.OriginalTarget, "b")
	}
}

func TestMaxVisits_ErrorWhenNoFallback(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "task_a", OnEvent: map[string]string{"Loop": "b"}},
			"b": {
				PromptTask: "task_b",
				MaxVisits:  1,
				OnEvent:    map[string]string{"Back": "a"},
			},
		},
	}
	sm := NewStateMachine(spec)

	// First visit succeeds
	if _, err := sm.ProcessEvent("Loop"); err != nil { // a -> b (visit 1)
		t.Fatal(err)
	}
	if _, err := sm.ProcessEvent("Back"); err != nil { // b -> a
		t.Fatal(err)
	}

	// Second attempt should fail — no OnMaxVisits fallback
	_, err := sm.ProcessEvent("Loop")
	if !errors.Is(err, ErrMaxVisitsExceeded) {
		t.Errorf("expected ErrMaxVisitsExceeded, got: %v", err)
	}
	// State should not change on error
	if sm.CurrentState() != "a" {
		t.Errorf("state should remain a on error, got %q", sm.CurrentState())
	}
}

func TestMaxVisits_EntryStateCounted(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "task_a", OnEvent: map[string]string{"Next": "b"}},
			"b": {PromptTask: "task_b"},
		},
	}
	sm := NewStateMachine(spec)

	ctx := sm.Context()
	if ctx.VisitCounts["a"] != 1 {
		t.Errorf("entry state visit count = %d, want 1", ctx.VisitCounts["a"])
	}
}

func TestMaxVisits_RedirectTargetVisitCounted(t *testing.T) {
	spec := &Spec{
		Version: 1,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "task_a", OnEvent: map[string]string{"Loop": "b"}},
			"b": {
				PromptTask:  "task_b",
				MaxVisits:   1,
				OnMaxVisits: "c",
				OnEvent:     map[string]string{"Back": "a"},
			},
			"c": {PromptTask: "task_c"},
		},
	}
	sm := NewStateMachine(spec)

	// Visit b once
	if _, err := sm.ProcessEvent("Loop"); err != nil { // a -> b (visit 1)
		t.Fatal(err)
	}
	if _, err := sm.ProcessEvent("Back"); err != nil { // b -> a
		t.Fatal(err)
	}

	// Second attempt redirects to c
	if _, err := sm.ProcessEvent("Loop"); err != nil { // a -> redirect to c
		t.Fatal(err)
	}

	ctx := sm.Context()
	if ctx.VisitCounts["c"] != 1 {
		t.Errorf("redirect target visit count = %d, want 1", ctx.VisitCounts["c"])
	}
}

func TestProcessEvent_ReturnsTransitionResult(t *testing.T) {
	sm := NewStateMachine(linearSpec())

	result, err := sm.ProcessEvent("Next") // a -> b
	if err != nil {
		t.Fatal(err)
	}
	if result.From != "a" {
		t.Errorf("From = %q, want %q", result.From, "a")
	}
	if result.To != "b" {
		t.Errorf("To = %q, want %q", result.To, "b")
	}
	if result.Event != "Next" {
		t.Errorf("Event = %q, want %q", result.Event, "Next")
	}
	if result.Redirected {
		t.Error("expected Redirected=false for normal transition")
	}
	if result.OriginalTarget != "" {
		t.Errorf("OriginalTarget = %q, want empty", result.OriginalTarget)
	}
}

func TestVisitCounts_Persisted(t *testing.T) {
	sm := NewStateMachine(linearSpec())
	_, _ = sm.ProcessEvent("Next") // a -> b

	ctx := sm.Context()
	clone := ctx.Clone()

	if clone.VisitCounts["a"] != 1 {
		t.Errorf("clone VisitCounts[a] = %d, want 1", clone.VisitCounts["a"])
	}
	if clone.VisitCounts["b"] != 1 {
		t.Errorf("clone VisitCounts[b] = %d, want 1", clone.VisitCounts["b"])
	}

	// Mutating original should not affect clone
	ctx.VisitCounts["a"] = 999
	if clone.VisitCounts["a"] != 1 {
		t.Error("Clone() should deep-copy VisitCounts")
	}
}

// --- Budget tests ---

func TestBudget_MaxTotalVisitsExhausted(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t", OnEvent: map[string]string{"Back": "a"}},
		},
		Engine: map[string]any{
			"budget": map[string]any{"max_total_visits": 3},
		},
	}
	sm := NewStateMachine(spec)
	// Entry = "a" (visit 1). Budget = 3 total visits.
	if _, err := sm.ProcessEvent("Go"); err != nil { // → b (visit 2)
		t.Fatalf("ProcessEvent(Go): %v", err)
	}
	if _, err := sm.ProcessEvent("Back"); err != nil { // → a (visit 3, at limit)
		t.Fatalf("ProcessEvent(Back): %v", err)
	}

	// Next transition should fail — total visits would be 4
	_, err := sm.ProcessEvent("Go")
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected ErrBudgetExhausted, got: %v", err)
	}
}

func TestBudget_MaxToolCallsExhausted(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t"},
		},
		Engine: map[string]any{
			"budget": map[string]any{"max_tool_calls": 5},
		},
	}
	sm := NewStateMachine(spec)
	sm.IncrementToolCalls(5) // hit the limit

	_, err := sm.ProcessEvent("Go")
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected ErrBudgetExhausted, got: %v", err)
	}
}

func TestBudget_MaxWallTimeExhausted(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t"},
		},
		Engine: map[string]any{
			"budget": map[string]any{"max_wall_time_sec": 10},
		},
	}
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sm := NewStateMachine(spec).WithTimeFunc(func() time.Time {
		return baseTime.Add(11 * time.Second)
	})
	sm.context.StartedAt = baseTime

	_, err := sm.ProcessEvent("Go")
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected ErrBudgetExhausted, got: %v", err)
	}
}

func TestBudget_NoBudgetIsUnlimited(t *testing.T) {
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", OnEvent: map[string]string{"Go": "b"}},
			"b": {PromptTask: "t"},
		},
		// No Engine/budget
	}
	sm := NewStateMachine(spec)
	sm.IncrementToolCalls(9999)

	if _, err := sm.ProcessEvent("Go"); err != nil {
		t.Fatalf("expected no error without budget, got: %v", err)
	}
}

func TestBudget_PrecedesMaxVisits(t *testing.T) {
	// Budget exhaustion should take precedence over max_visits
	spec := &Spec{
		Version: 2,
		Entry:   "a",
		States: map[string]*State{
			"a": {PromptTask: "t", MaxVisits: 100, OnMaxVisits: "fallback",
				OnEvent: map[string]string{"Go": "a"}},
			"fallback": {PromptTask: "t"},
		},
		Engine: map[string]any{
			"budget": map[string]any{"max_total_visits": 2},
		},
	}
	sm := NewStateMachine(spec)
	// Entry = "a" (visit 1). Budget = 2 total visits.
	if _, err := sm.ProcessEvent("Go"); err != nil { // → a (visit 2, at budget)
		t.Fatalf("ProcessEvent(Go): %v", err)
	}
	_, err := sm.ProcessEvent("Go") // budget exhausted before max_visits
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("budget should take precedence, got: %v", err)
	}
}

func TestIncrementToolCalls(t *testing.T) {
	spec := &Spec{Version: 1, Entry: "a", States: map[string]*State{
		"a": {PromptTask: "t"},
	}}
	sm := NewStateMachine(spec)
	sm.IncrementToolCalls(3)
	sm.IncrementToolCalls(2)

	ctx := sm.Context()
	if ctx.TotalToolCalls != 5 {
		t.Errorf("TotalToolCalls = %d, want 5", ctx.TotalToolCalls)
	}
}

func TestTotalVisits(t *testing.T) {
	ctx := NewContext("a", time.Now())
	if ctx.TotalVisits() != 1 {
		t.Errorf("TotalVisits = %d, want 1", ctx.TotalVisits())
	}
	ctx.RecordTransition("a", "b", "Go", time.Now())
	if ctx.TotalVisits() != 2 {
		t.Errorf("TotalVisits = %d, want 2", ctx.TotalVisits())
	}
	ctx.RecordTransition("b", "a", "Back", time.Now())
	if ctx.TotalVisits() != 3 {
		t.Errorf("TotalVisits = %d, want 3", ctx.TotalVisits())
	}
}
