package sdk

import (
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
// legitimate packs. It uses a genuinely valid validator so CompileValidators is
// actually reached — a pack declaring no validators would short-circuit before
// the check and the test could not fail.
func TestOpen_ValidValidatorsSucceed(t *testing.T) {
	conv, err := Open("./testdata/packs/guardrail-valid-validator.pack.json", "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
	)

	require.NoError(t, err)
	require.NotNil(t, conv)
	defer conv.Close()
}
