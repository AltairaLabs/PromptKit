package guardrails_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// countingTopicClassifier records whether it was consulted at all.
type countingTopicClassifier struct{ calls int }

func (c *countingTopicClassifier) ClassifyTopic(
	_ context.Context, _ classify.TopicRequest,
) (classify.TopicResult, error) {
	c.calls++
	return classify.TopicResult{Decision: classify.TopicDeny, Raw: "off-topic"}, nil
}

func topicPolicyParams() map[string]any {
	return map[string]any{
		"description": "Helps users operate AltairaLabs products.",
		"allowed":     []any{"Omnia and PromptKit"},
		"disallowed":  []any{"politics"},
	}
}

func ctxWithTopicClassifier(t *testing.T, c classify.TopicClassifier) context.Context {
	t.Helper()
	reg := classify.NewRegistry()
	classify.RegisterBackendDefaulting(reg, "topic-control", c)
	return classify.WithRegistry(context.Background(), reg)
}

func offTopicRequest() *hooks.ProviderRequest {
	return &hooks.ProviderRequest{Messages: []types.Message{
		{Role: "user", Content: "Who should I vote for?"},
	}}
}

// TestTopicPolicy_DirectDeclarationGatesInput is the control for the test
// below: declared directly as a pack validator, topic_policy picks up its
// `direction: input` default and denies the user's message before the provider
// is called.
func TestTopicPolicy_DirectDeclarationGatesInput(t *testing.T) {
	cls := &countingTopicClassifier{}
	compiled, err := guardrails.CompileValidatorsWithRegistry([]prompt.ValidatorConfig{
		{Type: "topic_policy", Params: topicPolicyParams()},
	}, nil)
	require.NoError(t, err)
	require.Len(t, compiled, 1)

	decision := compiled[0].BeforeCall(ctxWithTopicClassifier(t, cls), offTopicRequest())

	assert.False(t, decision.Allow, "an off-topic message must be denied before the provider call")
	assert.True(t, decision.Enforced)
	assert.Equal(t, 1, cls.calls, "the classifier must judge the user's input")
}

// TestTopicPolicy_WrappedInheritsTheInnerCheckDirection covers what used to be
// a silent disarming: a wrapper resolved ParamDefaults on the OUTER type name,
// so a topic_policy wrapped in `type: guardrail` looked up
// ParamDefaults["guardrail"] — which does not exist — and direction fell back
// to output. BeforeCall then returned Allow without evaluating anything: the
// off-topic answer was generated first and only judged afterwards, if at all.
//
// The wrapper now inherits the inner check's direction default, so wrapped and
// direct declarations gate the same side. The assertions below are deliberately
// identical to TestTopicPolicy_DirectDeclarationGatesInput — that equivalence is
// the property worth pinning.
func TestTopicPolicy_WrappedInheritsTheInnerCheckDirection(t *testing.T) {
	for _, outer := range []string{"guardrail", "assertion"} {
		t.Run(outer, func(t *testing.T) {
			cls := &countingTopicClassifier{}
			compiled, err := guardrails.CompileValidatorsWithRegistry([]prompt.ValidatorConfig{
				{Type: outer, Params: map[string]any{
					"eval_type":   "topic_policy",
					"eval_params": topicPolicyParams(),
				}},
			}, nil)
			require.NoError(t, err)
			require.Len(t, compiled, 1)

			decision := compiled[0].BeforeCall(ctxWithTopicClassifier(t, cls), offTopicRequest())

			assert.False(t, decision.Allow,
				"a wrapped topic_policy must gate input exactly as a direct declaration does")
			assert.Equal(t, 1, cls.calls,
				"the classifier must judge the user's message even when the check is wrapped")
		})
	}
}

// TestTopicPolicy_WrappedRejectsInnerParamTypos is the other half of the same
// hole. The wrapper never ran the inner handler's ParamValidator, so a
// misspelled key inside eval_params loaded clean and produced a policy missing
// the exclusions its author wrote — a guardrail that appears configured and
// enforces something else.
//
// Direct declarations have always failed loudly on this. Wrapped ones now do
// too.
func TestTopicPolicy_WrappedRejectsInnerParamTypos(t *testing.T) {
	for _, outer := range []string{"guardrail", "assertion"} {
		t.Run(outer, func(t *testing.T) {
			typo := topicPolicyParams()
			typo["dissallowed"] = typo["disallowed"]
			delete(typo, "disallowed")

			_, err := guardrails.CompileValidatorsWithRegistry([]prompt.ValidatorConfig{
				{Type: outer, Params: map[string]any{
					"eval_type":   "topic_policy",
					"eval_params": typo,
				}},
			}, nil)

			require.Error(t, err, "a typo'd inner param must fail the load, not load unprotected")
			assert.Contains(t, err.Error(), "dissallowed")
		})
	}
}
