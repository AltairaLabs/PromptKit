package assertions

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// PackEvalTypePrefix is prepended to eval types when converting to assertion results.
// Report renderers use this prefix to distinguish pack eval results from scenario assertions.
const PackEvalTypePrefix = "pack_eval:"

// ConvertEvalResults transforms a slice of EvalResult into ConversationValidationResult entries.
// Each result is tagged with the PackEvalTypePrefix so renderers can group them separately.
// This function is used by both the PackEvalAdapter (engine) and the statestore when
// building AssertionsSummary from eval results.
func ConvertEvalResults(results []evals.EvalResult) []ConversationValidationResult {
	if len(results) == 0 {
		return nil
	}

	converted := make([]ConversationValidationResult, 0, len(results))
	for i := range results {
		converted = append(converted, convertOneEvalResult(&results[i]))
	}
	return converted
}

// convertOneEvalResult converts a single EvalResult to ConversationValidationResult.
func convertOneEvalResult(r *evals.EvalResult) ConversationValidationResult {
	msg := r.Explanation
	if !r.Passed && r.Error != "" {
		msg = r.Error
	}

	details := make(map[string]interface{})
	details["eval_id"] = r.EvalID
	details["duration_ms"] = r.DurationMs
	if r.Score != nil {
		details["score"] = *r.Score
	}
	if r.MetricValue != nil {
		details["metric_value"] = *r.MetricValue
	}
	if r.Error != "" {
		details["error"] = r.Error
	}

	return ConversationValidationResult{
		Type:    fmt.Sprintf("%s%s", PackEvalTypePrefix, r.Type),
		Passed:  r.Passed,
		Message: msg,
		Details: details,
	}
}
