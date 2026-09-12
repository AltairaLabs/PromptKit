package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// TestGuardrailEvent_CarriesTheEvalScore pins that the score reaching observers
// is the eval's real score.
//
// recordGuardrailFiring reads Metadata["score"] with a float64 type assertion,
// but the guardrail adapter stores evals.EvalResult.Score, which is a
// *float64 — so the assertion always failed and every guardrail event reported
// Score: 0 while runtime-events.md advertises "evaluation score (0.0-1.0)".
//
// This must drive the REAL adapter: a test hook putting a plain float64 in the
// decision metadata would not reproduce a pointer-type mismatch.
func TestGuardrailEvent_CarriesTheEvalScore(t *testing.T) {
	// max_characters far below the mock's reply length, so the guardrail fires
	// with a fractional score rather than a pass.
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{
		"max_characters": 5,
	})
	require.NoError(t, err)

	_, getEvents := runStageCollectingValidations(t,
		mock.NewProvider("p", "m", false),
		hooks.NewRegistry(hooks.WithProviderHook(guard)),
		"hello",
	)

	got := getEvents()
	require.Len(t, got, 1, "the guardrail should have fired once")
	assert.Greater(t, got[0].Score, 0.0,
		"the event must carry the eval's real score, not the zero value from a failed type assertion")
	assert.LessOrEqual(t, got[0].Score, 1.0, "score is a 0..1 ratio")
}
