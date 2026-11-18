package guardrails

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// Registry.BlockMessages lists the message each guardrail speaks when it
// blocks: its configured message, else the default, each once, in
// registration order.
func TestRegistry_BlockMessagesFromGuardrails(t *testing.T) {
	banned := map[string]any{"words": []any{"bruno"}}
	newHook := func(opts ...GuardrailOption) hooks.ProviderHook {
		h, err := NewGuardrailHook("banned_words", banned, opts...)
		require.NoError(t, err)
		return h
	}
	reg := hooks.NewRegistry(
		hooks.WithProviderHook(newHook(WithMessage("We don't talk about Bruno."))),
		hooks.WithProviderHook(newHook()),
		hooks.WithProviderHook(newHook(WithMessage("We don't talk about Bruno."))),
		hooks.WithProviderHook(newHook()),
		hooks.WithProviderHook(newHook(WithMessage("No."))),
	)
	assert.Equal(t, []string{"We don't talk about Bruno.", prompt.DefaultBlockedMessage, "No."},
		reg.BlockMessages())
}

// A registry with no guardrail, or none at all, has no block message.
func TestRegistry_BlockMessagesNone(t *testing.T) {
	var nilReg *hooks.Registry
	assert.Empty(t, nilReg.BlockMessages())
	assert.Empty(t, hooks.NewRegistry().BlockMessages())
}
