package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func TestSmoke(t *testing.T) {
	provider := mock.NewProvider("mock", "mock-model", false)
	conv, err := sdk.Open("hooks.pack.json", "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	defer conv.Close()

	_, err = conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
}

// TestInputGuardrailBlocksGracefully pins the headline behavior this example
// demonstrates: an Enforced input guardrail blocks before any provider call,
// Send returns no error, and the response carries the canned message plus a
// validation record naming the guardrail. Hermetic — the mock provider would
// fail the test if it were ever invoked for the blocked turn (mock.Provider's
// canned response doesn't match "I can't help with transfers.").
func TestInputGuardrailBlocksGracefully(t *testing.T) {
	provider := mock.NewProvider("mock", "mock-model", false)
	conv, err := sdk.Open("hooks.pack.json", "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithGuardrail(
			guardrails.Input("regex", map[string]any{
				"pattern":      `(?i)\bwire transfer\b`,
				"expect_match": false,
			}, guardrails.WithMessage("I can't help with transfers.")),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "Can you help me set up a wire transfer?")
	require.NoError(t, err, "an Enforced input guardrail must not surface as a Send error")
	require.Equal(t, "I can't help with transfers.", resp.Text())

	require.NotEmpty(t, resp.Validations(), "blocked turn must carry a validation record")
	found := false
	for _, v := range resp.Validations() {
		if v.ValidatorType == "regex" {
			require.False(t, v.Passed)
			require.Equal(t, "input", v.Details["direction"])
			found = true
		}
	}
	require.True(t, found, "expected a regex validation result")
}

// TestPackDeclaredInputValidatorBlocksGracefully pins the pack-declared path:
// hooks.pack.json's "chat" prompt validators block a "routing number" input
// with zero Go-side guardrail registration.
func TestPackDeclaredInputValidatorBlocksGracefully(t *testing.T) {
	provider := mock.NewProvider("mock", "mock-model", false)
	conv, err := sdk.Open("hooks.pack.json", "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "What's your bank's routing number?")
	require.NoError(t, err)
	require.Equal(t, "I can't help with bank routing numbers.", resp.Text())
	require.NotEmpty(t, resp.Validations())
	require.Equal(t, "regex", resp.Validations()[0].ValidatorType)
}

// TestInputFuncGuardrailBlocksGracefully pins the bespoke plain-function path
// (guardrails.InputFunc): no interface to implement, and — when the func's
// hooks.Enforced call includes a validator_type in its metadata — the same
// structured validation record as the eval-backed guardrails above.
func TestInputFuncGuardrailBlocksGracefully(t *testing.T) {
	provider := mock.NewProvider("mock", "mock-model", false)
	conv, err := sdk.Open("hooks.pack.json", "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithGuardrail(
			guardrails.InputFunc("no-ssn", func(_ context.Context, in *hooks.InputRequest) hooks.Decision {
				if in.UserInput == "what's my ssn" {
					in.Replacement = "I can't help with that request."
					return hooks.Enforced("ssn requested", map[string]any{"validator_type": "no-ssn"})
				}
				return hooks.Allow
			}),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "what's my ssn")
	require.NoError(t, err)
	require.Equal(t, "I can't help with that request.", resp.Text())
	require.Len(t, resp.Validations(), 1)
	require.Equal(t, "no-ssn", resp.Validations()[0].ValidatorType)
}
