package guardrails

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Coverage for https://github.com/AltairaLabs/PromptKit/issues/1717: a handler
// that exists only in a caller-supplied registry must be usable as a guardrail
// through both entry points. Every case asserts the built hook *enforces*, not
// merely that construction returned no error — the failure mode being fixed is
// a guardrail that quietly is not there.

// customOnlyType is registered nowhere by default, so any test that gets a
// working hook for it proves the caller's registry was consulted.
const customOnlyType = "custom_registry_only_1717"

// registryWithCustomType returns a default registry plus a handler that always
// scores 0.0 — below the guardrail's implicit 1.0 threshold, so it enforces.
func registryWithCustomType() *evals.EvalTypeRegistry {
	r := evals.NewEvalTypeRegistry()
	r.Register(&stubHandler{
		typeName: customOnlyType,
		result:   &evals.EvalResult{Score: floatPtr(0.0), Explanation: "custom handler fired"},
	})
	return r
}

// enforcesOnOutput runs the hook's AfterCall over a canned response and reports
// whether the guardrail acted. Enforcement (not Allow) is the observable that a
// dropped guardrail cannot produce.
func enforcesOnOutput(t *testing.T, h hooks.ProviderHook) bool {
	t.Helper()
	resp := &hooks.ProviderResponse{
		Message: types.Message{Role: "assistant", Content: "some assistant output"},
	}
	d := h.AfterCall(context.Background(), &hooks.ProviderRequest{}, resp)
	return !d.Allow
}

func TestCompileValidatorsWithRegistry_ResolvesCustomType(t *testing.T) {
	enabled := true
	built, err := CompileValidatorsWithRegistry([]prompt.ValidatorConfig{
		{Type: customOnlyType, Enabled: &enabled, Params: map[string]any{}},
	}, registryWithCustomType())

	require.NoError(t, err, "a custom type in the supplied registry is not an unknown type")
	require.Len(t, built, 1)
	assert.True(t, enforcesOnOutput(t, built[0]),
		"the compiled hook must run the custom handler and enforce")
}

// TestCompileValidatorsWithRegistry_UnknownToTheSuppliedRegistryIsFatal pins
// that supplying a registry narrows resolution rather than disabling the
// unknown-type check: a type absent from *the caller's* registry is still fatal.
func TestCompileValidatorsWithRegistry_UnknownToTheSuppliedRegistryIsFatal(t *testing.T) {
	enabled := true
	built, err := CompileValidatorsWithRegistry([]prompt.ValidatorConfig{
		{Type: "no_such_eval_type_anywhere", Enabled: &enabled, Params: map[string]any{}},
	}, registryWithCustomType())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownGuardrailType))
	assert.Empty(t, built)
}

// TestCompileValidatorsWithRegistry_NilRegistryUsesDefaults pins the
// nil-means-default contract that keeps CompileValidators (which passes nil)
// working unchanged.
func TestCompileValidatorsWithRegistry_NilRegistryUsesDefaults(t *testing.T) {
	enabled := true
	built, err := CompileValidatorsWithRegistry([]prompt.ValidatorConfig{
		{Type: "length", Enabled: &enabled, Params: map[string]any{"max_characters": 100}},
	}, nil)

	require.NoError(t, err)
	require.Len(t, built, 1, "a nil registry must still resolve built-in types")
	assert.Equal(t, "length", built[0].Name())
}

func TestValidatorsToHooksWithRegistry_ResolvesCustomType(t *testing.T) {
	enabled := true
	built := ValidatorsToHooksWithRegistry([]prompt.ValidatorConfig{
		{Type: customOnlyType, Enabled: &enabled, Params: map[string]any{}},
	}, registryWithCustomType())

	require.Len(t, built, 1,
		"the lenient form must not skip a type the supplied registry knows")
	assert.True(t, enforcesOnOutput(t, built[0]))
}

// TestValidatorsToHooksWithRegistry_StillSkipsTrulyUnknownType pins that the
// lenient path keeps its #1680 behavior for a genuinely unknown type — the
// registry parameter changes *which* types are known, not the failure policy.
func TestValidatorsToHooksWithRegistry_StillSkipsTrulyUnknownType(t *testing.T) {
	enabled := true
	built := ValidatorsToHooksWithRegistry([]prompt.ValidatorConfig{
		{Type: "no_such_eval_type_anywhere", Enabled: &enabled, Params: map[string]any{}},
		{Type: customOnlyType, Enabled: &enabled, Params: map[string]any{}},
	}, registryWithCustomType())

	assert.Len(t, built, 1, "one bad entry must not break the others")
}

func TestSpecHookWithRegistry_ResolvesCustomType(t *testing.T) {
	spec := Output(customOnlyType, nil)

	h, err := spec.HookWithRegistry(registryWithCustomType())

	require.NoError(t, err)
	assert.True(t, enforcesOnOutput(t, h))
}

// TestSpecHook_DefaultRegistryDoesNotKnowCustomType is the contrast case: the
// same Spec built without a registry fails. It pins that the custom type is
// genuinely absent from the defaults, so the test above cannot pass by
// accident, and that Hook() still means "default registry".
func TestSpecHook_DefaultRegistryDoesNotKnowCustomType(t *testing.T) {
	_, err := Output(customOnlyType, nil).Hook()

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownGuardrailType))
}

// TestSpecHookWithRegistry_NilMatchesHook pins that HookWithRegistry(nil) and
// Hook() are the same path, which is what lets Hook() keep working for every
// existing caller.
func TestSpecHookWithRegistry_NilMatchesHook(t *testing.T) {
	spec := Output("length", map[string]any{"max_characters": 10})

	fromNil, errNil := spec.HookWithRegistry(nil)
	fromHook, errHook := spec.Hook()

	require.NoError(t, errNil)
	require.NoError(t, errHook)
	assert.Equal(t, fromHook.Name(), fromNil.Name())
}

// TestSpecHookWithRegistry_FuncSpecIgnoresRegistry pins that func-backed
// guardrails do not consult the registry at all: they must build against an
// empty one, since there is no eval type to look up.
func TestSpecHookWithRegistry_FuncSpecIgnoresRegistry(t *testing.T) {
	spec := OutputFunc("no-op", func(context.Context, *hooks.OutputRequest) hooks.Decision {
		return hooks.Enforced("always", nil)
	})

	h, err := spec.HookWithRegistry(evals.NewEmptyEvalTypeRegistry())

	require.NoError(t, err)
	require.Equal(t, "no-op", h.Name())
	assert.True(t, enforcesOnOutput(t, h))
}

// TestSpecHookWithRegistry_ZeroValueSpec pins that the zero-Spec guard survives
// on the registry-aware entry point too — it must not nil-deref.
func TestSpecHookWithRegistry_ZeroValueSpec(t *testing.T) {
	var spec Spec

	_, err := spec.HookWithRegistry(registryWithCustomType())

	require.ErrorIs(t, err, ErrEmptySpec)
}
