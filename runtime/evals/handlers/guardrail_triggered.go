package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// GuardrailTriggeredHandler checks if a specific eval or guardrail
// triggered (or didn't trigger) as expected.
//
// It searches EvalContext.PriorResults for an eval whose Type or
// EvalID matches the validator_type parameter. Pipeline-level guardrail
// results from message.Validations are automatically seeded into
// PriorResults by BuildEvalContext, so all guardrail outcomes are
// available through a single lookup path.
//
// Params: validator_type string, should_trigger bool (default true).
type GuardrailTriggeredHandler struct{}

// Type returns the eval type identifier.
func (h *GuardrailTriggeredHandler) Type() string { return "guardrail_triggered" }

// Eval checks PriorResults for a matching guardrail or eval outcome.
func (h *GuardrailTriggeredHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	validatorType := extractValidatorType(params)
	if validatorType == "" {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: "validator_type parameter required",
		}, nil
	}

	shouldTrigger := true
	if v, ok := params["should_trigger"].(bool); ok {
		shouldTrigger = v
	}

	// Check PriorResults — includes both earlier evals in this batch and
	// pipeline-level guardrail results seeded by BuildEvalContext.
	if prior := findPriorResult(evalCtx.PriorResults, validatorType); prior != nil {
		triggered := !prior.IsPassed()
		return h.buildResult(validatorType, triggered, shouldTrigger), nil
	}

	// Not found.
	passed := !shouldTrigger
	msg := fmt.Sprintf("expected validator %q to run but it did not", validatorType)
	if passed {
		msg = fmt.Sprintf("validator %q did not run (as expected)", validatorType)
	}
	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(passed),
		Explanation: msg,
		Value:       map[string]any{"triggered": false, "validator_type": validatorType},
	}, nil
}

// buildResult constructs the eval result from triggered/shouldTrigger.
func (h *GuardrailTriggeredHandler) buildResult(
	validatorType string, triggered, shouldTrigger bool,
) *evals.EvalResult {
	passed := shouldTrigger == triggered
	if !passed {
		action := "fail"
		if !shouldTrigger {
			action = "pass"
		}
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: fmt.Sprintf("expected validator %q to %s but it did not", validatorType, action),
			Value:       map[string]any{"triggered": triggered, "validator_type": validatorType},
		}
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Explanation: fmt.Sprintf("validator %q behaved as expected", validatorType),
		Value:       map[string]any{"triggered": triggered, "validator_type": validatorType},
		Details:     map[string]any{"validator": validatorType, "triggered": triggered},
	}
}

// findPriorResult searches PriorResults for a matching eval by Type or EvalID.
func findPriorResult(results []evals.EvalResult, validatorType string) *evals.EvalResult {
	for i := len(results) - 1; i >= 0; i-- {
		if results[i].Type == validatorType || results[i].EvalID == validatorType {
			return &results[i]
		}
	}
	return nil
}

func extractValidatorType(params map[string]any) string {
	if v, _ := params["validator_type"].(string); v != "" {
		return v
	}
	v, _ := params["validator"].(string)
	return v
}
