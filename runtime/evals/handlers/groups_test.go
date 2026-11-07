package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
)

// judgeAndClassifierTypes are the built-in handlers that call a judge model or
// a classifier. Each must be long-running and external, so a host that runs
// only fast-running evals inline does not block a turn on a model call.
var judgeAndClassifierTypes = []string{
	"llm_judge", "llm_judge_session", "llm_judge_tool_calls",
	"faithfulness", "answer_relevancy", "contextual_precision",
	"contextual_recall", "contextual_relevancy", "hallucination",
	"bias", "toxicity", "pii_leakage", "role_violation",
	"audio_emotion", "image_moderation", "text_toxicity", "text_sentiment",
	"topic_policy",
}

func TestJudgeAndClassifierHandlersDeclareGroups(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	want := []string{evals.DefaultEvalGroup, evals.GroupLongRunning, evals.GroupExternal}
	for _, typ := range judgeAndClassifierTypes {
		t.Run(typ, func(t *testing.T) {
			h, err := registry.Get(typ)
			if err != nil {
				t.Fatalf("handler %q not registered: %v", typ, err)
			}
			d, ok := h.(evals.GroupDeclarer)
			if !ok {
				t.Fatalf("%T does not implement evals.GroupDeclarer", h)
			}
			assert.Equal(t, []string{evals.GroupLongRunning, evals.GroupExternal}, d.EvalGroups())
			assert.Equal(t, want, evals.DefaultGroupsForType(typ))
		})
	}
}

func TestDefaultGroupsForType_AliasFollowsTarget(t *testing.T) {
	// llm_judge_conversation is an alias of llm_judge_session.
	assert.Equal(t,
		evals.DefaultGroupsForType("llm_judge_session"),
		evals.DefaultGroupsForType("llm_judge_conversation"))
}

func TestGroups_WrapperFollowsInnerEval(t *testing.T) {
	judged := &evals.EvalDef{
		Type:   evals.WrapperTypeAssertion,
		Params: map[string]any{"eval_type": "faithfulness"},
	}
	assert.Equal(t,
		[]string{evals.DefaultEvalGroup, evals.GroupLongRunning, evals.GroupExternal},
		evals.Groups(judged))

	cheap := &evals.EvalDef{
		Type:   evals.WrapperTypeGuardrail,
		Params: map[string]any{"eval_type": "contains"},
	}
	assert.Equal(t, []string{evals.DefaultEvalGroup, evals.GroupFastRunning}, evals.Groups(cheap))
}
