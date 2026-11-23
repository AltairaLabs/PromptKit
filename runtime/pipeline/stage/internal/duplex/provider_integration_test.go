package duplex

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A realtime turn a guardrail blocked drops its reasoning too, whether it
// completed or was cut short after the block.
func TestDuplexEnforcedTurn_DropsTheReasoning(t *testing.T) {
	msg := thoughtful()
	markEnforcedTurn(msg, &core.StreamElement{})
	assert.Equal(t, types.FinishReasonSafety, msg.FinishReason)
	assert.Nil(t, msg.Reasoning)

	s := newDuplexStageForUnit()
	s.accumulatedText.WriteString("the banned")
	s.accumulatedReasoning.WriteString("reasoning toward it")
	s.turnGuard.blocked = true
	elem := s.chunkToElement(&providers.StreamChunk{Interrupted: true})
	require.NotNil(t, elem.Message)
	assert.Equal(t, types.FinishReasonSafety, elem.Message.FinishReason)
	assert.Nil(t, elem.Message.Reasoning)

	unblocked := newDuplexStageForUnit()
	unblocked.accumulatedReasoning.WriteString("still thinking")
	elem = unblocked.chunkToElement(&providers.StreamChunk{Interrupted: true})
	require.NotNil(t, elem.Message)
	assert.Equal(t, &types.ReasoningTrace{Text: "still thinking"}, elem.Message.Reasoning)
}

// A realtime reply a guardrail replaces after some of it streamed ends with
// the guardrail's message marked as replacing it: by a streaming block, a
// streaming deny, and a completed-turn enforcement.
func TestDuplexGuardrailCut_BlockedTextIsMarkedAsReplacingTheReply(t *testing.T) {
	for name, tc := range map[string]struct {
		hook   hooks.ProviderHook
		chunks []string
		want   []string
	}{
		"streaming block": {
			bannedWordsHook(t, "forbidden"), []string{"this is ", "forbidden "},
			[]string{"this is ", "replace:I can't say that."},
		},
		"streaming deny": {
			&maxCharsChunkInterceptor{maxChars: 10}, []string{"short ", "and too long"},
			[]string{"short ", "replace:" + prompt.DefaultBlockedMessage},
		},
		"completed-turn enforcement": {
			&enforceAfterCallHook{replacement: "No."}, []string{"the plan"},
			[]string{"the plan", "replace:No."},
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(tc.hook)))
			chunks := make([]providers.StreamChunk, 0, len(tc.chunks)+1)
			for _, c := range tc.chunks {
				chunks = append(chunks, transcriptChunk(c, pcm(10)))
			}
			elems, err := feedChunks(t, s, append(chunks, stopChunk())...)
			require.NoError(t, err)
			assert.Equal(t, tc.want, liveText(elems))
		})
	}
}

// The rest of a realtime reply a guardrail truncated follows what was sent.
func TestDuplexGuardrailCut_TruncationRestDoesNotReplaceTheText(t *testing.T) {
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 20})
	require.NoError(t, err)
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(guard)))

	elems, err := feedChunks(t, s,
		transcriptChunk("Hello there ", nil),
		transcriptChunk("my friend, how are you", nil),
		stopChunk(),
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"Hello there ", "my frien"}, liveText(elems))
}

// A realtime reply's live text holds back what could be the start of a banned
// word: a word split across transcript deltas never reaches the caller in
// part, and held text that is not the word is released.
func TestDuplexGuardrail_LiveTextHoldsTheStartOfABannedWord(t *testing.T) {
	guard := guardrailHook(t, "content_excludes", map[string]any{"patterns": []any{"pineapple"}},
		guardrails.WithMessage(noPineappleMsg))
	for name, tc := range map[string]struct {
		chunks []string
		want   []string
	}{
		"split banned word": {[]string{"Say P", "ine", "apple"}, []string{"Say ", "replace:" + noPineappleMsg}},
		"held then cleared": {[]string{"Pine", " cones"}, []string{"Pine cones"}},
		"held at turn end":  {[]string{"I like ", "Pine"}, []string{"I like ", "Pine"}},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(guard)))
			chunks := make([]providers.StreamChunk, 0, len(tc.chunks)+1)
			for _, c := range tc.chunks {
				chunks = append(chunks, transcriptChunk(c, pcm(10)))
			}
			elems, err := feedChunks(t, s, append(chunks, stopChunk())...)
			require.NoError(t, err)
			assert.Equal(t, tc.want, liveTextOf(elems))
		})
	}
}

// A truncation that keeps text the caller has not had yet, because it was
// held, sends that text as the rest of the reply, not as a replacement.
func TestTruncationSendsTheHeldTextItKeeps(t *testing.T) {
	limit := func() hooks.ProviderHook {
		return guardrailHook(t, "length", map[string]any{"max_characters": 6})
	}
	excludes := func() hooks.ProviderHook {
		return guardrailHook(t, "content_excludes", map[string]any{"patterns": []any{"pineapple"}})
	}

	t.Run("text path", func(t *testing.T) {
		stream := make(chan providers.StreamChunk, 8)
		sendScriptedDeltas(stream, "Say P", "ine cones")
		close(stream)
		elems, err := runProviderStage(t, guardedStage(stream, limit(), excludes()), "hi")
		require.NoError(t, err)
		assert.Equal(t, []string{"Say ", "Pi"}, liveText(elems))
		assert.Equal(t, "Say Pi", lastAssistantOf(t, elems).Content)
	})

	t.Run("realtime", func(t *testing.T) {
		s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(limit()), hooks.WithProviderHook(excludes())))
		elems, err := feedChunks(t, s,
			transcriptChunk("Say P", pcm(10)), transcriptChunk("ine cones", pcm(10)), stopChunk())
		require.NoError(t, err)
		assert.Equal(t, []string{"Say ", "Pi"}, liveText(elems))
	})
}

// A realtime truncation that keeps held text is still a truncation: the
// truncated reply is the model's own text, which the completed-turn checks
// judge.
func TestDuplexGuardrail_TruncationKeepingHeldTextIsStillJudged(t *testing.T) {
	seen := &afterCallTexts{}
	s, _ := cutStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(guardrailHook(t, "length", map[string]any{"max_characters": 6})),
		hooks.WithProviderHook(guardrailHook(t, "content_excludes", map[string]any{"patterns": []any{"pineapple"}})),
		hooks.WithProviderHook(seen)))

	_, err := feedChunks(t, s, transcriptChunk("Say P", pcm(10)), transcriptChunk("ine cones", pcm(10)), stopChunk())
	require.NoError(t, err)
	assert.Equal(t, []string{"Say Pi"}, seen.seen)
}

// Text held when the caller barges in is dropped with the cut reply: the next
// reply's live text does not start with it.
func TestDuplexGuardrail_HeldTextIsDroppedOnBargeIn(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(
		guardrailHook(t, "content_excludes", map[string]any{"patterns": []any{"pineapple"}}))))

	elems, err := feedChunks(t, s,
		transcriptChunk("Say P", pcm(10)), providers.StreamChunk{Interrupted: true},
		transcriptChunk("Hello", pcm(10)), stopChunk())
	require.NoError(t, err)
	assert.Equal(t, []string{"Say ", "Hello"}, liveText(elems))
}
