package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register default handlers
)

// TestArenaEvalOrchestratorFailsOnMissingRequiredParams confirms that
// Arena's fail-fast path picks up param-validation errors from
// ValidateEvalTypes, not just unknown-type errors. This is a side effect
// of extending ValidateEvalTypes to call ParamValidator — Arena gains
// stricter checks for free.
//
// This test asserts the contract at the ValidateEvalTypes level because
// buildEvalOrchestrator in builder_integration.go uses the same function,
// and a direct call avoids standing up a full arena pack fixture.
func TestArenaEvalOrchestratorFailsOnMissingRequiredParams(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()
	defs := []evals.EvalDef{
		{
			ID:      "max-length-bad",
			Type:    "max_length",
			Trigger: evals.TriggerEveryTurn,
			Params:  map[string]any{}, // missing required max_characters
		},
	}

	errs := evals.ValidateEvalTypes(defs, reg)
	require.NotEmpty(t, errs, "Arena fail-fast must surface missing-param error")
	assert.Contains(t, strings.ToLower(errs[0]), "max")
	assert.Contains(t, errs[0], "max-length-bad")
}
