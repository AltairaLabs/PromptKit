package guardrails_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// policyResponse is what topic_policy answers with under on_deny: respond for
// topicPolicyParams' allow-list.
const policyResponse = "I can only help with: Omnia and PromptKit."

func compileTopicPolicy(t *testing.T, v prompt.ValidatorConfig) hooks.ProviderHook {
	t.Helper()
	compiled, err := guardrails.CompileValidatorsWithRegistry([]prompt.ValidatorConfig{v}, nil)
	require.NoError(t, err)
	require.Len(t, compiled, 1)
	return compiled[0]
}

func topicPolicyWith(extra map[string]any) map[string]any {
	params := topicPolicyParams()
	for k, v := range extra {
		params[k] = v
	}
	return params
}

// on_deny decides what text replaces a denied turn. block keeps the
// validator's message, else the generic blocked message; respond answers with
// the policy's own response, naming what the assistant can help with.
func TestTopicPolicy_OnDenySelectsTheReplacementText(t *testing.T) {
	cases := map[string]struct {
		onDeny  string
		message string
		want    string
	}{
		"block, no message":   {"block", "", prompt.DefaultBlockedMessage},
		"block, message":      {"block", "Ask me about Omnia.", "Ask me about Omnia."},
		"respond, no message": {"respond", "", policyResponse},
		"respond, message":    {"respond", "Ask me about Omnia.", "Ask me about Omnia."},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hook := compileTopicPolicy(t, prompt.ValidatorConfig{
				Type:    "topic_policy",
				Message: tc.message,
				Params:  topicPolicyWith(map[string]any{"on_deny": tc.onDeny}),
			})
			req := offTopicRequest()

			decision := hook.BeforeCall(ctxWithTopicClassifier(t, &countingTopicClassifier{}), req)

			require.False(t, decision.Allow)
			assert.True(t, decision.Enforced)
			assert.Equal(t, tc.want, req.Replacement)
		})
	}
}

// An output-direction respond rewrites the model's reply with the policy's
// response, and a wrapped declaration behaves the same as a direct one.
func TestTopicPolicy_RespondReplacesTheModelsReply(t *testing.T) {
	for name, v := range map[string]prompt.ValidatorConfig{
		"direct": {Type: "topic_policy", Params: topicPolicyWith(map[string]any{
			"on_deny": "respond", "direction": "output",
		})},
		"wrapped": {Type: "guardrail", Params: map[string]any{
			"eval_type":   "topic_policy",
			"direction":   "output",
			"eval_params": topicPolicyWith(map[string]any{"on_deny": "respond"}),
		}},
	} {
		t.Run(name, func(t *testing.T) {
			hook := compileTopicPolicy(t, v)
			resp := &hooks.ProviderResponse{Message: types.Message{
				Role: "assistant", Content: "Vote for whoever you like.",
			}}

			decision := hook.AfterCall(ctxWithTopicClassifier(t, &countingTopicClassifier{}), offTopicRequest(), resp)

			require.False(t, decision.Allow)
			assert.Equal(t, policyResponse, resp.Message.Content)
		})
	}
}
