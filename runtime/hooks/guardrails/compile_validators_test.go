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
// an unknown eval type. These pin both halves side by side, and pin the
// exported sentinel callers are expected to match on.

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

func TestCompileValidators_UnusableParamsAreSkippedNotFatal(t *testing.T) {
	enabled := true
	// `length` requires one of max/max_characters/max_chars; supplying none
	// fails its ValidateParams, which is the non-fatal class.
	hooks, err := CompileValidators([]prompt.ValidatorConfig{
		{Type: "length", Enabled: &enabled, Params: map[string]any{}},
		{Type: "length", Enabled: &enabled, Params: map[string]any{"max_characters": 100}},
	})

	require.NoError(t, err, "unusable params must not break the whole set")
	assert.Len(t, hooks, 1, "the usable validator must still be returned")
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
