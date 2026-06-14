package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
)

// fakeExec returns canned output per step id and records the resolved input it saw.
type fakeExec struct {
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
	f.calls = append(f.calls, step.ID)
	f.seen[step.ID] = input
	f.attempt[step.ID]++
	if fails := f.errOn[step.ID]; f.attempt[step.ID] <= fails {
		return nil, context.DeadlineExceeded
	}
	out := f.outputs[step.ID]
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

func TestExecute_BranchKind_Stub(t *testing.T) {
	// runBranch is a stub that does nothing; the branch step produces no output.
	// Output must be set explicitly — using a preceding leaf step here.
	comp := &composition.Composition{
		Version: 1,
		Output:  "before",
		Steps: []*composition.Step{
			{ID: "before", Kind: composition.KindPrompt, Input: "x"},
			{ID: "br", Kind: composition.KindBranch},
		},
	}
	fe := newFakeExec(map[string]any{"before": "done"})
	out, err := New(fe.exec).Execute(context.Background(), comp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `"done"` {
		t.Errorf("branch stub output = %s", out)
	}
}

func TestExecute_ParallelKind_ErrorStub(t *testing.T) {
	comp := &composition.Composition{
		Version: 1,
		Steps:   []*composition.Step{{ID: "p", Kind: composition.KindParallel}},
	}
	fe := newFakeExec(nil)
	_, err := New(fe.exec).Execute(context.Background(), comp, nil)
	if err == nil {
		t.Fatal("expected error from parallel stub")
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

func TestDecodeOutput_Empty(t *testing.T) {
	v, err := decodeOutput(nil)
	if err != nil || v != nil {
		t.Errorf("empty decodeOutput = (%v, %v), want (nil, nil)", v, err)
	}
}
