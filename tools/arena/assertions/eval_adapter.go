package assertions

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// PackEvalTypePrefix is prepended to eval types when converting to assertion results.
// Report renderers use this prefix to distinguish pack eval results from scenario assertions.
const PackEvalTypePrefix = "pack_eval:"

// ExtractScore extracts a float64 score from a Details map.
// Returns (score, true) if present and numeric, (0, false) otherwise.
func ExtractScore(details map[string]interface{}) (float64, bool) {
	if details == nil {
		return 0, false
	}
	v, ok := details["score"]
	if !ok {
		return 0, false
	}
	switch s := v.(type) {
	case float64:
		return s, true
	case float32:
		return float64(s), true
	case int:
		return float64(s), true
	case int64:
		return float64(s), true
	default:
		return 0, false
	}
}

// ExtractDurationMs extracts eval duration in milliseconds from a Details map.
// Returns (duration, true) if present and numeric, (0, false) otherwise.
func ExtractDurationMs(details map[string]interface{}) (int64, bool) {
	if details == nil {
		return 0, false
	}
	v, ok := details["duration_ms"]
	if !ok {
		return 0, false
	}
	switch d := v.(type) {
	case int64:
		return d, true
	case float64:
		return int64(d), true
	case int:
		return int64(d), true
	default:
		return 0, false
	}
}

// ExtractEvalID extracts the eval ID string from a Details map.
// Returns (id, true) if present and a string, ("", false) otherwise.
func ExtractEvalID(details map[string]interface{}) (string, bool) {
	if details == nil {
		return "", false
	}
	v, ok := details["eval_id"]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// IsPackEval returns true if the ConversationValidationResult originated from a pack eval.
func IsPackEval(cvr ConversationValidationResult) bool {
	return strings.HasPrefix(cvr.Type, PackEvalTypePrefix)
}

// IsSkipped checks whether a Details map indicates the eval was skipped.
// Returns (reason, true) if skipped, ("", false) otherwise.
func IsSkipped(details map[string]interface{}) (string, bool) {
	if details == nil {
		return "", false
	}
	v, ok := details["skipped"]
	if !ok {
		return "", false
	}
	skipped, isBool := v.(bool)
	if !isBool || !skipped {
		return "", false
	}
	reason, _ := details["skip_reason"].(string)
	return reason, true
}

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
	if r.Skipped {
		details["skipped"] = true
		details["skip_reason"] = r.SkipReason
	}

	return ConversationValidationResult{
		Type:    fmt.Sprintf("%s%s", PackEvalTypePrefix, r.Type),
		Passed:  r.Passed,
		Message: msg,
		Details: details,
	}
}
