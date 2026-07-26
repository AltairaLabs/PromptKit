package guardrails

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestInput_SetsInputDirection(t *testing.T) {
	h, err := Input("length", map[string]any{"max_characters": 5}).Hook()
	require.NoError(t, err)

	// Output phase must be inert for an input guardrail.
	d := h.AfterCall(context.Background(), nil, &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", Content: "way too long to pass"},
	})
	assert.True(t, d.Allow, "input guardrail must not evaluate output")
}

func TestOutput_SetsOutputDirection(t *testing.T) {
	h, err := Output("length", map[string]any{"max_characters": 5}).Hook()
	require.NoError(t, err)

	d := h.BeforeCall(context.Background(), &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "user", Content: "way too long to pass"}},
	})
	assert.True(t, d.Allow, "output guardrail must not evaluate input")
}

func TestInput_PropagatesConstructionError(t *testing.T) {
	_, err := Input("no_such_eval_type_anywhere", nil).Hook()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no_such_eval_type_anywhere")
}

func TestInputFunc_EvaluatesUserInputAndCarriesReplacement(t *testing.T) {
	h, err := InputFunc("no-wires", func(_ context.Context, in *hooks.InputRequest) hooks.Decision {
		if strings.Contains(in.UserInput, "wire transfer") {
			in.Replacement = "I can't help with transfers."
			return hooks.Enforced("wire transfer requested", nil)
		}
		return hooks.Allow
	}).Hook()
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Round:    1,
		Messages: []types.Message{{Role: "user", Content: "do a wire transfer"}},
	}
	d := h.BeforeCall(context.Background(), req)

	require.False(t, d.Allow)
	assert.True(t, d.Enforced)
	assert.Equal(t, "I can't help with transfers.", req.Replacement)
	assert.Equal(t, "no-wires", h.Name())

	// AfterCall is inert.
	assert.True(t, h.AfterCall(context.Background(), req, &hooks.ProviderResponse{}).Allow)
}

func TestInputFunc_DefaultsReplacementWhenUnset(t *testing.T) {
	h, err := InputFunc("no-replacement", func(_ context.Context, _ *hooks.InputRequest) hooks.Decision {
		return hooks.Enforced("blocked", nil)
	}).Hook()
	require.NoError(t, err)

	req := &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	}
	d := h.BeforeCall(context.Background(), req)

	require.False(t, d.Allow)
	assert.Equal(t, prompt.DefaultBlockedMessage, req.Replacement)
}

func TestInputFunc_SkipsWhenLastMessageNotUser(t *testing.T) {
	calls := 0
	h, err := InputFunc("counter", func(_ context.Context, _ *hooks.InputRequest) hooks.Decision {
		calls++
		return hooks.Allow
	}).Hook()
	require.NoError(t, err)

	h.BeforeCall(context.Background(), &hooks.ProviderRequest{
		Messages: []types.Message{
			{Role: "user", Content: "hi"},
			{Role: "tool", Content: "tool output"},
		},
	})

	assert.Equal(t, 0, calls, "input func must not run on tool-result rounds")
}

func TestOutputFunc_EvaluatesResponseContent(t *testing.T) {
	h, err := OutputFunc("no-secrets", func(_ context.Context, out *hooks.OutputRequest) hooks.Decision {
		if strings.Contains(out.Content, "sk-") {
			out.Message.Content = "[redacted]"
			return hooks.Enforced("key leak", nil)
		}
		return hooks.Allow
	}).Hook()
	require.NoError(t, err)

	resp := &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", Content: "your key is sk-abc123"},
	}
	d := h.AfterCall(context.Background(), &hooks.ProviderRequest{}, resp)

	require.False(t, d.Allow)
	assert.True(t, d.Enforced)
	assert.Equal(t, "[redacted]", resp.Message.Content)

	// BeforeCall is inert.
	assert.True(t, h.BeforeCall(context.Background(), &hooks.ProviderRequest{}).Allow)
}
