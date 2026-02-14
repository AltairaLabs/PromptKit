package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// JSONValidHandler checks if CurrentOutput is valid JSON.
// No required params.
type JSONValidHandler struct{}

// Type returns the eval type identifier.
func (h *JSONValidHandler) Type() string { return "json_valid" }

// Eval checks that the current output is parseable JSON.
func (h *JSONValidHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	_ map[string]any,
) (result *evals.EvalResult, err error) {
	var target any
	parseErr := json.Unmarshal(
		[]byte(evalCtx.CurrentOutput), &target,
	)

	if parseErr != nil {
		return &evals.EvalResult{
			Type:   h.Type(),
			Passed: false,
			Explanation: fmt.Sprintf(
				"invalid JSON: %v", parseErr,
			),
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Explanation: "output is valid JSON",
	}, nil
}
