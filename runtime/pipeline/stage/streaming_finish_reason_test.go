package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestStreamingRound_StampsProviderFinishReason pins that a streaming turn
// carries the provider's finish reason on the assistant message.
//
// processStreamChunks already returns it — the value is used for the
// provider.call.completed event — but it was never stamped on responseMsg, so
// the streaming path reported an empty FinishReason where the non-streaming
// path reported the real one. That divergence became caller-visible once the
// SDK started surfacing FinishReason (#1681): a streaming turn ending for
// max_output_tokens or refusal looked identical to a normal completion.
func TestStreamingRound_StampsProviderFinishReason(t *testing.T) {
	// mock.NewProvider streams and emits FinishReason "stop" on its final chunk.
	provider := mock.NewProvider("p", "m", false)

	stage := NewProviderStageWithEmitter(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil)

	elems, err := runProviderStage(t, stage, "hello")
	require.NoError(t, err)

	msgs := assistantMessages(elems)
	require.NotEmpty(t, msgs, "the streaming turn must emit an assistant message")

	last := msgs[len(msgs)-1]
	assert.Equal(t, "stop", last.FinishReason,
		"the streaming path must stamp the provider's finish reason, not drop it")
}

// TestStreamingRound_EnforcedFinishReasonWins guards the interaction with
// guardrail enforcement: when an output guardrail enforces, the turn is marked
// safety rather than the provider's own reason, and stamping the provider value
// must not overwrite that.
func TestStreamingRound_EnforcedFinishReasonWins(t *testing.T) {
	provider := mock.NewProvider("p", "m", false)

	reg := hooks.NewRegistry(hooks.WithProviderHook(
		&enforceAfterCallHook{replacement: "I can't help with that."},
	))
	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	elems, err := runProviderStage(t, stage, "hello")
	require.NoError(t, err)

	msgs := assistantMessages(elems)
	require.NotEmpty(t, msgs)

	last := msgs[len(msgs)-1]
	assert.Equal(t, types.FinishReasonSafety, last.FinishReason,
		"enforcement must win over the provider's finish reason")
}
