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

// TestTopicPolicy_WrappedInGuardrailDoesNotGateInput pins CURRENT, BROKEN
// behavior so the day it changes, someone is told.
//
// evals.ApplyDefaults is keyed on the OUTER type name, so a topic_policy wrapped
// in `type: guardrail` resolves ParamDefaults["guardrail"] — which does not
// exist — and `direction` falls back to the factory's DirectionOutput. BeforeCall
// then returns Allow without evaluating: the topic gate never runs on input, the
// off-topic answer is generated, and only then judged.
//
// That defaults-resolution bug lives in shared wrapper machinery that every eval
// type carrying defaults would hit; topic_policy is simply the only one today.
// It is deliberately NOT fixed here — it needs its own review. What is in scope
// is that the docs no longer invite wrapping (see checks.md), and that this test
// exists: if a wrapper fix lands, this test fails and its author learns that
// topic_policy's input gate is one of the things they just turned on.
func TestTopicPolicy_WrappedInGuardrailDoesNotGateInput(t *testing.T) {
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

			assert.True(t, decision.Allow,
				"KNOWN GAP: the wrapper resolves the OUTER type's defaults, so direction "+
					"reverts to output and the input gate never runs. If this now fails, the "+
					"wrapper-defaults bug has been fixed — update this test to the direct "+
					"declaration's expectations and say so in the changelog.")
			assert.Equal(t, 0, cls.calls,
				"KNOWN GAP: the classifier is never consulted on the user's message when wrapped")
		})
	}
}
