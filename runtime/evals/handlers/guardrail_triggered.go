package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// GuardrailTriggeredHandler checks if a specific guardrail validator
// triggered (or didn't trigger) as expected.
// Params: validator_type string, should_trigger bool (default true).
//
// Deprecated: guardrail_triggered relies on pipeline-level Validations attached
// to messages. Prefer using assertion or guardrail wrappers around eval handlers
// directly. This handler will be removed in a future version.
type GuardrailTriggeredHandler struct{}

// Type returns the eval type identifier.
func (h *GuardrailTriggeredHandler) Type() string { return "guardrail_triggered" }

// Eval checks guardrail validation results from the last assistant message.
func (h *GuardrailTriggeredHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	validatorType := extractValidatorType(params)
	if validatorType == "" {
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Explanation: "validator_type parameter required",
		}, nil
	}

	shouldTrigger := true
	if v, ok := params["should_trigger"].(bool); ok {
		shouldTrigger = v
	}

	found := findValidationResult(evalCtx.Messages, validatorType)
	return h.evaluateResult(found, validatorType, shouldTrigger), nil
}

func extractValidatorType(params map[string]any) string {
	if v, _ := params["validator_type"].(string); v != "" {
		return v
	}
	v, _ := params["validator"].(string)
	return v
}

func findValidationResult(messages []types.Message, validatorType string) *types.ValidationResult {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != roleAssistant {
			continue
		}
		for j := range messages[i].Validations {
			if guardrailTypeMatches(messages[i].Validations[j].ValidatorType, validatorType) {
				return &messages[i].Validations[j]
			}
		}
		break
	}
	return nil
}

func (h *GuardrailTriggeredHandler) evaluateResult(
	found *types.ValidationResult, validatorType string, shouldTrigger bool,
) *evals.EvalResult {
	if found == nil {
		passed := !shouldTrigger
		msg := fmt.Sprintf("expected validator %q to run but it did not", validatorType)
		if passed {
			msg = fmt.Sprintf("validator %q did not run (as expected)", validatorType)
		}
		return &evals.EvalResult{
			Type: h.Type(), Passed: passed,
			Score:       boolScore(passed),
			Explanation: msg,
			Value:       map[string]any{"triggered": false, "validator_type": validatorType},
		}
	}

	triggered := !found.Passed
	passed := shouldTrigger == triggered
	if !passed {
		action := "fail"
		if !shouldTrigger {
			action = "pass"
		}
		return &evals.EvalResult{
			Type: h.Type(), Passed: false,
			Score:       boolScore(false),
			Explanation: fmt.Sprintf("expected validator %q to %s but it did not", validatorType, action),
			Value:       map[string]any{"triggered": triggered, "validator_type": validatorType},
		}
	}

	return &evals.EvalResult{
		Type: h.Type(), Passed: true,
		Score:       boolScore(true),
		Explanation: fmt.Sprintf("validator %q behaved as expected", validatorType),
		Value:       map[string]any{"triggered": triggered, "validator_type": validatorType},
		Details:     map[string]any{"validator": validatorType, "triggered": triggered},
	}
}

// guardrailTypeMatches checks validator type with friendly name matching.
func guardrailTypeMatches(validatorType, expectedName string) bool {
	if validatorType == expectedName {
		return true
	}
	friendlyName := snakeToPascal(expectedName)
	if friendlyName == "" {
		return false
	}
	return validatorType == friendlyName+"Validator" ||
		validatorType == "*validators."+friendlyName+"Validator"
}

func snakeToPascal(s string) string {
	if s == "" {
		return ""
	}
	var result []byte
	capitalizeNext := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			capitalizeNext = true
			continue
		}
		if capitalizeNext && c >= 'a' && c <= 'z' {
			result = append(result, c-('a'-'A'))
			capitalizeNext = false
		} else {
			result = append(result, c)
			capitalizeNext = false
		}
	}
	return strings.TrimSpace(string(result))
}
