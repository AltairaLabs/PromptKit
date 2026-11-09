package sdk

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// Policy for an unusable pack validator: both shapes of "this declaration
// cannot work" are fatal at Open().
//
// An unknown eval `type` has no legitimate use — it is a typo, and silently
// dropping it leaves a conversation with no protection while Open() reports
// success. That is fail-open on a safety control.
//
// A registered type whose own ParamValidator rejects the params is the same
// failure wearing a different hat, and used to be warned about and skipped on a
// forward-compatibility argument: that a pack authored against a newer runtime
// may carry params this build does not understand. It does not hold. A build
// that does not know the type at all already fails; a build that does know it
// has the handler's own verdict that this validator cannot run. Skipping it
// converted a typo'd safety control into an unprotected conversation — a
// topic_policy pack with `dissallowed:` opened cleanly with zero guardrails.

// TestOpen_UnknownValidatorTypeIsFatal pins that a typo'd validator type fails
// loudly instead of silently yielding an unprotected conversation.
func TestOpen_UnknownValidatorTypeIsFatal(t *testing.T) {
	_, err := Open("./testdata/packs/guardrail-bad-type.pack.json", "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
	)

	require.Error(t, err, "an unknown validator type must not be silently dropped")
	assert.Contains(t, err.Error(), "pii_leakge",
		"the error must name the offending type so the typo is obvious")
}

// TestOpen_UnusableValidatorParamsAreFatal pins the other half. `length`
// requires one of max/max_characters/max_chars, so supplying none fails its
// ValidateParams — and that must fail Open rather than drop the guardrail.
func TestOpen_UnusableValidatorParamsAreFatal(t *testing.T) {
	_, err := Open("./testdata/packs/guardrail-bad-params.pack.json", "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
	)

	require.Error(t, err, "a validator its own handler rejects must not be silently dropped")
	assert.Contains(t, err.Error(), "length",
		"the error must name the offending validator type")
}

// TestOpen_TopicPolicyTypoIsFatalNotUnprotected is the safety-control case the
// policy exists for, and the one that motivated flipping it. The pack misspells
// `disallowed` as `dissallowed`. topic_policy rejects unknown keys precisely so
// a lost deny-list cannot go unnoticed — but the compiler logged the rejection,
// skipped the validator, and Open() returned a conversation with zero guardrails
// and a nil error, so every off-topic turn reached the agent.
//
// The assertion is deliberately two-sided: Open must error, AND (below) a
// correct pack must still produce a guardrail, so this cannot pass by refusing
// everything.
func TestOpen_TopicPolicyTypoIsFatalNotUnprotected(t *testing.T) {
	conv, err := Open("./testdata/packs/guardrail-topic-policy-typo.pack.json", "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
	)

	require.Error(t, err,
		"a topic_policy whose deny-list key is misspelled must fail load, not open unprotected")
	assert.Nil(t, conv, "no conversation may be handed back when the guardrail could not be built")
	assert.Contains(t, err.Error(), "dissallowed",
		"the error must name the misspelled key so the fix is obvious")
}

// TestOpen_ValidValidatorsSucceed guards against the checks above rejecting
// legitimate packs, and shows the validator the pack declares is the one that
// runs: a reply under its 500-character limit passes untouched, and one over it
// is caught by the length validator.
func TestOpen_ValidValidatorsSucceed(t *testing.T) {
	open := func(t *testing.T, reply string) *Conversation {
		t.Helper()
		conv, err := Open("./testdata/packs/guardrail-valid-validator.pack.json", "chat",
			WithProvider(mock.NewProviderWithRepository("mock", "mock-model", false,
				mock.NewInMemoryMockRepository(reply))),
			WithSkipSchemaValidation(),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })
		return conv
	}

	t.Run("short reply passes", func(t *testing.T) {
		resp, err := open(t, "short answer").Send(context.Background(), "hi")
		require.NoError(t, err)
		assert.Equal(t, "short answer", resp.Text())
		assert.Empty(t, resp.Validations(), "a passing reply records no guardrail firing")
	})

	t.Run("long reply is caught by the declared length validator", func(t *testing.T) {
		resp, err := open(t, strings.Repeat("x", 600)).Send(context.Background(), "hi")
		require.NoError(t, err)
		var failed []string
		for _, v := range resp.Validations() {
			if !v.Passed {
				failed = append(failed, v.ValidatorType)
			}
		}
		assert.Equal(t, []string{"length"}, failed)
		assert.Equal(t, strings.Repeat("x", 500), resp.Text(), "the reply is cut to the declared limit")
	})
}
