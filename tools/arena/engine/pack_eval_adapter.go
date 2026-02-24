package engine

import (
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// PackEvalAdapter converts evals.EvalResult to assertions.ConversationValidationResult
// so pack eval results flow through the existing Arena assertion reporting pipeline.
//
// Deprecated: The shared assertions.ConvertEvalResults() function should be preferred
// for new code. PackEvalAdapter delegates to it and is kept for backward compatibility.
type PackEvalAdapter struct{}

// Convert transforms a slice of EvalResult into ConversationValidationResult entries.
// Each result is tagged with the assertions.PackEvalTypePrefix so renderers can group them separately.
func (a *PackEvalAdapter) Convert(results []evals.EvalResult) []assertions.ConversationValidationResult {
	return assertions.ConvertEvalResults(results)
}
