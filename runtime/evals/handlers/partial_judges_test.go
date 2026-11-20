package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// A judge given a reply still being written is told so, after the check's own
// criteria; the same check on a complete reply is not.
func TestJudgeHandlers_TellTheJudgeAPartialReplyMayBeIncomplete(t *testing.T) {
	for name, h := range map[string]evals.EvalTypeHandler{
		"llm_judge": &LLMJudgeHandler{},
		"toxicity":  &ToxicityHandler{},
	} {
		t.Run(name, func(t *testing.T) {
			judge := &extraCapturingJudge{}
			evalCtx := judgeEvalCtx(judge)
			evalCtx.Partial = true
			_, err := h.Eval(context.Background(), evalCtx, map[string]any{"criteria": "Only the weather."})
			require.NoError(t, err)
			assert.Equal(t, "Only the weather.\n\n"+partialReplyNote, judge.opts.Criteria)

			_, err = h.Eval(context.Background(), judgeEvalCtx(judge), map[string]any{"criteria": "Only the weather."})
			require.NoError(t, err)
			assert.Equal(t, "Only the weather.", judge.opts.Criteria)
		})
	}
}

// A partial reply judged with no criteria of the check's own gets the note
// as its criteria.
func TestWithPartialNote_NoCriteria(t *testing.T) {
	opts := JudgeOpts{}
	withPartialNote(&opts, &evals.EvalContext{Partial: true})
	assert.Equal(t, partialReplyNote, opts.Criteria)

	opts = JudgeOpts{Criteria: "c"}
	withPartialNote(&opts, nil)
	assert.Equal(t, "c", opts.Criteria)
}

// Topic, policy and PII-style judges hold a streamed reply per segment by
// default; a free-form llm_judge holds the whole turn.
func TestPartialJudges_SegmentHoldByDefault(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()
	for evalType, want := range map[string]bool{
		"llm_judge": false, "topic_policy": true, "pii_leakage": true, "toxicity": true,
		"bias": true, "role_violation": true, "text_toxicity": true,
	} {
		h, err := reg.Get(evalType)
		require.NoError(t, err)
		pj, ok := h.(evals.PartialJudge)
		require.True(t, ok, evalType)
		assert.Equal(t, want, pj.SegmentHoldByDefault(), evalType)
	}
	for _, evalType := range []string{"answer_relevancy", "faithfulness", "llm_judge_session", "content_excludes"} {
		h, err := reg.Get(evalType)
		require.NoError(t, err)
		_, ok := h.(evals.PartialJudge)
		assert.False(t, ok, "%s cannot judge a partial reply", evalType)
	}
}
