package duplex

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// After a guardrail cuts a reply, nothing of that reply may carry
// Meta.Interrupted: a flush-on-cut pacer counts it as a second cut and drops
// the cue audio queued before it. Neither the reply's end on OpenAI Realtime
// (a canceled response.done, which reports as interrupted) nor Gemini Live's
// blocked reply, which goes on generating, is a cut.
func TestDuplexGuardrailCut_CutReplyEndIsNotASecondCut(t *testing.T) {
	interrupted := types.FinishReasonInterrupted
	cases := map[string][]providers.StreamChunk{
		"canceled end": {
			transcriptChunk("forbidden ", pcm(10)),
			transcriptChunk("words", pcm(20)),
			{FinishReason: &interrupted},
		},
		"continuing reply": {
			transcriptChunk("forbidden ", pcm(10)),
			transcriptChunk("and more", pcm(30)),
			transcriptChunk(" and more", pcm(30)),
			stopChunk(),
		},
	}
	for name, chunks := range cases {
		t.Run(name, func(t *testing.T) {
			s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "forbidden"))))
			elems, err := feedChunks(t, s, chunks...)
			require.NoError(t, err)

			var cuts int
			for _, e := range elems {
				if core.IsCut(&e) {
					cuts++
				}
			}
			assert.Equal(t, 1, cuts, "the guardrail's Interrupt is the only cut")
			msg, end := turnMessage(t, elems)
			assert.Equal(t, types.FinishReasonSafety, msg.FinishReason)
			assert.False(t, end.Meta.Interrupted)
		})
	}
}
