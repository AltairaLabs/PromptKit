package evals

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
)

// WrapperTypeAssertion is the eval type name for the assertion wrapper handler.
const WrapperTypeAssertion = "assertion"

// WrapperTypeGuardrail is the eval type name for the guardrail wrapper handler.
const WrapperTypeGuardrail = "guardrail"

// scoreThresholdParamKeys are the wrapper-level param names that convert an
// inner eval's score into a verdict. They are deliberately *not* valid on an
// eval handler — handlers call rejectThresholdParams to refuse them — so a
// wrapper must consume them and strip them before invoking its inner handler.
var scoreThresholdParamKeys = []string{"min_score", "max_score"}

// ScoreThresholds turns an eval score into a verdict, and is the single
// implementation of that decision.
//
// Every role that wraps an eval needs it: the "assertion" and "guardrail" eval
// types here, and the pipeline's guardrail hook adapter. The adapter used to
// carry its own copy hardcoded at `< 1.0` in three places, which ignored
// min_score entirely and so made continuous-score handlers (cost_budget,
// latency_budget) untunable and effectively unusable as guardrails (#1707).
type ScoreThresholds struct {
	// Min fails a score below it. Max fails a score above it. Both optional;
	// with neither set, Triggered requires a perfect 1.0.
	Min *float64
	Max *float64
}

// ExtractScoreThresholds reads min_score/max_score from wrapper params.
func ExtractScoreThresholds(params map[string]any) ScoreThresholds {
	return ScoreThresholds{
		Min: extractOptionalFloat64(params, "min_score"),
		Max: extractOptionalFloat64(params, "max_score"),
	}
}

// StripScoreThresholds returns params without the threshold keys, so they never
// reach an inner eval handler. Handlers that call rejectThresholdParams return
// an error result scoring 0 when they see one, which a guardrail reads as
// "block" — so forwarding a threshold turns the guardrail into an always-block
// rather than merely ignoring the param.
//
// The input map is never mutated; callers may share it across turns.
func StripScoreThresholds(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	present := false
	for _, key := range scoreThresholdParamKeys {
		if _, ok := params[key]; ok {
			present = true
			break
		}
	}
	if !present {
		return params
	}
	stripped := make(map[string]any, len(params))
	for k, v := range params {
		stripped[k] = v
	}
	for _, key := range scoreThresholdParamKeys {
		delete(stripped, key)
	}
	return stripped
}

// Triggered reports whether a result trips a guardrail under these thresholds.
//
// A nil result or nil score means the handler could not judge, and a safety
// mechanism that cannot judge blocks: this is fail-closed, matching what the
// pipeline's guardrail hook has always done. Note the assertion role makes the
// opposite choice — see AssertionEvalHandler.applyThresholds — because failing
// a *test* over a handler that declined to score would be noise, whereas
// allowing unjudged content through a *guardrail* is a hole.
func (t ScoreThresholds) Triggered(result *EvalResult) bool {
	if result == nil || result.Score == nil {
		return true
	}
	minScore := t.Min
	if minScore == nil && t.Max == nil {
		perfect := 1.0
		minScore = &perfect
	}
	if minScore != nil && *result.Score < *minScore {
		return true
	}
	if t.Max != nil && *result.Score > *t.Max {
		return true
	}
	return false
}

// extractOptionalFloat64 extracts a float64 from params, handling int->float64.
func extractOptionalFloat64(params map[string]any, key string) *float64 {
	v, ok := params[key]
	if !ok {
		return nil
	}
	switch n := v.(type) {
	case float64:
		return &n
	case int:
		f := float64(n)
		return &f
	case int64:
		f := float64(n)
		return &f
	default:
		return nil
	}
}

// extractParamString extracts a string param with a default.
func extractParamString(params map[string]any, key, defaultVal string) string {
	if v, ok := params[key].(string); ok {
		return v
	}
	return defaultVal
}

// extractEvalParams extracts the nested eval_params map from wrapper params.
// Returns nil if eval_params is not set or not a map.
func extractEvalParams(params map[string]any) map[string]any {
	if ep, ok := params["eval_params"].(map[string]any); ok {
		return ep
	}
	return nil
}

// AssertionEvalHandler is a registered eval type ("assertion") that wraps an
// inner eval and applies pass/fail judgment based on score thresholds.
//
// Params structure:
//
//	{
//	  "eval_type":  "llm_judge",           // inner eval type (required)
//	  "eval_params": { "criteria": "..." }, // params for inner eval
//	  "min_score":  0.8,                    // assertion threshold (optional)
//	  "max_score":  1.0                     // assertion threshold (optional)
//	}
type AssertionEvalHandler struct {
	registry *EvalTypeRegistry
}

// Type returns the registered eval type name.
func (h *AssertionEvalHandler) Type() string { return WrapperTypeAssertion }

// Eval resolves the inner handler from the registry, executes it, and applies
// threshold-based pass/fail judgment.
func (h *AssertionEvalHandler) Eval(
	ctx context.Context, evalCtx *EvalContext, params map[string]any,
) (*EvalResult, error) {
	evalType, ok := params["eval_type"].(string)
	if !ok || evalType == "" {
		return nil, fmt.Errorf("assertion handler requires eval_type param")
	}

	handler, err := h.registry.Get(evalType)
	if err != nil {
		return nil, fmt.Errorf("assertion inner eval: %w", err)
	}

	minScore := extractOptionalFloat64(params, "min_score")
	maxScore := extractOptionalFloat64(params, "max_score")
	innerParams := extractEvalParams(params)

	result, err := handler.Eval(ctx, evalCtx, innerParams)
	if err != nil {
		return nil, err
	}

	// State the role and the boolean. Do NOT touch Value: the assertion's
	// judgement is its own field, and the inner eval's output — the rubric
	// breakdown, the judge's structured reasoning — is the thing a consumer
	// most wants and the thing this used to overwrite (#1875).
	passed := h.applyThresholds(result, minScore, maxScore)
	result.Kind = events.EvalKindAssertion
	result.Passed = &passed
	return result, nil
}

// applyThresholds determines pass/fail from score thresholds.
// When no explicit thresholds are configured, defaults to min_score=1.0
// (inner eval must fully pass).
func (h *AssertionEvalHandler) applyThresholds(
	result *EvalResult, minScore, maxScore *float64,
) bool {
	if minScore == nil && maxScore == nil {
		// Default: inner eval must score 1.0 to pass the assertion.
		defaultMin := 1.0
		minScore = &defaultMin
	}
	if result.Score == nil {
		return true
	}
	passed := true
	if minScore != nil {
		passed = passed && *result.Score >= *minScore
	}
	if maxScore != nil {
		passed = passed && *result.Score <= *maxScore
	}
	return passed
}

// GuardrailEvalHandler is a registered eval type ("guardrail") that wraps an
// inner eval and determines whether the guardrail was triggered.
//
// Params structure:
//
//	{
//	  "eval_type":   "content_excludes",     // inner eval type (required)
//	  "eval_params": { "patterns": ["..."] }, // params for inner eval
//	  "action":      "block",                 // guardrail action (optional, default: "block")
//	  "min_score":   0.8                      // trigger threshold (optional)
//	}
type GuardrailEvalHandler struct {
	registry *EvalTypeRegistry
}

// Type returns the registered eval type name.
func (h *GuardrailEvalHandler) Type() string { return WrapperTypeGuardrail }

// Eval resolves the inner handler from the registry, executes it, and determines
// whether the guardrail was triggered.
func (h *GuardrailEvalHandler) Eval(
	ctx context.Context, evalCtx *EvalContext, params map[string]any,
) (*EvalResult, error) {
	evalType, ok := params["eval_type"].(string)
	if !ok || evalType == "" {
		return nil, fmt.Errorf("guardrail handler requires eval_type param")
	}

	handler, err := h.registry.Get(evalType)
	if err != nil {
		return nil, fmt.Errorf("guardrail inner eval: %w", err)
	}

	action := extractParamString(params, "action", "block")
	thresholds := ExtractScoreThresholds(params)
	innerParams := extractEvalParams(params)

	result, err := handler.Eval(ctx, evalCtx, innerParams)
	if err != nil {
		return nil, err
	}

	// Shared with the pipeline's guardrail hook adapter so the two cannot drift
	// again. This converges one difference: a nil score now counts as triggered
	// (fail-closed) where this path previously read it as not-triggered. A
	// guardrail whose handler could not produce a score has not cleared
	// anything, and no test pinned the old behavior.
	triggered := thresholds.Triggered(result)

	// A guardrail coerces to a boolean just as an assertion does, so it states
	// one the same way. It had none before: its outcome reached a consumer only
	// as Details["triggered"], a convention every consumer had to know and one
	// the event schema said nothing about (#1874). Passed is the inverse of
	// triggered — a guardrail that fired did not pass.
	//
	// Details keeps both keys. They are what existing consumers read, and
	// "action" is not expressible as a boolean anyway.
	passed := !triggered
	result.Kind = events.EvalKindGuardrail
	result.Passed = &passed

	if result.Details == nil {
		result.Details = make(map[string]any)
	}
	result.Details["triggered"] = triggered
	result.Details["action"] = action
	return result, nil
}
