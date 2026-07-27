package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
)

// Policy for an unusable pack validator, split by how ambiguous the mistake is.
//
// An unknown eval `type` has no legitimate use — it is a typo, and silently
// dropping it leaves a conversation with no protection while Open() reports
// success. That is fail-open on a safety control, so it must be fatal.
//
// A validator whose params fail validation keeps the long-standing
// warn-and-skip contract ("one bad entry does not break the others"): a pack
// authored against a newer runtime can legitimately carry params this build
// does not understand, and refusing to start would make packs
// forward-incompatible.

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

// TestOpen_UnusableValidatorParamsAreSkipped pins the other half: a real eval
// type with unusable params is warned about and skipped, and Open() succeeds.
// `length` requires one of max/max_characters/max_chars, so supplying none
// fails its ValidateParams.
func TestOpen_UnusableValidatorParamsAreSkipped(t *testing.T) {
	conv, err := Open("./testdata/packs/guardrail-bad-params.pack.json", "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
	)

	require.NoError(t, err,
		"unusable params keep the warn-and-skip contract — one bad entry must not break Open")
	require.NotNil(t, conv)
	defer conv.Close()
}

// TestOpen_ValidValidatorsSucceed guards against the unknown-type check
// rejecting legitimate packs. It uses the bad-params pack's sibling with a
// genuinely valid validator so CompileValidators is actually reached — a pack
// declaring no validators would short-circuit before the check and the test
// could not fail.
func TestOpen_ValidValidatorsSucceed(t *testing.T) {
	conv, err := Open("./testdata/packs/guardrail-valid-validator.pack.json", "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
	)

	require.NoError(t, err)
	require.NotNil(t, conv)
	defer conv.Close()
}
