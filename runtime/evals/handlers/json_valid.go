package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// JSONValidHandler checks if CurrentOutput is valid JSON.
// Params: allow_wrapped bool, extract_json bool (both optional).
type JSONValidHandler struct{}

// Type returns the eval type identifier.
func (h *JSONValidHandler) Type() string { return "json_valid" }

// Eval checks that the current output is parseable JSON.
func (h *JSONValidHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (result *evals.EvalResult, err error) {
	allowWrapped := extractBool(params, "allow_wrapped")
	extractJSON := extractBool(params, "extract_json")

	content := evalCtx.CurrentOutput
	if allowWrapped || extractJSON {
		if extracted := extractJSONFromContent(content, allowWrapped, extractJSON); extracted != "" {
			content = extracted
		}
	}

	var target any
	parseErr := json.Unmarshal([]byte(content), &target)

	if parseErr != nil {
		return &evals.EvalResult{
			Type:  h.Type(),
			Score: boolScore(false),
			Value: map[string]any{"valid": false},
			Explanation: fmt.Sprintf(
				"invalid JSON: %v", parseErr,
			),
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Value:       map[string]any{"valid": true},
		Explanation: "output is valid JSON",
	}, nil
}
