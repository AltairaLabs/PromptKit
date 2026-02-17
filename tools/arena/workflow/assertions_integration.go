package workflow

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	asrt "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// workflowExtras builds the Extras map for workflow assertions.
// The executor populates this with the current driver state.
//
// This function depends on the arena conversation assertion registry, which
// requires specific validator registrations, so it is tested via integration tests.
func workflowExtras(driver Driver) map[string]interface{} {
	extras := map[string]interface{}{
		"workflow_current_state": driver.CurrentState(),
		"workflow_complete":      driver.IsComplete(),
	}
	return extras
}

// transitionsSnapshot returns a copy of the accumulated transitions for assertion extras.
//
// This function is part of the assertion integration layer, so it is tested
// via integration tests rather than unit tests.
func (e *Executor) transitionsSnapshot() []interface{} {
	result := make([]interface{}, len(e.transitions))
	for i, t := range e.transitions {
		result[i] = map[string]interface{}{
			"from":    t.From,
			"to":      t.To,
			"event":   t.Event,
			"context": t.Context,
		}
	}
	return result
}

// runAssertions evaluates step-level assertions and returns results and the first error.
//
// This function depends on the arena conversation assertion registry, which
// requires specific validator registrations, so it is tested via integration tests.
func (e *Executor) runAssertions(
	ctx context.Context,
	driver Driver,
	configs []asrt.AssertionConfig,
	response string,
) (results []asrt.ConversationValidationResult, firstErr string) {
	extras := workflowExtras(driver)
	extras["workflow_transitions"] = e.transitionsSnapshot()
	results = evaluateAssertions(ctx, configs, response, extras)
	for _, ar := range results {
		if !ar.Passed {
			return results, fmt.Sprintf("assertion %q failed: %s", ar.Type, ar.Message)
		}
	}
	return results, ""
}

// evaluateAssertions runs turn-level assertions against response text.
// It builds a ConversationContext with the assistant response and workflow metadata.
//
// This function depends on the arena conversation assertion registry, which
// requires specific validator registrations, so it is tested via integration tests.
func evaluateAssertions(
	ctx context.Context,
	configs []asrt.AssertionConfig,
	response string,
	extras map[string]interface{},
) []asrt.ConversationValidationResult {
	convCtx := &asrt.ConversationContext{
		AllTurns: []types.Message{
			{Role: "assistant", Content: response},
		},
		Metadata: asrt.ConversationMetadata{
			Extras: extras,
		},
	}

	var assertions []asrt.ConversationAssertion
	for _, c := range configs {
		assertions = append(assertions, asrt.ConversationAssertion(c))
	}

	reg := asrt.NewConversationAssertionRegistry()
	return reg.ValidateConversations(ctx, assertions, convCtx)
}
