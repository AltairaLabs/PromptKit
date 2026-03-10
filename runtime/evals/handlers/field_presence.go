package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// FieldPresenceHandler checks that required fields are present in the output.
// Params: fields []string (field names to look for, case-insensitive).
type FieldPresenceHandler struct{}

// Type returns the eval type identifier.
func (h *FieldPresenceHandler) Type() string { return "field_presence" }

// Eval checks if each field name appears in CurrentOutput (case-insensitive).
func (h *FieldPresenceHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	fields := extractStringSlice(params, "fields")
	if len(fields) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      true,
			Explanation: "no fields to check",
		}, nil
	}

	var found, missing []string
	for _, field := range fields {
		if containsInsensitive(evalCtx.CurrentOutput, field) {
			found = append(found, field)
		} else {
			missing = append(missing, field)
		}
	}

	total := len(fields)
	score := float64(len(found)) / float64(total)
	passed := len(missing) == 0

	explanation := fmt.Sprintf("found %d/%d fields", len(found), total)
	if len(missing) > 0 {
		explanation += ": missing " + strings.Join(missing, ", ")
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      passed,
		Score:       &score,
		Explanation: explanation,
		Details: map[string]any{
			"found":   found,
			"missing": missing,
			"total":   total,
		},
	}, nil
}
