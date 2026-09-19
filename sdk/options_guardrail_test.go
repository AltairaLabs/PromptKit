package sdk

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// applyGuardrailOption applies opt to a fresh config and resolves the deferred
// guardrail specs, which is what applyOptions does at Open() time. Guardrails
// are built after every option has been seen so WithEvalRegistry can be passed
// in any position (#1717), so a test asserting on the built hooks has to run
// that second step too.
func applyGuardrailOption(t *testing.T, opts ...Option) (*config, error) {
	t.Helper()
	c := &config{}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return c, err
		}
	}
	return c, c.resolveGuardrails()
}

func TestWithGuardrail_RegistersProviderHooks(t *testing.T) {
	c, err := applyGuardrailOption(t, WithGuardrail(
		guardrails.Input("length", map[string]any{"max_characters": 100}),
		guardrails.OutputFunc("no-secrets", func(_ context.Context, out *hooks.OutputRequest) hooks.Decision {
			if strings.Contains(out.Content, "sk-") {
				return hooks.Enforced("key leak", nil)
			}
			return hooks.Allow
		}),
	))

	require.NoError(t, err)
	assert.Len(t, c.providerHooks, 2)

	reg := c.buildHookRegistry()
	require.NotNil(t, reg)
	assert.False(t, reg.IsEmpty())
}

func TestWithGuardrail_SurfacesConstructionError(t *testing.T) {
	c, err := applyGuardrailOption(t, WithGuardrail(
		guardrails.Input("no_such_eval_type_anywhere", nil),
	))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no_such_eval_type_anywhere")
	assert.Empty(t, c.providerHooks, "a failed spec must register nothing")
}

// TestWithGuardrail_ErrorSurfacesFromApplyOptions pins that the deferred build
// is actually wired into the option-application path. Without the
// resolveGuardrails call in applyOptions, an unknown eval type would no longer
// fail Open at all — the specs would simply never be built, which is the exact
// fail-open shape #1717 is about.
func TestWithGuardrail_ErrorSurfacesFromApplyOptions(t *testing.T) {
	_, err := applyOptions("assistant", []Option{
		WithGuardrail(guardrails.Input("no_such_eval_type_anywhere", nil)),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no_such_eval_type_anywhere")
}

// TestApplyOptions_BuildsGuardrailHooks is the positive counterpart: a valid
// spec must reach cfg.providerHooks through applyOptions, so the pipeline sees
// it. A resolveGuardrails call that silently produced nothing would pass the
// error test above but fail this one.
func TestApplyOptions_BuildsGuardrailHooks(t *testing.T) {
	cfg, err := applyOptions("assistant", []Option{
		WithGuardrail(guardrails.Input("length", map[string]any{"max_characters": 100})),
	})

	require.NoError(t, err)
	require.Len(t, cfg.providerHooks, 1)
	assert.Equal(t, "length", cfg.providerHooks[0].Name())
	assert.Empty(t, cfg.pendingGuardrails, "resolved specs must not be left pending")
}

// TestWithGuardrail_ValidThenInvalidRegistersNothing pins the "build all specs
// before appending any" guarantee. A single failing spec can't distinguish a
// correct implementation from a buggy append-as-you-go one, since there is
// nothing to have appended before the failure either way — this test puts a
// valid spec first so a partial-registration bug would leave one hook behind.
func TestWithGuardrail_ValidThenInvalidRegistersNothing(t *testing.T) {
	c, err := applyGuardrailOption(t, WithGuardrail(
		guardrails.Input("length", map[string]any{"max_characters": 100}),
		guardrails.Input("no_such_eval_type_anywhere", nil),
	))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no_such_eval_type_anywhere")
	assert.Empty(t, c.providerHooks, "a valid spec preceding a failing one must not be partially registered")
}

// TestWithGuardrail_ZeroValueSpecErrors pins the reachable path for an
// unassigned Spec: a caller pre-sizing a slice — make([]guardrails.Spec, n) —
// and forgetting to fill an entry. That config mistake must surface as an Open()
// error, not a nil-deref panic inside the SDK.
func TestWithGuardrail_ZeroValueSpecErrors(t *testing.T) {
	specs := make([]guardrails.Spec, 2)
	specs[0] = guardrails.Input("length", map[string]any{"max_characters": 100})
	// specs[1] left unassigned

	c, err := applyGuardrailOption(t, WithGuardrail(specs...))

	require.ErrorIs(t, err, guardrails.ErrEmptySpec)
	assert.Empty(t, c.providerHooks, "a failed spec must register nothing")
}

// TestWithGuardrail_ConstructorOverridesParamsDirection pins that the
// directional constructor is authoritative: a stray params["direction"] cannot
// silently flip an Output guardrail into an input one.
func TestWithGuardrail_ConstructorOverridesParamsDirection(t *testing.T) {
	c, err := applyGuardrailOption(t, WithGuardrail(
		guardrails.Output("length", map[string]any{
			"max_characters": 5,
			"direction":      "input", // ignored: Output() wins
		}),
	))

	require.NoError(t, err)
	require.Len(t, c.providerHooks, 1)

	// Prove it behaves as an output guardrail: input is not evaluated.
	d := c.providerHooks[0].BeforeCall(context.Background(), &hooks.ProviderRequest{
		Messages: []types.Message{{Role: "user", Content: "way too long to pass"}},
	})
	assert.True(t, d.Allow, "Output() must not gate input regardless of params")
}

// namedHook is a do-nothing ProviderHook used to pin registration order.
type namedHook struct{ name string }

func (h *namedHook) Name() string { return h.name }

func (h *namedHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *namedHook) AfterCall(
	context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

// TestWithGuardrail_PreservesDeclarationOrder pins that deferring the build does
// not reorder hooks. Hooks execute in registration order and the first deny
// short-circuits, so a guardrail declared between two WithProviderHook calls has
// to stay between them. A resolveGuardrails that simply appended the built hooks
// would put "guard" last and this test would catch it.
func TestWithGuardrail_PreservesDeclarationOrder(t *testing.T) {
	c, err := applyGuardrailOption(t,
		WithProviderHook(&namedHook{name: "first"}),
		WithGuardrail(guardrails.Input("length", map[string]any{"max_characters": 100})),
		WithProviderHook(&namedHook{name: "last"}),
	)

	require.NoError(t, err)
	require.Len(t, c.providerHooks, 3)
	names := []string{
		c.providerHooks[0].Name(),
		c.providerHooks[1].Name(),
		c.providerHooks[2].Name(),
	}
	assert.Equal(t, []string{"first", "length", "last"}, names)
}
