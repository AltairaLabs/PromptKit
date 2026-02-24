package workflow

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	asrt "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// runAssertions evaluates step-level assertions via the TurnEvalRunner and returns
// results and the first error.
func (e *Executor) runAssertions(
	ctx context.Context,
	_ Driver,
	configs []asrt.AssertionConfig,
	response string,
) (results []asrt.ConversationValidationResult, firstErr string) {
	if e.turnEvalRunner == nil {
		return nil, ""
	}

	messages := []types.Message{{Role: "assistant", Content: response}}
	evalResults := e.turnEvalRunner.RunAssertionsAsEvals(
		ctx, configs, messages, 0, e.sessionID,
		evals.TriggerEveryTurn,
	)
	results = asrt.ConvertEvalResults(evalResults)

	for _, ar := range results {
		if !ar.Passed {
			return results, fmt.Sprintf("assertion %q failed: %s", ar.Type, ar.Message)
		}
	}
	return results, ""
}
