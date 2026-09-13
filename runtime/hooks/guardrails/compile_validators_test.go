package guardrails

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// CompileValidators and ValidatorsToHooks deliberately differ on how they treat
// a validator that cannot be built. These pin both halves side by side, and pin
// the exported sentinels callers are expected to match on.

func TestCompileValidators_UnknownTypeIsFatalAndMatchesSentinel(t *testing.T) {
	enabled := true
	hooks, err := CompileValidators([]prompt.ValidatorConfig{
		{Type: "length", Enabled: &enabled, Params: map[string]any{"max_characters": 100}},
		{Type: "no_such_eval_type_anywhere", Enabled: &enabled, Params: map[string]any{}},
	})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownGuardrailType),
		"callers must be able to match the sentinel with errors.Is")
	assert.Contains(t, err.Error(), "no_such_eval_type_anywhere",
		"the error must name the offending type")
	assert.Empty(t, hooks,
		"no partial guardrail set on a fatal error — a caller must not proceed half-protected")
}

// TestCompileValidators_HandlerRejectedParamsAreFatalAndMatchSentinel pins the
// second fatal class. This used to be warn-and-skip, on the argument that a pack
// authored against a newer runtime may carry params this build cannot read —
// but a handler's own ValidateParams rejecting its own params is not that. It is
// a statement that this validator cannot run, and skipping it left the caller
// with a guardrail-shaped hole and a successful load.
func TestCompileValidators_HandlerRejectedParamsAreFatalAndMatchSentinel(t *testing.T) {
	enabled := true
	// `length` requires one of max/max_characters/max_chars; supplying none
	// fails its ValidateParams.
	hooks, err := CompileValidators([]prompt.ValidatorConfig{
		{Type: "length", Enabled: &enabled, Params: map[string]any{}},
		{Type: "length", Enabled: &enabled, Params: map[string]any{"max_characters": 100}},
	})

	require.Error(t, err, "a validator its own handler rejects must not be silently dropped")
	assert.True(t, errors.Is(err, ErrInvalidGuardrailParams),
		"callers must be able to match the sentinel with errors.Is")
	assert.False(t, errors.Is(err, ErrUnknownGuardrailType),
		"the two fatal classes stay distinguishable — the type here is registered")
	assert.Contains(t, err.Error(), "max_characters",
		"the handler's own message must survive so the fix is obvious")
	assert.Empty(t, hooks,
		"no partial guardrail set on a fatal error — a caller must not proceed half-protected")
}

// TestValidatorsToHooks_StaysLenientOnRejectedParams pins that the deprecated
// lenient entry points did NOT move with CompileValidators. Their contract is
// explicitly "log and skip everything unusable"; existing callers rely on it.
func TestValidatorsToHooks_StaysLenientOnRejectedParams(t *testing.T) {
	enabled := true
	hooks := ValidatorsToHooks([]prompt.ValidatorConfig{
		{Type: "length", Enabled: &enabled, Params: map[string]any{}},
		{Type: "length", Enabled: &enabled, Params: map[string]any{"max_characters": 100}},
	})

	assert.Len(t, hooks, 1,
		"the lenient form skips the rejected params and still returns the usable validator")
}

func TestValidatorsToHooks_StaysLenientOnUnknownType(t *testing.T) {
	enabled := true
	// The deprecated form must keep its documented behavior: skip everything
	// unusable, including an unknown type, and return the rest. Changing this
	// would break existing callers.
	hooks := ValidatorsToHooks([]prompt.ValidatorConfig{
		{Type: "no_such_eval_type_anywhere", Enabled: &enabled, Params: map[string]any{}},
		{Type: "length", Enabled: &enabled, Params: map[string]any{"max_characters": 100}},
	})

	assert.Len(t, hooks, 1,
		"the lenient form skips the unknown type and still returns the usable validator")
}
