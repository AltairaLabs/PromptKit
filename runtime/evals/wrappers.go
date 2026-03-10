package evals

import "context"

// wrapperParams are the param keys consumed by wrapper handlers.
// These are extracted from params before delegating to the inner eval.
var wrapperParams = map[string]bool{
	"min_score": true, "max_score": true,
	"action": true, "direction": true,
}

// extractInnerParams returns params with wrapper-specific keys removed.
func extractInnerParams(params map[string]any) map[string]any {
	inner := make(map[string]any, len(params))
	for k, v := range params {
		if !wrapperParams[k] {
			inner[k] = v
		}
	}
	return inner
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

// AssertionEvalHandler wraps an inner eval and applies pass/fail judgment
// based on score thresholds from params (min_score, max_score) or from
// a Threshold injected by the EvalRunner.
type AssertionEvalHandler struct {
	Inner     EvalTypeHandler
	EvalType  string     // the inner eval type name
	Threshold *Threshold // optional: injected by EvalRunner from EvalDef.Threshold
}

// Type returns the wrapper type identifier.
func (h *AssertionEvalHandler) Type() string { return "assertion:" + h.EvalType }

// Eval executes the inner eval and applies threshold-based pass/fail.
func (h *AssertionEvalHandler) Eval(
	ctx context.Context, evalCtx *EvalContext, params map[string]any,
) (*EvalResult, error) {
	// Resolve thresholds: Threshold field (from runner) takes precedence over params.
	minScore, maxScore := h.resolveThresholds(params)
	innerParams := extractInnerParams(params)

	result, err := h.Inner.Eval(ctx, evalCtx, innerParams)
	if err != nil {
		return nil, err
	}

	passed := true
	if result.Score != nil {
		if minScore != nil {
			passed = passed && *result.Score >= *minScore
		}
		if maxScore != nil {
			passed = passed && *result.Score <= *maxScore
		}
	}

	// When no explicit thresholds, derive from score
	if minScore == nil && maxScore == nil {
		passed = result.IsPassed()
	}

	result.Passed = passed
	if result.Details == nil {
		result.Details = make(map[string]any)
	}
	result.Details["passed"] = passed
	return result, nil
}

// applyThresholds applies threshold-based pass/fail to an already-computed result.
// Used by EvalRunner to apply EvalDef.Threshold without re-running the handler.
func (h *AssertionEvalHandler) applyThresholds(result *EvalResult) {
	if h.Threshold == nil {
		return
	}
	passed := true
	if result.Score != nil {
		if h.Threshold.MinScore != nil {
			passed = passed && *result.Score >= *h.Threshold.MinScore
		}
		if h.Threshold.MaxScore != nil {
			passed = passed && *result.Score <= *h.Threshold.MaxScore
		}
	}
	if h.Threshold.MinScore == nil && h.Threshold.MaxScore == nil {
		passed = result.IsPassed()
	}
	result.Passed = passed
	if result.Details == nil {
		result.Details = make(map[string]any)
	}
	result.Details["passed"] = passed
}

// resolveThresholds returns min/max score from the Threshold field or params.
func (h *AssertionEvalHandler) resolveThresholds(
	params map[string]any,
) (minScore, maxScore *float64) {
	if h.Threshold != nil {
		return h.Threshold.MinScore, h.Threshold.MaxScore
	}
	return extractOptionalFloat64(params, "min_score"),
		extractOptionalFloat64(params, "max_score")
}

// GuardrailEvalHandler wraps an inner eval and applies guardrail judgment.
// It checks if the eval result indicates a violation (score < 1.0 by default).
type GuardrailEvalHandler struct {
	Inner    EvalTypeHandler
	EvalType string
}

// Type returns the wrapper type identifier.
func (h *GuardrailEvalHandler) Type() string { return "guardrail:" + h.EvalType }

// Eval executes the inner eval and determines if the guardrail was triggered.
func (h *GuardrailEvalHandler) Eval(
	ctx context.Context, evalCtx *EvalContext, params map[string]any,
) (*EvalResult, error) {
	action := extractParamString(params, "action", "block")
	minScore := extractOptionalFloat64(params, "min_score")
	innerParams := extractInnerParams(params)

	result, err := h.Inner.Eval(ctx, evalCtx, innerParams)
	if err != nil {
		return nil, err
	}

	// Determine if the guardrail was triggered
	triggered := false
	if minScore != nil && result.Score != nil {
		triggered = *result.Score < *minScore
	} else {
		// Default: triggered if the inner eval failed (score < 1.0)
		triggered = !result.IsPassed()
	}

	if result.Details == nil {
		result.Details = make(map[string]any)
	}
	result.Details["triggered"] = triggered
	result.Details["action"] = action
	result.Passed = !triggered
	return result, nil
}
