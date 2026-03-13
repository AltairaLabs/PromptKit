package evals

import (
	"context"
	"fmt"
)

// WrapperTypeAssertion is the eval type name for the assertion wrapper handler.
const WrapperTypeAssertion = "assertion"

// WrapperTypeGuardrail is the eval type name for the guardrail wrapper handler.
const WrapperTypeGuardrail = "guardrail"

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

	passed := h.applyThresholds(result, minScore, maxScore)
	result.Value = passed
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
	minScore := extractOptionalFloat64(params, "min_score")
	innerParams := extractEvalParams(params)

	result, err := handler.Eval(ctx, evalCtx, innerParams)
	if err != nil {
		return nil, err
	}

	// Determine if the guardrail was triggered
	triggered := false
	if minScore != nil && result.Score != nil {
		triggered = *result.Score < *minScore
	} else {
		triggered = result.Score != nil && *result.Score < 1.0
	}

	if result.Details == nil {
		result.Details = make(map[string]any)
	}
	result.Details["triggered"] = triggered
	result.Details["action"] = action
	return result, nil
}
