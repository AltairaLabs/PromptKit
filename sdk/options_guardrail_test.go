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

// TestWithGuardrail_ValidThenInvalidRegistersNothing pins the "build all specs
// before appending any" guarantee. A single failing spec can't distinguish a
// correct implementation from a buggy append-as-you-go one, since there is
// nothing to have appended before the failure either way — this test puts a
// valid spec first so a partial-registration bug would leave one hook behind.
func TestWithGuardrail_ValidThenInvalidRegistersNothing(t *testing.T) {
	c := &config{}

	err := WithGuardrail(
		guardrails.Input("length", map[string]any{"max_characters": 100}),
		guardrails.Input("no_such_eval_type_anywhere", nil),
	)(c)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no_such_eval_type_anywhere")
	assert.Empty(t, c.providerHooks, "a valid spec preceding a failing one must not be partially registered")
}

// TestWithGuardrail_ZeroValueSpecErrors pins the reachable path for an
// unassigned Spec: a caller pre-sizing a slice — make([]guardrails.Spec, n) —
// and forgetting to fill an entry. That config mistake must surface as an Open()
// error, not a nil-deref panic inside the SDK.
func TestWithGuardrail_ZeroValueSpecErrors(t *testing.T) {
	c := &config{}
	specs := make([]guardrails.Spec, 2)
	specs[0] = guardrails.Input("length", map[string]any{"max_characters": 100})
	// specs[1] left unassigned

	err := WithGuardrail(specs...)(c)

	require.ErrorIs(t, err, guardrails.ErrEmptySpec)
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
