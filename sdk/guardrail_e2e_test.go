package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// End-to-end guardrail behavior through the public SDK surface.
//
// These deliberately go through sdk.Open + Send rather than exercising the
// runtime in isolation. Verifying the hook plumbing separately from the eval
// handlers is exactly how a silently-non-firing input guardrail shipped
// (#1679): each half passed its own tests.

const guardrailTestPack = "./testdata/packs/guardrail-test.pack.json"

// TestGuardrail_InputBlockIsVisibleToCaller pins that a caller can detect a
// guardrail-blocked turn through the public API. The runtime marks the canned
// turn types.FinishReasonSafety; before the fix, buildResponse rebuilt the
// message from a narrower struct and dropped the field, so an application had
// no reliable way to tell a policy block from a real model reply (#1681).
func TestGuardrail_InputBlockIsVisibleToCaller(t *testing.T) {
	conv, err := Open(guardrailTestPack, "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithGuardrail(
			guardrails.Input("banned_words", map[string]any{
				"words": []any{"wire transfer"},
			}, guardrails.WithMessage("I can't help with transfers.")),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "please arrange a wire transfer")

	require.NoError(t, err, "an enforcing input guardrail must not error the send")
	assert.Equal(t, "I can't help with transfers.", resp.Text(),
		"the canned message must reach the caller")
	require.NotNil(t, resp.Message())
	assert.Equal(t, types.FinishReasonSafety, resp.Message().FinishReason,
		"a blocked turn must be detectable via FinishReason, not by matching text")
}

// TestGuardrail_NormalTurnIsNotMarkedBlocked is the discriminating half: an
// unblocked turn must NOT report the safety finish reason. Without this, a fix
// that hardcoded FinishReasonSafety everywhere would pass the test above.
func TestGuardrail_NormalTurnIsNotMarkedBlocked(t *testing.T) {
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

	resp, err := conv.Send(context.Background(), "what is the capital of France?")

	require.NoError(t, err)
	require.NotNil(t, resp.Message())
	assert.NotEqual(t, types.FinishReasonSafety, resp.Message().FinishReason,
		"a clean turn must not be reported as safety-blocked")
	assert.NotEmpty(t, resp.Text(), "a clean turn must carry the provider's reply")
}

// TestGuardrail_InputBlockRecordsValidation pins the other observable signal —
// the ValidationResult naming the guardrail that fired — so an application can
// report *which* policy blocked the turn, not merely that one did.
func TestGuardrail_InputBlockRecordsValidation(t *testing.T) {
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

	resp, err := conv.Send(context.Background(), "please arrange a wire transfer")
	require.NoError(t, err)

	validations := resp.Validations()
	require.NotEmpty(t, validations, "a guardrail firing must be recorded")
	assert.Equal(t, "banned_words", validations[0].ValidatorType)
	assert.False(t, validations[0].Passed)
	assert.Equal(t, "input", validations[0].Details["direction"])
}
