package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func scoreVal(t *testing.T, r *evals.EvalResult) float64 {
	t.Helper()
	if r.Score == nil {
		t.Fatalf("nil score (err=%q)", r.Error)
	}
	return *r.Score
}

func TestCompositionStepOutput(t *testing.T) {
	ctx := &evals.EvalContext{Metadata: map[string]any{
		mdStepOutputs: map[string]json.RawMessage{"classify": json.RawMessage(`{"type":"paper"}`)},
	}}
	h := &CompositionStepOutputHandler{}

	got, err := h.Eval(context.Background(), ctx, map[string]any{"step": "classify", "contains": "paper"})
	if err != nil || scoreVal(t, got) != 1.0 {
		t.Fatalf("contains match: score=%v err=%v", got.Score, err)
	}
	got, _ = h.Eval(context.Background(), ctx, map[string]any{"step": "classify", "contains": "memo"})
	if scoreVal(t, got) != 0.0 {
		t.Errorf("non-match should score 0")
	}
	got, _ = h.Eval(context.Background(), ctx, map[string]any{"step": "missing", "contains": "x"})
	if scoreVal(t, got) != 0.0 {
		t.Errorf("missing step should score 0")
	}
	// missing required param + threshold rejection both error
	if got, _ = h.Eval(context.Background(), ctx, map[string]any{"contains": "x"}); got.Error == "" {
		t.Error("missing step param should error")
	}
	if got, _ = h.Eval(context.Background(), ctx, map[string]any{"step": "classify", "min_score": 0.5}); got.Error == "" {
		t.Error("min_score must be rejected")
	}
}

func TestCompositionBranchTaken(t *testing.T) {
	ctx := &evals.EvalContext{Metadata: map[string]any{
		mdBranchTaken: map[string]string{"route": "paper"},
	}}
	h := &CompositionBranchTakenHandler{}
	if got, _ := h.Eval(context.Background(), ctx, map[string]any{"branch": "route", "expected": "paper"}); scoreVal(t, got) != 1.0 {
		t.Error("matching branch should score 1")
	}
	if got, _ := h.Eval(context.Background(), ctx, map[string]any{"branch": "route", "expected": "general"}); scoreVal(t, got) != 0.0 {
		t.Error("mismatched branch should score 0")
	}
	if got, _ := h.Eval(context.Background(), ctx, map[string]any{"branch": "route"}); got.Error == "" {
		t.Error("missing expected param should error")
	}
}

func TestCompositionParallelComplete(t *testing.T) {
	ctx := &evals.EvalContext{Metadata: map[string]any{
		mdParallelStatus: map[string]string{"meta": "complete"},
	}}
	h := &CompositionParallelCompleteHandler{}
	if got, _ := h.Eval(context.Background(), ctx, map[string]any{"parallel": "meta"}); scoreVal(t, got) != 1.0 {
		t.Error("complete parallel should score 1")
	}
	if got, _ := h.Eval(context.Background(), ctx, map[string]any{"parallel": "absent"}); scoreVal(t, got) != 0.0 {
		t.Error("unknown parallel should score 0")
	}
}

func TestCompositionOutput(t *testing.T) {
	ctx := &evals.EvalContext{CurrentOutput: `{"summary":"done"}`}
	h := &CompositionOutputHandler{}
	if got, _ := h.Eval(context.Background(), ctx, map[string]any{"contains": "done"}); scoreVal(t, got) != 1.0 {
		t.Error("contains match should score 1")
	}
	if got, _ := h.Eval(context.Background(), ctx, map[string]any{"equals": `{"summary":"done"}`}); scoreVal(t, got) != 1.0 {
		t.Error("equals match should score 1")
	}
	if got, _ := h.Eval(context.Background(), ctx, map[string]any{"contains": "missing"}); scoreVal(t, got) != 0.0 {
		t.Error("non-match should score 0")
	}
	if got, _ := h.Eval(context.Background(), ctx, map[string]any{"max_score": 0.9}); got.Error == "" {
		t.Error("max_score must be rejected")
	}
	// no contains/equals → matches on non-empty output
	if got, _ := h.Eval(context.Background(), ctx, map[string]any{}); scoreVal(t, got) != 1.0 {
		t.Error("non-empty output with no matcher should score 1")
	}
	empty := &evals.EvalContext{CurrentOutput: ""}
	if got, _ := h.Eval(context.Background(), empty, map[string]any{}); scoreVal(t, got) != 0.0 {
		t.Error("empty output with no matcher should score 0")
	}
}

// TestCompositionMetadata_AltTypes covers the tolerant accessors for
// composition metadata supplied as map[string]string / map[string]any
// (not just the concrete recorder types).
func TestCompositionMetadata_AltTypes(t *testing.T) {
	// step outputs as map[string]string
	stepCtxStr := &evals.EvalContext{Metadata: map[string]any{
		mdStepOutputs: map[string]string{"classify": "paper"},
	}}
	if got, _ := (&CompositionStepOutputHandler{}).Eval(context.Background(), stepCtxStr,
		map[string]any{"step": "classify", "contains": "paper"}); scoreVal(t, got) != 1.0 {
		t.Error("map[string]string step outputs should resolve")
	}
	// step outputs as map[string]any
	stepCtxAny := &evals.EvalContext{Metadata: map[string]any{
		mdStepOutputs: map[string]any{"classify": "paper"},
	}}
	if got, _ := (&CompositionStepOutputHandler{}).Eval(context.Background(), stepCtxAny,
		map[string]any{"step": "classify", "equals": "paper"}); scoreVal(t, got) != 1.0 {
		t.Error("map[string]any step outputs should resolve")
	}
	// branch-taken as map[string]any
	brCtxAny := &evals.EvalContext{Metadata: map[string]any{
		mdBranchTaken: map[string]any{"route": "paper"},
	}}
	if got, _ := (&CompositionBranchTakenHandler{}).Eval(context.Background(), brCtxAny,
		map[string]any{"branch": "route", "expected": "paper"}); scoreVal(t, got) != 1.0 {
		t.Error("map[string]any branch-taken should resolve")
	}
}
