package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// extraCapturingJudge records the options a handler hands its judge.
type extraCapturingJudge struct {
	opts JudgeOpts
}

func (c *extraCapturingJudge) Judge(_ context.Context, opts JudgeOpts) (*JudgeResult, error) {
	c.opts = opts
	return &JudgeResult{Score: 1, Reasoning: "ok"}, nil
}

func judgeEvalCtx(judge JudgeProvider) *evals.EvalContext {
	return &evals.EvalContext{
		CurrentOutput: "the answer",
		Metadata:      map[string]any{"judge_provider": judge},
	}
}

// A judge-family check's `extra` reaches the judge: a host JudgeProvider sees
// it on JudgeOpts.Extra, for every handler that builds its request through
// the shared judge path.
func TestJudgeHandlers_PassExtraToTheJudge(t *testing.T) {
	extra := map[string]any{"reasoning_effort": "low", "top_k": 5}
	for name, h := range map[string]evals.EvalTypeHandler{
		"llm_judge":         &LLMJudgeHandler{},
		"llm_judge_session": &LLMJudgeSessionHandler{},
		"faithfulness":      &FaithfulnessHandler{},
	} {
		t.Run(name, func(t *testing.T) {
			judge := &extraCapturingJudge{}
			res, err := h.Eval(context.Background(), judgeEvalCtx(judge), map[string]any{
				"criteria": "is it right?",
				"context":  "the retrieved chunk",
				"extra":    extra,
			})
			require.NoError(t, err)
			require.Empty(t, res.Error)
			assert.Equal(t, extra, judge.opts.Extra)
		})
	}
}

// The built-in judges forward `extra` on the prediction request's provider-
// specific Metadata, so it reaches the provider grading the content.
func TestProviderJudge_ForwardsExtraAsRequestMetadata(t *testing.T) {
	p := &zeroTempJudgeProvider{Provider: mock.NewProvider("judge", "m", false)}
	res, err := (&LLMJudgeHandler{}).Eval(context.Background(), judgeEvalCtx(NewProviderJudge(p)), map[string]any{
		"criteria": "is it right?",
		"extra":    map[string]any{"reasoning_effort": "low"},
	})
	require.NoError(t, err)
	require.Empty(t, res.Error)
	assert.Equal(t, "low", p.got.Metadata["reasoning_effort"])
}
