package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// TestFilterInvalidEvalDefs_QuotedIDStillFilteredAndWarned covers the
// adjacent hole called out in #1950: ValidateEvalTypes formats messages with
// %q, so an eval id containing a double quote is escaped and the parser's
// `":` search lands in the wrong place. The def then came back with an empty
// id and was neither logged nor filtered — the validator rejected it and it
// ran anyway.
func TestFilterInvalidEvalDefs_QuotedIDStillFilteredAndWarned(t *testing.T) {
	logs := captureLogs(t)

	reg := evals.NewEvalTypeRegistry()
	defs := []evals.EvalDef{
		{ID: "good", Type: "max_length", Trigger: evals.TriggerEveryTurn, Params: map[string]any{"max": 100}},
		{ID: `we"ird`, Type: "nonexistent_type", Trigger: evals.TriggerEveryTurn},
	}

	filtered := filterInvalidEvalDefs(defs, reg)
	require.Len(t, filtered, 1, "a def the validator rejected must not survive because its id was hard to parse")
	assert.Equal(t, "good", filtered[0].ID)
	assert.Contains(t, logs.String(), "Skipping unusable pack eval")
	assert.Contains(t, logs.String(), `we\"ird`)
}
