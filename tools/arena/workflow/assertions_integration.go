package workflow

import (
	"context"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	asrt "github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

// evaluateAssertions runs turn-level assertions against response text.
// It builds a minimal ConversationContext with one assistant message.
//
// This function depends on the arena conversation assertion registry, which
// requires specific validator registrations, so it is tested via integration tests.
func evaluateAssertions(
	ctx context.Context,
	configs []asrt.AssertionConfig,
	response string,
) []asrt.ConversationValidationResult {
	convCtx := &asrt.ConversationContext{
		AllTurns: []types.Message{
			{Role: "assistant", Content: response},
		},
	}

	var assertions []asrt.ConversationAssertion
	for _, c := range configs {
		assertions = append(assertions, asrt.ConversationAssertion(c))
	}

	reg := asrt.NewConversationAssertionRegistry()
	return reg.ValidateConversations(ctx, assertions, convCtx)
}
