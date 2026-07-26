package sdk

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestWithGuardrail_RegistersProviderHooks(t *testing.T) {
	c := &config{}

	err := WithGuardrail(
		guardrails.Input("length", map[string]any{"max_characters": 100}),
		guardrails.OutputFunc("no-secrets", func(_ context.Context, out *hooks.OutputRequest) hooks.Decision {
			if strings.Contains(out.Content, "sk-") {
				return hooks.Enforced("key leak", nil)
			}
			return hooks.Allow
		}),
	)(c)

	require.NoError(t, err)
	assert.Len(t, c.providerHooks, 2)

	reg := c.buildHookRegistry()
	require.NotNil(t, reg)
	assert.False(t, reg.IsEmpty())
}

func TestWithGuardrail_SurfacesConstructionError(t *testing.T) {
	c := &config{}

	err := WithGuardrail(guardrails.Input("no_such_eval_type_anywhere", nil))(c)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no_such_eval_type_anywhere")
	assert.Empty(t, c.providerHooks, "a failed spec must register nothing")
}

// TestWithGuardrail_ConstructorOverridesParamsDirection pins that the
// directional constructor is authoritative: a stray params["direction"] cannot
// silently flip an Output guardrail into an input one.
func TestWithGuardrail_ConstructorOverridesParamsDirection(t *testing.T) {
	c := &config{}

	err := WithGuardrail(
		guardrails.Output("length", map[string]any{
			"max_characters": 5,
			"direction":      "input", // ignored: Output() wins
		}),
	)(c)

	require.NoError(t, err)
	require.Len(t, c.providerHooks, 1)

	// Prove it behaves as an output guardrail: input is not evaluated.
	d := c.providerHooks[0].BeforeCall(context.Background(), &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "user", Content: "way too long to pass"}},
	})
	assert.True(t, d.Allow, "Output() must not gate input regardless of params")
}
