package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// packDeclaredValidators mirrors the two output validators a real pack declares
// under spec.validators — the shape a PromptConfig's YAML compiles to.
func packDeclaredValidators() []prompt.ValidatorConfig {
	return []prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]any{
			"words":          []any{"damn", "crap", "hell"},
			"case_sensitive": false,
		}},
		{Type: "length", Params: map[string]any{"max_characters": 100}},
	}
}

// packGuardrailRegistry compiles pack validators exactly as a host does before
// handing them to a ProviderStage. CompileValidators is used rather than
// hand-constructing adapters so a break anywhere in the pack → hook path
// (unknown type, param normalization, direction defaulting) fails this test
// rather than being papered over by a pre-built hook.
func packGuardrailRegistry(t *testing.T) *hooks.Registry {
	t.Helper()
	compiled, err := guardrails.CompileValidators(packDeclaredValidators())
	require.NoError(t, err)
	require.Len(t, compiled, 2, "both pack validators must compile; a silently dropped one is a fail-open")

	opts := make([]hooks.Option, 0, len(compiled))
	for _, h := range compiled {
		opts = append(opts, hooks.WithProviderHook(h))
	}
	return hooks.NewRegistry(opts...)
}

// providerReturning builds a mock provider whose every reply is text.
func providerReturning(text string) *mock.Provider {
	return mock.NewProviderWithRepository("p", "m", false, mock.NewInMemoryMockRepository(text))
}

// TestPackDeclaredValidator_RecordsValidationOnMessage pins that a validator
// declared in a pack produces a types.ValidationResult on the assistant message,
// on both provider paths, for both a content blocker and a length limit.
//
// The gap this closes is an observability one with safety consequences: the
// guardrail can enforce correctly (content rewritten) while recording nothing,
// and nothing downstream would notice. The guardrail_triggered eval reads
// msg.Validations via EvalContext.PriorResults, so an unrecorded firing is an
// assertion that silently passes for the wrong reason (#1740).
//
// Each case is chosen so exactly one validator can trip, because
// Registry.RunAfterProviderCall and RunOnChunk both short-circuit on the first
// non-Allow decision — a reply that trips both would only ever prove the first
// hook in registration order runs.
//
// Mutations these assertions catch:
//   - a validator dropped between CompileValidators and the stage (Len == 2 above)
//   - recordGuardrailFiring not stamping msg.Validations → empty Validations
//   - the wrong hook firing, or a shared adapter leaking between cases →
//     ValidatorType mismatch
//   - the output direction not being stamped → Details["direction"] absent, which
//     makes a direction-qualified guardrail_triggered assertion match nothing
//   - enforcement not applied → the offending text survives in Content
func TestPackDeclaredValidator_RecordsValidationOnMessage(t *testing.T) {
	const longClean = "The history of computing stretches back thousands of years, from the " +
		"ancient abacus used in Mesopotamia through to the machines on every desk today."

	tests := []struct {
		name          string
		streaming     bool
		reply         string
		wantValidator string
		// wantContentNot is text the enforced reply must no longer contain.
		wantContentNot string
	}{
		{
			name:           "non-streaming banned_words",
			reply:          "well damn that is crap",
			wantValidator:  "banned_words",
			wantContentNot: "damn",
		},
		{
			name:           "streaming banned_words",
			streaming:      true,
			reply:          "well damn that is crap",
			wantValidator:  "banned_words",
			wantContentNot: "damn",
		},
		{
			name:          "non-streaming length",
			reply:         longClean,
			wantValidator: "length",
		},
		{
			name:          "streaming length",
			streaming:     true,
			reply:         longClean,
			wantValidator: "length",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var provider providers.Provider = providerReturning(tc.reply)
			if !tc.streaming {
				provider = &nonStreamingProvider{Provider: provider}
			}

			stage := NewProviderStageWithHooks(provider, nil, nil,
				&ProviderConfig{MaxTokens: 200, Streaming: tc.streaming},
				nil, packGuardrailRegistry(t))

			elems, err := runProviderStage(t, stage, "say something")
			require.NoError(t, err,
				"an enforcing guardrail rewrites the turn; it must not abort the pipeline")

			msgs := assistantMessages(elems)
			require.Len(t, msgs, 1)

			require.NotEmpty(t, msgs[0].Validations,
				"a pack-declared validator that enforced must also record a ValidationResult; "+
					"without one guardrail_triggered reports the guardrail never ran")
			v := msgs[0].Validations[0]

			assert.Equal(t, tc.wantValidator, v.ValidatorType)
			assert.False(t, v.Passed, "a recorded validation is a firing, which never passes")
			assert.Equal(t, "output", v.Details["direction"],
				"a pack validator defaults to the output direction; "+
					"a direction-qualified assertion matches nothing without this")

			if tc.wantContentNot != "" {
				assert.NotContains(t, msgs[0].Content, tc.wantContentNot,
					"the guardrail must rewrite the offending text, not merely record it")
			}
		})
	}
}

// TestPackDeclaredValidator_CleanReplyRecordsNothing is the negative half: a
// reply that trips neither validator must reach the caller untouched and carry
// no validations.
//
// Without it, a mutation that stamped a validation unconditionally — or one that
// enforced on every turn — would still satisfy the test above, and every
// should_trigger:false assertion downstream would start failing.
func TestPackDeclaredValidator_CleanReplyRecordsNothing(t *testing.T) {
	const clean = "Weather patterns are fascinating!"

	for _, streaming := range []bool{false, true} {
		name := "non-streaming"
		if streaming {
			name = "streaming"
		}
		t.Run(name, func(t *testing.T) {
			var provider providers.Provider = providerReturning(clean)
			if !streaming {
				provider = &nonStreamingProvider{Provider: provider}
			}

			stage := NewProviderStageWithHooks(provider, nil, nil,
				&ProviderConfig{MaxTokens: 200, Streaming: streaming},
				nil, packGuardrailRegistry(t))

			elems, err := runProviderStage(t, stage, "how is the weather")
			require.NoError(t, err)

			msgs := assistantMessages(elems)
			require.Len(t, msgs, 1)
			assert.Equal(t, clean, msgs[0].Content,
				"a clean reply must reach the caller verbatim")
			assert.Empty(t, msgs[0].Validations,
				"nothing fired, so there is no firing to record")
		})
	}
}
