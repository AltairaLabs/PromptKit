package sdk_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// Regression coverage for https://github.com/AltairaLabs/PromptKit/issues/1717.
//
// A handler registered only in a caller-supplied evals.EvalTypeRegistry could
// not back a guardrail by either entry point: both resolved eval types against
// a freshly built default registry, so the custom type was unknown. The
// programmatic path failed Open outright and the pack path — which logs and
// skips a validator it cannot construct — silently ran the conversation with no
// guardrail at all.
//
// Every test here therefore asserts on the *enforced content of a real turn*.
// Asserting only that Open succeeds cannot fail for the pack path, because the
// bug there is a silent no-op.

// customGuardrailType is deliberately absent from evals.NewEvalTypeRegistry's
// built-in set. If a future built-in claims this name these tests stop proving
// anything, so keep it obviously bespoke.
const customGuardrailType = "test_forbidden_token_1717"

// forbiddenToken is the substring the custom handler scores against.
const forbiddenToken = "kumquat"

// forbiddenTokenHandler is a custom eval handler that exists only in this test.
// It scores 1.0 for clean content and 0.0 when the forbidden token appears —
// zero trips the guardrail's default "requires a perfect 1.0" threshold.
type forbiddenTokenHandler struct{}

func (h *forbiddenTokenHandler) Type() string { return customGuardrailType }

func (h *forbiddenTokenHandler) Eval(
	_ context.Context, evalCtx *evals.EvalContext, _ map[string]any,
) (*evals.EvalResult, error) {
	score := 1.0
	explanation := "clean"
	if strings.Contains(strings.ToLower(evalCtx.CurrentOutput), forbiddenToken) {
		score = 0.0
		explanation = "forbidden token present"
	}
	return &evals.EvalResult{
		Type:        customGuardrailType,
		Score:       &score,
		Explanation: explanation,
	}, nil
}

// customEvalRegistry returns a default registry with the bespoke handler added,
// mirroring what a caller of sdk.WithEvalRegistry builds.
func customEvalRegistry() *evals.EvalTypeRegistry {
	r := evals.NewEvalTypeRegistry()
	r.Register(&forbiddenTokenHandler{})
	return r
}

// packWithCustomValidator returns promptpack JSON declaring the custom eval
// type as a prompt validator. Schema validation is skipped when opening it —
// the type is intentionally not one the published schema knows.
func packWithCustomValidator(blockedMessage string) []byte {
	return []byte(`{
  "id": "test-pack-1717",
  "version": "1.0.0",
  "description": "issue #1717 regression",
  "prompts": {
    "default": {
      "id": "default",
      "name": "Default",
      "system_template": "You are helpful.",
      "validators": [
        {
          "type": "` + customGuardrailType + `",
          "enabled": true,
          "params": {"message": "` + blockedMessage + `"}
        }
      ]
    }
  }
}`)
}

// packWithoutValidators is the same pack with no validators declared, used by
// the programmatic (WithGuardrail) cases.
func packWithoutValidators() []byte {
	return []byte(`{
  "id": "test-pack-1717-plain",
  "version": "1.0.0",
  "description": "issue #1717 regression",
  "prompts": {
    "default": {
      "id": "default",
      "name": "Default",
      "system_template": "You are helpful."
    }
  }
}`)
}

// sendOnce opens a conversation over a mock provider returning cannedResponse,
// sends one turn, and returns the assistant text the caller receives.
func sendOnce(t *testing.T, packJSON []byte, cannedResponse string, opts ...sdk.Option) string {
	t.Helper()
	packPath := writeTestPack(t, packJSON)

	repo := mock.NewInMemoryMockRepository(cannedResponse)
	provider := mock.NewProviderWithRepository("mock-1717", "mock-model", false, repo)

	all := append([]sdk.Option{
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	}, opts...)

	conv, err := sdk.Open(packPath, "default", all...)
	require.NoError(t, err, "open must succeed with the custom handler supplied via WithEvalRegistry")
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	require.NotNil(t, resp)
	return resp.Text()
}

// TestCustomEvalRegistry_GuardrailOptionEnforces covers the programmatic
// WithGuardrail path. Both option orders are exercised because the fix defers
// guardrail construction until every option has been applied: building eagerly
// inside WithGuardrail works only when WithEvalRegistry happens to come first.
func TestCustomEvalRegistry_GuardrailOptionEnforces(t *testing.T) {
	const blocked = "blocked by the custom guardrail"
	const dirty = "Here is a kumquat for you."

	guardrailOpt := sdk.WithGuardrail(
		guardrails.Output(customGuardrailType, nil, guardrails.WithMessage(blocked)),
	)

	cases := []struct {
		name string
		opts []sdk.Option
	}{
		{
			name: "registry declared first",
			opts: []sdk.Option{sdk.WithEvalRegistry(customEvalRegistry()), guardrailOpt},
		},
		{
			name: "guardrail declared first",
			opts: []sdk.Option{guardrailOpt, sdk.WithEvalRegistry(customEvalRegistry())},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Enforcement: the canned response trips the custom handler, so the
			// caller must receive the guardrail's message instead. Mutations
			// caught: resolving against the default registry (Open errors on an
			// unknown type), and building the hook but never registering it
			// (the raw response comes back).
			got := sendOnce(t, packWithoutValidators(), dirty, tc.opts...)
			assert.Equal(t, blocked, got,
				"custom-registry guardrail must block the offending response")
			assert.NotContains(t, got, forbiddenToken)
		})
	}
}

// TestCustomEvalRegistry_GuardrailOptionAllowsCleanTurn is the negative control
// for the case above. Without it, a guardrail that blocked unconditionally —
// for example one whose handler always scores nil, which the threshold treats
// as fail-closed — would satisfy the enforcement assertion for the wrong
// reason.
func TestCustomEvalRegistry_GuardrailOptionAllowsCleanTurn(t *testing.T) {
	const clean = "Here is a perfectly ordinary answer."

	got := sendOnce(t, packWithoutValidators(), clean,
		sdk.WithEvalRegistry(customEvalRegistry()),
		sdk.WithGuardrail(
			guardrails.Output(customGuardrailType, nil, guardrails.WithMessage("blocked")),
		),
	)

	assert.Equal(t, clean, got, "clean content must pass the custom guardrail untouched")
}

// TestCustomEvalRegistry_PackValidatorEnforces covers the pack `validators:`
// path. This is the case the fail-open policy hid: an unusable validator is
// logged and skipped, so before the fix Open() succeeded and the turn came back
// unmodified. Asserting the enforced text is what makes this test able to fail.
func TestCustomEvalRegistry_PackValidatorEnforces(t *testing.T) {
	const blocked = "blocked by the custom pack validator"
	const dirty = "Have a kumquat, on the house."

	got := sendOnce(t, packWithCustomValidator(blocked), dirty,
		sdk.WithEvalRegistry(customEvalRegistry()),
	)

	assert.Equal(t, blocked, got,
		"a pack validator naming a custom eval type must enforce, not be skipped")
	assert.NotContains(t, got, forbiddenToken)
}

// TestCustomEvalRegistry_PackValidatorAllowsCleanTurn is the negative control
// for the pack path: the validator must be discriminating, not an always-block.
func TestCustomEvalRegistry_PackValidatorAllowsCleanTurn(t *testing.T) {
	const clean = "Nothing controversial here at all."

	got := sendOnce(t, packWithCustomValidator("blocked"), clean,
		sdk.WithEvalRegistry(customEvalRegistry()),
	)

	assert.Equal(t, clean, got, "clean content must pass the custom pack validator untouched")
}

// TestCustomEvalRegistry_PackValidatorWithoutRegistryStillFatal pins that
// threading the registry through did not weaken the unknown-type policy: with
// no custom registry supplied the type really is unknown, and CompileValidators
// must still fail Open rather than drop the guardrail.
func TestCustomEvalRegistry_PackValidatorWithoutRegistryStillFatal(t *testing.T) {
	packPath := writeTestPack(t, packWithCustomValidator("blocked"))

	repo := mock.NewInMemoryMockRepository("anything")
	provider := mock.NewProviderWithRepository("mock-1717", "mock-model", false, repo)

	_, err := sdk.Open(packPath, "default",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, guardrails.ErrUnknownGuardrailType)
	assert.Contains(t, err.Error(), customGuardrailType)
}
