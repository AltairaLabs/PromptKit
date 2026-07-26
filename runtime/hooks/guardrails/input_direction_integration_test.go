package guardrails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// These tests exercise the adapter together with the REAL eval registry and the
// REAL handlers. That junction is what shipped broken: the adapter sets
// EvalContext.CurrentOutput to the user's text for direction=input, but some
// content handlers ignore CurrentOutput and re-derive the content under test by
// scanning messages filtered to assistant role — so they never saw the user's
// message and silently allowed everything.
//
// Unit tests on either side alone pass. Only the pair catches it.

// TestInputGuardrail_BannedWordsFiresOnUserInput pins the headline case: a
// banned_words guardrail declared with direction=input must fire on the user's
// message. Before the fix it scanned assistant messages only, found nothing,
// scored 1.0 and allowed — a silent no-op on a safety check.
func TestInputGuardrail_BannedWordsFiresOnUserInput(t *testing.T) {
	hook, err := NewGuardrailHook("banned_words", map[string]any{
		"words":     []any{"wire transfer"},
		"direction": "input",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			{Role: "user", Content: "please arrange a wire transfer"},
		},
	}

	d := hook.BeforeCall(context.Background(), req)

	require.False(t, d.Allow,
		"banned_words with direction=input must fire on the user's message")
	assert.True(t, d.Enforced, "guardrails enforce rather than deny")
}

// TestInputGuardrail_BannedWordsAllowsCleanUserInput is the negative half — the
// guardrail must not fire on input that does not contain a banned word.
// Without this, a fix that simply always fires would pass the test above.
func TestInputGuardrail_BannedWordsAllowsCleanUserInput(t *testing.T) {
	hook, err := NewGuardrailHook("banned_words", map[string]any{
		"words":     []any{"wire transfer"},
		"direction": "input",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			{Role: "user", Content: "what is the capital of France?"},
		},
	}

	d := hook.BeforeCall(context.Background(), req)

	assert.True(t, d.Allow, "clean user input must pass")
}

// TestOutputGuardrail_BannedWordsStillScansAssistantOnly guards the fix against
// over-reach. As an OUTPUT guardrail, banned_words must keep judging the
// assistant's response and must NOT start failing because a *user* message
// contained the banned word — that is the pre-existing, correct behavior and the
// reason the handlers filter on role in the first place.
func TestOutputGuardrail_BannedWordsStillScansAssistantOnly(t *testing.T) {
	hook, err := NewGuardrailHook("banned_words", map[string]any{
		"words":     []any{"wire transfer"},
		"direction": "output",
	})
	require.NoError(t, err)

	resp := &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", Content: "I can help with that."},
	}
	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			// The user said the banned phrase; the assistant did not.
			{Role: "user", Content: "please arrange a wire transfer"},
		},
	}

	d := hook.AfterCall(context.Background(), req, resp)

	assert.True(t, d.Allow,
		"an output guardrail must judge the assistant's reply, not the user's message")
}

// TestInputGuardrail_ContentIncludesAnySeesUserInput covers the second affected
// handler. contains_any scores whether the content includes one of the patterns,
// so when it cannot see the user's message it reports "not found" and fires
// spuriously — the same root cause surfacing as a false positive instead of a
// false negative.
func TestInputGuardrail_ContentIncludesAnySeesUserInput(t *testing.T) {
	hook, err := NewGuardrailHook("content_includes_any", map[string]any{
		"patterns":  []any{"hello"},
		"direction": "input",
	})
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{
			{Role: "user", Content: "hello there"},
		},
	}

	d := hook.BeforeCall(context.Background(), req)

	assert.True(t, d.Allow,
		"contains_any must see the user's message and find the required pattern")
}
