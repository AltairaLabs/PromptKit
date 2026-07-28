package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
)

// enforcingChunkInterceptor fires once from OnChunk with a caller-owned
// metadata map, so a test can mutate that map after the run and observe
// whether the recorded validation aliased it.
type enforcingChunkInterceptor struct {
	meta  map[string]any
	fired bool
}

func (h *enforcingChunkInterceptor) Name() string { return "enforcing_chunk_test" }

func (h *enforcingChunkInterceptor) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforcingChunkInterceptor) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *enforcingChunkInterceptor) OnChunk(
	_ context.Context, _ *providers.StreamChunk,
) hooks.Decision {
	if h.fired {
		return hooks.Allow
	}
	h.fired = true
	// Enforced rather than Deny: the stage stops reading the stream but returns
	// the message normally, so the stamped validation is observable.
	return hooks.Enforced("streaming guardrail fired", h.meta)
}

// TestChunkInterceptorFiring_StampedLikeEveryOtherFiring pins that a streaming
// chunk-interceptor firing is recorded through the same builder as a
// before/after-call firing.
//
// The chunk path used to hand-roll its types.ValidationResult. Each assertion
// below catches one concrete way that hand-rolled struct differed:
//
//   - Details["direction"] — the hand-rolled stamp never set it, so an assertion
//     could not tell a streaming firing from an input one.
//   - Timestamp — the hand-rolled stamp left it at the zero value.
//   - Details["score"] — copied verbatim it is the adapter's *float64, so
//     Details["score"].(float64) yielded 0, false for every consumer of
//     sdk.Response.Validations().
//   - the post-run mutation — the hand-rolled stamp aliased d.Metadata, so a
//     hook holding its own map could rewrite an already-recorded validation.
func TestChunkInterceptorFiring_StampedLikeEveryOtherFiring(t *testing.T) {
	score := 0.75
	meta := map[string]any{
		"validator_type": "streaming_guard",
		"score":          &score,
	}
	interceptor := &enforcingChunkInterceptor{meta: meta}

	stage := NewProviderStageWithHooks(
		mock.NewProvider("p", "m", false), nil, nil,
		&ProviderConfig{MaxTokens: 100}, nil,
		hooks.NewRegistry(hooks.WithProviderHook(interceptor)),
	)

	elems, err := runProviderStage(t, stage, "tell me a long story")
	require.NoError(t, err, "an enforced chunk decision stops the stream, it does not fail the turn")
	require.True(t, interceptor.fired, "the interceptor must have fired for this test to mean anything")

	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	require.Len(t, msgs[0].Validations, 1, "the streaming firing must be stamped on the message")
	v := msgs[0].Validations[0]

	assert.Equal(t, "streaming_guard", v.ValidatorType)
	assert.False(t, v.Passed)
	assert.Equal(t, "output", v.Details["direction"],
		"a streaming firing is an output firing; the hand-rolled stamp omitted direction entirely")
	assert.False(t, v.Timestamp.IsZero(),
		"the hand-rolled stamp left Timestamp at the zero value")

	// The hook still owns meta. Mutating it must not reach the recorded
	// validation — the builder copies precisely to prevent that. Checked before
	// the score assertion so a score regression cannot mask an aliasing one.
	meta["validator_type"] = "tampered"
	meta["injected"] = true
	assert.Equal(t, "streaming_guard", v.Details["validator_type"],
		"the stamp must copy the decision metadata, not alias it")
	assert.NotContains(t, v.Details, "injected",
		"a key added to the hook's map after the fact must not appear in a recorded validation")

	got, ok := v.Details["score"].(float64)
	require.True(t, ok, "Details is public API; score must be a plain float64, not the adapter's *float64")
	assert.InDelta(t, 0.75, got, 1e-9)
}

// TestGuardrailFiring_DetailsScoreIsAPlainFloat64 pins the score shape in
// ValidationResult.Details through the REAL guardrail adapter.
//
// This must drive the real adapter rather than a test hook: the adapter stores
// evals.EvalResult.Score, which is a *float64, and a test hook putting a plain
// float64 in decision metadata could not reproduce the pointer that leaked into
// Details (rendered as `score:0x40adf566cc0` in a live Response.Validations()).
// metadataScore already fixed this for the event payload; the mutation this
// assertion catches is leaving Details untouched, where a consumer doing
// Details["score"].(float64) gets 0, false.
func TestGuardrailFiring_DetailsScoreIsAPlainFloat64(t *testing.T) {
	// max_characters far below the mock's reply length, so the guardrail fires
	// with a fractional score rather than a pass.
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{
		"max_characters": 5,
	})
	require.NoError(t, err)

	elems, getEvents := runStageCollectingValidations(t,
		mock.NewProvider("p", "m", false),
		hooks.NewRegistry(hooks.WithProviderHook(guard)),
		"hello",
	)

	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	require.NotEmpty(t, msgs[0].Validations, "the guardrail firing must be stamped on the message")

	// This configuration fires on both recording paths — the chunk interceptor
	// mid-stream and the AfterCall check on the assembled message — so every
	// entry is checked rather than only the first.
	for i, v := range msgs[0].Validations {
		score, ok := v.Details["score"].(float64)
		require.Truef(t, ok,
			"validation %d: Details[\"score\"] must type-assert to float64; "+
				"the adapter's *float64 must be dereferenced on copy (got %T)", i, v.Details["score"])
		assert.Greaterf(t, score, 0.0,
			"validation %d: the dereferenced score must be the eval's real score, not a zero placeholder", i)
		assert.LessOrEqualf(t, score, 1.0, "validation %d: score is a 0..1 ratio", i)
	}

	// The event payload already carried the dereferenced score. It must still
	// agree with Details, so the two observability surfaces cannot drift.
	got := getEvents()
	require.NotEmpty(t, got)
	assert.InDelta(t, got[0].Score, msgs[0].Validations[0].Details["score"], 1e-9,
		"the event score and the Details score describe the same firing")
}
