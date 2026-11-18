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

// A length check with a limit truncates the reply and speaks no message; one
// without a limit blocks, and lists its message.
func TestRegistry_BlockMessagesSkipsTruncatingLengthCheck(t *testing.T) {
	limited, err := NewGuardrailHook("max_length", map[string]any{"max_characters": 10},
		WithMessage("Too long."))
	require.NoError(t, err)
	banned, err := NewGuardrailHook("banned_words", map[string]any{"words": []any{"bruno"}})
	require.NoError(t, err)
	reg := hooks.NewRegistry(hooks.WithProviderHook(limited), hooks.WithProviderHook(banned))
	assert.Equal(t, []string{prompt.DefaultBlockedMessage}, reg.BlockMessages())
}

// A registry with no guardrail, or none at all, has no block message.
func TestRegistry_BlockMessagesNone(t *testing.T) {
	var nilReg *hooks.Registry
	assert.Empty(t, nilReg.BlockMessages())
	assert.Empty(t, hooks.NewRegistry().BlockMessages())
}
