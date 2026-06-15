package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// Composition assertion handlers (RFC 0010 Arena testability). They read the
// per-step observability injected into EvalContext.Metadata by the Arena
// CompositionMetadataProvider:
//   - composition_step_outputs   map of step id -> step output (raw JSON)
//   - composition_branch_taken   map of branch step id -> taken target
//   - composition_parallel_status map of parallel step id -> "complete"
// and the composition's final output via EvalContext.CurrentOutput.
//
// Per the package convention these are pure primitives: they emit a raw Score
// (and MetricValue) and put detail in Details; threshold judgment lives on the
// `type: assertion` wrapper, so min_score/max_score are rejected here.

const (
	mdStepOutputs    = "composition_step_outputs"
	mdBranchTaken    = "composition_branch_taken"
	mdParallelStatus = "composition_parallel_status"
	parallelComplete = "complete"

	paramStep     = "step"
	paramContains = "contains"
	paramEquals   = "equals"
	detailOutput  = "output"
)

// CompositionStepOutputHandler asserts a named step's output contains/equals a value.
// Params: step (string, required); one of contains|equals (string).
type CompositionStepOutputHandler struct{}

// Type returns the eval type identifier.
func (h *CompositionStepOutputHandler) Type() string { return "composition_step_output" }

// Eval matches the named step's recorded output against contains/equals.
func (h *CompositionStepOutputHandler) Eval(
	_ context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	if msg := rejectThresholdParams(params); msg != "" {
		return errorResult(h.Type(), msg), nil
	}
	step, ok := params[paramStep].(string)
	if !ok || step == "" {
		return errorResult(h.Type(), "missing required param: step"), nil
	}
	outputs := compositionStepOutputs(evalCtx.Metadata)
	got, found := outputs[step]
	if !found {
		return scored(h.Type(), false,
			fmt.Sprintf("step %q produced no recorded output", step),
			map[string]any{paramStep: step}), nil
	}
	matched, mode, want := matchStringParam(got, params)
	return scored(h.Type(), matched,
		fmt.Sprintf("step %q output %s %q", step, mode, want),
		map[string]any{paramStep: step, detailOutput: got}), nil
}

// CompositionBranchTakenHandler asserts a branch step took the expected target.
// Params: branch (string, required), expected (string, required).
type CompositionBranchTakenHandler struct{}

// Type returns the eval type identifier.
func (h *CompositionBranchTakenHandler) Type() string { return "composition_branch_taken" }

// Eval checks the recorded branch target equals expected.
func (h *CompositionBranchTakenHandler) Eval(
	_ context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	if msg := rejectThresholdParams(params); msg != "" {
		return errorResult(h.Type(), msg), nil
	}
	branch, ok := params["branch"].(string)
	if !ok || branch == "" {
		return errorResult(h.Type(), "missing required param: branch"), nil
	}
	expected, ok := params[keyExpected].(string)
	if !ok || expected == "" {
		return errorResult(h.Type(), "missing required param: expected"), nil
	}
	taken := compositionStringMap(evalCtx.Metadata, mdBranchTaken)
	got := taken[branch]
	return scored(h.Type(), got == expected,
		fmt.Sprintf("branch %q took %q, expected %q", branch, got, expected),
		map[string]any{"branch": branch, "taken": got, keyExpected: expected}), nil
}

// CompositionParallelCompleteHandler asserts a parallel step completed.
// Params: parallel (string, required).
type CompositionParallelCompleteHandler struct{}

// Type returns the eval type identifier.
func (h *CompositionParallelCompleteHandler) Type() string { return "composition_parallel_complete" }

// Eval checks the recorded parallel status is "complete".
func (h *CompositionParallelCompleteHandler) Eval(
	_ context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	if msg := rejectThresholdParams(params); msg != "" {
		return errorResult(h.Type(), msg), nil
	}
	parallel, ok := params["parallel"].(string)
	if !ok || parallel == "" {
		return errorResult(h.Type(), "missing required param: parallel"), nil
	}
	status := compositionStringMap(evalCtx.Metadata, mdParallelStatus)
	got := status[parallel]
	return scored(h.Type(), got == parallelComplete,
		fmt.Sprintf("parallel %q status %q", parallel, got),
		map[string]any{"parallel": parallel, "status": got}), nil
}

// CompositionOutputHandler asserts the composition's final output contains/equals a value.
// Params: one of contains|equals (string).
type CompositionOutputHandler struct{}

// Type returns the eval type identifier.
func (h *CompositionOutputHandler) Type() string { return "composition_output" }

// Eval matches the composition's final output (CurrentOutput) against contains/equals.
func (h *CompositionOutputHandler) Eval(
	_ context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	if msg := rejectThresholdParams(params); msg != "" {
		return errorResult(h.Type(), msg), nil
	}
	matched, mode, want := matchStringParam(evalCtx.CurrentOutput, params)
	return scored(h.Type(), matched,
		fmt.Sprintf("composition output %s %q", mode, want),
		map[string]any{"output": evalCtx.CurrentOutput}), nil
}

// --- helpers ---

// scored builds a pure-primitive EvalResult: raw Score + MetricValue + Details,
// never Value (the assertion wrapper sets Value).
func scored(handlerType string, pass bool, explanation string, details map[string]any) *evals.EvalResult {
	s := boolScore(pass)
	return &evals.EvalResult{
		Type:        handlerType,
		Score:       s,
		MetricValue: s,
		Explanation: explanation,
		Details:     details,
	}
}

// matchStringParam matches haystack against a contains or equals param.
// Returns (matched, mode, wanted). With neither param, matches on non-empty output.
func matchStringParam(haystack string, params map[string]any) (matched bool, mode, want string) {
	if eq, ok := params[paramEquals].(string); ok {
		return haystack == eq, paramEquals, eq
	}
	if c, ok := params[paramContains].(string); ok {
		return strings.Contains(haystack, c), paramContains, c
	}
	return haystack != "", "is non-empty", ""
}

// compositionStepOutputs normalizes composition_step_outputs metadata (which may
// be map[string]json.RawMessage, map[string]string, or map[string]any) to strings.
func compositionStepOutputs(meta map[string]any) map[string]string {
	out := map[string]string{}
	v, ok := meta[mdStepOutputs]
	if !ok {
		return out
	}
	switch m := v.(type) {
	case map[string]json.RawMessage:
		for k, raw := range m {
			out[k] = string(raw)
		}
	case map[string]string:
		for k, s := range m {
			out[k] = s
		}
	case map[string]any:
		for k, val := range m {
			if raw, ok := val.(json.RawMessage); ok {
				out[k] = string(raw)
			} else {
				out[k] = fmt.Sprint(val)
			}
		}
	}
	return out
}

// compositionStringMap normalizes a string-valued composition metadata map.
func compositionStringMap(meta map[string]any, key string) map[string]string {
	out := map[string]string{}
	v, ok := meta[key]
	if !ok {
		return out
	}
	switch m := v.(type) {
	case map[string]string:
		for k, s := range m {
			out[k] = s
		}
	case map[string]any:
		for k, val := range m {
			out[k] = fmt.Sprint(val)
		}
	}
	return out
}
