package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
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
// Params: validator_type string, should_trigger bool (default true),
// direction string (optional).
//
// direction narrows the search to firings the guardrail recorder stamped with
// that side: "input" (judged the user's message, before the call) or "output"
// (judged the assistant response). Omitted — or "both" — matches a firing in
// either direction, which is the historical behavior and stays byte-identical.
// Set it when an input and an output guardrail of the same eval type are both
// declared: without it only the last recorded firing is reachable, so neither
// can be targeted (#1718).
type GuardrailTriggeredHandler struct{}

const (
	// keyDirection is both the param name this handler filters on and the key
	// the guardrail recorder stamps into a ValidationResult's details. They are
	// deliberately the same word: the assertion names the side the same way the
	// guardrail declaration does.
	keyDirection = "direction"
	// keyTriggered is the reported key naming whether the validator fired.
	keyTriggered = "triggered"
)

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
	direction := directionFilter(params)

	// Check PriorResults — includes both earlier evals in this batch and
	// pipeline-level guardrail results seeded by BuildEvalContext.
	if prior := findPriorResult(evalCtx.PriorResults, validatorType, direction); prior != nil {
		triggered := prior.Score != nil && *prior.Score < 1.0
		return h.buildResult(validatorType, direction, triggered, shouldTrigger), nil
	}

	// Not found.
	passed := !shouldTrigger
	subject := describeValidator(validatorType, direction)
	msg := fmt.Sprintf("expected validator %s to run but it did not", subject)
	if passed {
		msg = fmt.Sprintf("validator %s did not run (as expected)", subject)
	}
	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(passed),
		Explanation: msg,
		Value:       triggeredValue(validatorType, direction, false),
	}, nil
}

// buildResult constructs the eval result from triggered/shouldTrigger.
func (h *GuardrailTriggeredHandler) buildResult(
	validatorType, direction string, triggered, shouldTrigger bool,
) *evals.EvalResult {
	passed := shouldTrigger == triggered
	subject := describeValidator(validatorType, direction)
	if !passed {
		action := "fail"
		if !shouldTrigger {
			action = "pass"
		}
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: fmt.Sprintf("expected validator %s to %s but it did not", subject, action),
			Value:       triggeredValue(validatorType, direction, triggered),
		}
	}

	details := map[string]any{"validator": validatorType, keyTriggered: triggered}
	if direction != "" {
		details[keyDirection] = direction
	}
	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Explanation: fmt.Sprintf("validator %s behaved as expected", subject),
		Value:       triggeredValue(validatorType, direction, triggered),
		Details:     details,
	}
}

// triggeredValue builds the reported value map, naming the direction only when
// the assertion asked for one so an unqualified assertion reports exactly what
// it always did.
func triggeredValue(validatorType, direction string, triggered bool) map[string]any {
	value := map[string]any{keyTriggered: triggered, "validator_type": validatorType}
	if direction != "" {
		value[keyDirection] = direction
	}
	return value
}

// describeValidator names the validator in an explanation, including the
// direction when one was requested — a direction-qualified assertion that finds
// nothing must say which side it looked at, otherwise the failure reads as "the
// guardrail never ran" when in fact it ran on the other side.
func describeValidator(validatorType, direction string) string {
	if direction == "" {
		return fmt.Sprintf("%q", validatorType)
	}
	return fmt.Sprintf("%q (direction %q)", validatorType, direction)
}

// findPriorResult searches PriorResults for a matching eval by Type or EvalID,
// newest first.
//
// direction, when non-empty, additionally requires the result's
// Details["direction"] to equal it. That key is stamped by the pipeline's
// guardrail recorder and carried into PriorResults by validationsToPriorResults.
// A result without the key — an ordinary eval, or any prior result from a
// non-guardrail source — never satisfies a direction-qualified search: reporting
// it would claim a side the recorder never asserted.
func findPriorResult(results []evals.EvalResult, validatorType, direction string) *evals.EvalResult {
	for i := len(results) - 1; i >= 0; i-- {
		if results[i].Type != validatorType && results[i].EvalID != validatorType {
			continue
		}
		if direction != "" && !hasDirection(results[i].Details, direction) {
			continue
		}
		return &results[i]
	}
	return nil
}

// hasDirection reports whether a prior result was recorded for the given side.
func hasDirection(details map[string]any, direction string) bool {
	d, _ := details[keyDirection].(string)
	return d == direction
}

// directionFilter reads the optional 'direction' param.
//
// Returns "" — match any direction, the pre-#1718 behavior — when the param is
// absent, empty, not a string, or "both". "both" is accepted because that is a
// legal direction on the guardrail *declaration*, but a firing is always
// recorded as one side or the other, so as a filter it can only mean "either".
func directionFilter(params map[string]any) string {
	d, _ := params[keyDirection].(string)
	if d == hooks.DirectionBoth {
		return ""
	}
	return d
}

func extractValidatorType(params map[string]any) string {
	if v, _ := params["validator_type"].(string); v != "" {
		return v
	}
	v, _ := params["validator"].(string)
	return v
}

// ValidateParams checks that the required 'validator_type' (or alias
// 'validator') param is set to a non-empty string, and that 'direction' — if
// present — names a side the guardrail recorder can actually stamp.
//
// A bad direction is an error here rather than a warning-and-fallback (which is
// what the guardrail factory does with the same param) because the failure modes
// are opposite: dropping a guardrail leaves a conversation unprotected, whereas
// an assertion with a misspelled direction silently matches nothing and reports
// "the guardrail never fired" — a green test that checks nothing.
func (h *GuardrailTriggeredHandler) ValidateParams(params map[string]any) error {
	if extractValidatorType(params) == "" {
		return fmt.Errorf(
			"%s requires a non-empty 'validator_type' (or 'validator') string param",
			h.Type())
	}
	raw, present := params[keyDirection]
	if !present || raw == nil {
		return nil
	}
	d, ok := raw.(string)
	if !ok {
		return fmt.Errorf("%s: 'direction' must be a string, got %T", h.Type(), raw)
	}
	switch d {
	case "", hooks.DirectionInput, hooks.DirectionOutput, hooks.DirectionBoth:
		return nil
	default:
		return fmt.Errorf(
			"%s: unknown 'direction' %q (want %q, %q or %q)",
			h.Type(), d, hooks.DirectionInput, hooks.DirectionOutput, hooks.DirectionBoth)
	}
}
