package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestStreamingRound_FinishReason pins what a streaming turn reports as its
// finish reason.
//
// processStreamChunks already returns the provider's reason — it feeds the
// provider.call.completed event — but it was never stamped on responseMsg, so
// the streaming path reported nothing where the non-streaming path reported the
// real value. That became caller-visible once the SDK started surfacing
// FinishReason (#1681): a streaming turn ending for max_output_tokens or
// refusal looked identical to a normal completion.
//
// The enforced case is the guard: stamping the provider's reason must not
// overwrite the safety marker a firing guardrail sets afterwards.
func TestStreamingRound_FinishReason(t *testing.T) {
	tests := []struct {
		name string
		// registry is nil for the plain case; mock.NewProvider streams and
		// emits FinishReason "stop" on its final chunk.
		registry *hooks.Registry
		want     string
		reason   string
	}{
		{
			name:   "stamps the provider's reason",
			want:   "stop",
			reason: "the streaming path must stamp the provider's finish reason, not drop it",
		},
		{
			name: "enforcement wins over the provider's reason",
			registry: hooks.NewRegistry(hooks.WithProviderHook(
				&enforceAfterCallHook{replacement: "I can't help with that."},
			)),
			want:   types.FinishReasonSafety,
			reason: "a firing guardrail must override the provider's finish reason",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage := NewProviderStageWithHooks(
				mock.NewProvider("p", "m", false), nil, nil,
				&ProviderConfig{MaxTokens: 100}, nil, tt.registry,
			)

			elems, err := runProviderStage(t, stage, "hello")
			require.NoError(t, err)

			msgs := assistantMessages(elems)
			require.NotEmpty(t, msgs, "the streaming turn must emit an assistant message")

			assert.Equal(t, tt.want, msgs[len(msgs)-1].FinishReason, tt.reason)
		})
	}
}
