package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// Compile-time interface checks: these break the build if any of the
// four handlers stop implementing ParamValidator.
var (
	_ evals.ParamValidator = (*MaxLengthHandler)(nil)
	_ evals.ParamValidator = (*MinLengthHandler)(nil)
	_ evals.ParamValidator = (*WorkflowStateIsHandler)(nil)
	_ evals.ParamValidator = (*GuardrailTriggeredHandler)(nil)
)

func TestMaxLengthValidateParams(t *testing.T) {
	h := &MaxLengthHandler{}

	// Happy paths.
	require.NoError(t, h.ValidateParams(map[string]any{"max_characters": 2000}))
	require.NoError(t, h.ValidateParams(map[string]any{"max": 2000}))
	require.NoError(t, h.ValidateParams(map[string]any{"max_chars": 2000}))

	// Missing required key.
	err := h.ValidateParams(map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max")

	// Nil params.
	err = h.ValidateParams(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max")

	// Zero is treated as missing.
	err = h.ValidateParams(map[string]any{"max_characters": 0})
	require.Error(t, err)

	// Negative is invalid.
	err = h.ValidateParams(map[string]any{"max_characters": -5})
	require.Error(t, err)
}

func TestMinLengthValidateParams(t *testing.T) {
	h := &MinLengthHandler{}

	require.NoError(t, h.ValidateParams(map[string]any{"min_characters": 10}))
	require.NoError(t, h.ValidateParams(map[string]any{"min": 10}))

	err := h.ValidateParams(map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "min")
}

func TestWorkflowStateIsValidateParams(t *testing.T) {
	h := &WorkflowStateIsHandler{}

	require.NoError(t, h.ValidateParams(map[string]any{"state": "greeting"}))

	err := h.ValidateParams(map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state")

	err = h.ValidateParams(map[string]any{"state": ""})
	require.Error(t, err)
}

func TestGuardrailTriggeredValidateParams(t *testing.T) {
	h := &GuardrailTriggeredHandler{}

	require.NoError(t, h.ValidateParams(map[string]any{"validator_type": "max_length"}))
	// Alias also works.
	require.NoError(t, h.ValidateParams(map[string]any{"validator": "max_length"}))

	err := h.ValidateParams(map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validator_type")
}
