package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
)

// The guardrail → assertion bridge, driven end to end.
//
// Each end of this bridge had its own test — the stage stamps
// msg.Validations["direction"], and validationsToPriorResults has unit tests —
// but nothing drove the join, and the join dropped Details, so an assertion
// could never see which side a guardrail judged (#1718).

// guardrailTriggeredDefs builds the eval definition an Arena scenario would
// declare for this assertion.
func guardrailTriggeredDefs(params map[string]any) []evals.EvalDef {
	return []evals.EvalDef{{
		ID:      "guardrail-fired",
		Type:    "guardrail_triggered",
		Trigger: evals.TriggerEveryTurn,
		Params:  params,
	}}
}

// TestGuardrail_InputFiringIsObservableByAssertion fires a real input guardrail
// through Open + Send, then asserts on it the way a test suite would: by running
// guardrail_triggered over the resulting transcript. Nothing here reaches into
// msg.Validations — the whole point is that the assertion path works.
func TestGuardrail_InputFiringIsObservableByAssertion(t *testing.T) {
	conv, err := Open(guardrailTestPack, "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithGuardrail(
			guardrails.Input("banned_words", map[string]any{
				"words": []any{"wire transfer"},
			}),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	ctx := context.Background()
	_, err = conv.Send(ctx, "please arrange a wire transfer")
	require.NoError(t, err)

	messages := conv.Messages(ctx)
	require.NotEmpty(t, messages)

	results, err := Evaluate(ctx, EvaluateOpts{
		EvalDefs: guardrailTriggeredDefs(map[string]any{
			"validator_type": "banned_words",
			"should_trigger": true,
			"direction":      "input",
		}),
		Messages: messages,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NotNil(t, results[0].Score)
	assert.Equal(t, 1.0, *results[0].Score,
		"an input firing must be observable through PriorResults: %s", results[0].Explanation)
}

// TestGuardrail_InputFiringDoesNotAnswerForTheOutputSide is the discriminating
// half. The same transcript, asserted against the other side, must fail — a
// bridge that carried the firing but not its direction would pass both.
func TestGuardrail_InputFiringDoesNotAnswerForTheOutputSide(t *testing.T) {
	conv, err := Open(guardrailTestPack, "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithGuardrail(
			guardrails.Input("banned_words", map[string]any{
				"words": []any{"wire transfer"},
			}),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	ctx := context.Background()
	_, err = conv.Send(ctx, "please arrange a wire transfer")
	require.NoError(t, err)

	results, err := Evaluate(ctx, EvaluateOpts{
		EvalDefs: guardrailTriggeredDefs(map[string]any{
			"validator_type": "banned_words",
			"should_trigger": true,
			"direction":      "output",
		}),
		Messages: conv.Messages(ctx),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NotNil(t, results[0].Score)
	assert.Equal(t, 0.0, *results[0].Score,
		"the guardrail judged the input, so an output-qualified assertion must not pass")
}

// TestGuardrail_UnqualifiedAssertionStillSeesTheFiring pins the compatibility
// half: the overwhelmingly common declaration omits direction, and must keep
// matching a firing from either side exactly as before.
func TestGuardrail_UnqualifiedAssertionStillSeesTheFiring(t *testing.T) {
	conv, err := Open(guardrailTestPack, "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithGuardrail(
			guardrails.Input("banned_words", map[string]any{
				"words": []any{"wire transfer"},
			}),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	ctx := context.Background()
	_, err = conv.Send(ctx, "please arrange a wire transfer")
	require.NoError(t, err)

	results, err := Evaluate(ctx, EvaluateOpts{
		EvalDefs: guardrailTriggeredDefs(map[string]any{
			"validator_type": "banned_words",
			"should_trigger": true,
		}),
		Messages: conv.Messages(ctx),
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NotNil(t, results[0].Score)
	assert.Equal(t, 1.0, *results[0].Score,
		"an assertion without direction must behave as it always did: %s", results[0].Explanation)
}
