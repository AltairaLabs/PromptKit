package stage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// A guardrail-blocked turn must stream like every other turn (#1716).
//
// The fixtures these tests build on (runProviderStage, assistantMessages,
// enforceBeforeCallHook, denyAllProviderHook, redactionRecordingProvider) live
// in stages_provider_hooks_test.go, alongside the rest of the provider-hook
// coverage.

const blockedTurnText = "I can't help with transfers."

// newBlockedTurnStage builds a streaming ProviderStage whose input guardrail
// enforces on every call, substituting blockedTurnText.
func newBlockedTurnStage(t *testing.T) (*ProviderStage, *redactionRecordingProvider) {
	t.Helper()

	provider := &redactionRecordingProvider{
		Provider:  mock.NewProvider("p", "m", false),
		streaming: true,
	}
	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{
		reason:      "input blocked",
		replacement: blockedTurnText,
	}))
	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)
	return stage, provider
}

// splitTurnStream separates a turn's elements into the text deltas (in emission
// order), the index of the first text delta, and the index of the first
// assistant Message element; the indices are -1 when absent.
//
// The indices let a test assert the delta reaches the caller *before* the
// complete message — the ordering StreamPipeline.accumulateResult relies on. It
// appends text arriving after an assistant response onto that response's
// content, so a delta emitted after the message would duplicate the reply.
func splitTurnStream(elems []StreamElement) (deltas []string, firstText, firstMsg int) {
	firstText, firstMsg = -1, -1
	for i := range elems {
		if elems[i].Text != nil && *elems[i].Text != "" {
			if firstText == -1 {
				firstText = i
			}
			deltas = append(deltas, *elems[i].Text)
		}
		if firstMsg == -1 && elems[i].Message != nil && elems[i].Message.Role == roleAssistant {
			firstMsg = i
		}
	}
	return deltas, firstText, firstMsg
}

// TestProviderStage_EnforcedPreCall_StreamsCannedTextExactlyOnce is the
// regression for #1716: enforcement returns before startStreamingRequest, so no
// chunk ever reached emitChunkElement and a blocked turn produced zero text
// deltas — the caller saw nothing until the whole message arrived at the end.
//
// The assertions cover the whole delivery, not merely that a delta exists:
// joining the deltas pins that they concatenate to exactly the canned text (a
// second emission would double it), and the assistant message carries it once
// more only as the turn's complete message, exactly as on an unblocked turn.
func TestProviderStage_EnforcedPreCall_StreamsCannedTextExactlyOnce(t *testing.T) {
	stage, provider := newBlockedTurnStage(t)

	elems, err := runProviderStage(t, stage, "please arrange a wire transfer")
	require.NoError(t, err)
	require.Equal(t, 0, provider.callCount(),
		"the blocked turn must still cost zero tokens")

	deltas, firstText, firstMsg := splitTurnStream(elems)

	require.NotEmpty(t, deltas,
		"a blocked turn must stream its reply, not deliver it only at the end")
	assert.Equal(t, blockedTurnText, strings.Join(deltas, ""),
		"the streamed deltas must concatenate to the canned text exactly once")

	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1, "the canned reply must be emitted as exactly one assistant message")
	assert.Equal(t, blockedTurnText, msgs[0].Content)

	require.NotEqual(t, -1, firstMsg, "the turn must still emit its complete message")
	assert.Less(t, firstText, firstMsg,
		"the delta must precede the complete message, or accumulateResult appends it "+
			"onto the response and the caller gets the reply twice")
}

// TestProviderStage_EnforcedPreCall_TextElementIsMarked pins the element-level
// metadata of the blocked turn's delta (PRE_LLM_GUARDRAILS_DESIGN.md §4.1.2).
//
// StreamingDelta is load-bearing for delivering the reply once, not decoration:
// a speech-out stage skips deltas and speaks the complete Message, so an
// unmarked delta would be synthesized in addition to the message and the blocked
// reply would be spoken twice. FinishReason is the marker a consumer reading
// element metadata (rather than Message.FinishReason) needs to tell a policy
// block from a model reply.
func TestProviderStage_EnforcedPreCall_TextElementIsMarked(t *testing.T) {
	stage, _ := newBlockedTurnStage(t)

	elems, err := runProviderStage(t, stage, "please arrange a wire transfer")
	require.NoError(t, err)

	var textElem *StreamElement
	for i := range elems {
		if elems[i].Text != nil && *elems[i].Text != "" {
			textElem = &elems[i]
			break
		}
	}
	require.NotNil(t, textElem, "the blocked turn must emit a text element")

	assert.True(t, textElem.Meta.StreamingDelta,
		"the canned text must be marked a streaming delta so a speech stage speaks "+
			"the message once instead of the delta and the message")
	require.NotNil(t, textElem.Meta.FinishReason,
		"the blocked turn's element must carry the finish reason (§4.1.2)")
	assert.Equal(t, types.FinishReasonSafety, *textElem.Meta.FinishReason)
}

// TestProviderStage_CleanStreamingTurn_DeltasAreNotMarkedSafety is the
// discriminating half: a fix that stamped FinishReasonSafety on every streamed
// element would satisfy the test above while making every normal turn look
// policy-blocked.
func TestProviderStage_CleanStreamingTurn_DeltasAreNotMarkedSafety(t *testing.T) {
	provider := &redactionRecordingProvider{
		Provider:  mock.NewProvider("p", "m", false),
		streaming: true,
	}
	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, hooks.NewRegistry())

	elems, err := runProviderStage(t, stage, "hello")
	require.NoError(t, err)
	require.Positive(t, provider.callCount(), "a clean turn must reach the provider")

	deltas, _, _ := splitTurnStream(elems)
	require.NotEmpty(t, deltas, "a clean streaming turn must produce text deltas")

	for i := range elems {
		if elems[i].Text == nil || elems[i].Meta.FinishReason == nil {
			continue
		}
		assert.NotEqual(t, types.FinishReasonSafety, *elems[i].Meta.FinishReason,
			"an unblocked turn's elements must not be marked safety-blocked")
	}
}

// TestProviderStage_DeniedPreCall_EmitsNoText pins that the deny path — which
// aborts the pipeline with HookDeniedError instead of substituting a reply —
// still streams nothing. Only an Enforced decision produces assistant text;
// streaming text for a denial would hand the caller content from a failed turn.
func TestProviderStage_DeniedPreCall_EmitsNoText(t *testing.T) {
	provider := &redactionRecordingProvider{
		Provider:  mock.NewProvider("p", "m", false),
		streaming: true,
	}
	reg := hooks.NewRegistry(hooks.WithProviderHook(&denyAllProviderHook{
		reason: "streaming blocked",
	}))
	stage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)

	elems, err := runProviderStage(t, stage, "hello")
	require.Error(t, err)

	deltas, _, _ := splitTurnStream(elems)
	assert.Empty(t, deltas, "a denied turn must not stream any assistant text")
}
