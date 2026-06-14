package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
)

// fakeExec returns canned output per step id and records the resolved input it saw.
// mu guards calls, seen, and attempt so fakeExec is safe to use from concurrent goroutines
// (e.g. parallel branch execution in TestExecute_ParallelBarrier).
type fakeExec struct {
	mu      sync.Mutex
	outputs map[string]any
	seen    map[string]json.RawMessage
	calls   []string
	errOn   map[string]int
	attempt map[string]int
}

func newFakeExec(outputs map[string]any) *fakeExec {
	return &fakeExec{
		outputs: outputs,
		seen:    map[string]json.RawMessage{},
		errOn:   map[string]int{},
		attempt: map[string]int{},
	}
}

func (f *fakeExec) exec(_ context.Context, step *composition.Step, input json.RawMessage) (json.RawMessage, error) {
	f.mu.Lock()
	f.calls = append(f.calls, step.ID)
	f.seen[step.ID] = input
	f.attempt[step.ID]++
	fails := f.errOn[step.ID]
	attempt := f.attempt[step.ID]
	out := f.outputs[step.ID]
	f.mu.Unlock()
	if attempt <= fails {
		return nil, context.DeadlineExceeded
	}
	raw, _ := json.Marshal(out)
	return raw, nil
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExecute_Sequential(t *testing.T) {
	comp := &composition.Composition{
		Version: 1,
		Output:  "second",
		Steps: []*composition.Step{
			{ID: "first", Kind: composition.KindPrompt, PromptTask: "p1", Input: "${input.text}"},
			{ID: "second", Kind: composition.KindPrompt, PromptTask: "p2", Input: "${first.output.label}"},
		},
	}
	fe := newFakeExec(map[string]any{
		"first":  map[string]any{"label": "greeting"},
		"second": map[string]any{"reply": "hi"},
	})
	eng := New(fe.exec)

	out, err := eng.Execute(context.Background(), comp, mustJSON(t, map[string]any{"text": "hello"}))
	if err != nil {
		t.Fatal(err)
	}

	if !reflectDeepEqual(fe.calls, []string{"first", "second"}) {
		t.Errorf("calls = %v", fe.calls)
	}
	if string(fe.seen["second"]) != `"greeting"` {
		t.Errorf("second input = %s", fe.seen["second"])
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !reflectDeepEqual(got, map[string]any{"reply": "hi"}) {
		t.Errorf("output = %#v", got)
	}
}

func TestExecute_DefaultOutputIsLastStep(t *testing.T) {
	comp := &composition.Composition{
		Version: 1,
		Steps: []*composition.Step{
			{ID: "a", Kind: composition.KindTool, Tool: "t.a", Args: map[string]any{"x": "${input.v}"}},
			{ID: "b", Kind: composition.KindTool, Tool: "t.b"},
		},
	}
	fe := newFakeExec(map[string]any{"a": "ra", "b": "rb"})
	out, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"v": 1}))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"rb"` {
		t.Errorf("default output = %s, want \"rb\"", out)
	}
	if string(fe.seen["a"]) != `{"x":1}` {
		t.Errorf("tool a args = %s", fe.seen["a"])
	}
}

func TestExecute_NilComposition(t *testing.T) {
	fe := newFakeExec(nil)
	_, err := New(fe.exec).Execute(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for nil composition")
	}
}

func TestExecute_NilInput(t *testing.T) {
	comp := &composition.Composition{
		Version: 1,
		Steps:   []*composition.Step{{ID: "x", Kind: composition.KindTool, Tool: "t"}},
	}
	fe := newFakeExec(map[string]any{"x": "ok"})
	out, err := New(fe.exec).Execute(context.Background(), comp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"ok"` {
		t.Errorf("nil-input output = %s", out)
	}
}

func TestExecute_AgentKind(t *testing.T) {
	comp := &composition.Composition{
		Version: 1,
		Steps:   []*composition.Step{{ID: "ag", Kind: composition.KindAgent, Input: "${input.q}"}},
	}
	fe := newFakeExec(map[string]any{"ag": "answer"})
	out, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"q": "hi"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"answer"` {
		t.Errorf("agent output = %s", out)
	}
	if string(fe.seen["ag"]) != `"hi"` {
		t.Errorf("agent resolved input = %s", fe.seen["ag"])
	}
}

func TestExecute_BranchKind_NoOutput(t *testing.T) {
	// A branch step produces no scope output; Output must be set explicitly.
	comp := &composition.Composition{
		Version: 1,
		Output:  "before",
		Steps: []*composition.Step{
			{ID: "before", Kind: composition.KindPrompt, Input: "x"},
			{ID: "br", Kind: composition.KindBranch,
				Predicate: &composition.Predicate{Path: "${input.v}", Op: "equals", Value: true}},
		},
	}
	fe := newFakeExec(map[string]any{"before": "done"})
	out, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"v": false}))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"done"` {
		t.Errorf("branch no-output = %s", out)
	}
}

func TestExecute_ParallelMissingReducer(t *testing.T) {
	// A parallel step with no reducer (Reduce: nil) must return an error immediately.
	comp := &composition.Composition{
		Version: 1,
		Steps:   []*composition.Step{{ID: "p", Kind: composition.KindParallel, Reduce: nil}},
	}
	fe := newFakeExec(nil)
	_, err := New(fe.exec).Execute(context.Background(), comp, nil)
	if err == nil {
		t.Fatal("expected error from parallel step with missing reducer")
	}
}

func TestExecute_UnknownKind(t *testing.T) {
	comp := &composition.Composition{
		Version: 1,
		Steps:   []*composition.Step{{ID: "x", Kind: "unknown"}},
	}
	fe := newFakeExec(nil)
	_, err := New(fe.exec).Execute(context.Background(), comp, nil)
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestExecute_StepError(t *testing.T) {
	comp := &composition.Composition{
		Version: 1,
		Steps:   []*composition.Step{{ID: "fail", Kind: composition.KindTool, Tool: "t"}},
	}
	fe := newFakeExec(nil)
	fe.errOn["fail"] = 1 // fail on first attempt
	_, err := New(fe.exec).Execute(context.Background(), comp, nil)
	if err == nil {
		t.Fatal("expected error from failing step")
	}
}

func TestExecute_OutputStepNotCompleted(t *testing.T) {
	// Output points to a non-existent step id.
	comp := &composition.Composition{
		Version: 1,
		Output:  "missing",
		Steps:   []*composition.Step{{ID: "x", Kind: composition.KindTool, Tool: "t"}},
	}
	fe := newFakeExec(map[string]any{"x": "v"})
	_, err := New(fe.exec).Execute(context.Background(), comp, nil)
	if err == nil {
		t.Fatal("expected error when output step did not complete")
	}
}

func TestExecute_EmptySteps(t *testing.T) {
	comp := &composition.Composition{Version: 1}
	fe := newFakeExec(nil)
	_, err := New(fe.exec).Execute(context.Background(), comp, nil)
	if err == nil {
		t.Fatal("expected error — no steps means no output")
	}
}

func TestShouldSkip_AllDepsSkipped(t *testing.T) {
	status := map[string]stepStatus{
		"a": statusSkipped,
		"b": statusSkipped,
	}
	step := &composition.Step{ID: "c", DependsOn: []string{"a", "b"}}
	if !shouldSkip(step, status) {
		t.Error("expected shouldSkip=true when all deps are skipped")
	}
}

func TestShouldSkip_OneDep_Completed(t *testing.T) {
	status := map[string]stepStatus{
		"a": statusSkipped,
		"b": statusCompleted,
	}
	step := &composition.Step{ID: "c", DependsOn: []string{"a", "b"}}
	if shouldSkip(step, status) {
		t.Error("expected shouldSkip=false when one dep completed")
	}
}

func TestShouldSkip_NoDeps(t *testing.T) {
	step := &composition.Step{ID: "c"}
	if shouldSkip(step, nil) {
		t.Error("expected shouldSkip=false for step with no deps")
	}
}

func TestExecute_BranchPredicateError(t *testing.T) {
	// An ordered comparison against a non-numeric value errors; runBranch must
	// wrap it with the branch step id and propagate it out of Execute.
	comp := &composition.Composition{
		Version: 1, Output: "after",
		Steps: []*composition.Step{
			{ID: "route", Kind: composition.KindBranch,
				Predicate: &composition.Predicate{Path: "${input.name}", Op: "less_than", Value: float64(3)},
				Then:      "after"},
			{ID: "after", Kind: composition.KindPrompt, PromptTask: "a", Input: "${input.name}"},
		},
	}
	fe := newFakeExec(map[string]any{"after": "A"})
	_, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"name": "bob"}))
	if err == nil {
		t.Fatal("expected predicate error to propagate")
	}
	if !strings.Contains(err.Error(), "route") {
		t.Errorf("error should name the branch step, got %v", err)
	}
}

func TestExecute_BranchTakenOutputConsumedDownstream(t *testing.T) {
	// Proves the taken branch's output is available in scope for a downstream step.
	comp := &composition.Composition{
		Version: 1, Output: "join",
		Steps: []*composition.Step{
			{ID: "route", Kind: composition.KindBranch,
				Predicate: &composition.Predicate{Path: "${input.go}", Op: "equals", Value: true},
				Then:      "paper", Else: "general"},
			{ID: "paper", Kind: composition.KindPrompt, PromptTask: "pp", Input: "${input.x}"},
			{ID: "general", Kind: composition.KindPrompt, PromptTask: "gg", Input: "${input.x}"},
			{ID: "join", Kind: composition.KindPrompt, PromptTask: "j",
				DependsOn: []string{"paper", "general"}, Input: "${paper.output.label}"},
		},
	}
	fe := newFakeExec(map[string]any{
		"paper":   map[string]any{"label": "PAPER"},
		"general": map[string]any{"label": "GENERAL"},
		"join":    "J",
	})
	if _, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"go": true, "x": 1})); err != nil {
		t.Fatal(err)
	}
	if string(fe.seen["join"]) != `"PAPER"` {
		t.Errorf("join should consume taken branch output, got %s", fe.seen["join"])
	}
}

func TestExecute_BranchThenEqualsElse(t *testing.T) {
	// Then and Else name the same step -> it must run regardless of predicate value.
	comp := &composition.Composition{
		Version: 1, Output: "merge",
		Steps: []*composition.Step{
			{ID: "route", Kind: composition.KindBranch,
				Predicate: &composition.Predicate{Path: "${input.go}", Op: "equals", Value: true},
				Then:      "merge", Else: "merge"},
			{ID: "merge", Kind: composition.KindPrompt, PromptTask: "m", Input: "${input.x}"},
		},
	}
	fe := newFakeExec(map[string]any{"merge": "M"})
	out, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"go": false, "x": 1}))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"M"` {
		t.Errorf("Then==Else target must run; calls=%v out=%s", fe.calls, out)
	}
}

func TestDecodeOutput_Empty(t *testing.T) {
	v, err := decodeOutput(nil)
	if err != nil || v != nil {
		t.Errorf("empty decodeOutput = (%v, %v), want (nil, nil)", v, err)
	}
}

// branchComp builds: classify -> route(branch) -> paper|general -> join(depends_on both).
func branchComp(predValue string) *composition.Composition {
	return &composition.Composition{
		Version: 1,
		Output:  "join",
		Steps: []*composition.Step{
			{ID: "classify", Kind: composition.KindPrompt, PromptTask: "c", Input: "${input.text}"},
			{ID: "route", Kind: composition.KindBranch,
				Predicate: &composition.Predicate{Path: "${classify.output.type}", Op: "equals", Value: predValue},
				Then:      "paper", Else: "general"},
			{ID: "paper", Kind: composition.KindPrompt, PromptTask: "pp", Input: "${input.text}"},
			{ID: "general", Kind: composition.KindPrompt, PromptTask: "gg", Input: "${input.text}"},
			{ID: "join", Kind: composition.KindPrompt, PromptTask: "j",
				DependsOn: []string{"paper", "general"}, Input: "${input.text}"},
		},
	}
}

func TestExecute_BranchThen(t *testing.T) {
	comp := branchComp("paper") // predicate true -> take 'then' (paper), skip 'general'
	fe := newFakeExec(map[string]any{
		"classify": map[string]any{"type": "paper"},
		"paper":    "P", "general": "G", "join": "J",
	})
	if _, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"text": "x"})); err != nil {
		t.Fatal(err)
	}
	if !reflectDeepEqual(fe.calls, []string{"classify", "paper", "join"}) {
		t.Errorf("calls = %v, want classify,paper,join", fe.calls)
	}
}

func TestExecute_BranchElse(t *testing.T) {
	comp := branchComp("memo") // predicate false -> take 'else' (general), skip 'paper'
	fe := newFakeExec(map[string]any{
		"classify": map[string]any{"type": "paper"},
		"paper":    "P", "general": "G", "join": "J",
	})
	if _, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"text": "x"})); err != nil {
		t.Fatal(err)
	}
	if !reflectDeepEqual(fe.calls, []string{"classify", "general", "join"}) {
		t.Errorf("calls = %v, want classify,general,join", fe.calls)
	}
}

func TestExecute_EmptyElseFallThrough(t *testing.T) {
	// predicate false, empty else -> skip 'then' target, fall through to next step.
	comp := &composition.Composition{
		Version: 1, Output: "after",
		Steps: []*composition.Step{
			{ID: "route", Kind: composition.KindBranch,
				Predicate: &composition.Predicate{Path: "${input.go}", Op: "equals", Value: true},
				Then:      "optional"},
			{ID: "optional", Kind: composition.KindPrompt, PromptTask: "o", Input: "${input.x}"},
			{ID: "after", Kind: composition.KindPrompt, PromptTask: "a", Input: "${input.x}"},
		},
	}
	fe := newFakeExec(map[string]any{"optional": "O", "after": "A"})
	if _, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"go": false, "x": 1})); err != nil {
		t.Fatal(err)
	}
	if !reflectDeepEqual(fe.calls, []string{"after"}) {
		t.Errorf("calls = %v, want after only (optional skipped)", fe.calls)
	}
}

func TestExecute_JoinSkippedWhenAllDepsSkipped(t *testing.T) {
	// A step depending only on a skipped target is itself skipped.
	comp := &composition.Composition{
		Version: 1, Output: "tail",
		Steps: []*composition.Step{
			{ID: "route", Kind: composition.KindBranch,
				Predicate: &composition.Predicate{Path: "${input.go}", Op: "equals", Value: true},
				Then:      "only"},
			{ID: "only", Kind: composition.KindPrompt, PromptTask: "o", Input: "${input.x}"},
			{ID: "dependent", Kind: composition.KindPrompt, PromptTask: "d",
				DependsOn: []string{"only"}, Input: "${input.x}"},
			{ID: "tail", Kind: composition.KindPrompt, PromptTask: "t", Input: "${input.x}"},
		},
	}
	fe := newFakeExec(map[string]any{"only": "O", "dependent": "D", "tail": "T"})
	if _, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"go": false, "x": 1})); err != nil {
		t.Fatal(err)
	}
	if !reflectDeepEqual(fe.calls, []string{"tail"}) {
		t.Errorf("calls = %v, want tail only (only+dependent skipped)", fe.calls)
	}
}

func TestExecute_ParallelErrorPath(t *testing.T) {
	// Both branches fail; errors.Join must surface BOTH branch names.
	comp := &composition.Composition{
		Version: 1, Output: "meta",
		Steps: []*composition.Step{
			{ID: "meta", Kind: composition.KindParallel,
				Branches: []*composition.Step{
					{ID: "a", Kind: composition.KindTool, Tool: "t.a", Args: map[string]any{"c": "${input.x}"}},
					{ID: "b", Kind: composition.KindTool, Tool: "t.b", Args: map[string]any{"c": "${input.x}"}},
				},
				Reduce: &composition.Reducer{Strategy: composition.ReduceBarrier, Into: "m"}},
		},
	}
	fe := newFakeExec(map[string]any{"a": "A", "b": "B"})
	fe.errOn["a"] = 1 // always fails (no retry)
	fe.errOn["b"] = 1
	_, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"x": 1}))
	if err == nil {
		t.Fatal("expected error when branches fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, `branch "a"`) || !strings.Contains(msg, `branch "b"`) {
		t.Errorf("joined error should name both failing branches, got %q", msg)
	}
}

func TestExecute_ParallelAppend(t *testing.T) {
	// append reducer must preserve branch order regardless of goroutine scheduling.
	comp := &composition.Composition{
		Version: 1, Output: "meta",
		Steps: []*composition.Step{
			{ID: "meta", Kind: composition.KindParallel,
				Branches: []*composition.Step{
					{ID: "first", Kind: composition.KindTool, Tool: "t.1", Args: map[string]any{"c": "${input.x}"}},
					{ID: "second", Kind: composition.KindTool, Tool: "t.2", Args: map[string]any{"c": "${input.x}"}},
					{ID: "third", Kind: composition.KindTool, Tool: "t.3", Args: map[string]any{"c": "${input.x}"}},
				},
				Reduce: &composition.Reducer{Strategy: composition.ReduceAppend, Into: "list"}},
		},
	}
	fe := newFakeExec(map[string]any{"first": "F", "second": "S", "third": "T"})
	out, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"x": 1}))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"list": []any{"F", "S", "T"}}
	if !reflectDeepEqual(got, want) {
		t.Errorf("append parallel = %#v, want %#v", got, want)
	}
}

func TestExecute_ParallelNestedBranchRejected(t *testing.T) {
	// A branch of kind=branch nested inside a parallel must be rejected at runtime.
	comp := &composition.Composition{
		Version: 1, Output: "meta",
		Steps: []*composition.Step{
			{ID: "meta", Kind: composition.KindParallel,
				Branches: []*composition.Step{
					{ID: "leaf", Kind: composition.KindTool, Tool: "t.l", Args: map[string]any{"c": "${input.x}"}},
					{ID: "nested", Kind: composition.KindBranch,
						Predicate: &composition.Predicate{Path: "${input.x}", Op: "equals", Value: float64(1)},
						Then:      "leaf"},
				},
				Reduce: &composition.Reducer{Strategy: composition.ReduceBarrier, Into: "m"}},
		},
	}
	fe := newFakeExec(map[string]any{"leaf": "L"})
	_, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"x": 1}))
	if err == nil {
		t.Fatal("expected error for nested branch inside parallel")
	}
	if !strings.Contains(err.Error(), "nested") {
		t.Errorf("error should mention nested, got %v", err)
	}
}

func TestExecute_ParallelBarrier(t *testing.T) {
	comp := &composition.Composition{
		Version: 1, Output: "consume",
		Steps: []*composition.Step{
			{ID: "meta", Kind: composition.KindParallel,
				Branches: []*composition.Step{
					{ID: "structure", Kind: composition.KindTool, Tool: "t.s", Args: map[string]any{"c": "${input.text}"}},
					{ID: "citations", Kind: composition.KindTool, Tool: "t.c", Args: map[string]any{"c": "${input.text}"}},
				},
				Reduce: &composition.Reducer{Strategy: composition.ReduceBarrier, Into: "metadata"}},
			{ID: "consume", Kind: composition.KindPrompt, PromptTask: "p",
				Input: "${meta.output.metadata}"},
		},
	}
	fe := newFakeExec(map[string]any{
		"structure": map[string]any{"sections": float64(3)},
		"citations": map[string]any{"count": float64(12)},
		"consume":   "done",
	})
	out, err := New(fe.exec).Execute(context.Background(), comp, mustJSON(t, map[string]any{"text": "body"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"done"` {
		t.Errorf("output = %s", out)
	}
	var consumeInput map[string]any
	if err := json.Unmarshal(fe.seen["consume"], &consumeInput); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"structure": map[string]any{"sections": float64(3)},
		"citations": map[string]any{"count": float64(12)},
	}
	if !reflectDeepEqual(consumeInput, want) {
		t.Errorf("consume input = %#v, want %#v", consumeInput, want)
	}
	ran := map[string]bool{}
	for _, id := range fe.calls {
		ran[id] = true
	}
	if !ran["structure"] || !ran["citations"] {
		t.Errorf("expected both branches to run, calls = %v", fe.calls)
	}
}
