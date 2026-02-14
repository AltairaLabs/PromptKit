package engine

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// PackEvalTypePrefix is prepended to eval types when converting to assertion results.
// Report renderers use this prefix to distinguish pack eval results from scenario assertions.
const PackEvalTypePrefix = "pack_eval:"

// PackEvalAdapter converts evals.EvalResult to assertions.ConversationValidationResult
// so pack eval results flow through the existing Arena assertion reporting pipeline.
type PackEvalAdapter struct{}

// Convert transforms a slice of EvalResult into ConversationValidationResult entries.
// Each result is tagged with the PackEvalTypePrefix so renderers can group them separately.
func (a *PackEvalAdapter) Convert(results []evals.EvalResult) []assertions.ConversationValidationResult {
	if len(results) == 0 {
		return nil
	}

	converted := make([]assertions.ConversationValidationResult, 0, len(results))
	for i := range results {
		converted = append(converted, a.convertOne(&results[i]))
	}
	return converted
}

// convertOne converts a single EvalResult to ConversationValidationResult.
func (a *PackEvalAdapter) convertOne(r *evals.EvalResult) assertions.ConversationValidationResult {
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

	return assertions.ConversationValidationResult{
		Type:    fmt.Sprintf("%s%s", PackEvalTypePrefix, r.Type),
		Passed:  r.Passed,
		Message: msg,
		Details: details,
	}
}
