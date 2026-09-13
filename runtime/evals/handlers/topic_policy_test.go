package handlers_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// fakeTopicClassifier records what it was asked and answers with a canned
// decision, or fails.
type fakeTopicClassifier struct {
	decision classify.TopicDecision
	err      error
	seen     classify.TopicRequest
	calls    int
}

func (f *fakeTopicClassifier) ClassifyTopic(
	_ context.Context, req classify.TopicRequest,
) (classify.TopicResult, error) {
	f.calls++
	f.seen = req
	if f.err != nil {
		return classify.TopicResult{}, f.err
	}
	return classify.TopicResult{Decision: f.decision, Raw: string(f.decision)}, nil
}

func ctxWithTopic(t *testing.T, c classify.TopicClassifier) context.Context {
	t.Helper()
	reg := classify.NewRegistry()
	classify.RegisterBackendDefaulting(reg, "topic-control", c)
	return classify.WithRegistry(context.Background(), reg)
}

func topicParams(overrides map[string]any) map[string]any {
	params := map[string]any{
		"description": "Helps users operate AltairaLabs products.",
		"allowed":     []any{"Omnia and PromptKit"},
		"disallowed":  []any{"politics"},
	}
	for k, v := range overrides {
		params[k] = v
	}
	return evals.ApplyDefaults("topic_policy", params)
}

func topicEvalCtx(message string, history ...types.Message) *evals.EvalContext {
	msgs := append(append([]types.Message{}, history...), types.Message{Role: "user", Content: message})
	return &evals.EvalContext{
		Messages:      msgs,
		CurrentOutput: message,
		SessionID:     "s1",
		ContentScope:  evals.ContentScopeCurrent,
	}
}

func TestTopicPolicy_AllowScoresOne(t *testing.T) {
	fake := &fakeTopicClassifier{decision: classify.TopicAllow}
	h := &handlers.TopicPolicyHandler{}

	res, err := h.Eval(ctxWithTopic(t, fake), topicEvalCtx("Can Omnia run on OpenShift?"), topicParams(nil))
	require.NoError(t, err)
	require.NotNil(t, res.Score)

	assert.InDelta(t, 1.0, *res.Score, 0.0001)
	assert.Equal(t, "allow", res.Details["decision"])
	assert.NotContains(t, res.Details, "confidence",
		"a label-emitting backend has no confidence; the key must be absent, not zero")
	assert.Nil(t, res.Value, "eval primitives never set Value")
}

func TestTopicPolicy_DenyScoresZero(t *testing.T) {
	fake := &fakeTopicClassifier{decision: classify.TopicDeny}
	h := &handlers.TopicPolicyHandler{}

	res, err := h.Eval(ctxWithTopic(t, fake), topicEvalCtx("Who should I vote for?"), topicParams(nil))
	require.NoError(t, err)
	require.NotNil(t, res.Score)

	assert.InDelta(t, 0.0, *res.Score, 0.0001)
	assert.Equal(t, "deny", res.Details["decision"])
	require.NotNil(t, res.MetricValue)
	assert.InDelta(t, 0.0, *res.MetricValue, 0.0001)
}

func TestTopicPolicy_UnknownHonorsOnUnknown(t *testing.T) {
	for _, tc := range []struct {
		onUnknown string
		want      float64
	}{
		{"deny", 0.0},
		{"allow", 1.0},
	} {
		t.Run(tc.onUnknown, func(t *testing.T) {
			fake := &fakeTopicClassifier{decision: classify.TopicUnknown}
			h := &handlers.TopicPolicyHandler{}

			res, err := h.Eval(
				ctxWithTopic(t, fake), topicEvalCtx("???"),
				topicParams(map[string]any{"on_unknown": tc.onUnknown}),
			)
			require.NoError(t, err)
			require.NotNil(t, res.Score)
			assert.InDelta(t, tc.want, *res.Score, 0.0001)
			assert.Equal(t, "unknown", res.Details["decision"])
		})
	}
}

func TestTopicPolicy_BackendErrorHonorsOnError(t *testing.T) {
	for _, tc := range []struct {
		onError string
		want    float64
	}{
		{"deny", 0.0},
		{"allow", 1.0},
	} {
		t.Run(tc.onError, func(t *testing.T) {
			fake := &fakeTopicClassifier{err: errors.New("connection refused")}
			h := &handlers.TopicPolicyHandler{}

			res, err := h.Eval(
				ctxWithTopic(t, fake), topicEvalCtx("hello"),
				topicParams(map[string]any{"on_error": tc.onError}),
			)
			require.NoError(t, err)
			require.NotNil(t, res.Score)
			assert.InDelta(t, tc.want, *res.Score, 0.0001)
			assert.Contains(t, res.Details["reason"], "connection refused")
		})
	}
}

// TestTopicPolicy_NoClassifierBoundDenies is the #1996 regression test: the
// neighboring classify handlers return Skipped here, which scores 1.0 and
// passes. A guardrail that silently does not run is the bug, not the feature.
func TestTopicPolicy_NoClassifierBoundDenies(t *testing.T) {
	h := &handlers.TopicPolicyHandler{}

	res, err := h.Eval(context.Background(), topicEvalCtx("anything"), topicParams(nil))
	require.NoError(t, err)
	require.NotNil(t, res.Score)

	assert.InDelta(t, 0.0, *res.Score, 0.0001, "an unbound classifier must deny, not skip")
	assert.False(t, res.Skipped, "this must not be reported as a skip")
	// Both unbound paths — no registry at all, and a registry with nothing
	// bound — end in the same actionable remediation sentence.
	assert.Contains(t, res.Details["reason"], "role: inference")
}

func TestTopicPolicy_NoClassifierBoundHonorsOnErrorAllow(t *testing.T) {
	h := &handlers.TopicPolicyHandler{}

	res, err := h.Eval(
		context.Background(), topicEvalCtx("anything"),
		topicParams(map[string]any{"on_error": "allow"}),
	)
	require.NoError(t, err)
	require.NotNil(t, res.Score)
	assert.InDelta(t, 1.0, *res.Score, 0.0001, "an explicit fail-open must still be honored")
}

// TestTopicPolicy_SendsPolicyAndBoundedHistory proves recent_turns actually
// slices history and that the judged message is the current user turn.
func TestTopicPolicy_SendsPolicyAndBoundedHistory(t *testing.T) {
	fake := &fakeTopicClassifier{decision: classify.TopicAllow}
	h := &handlers.TopicPolicyHandler{}

	history := []types.Message{
		{Role: "user", Content: "turn one"},
		{Role: "assistant", Content: "reply one"},
		{Role: "user", Content: "turn two"},
		{Role: "assistant", Content: "reply two"},
	}
	_, err := h.Eval(
		ctxWithTopic(t, fake),
		topicEvalCtx("What about Azure?", history...),
		topicParams(map[string]any{"recent_turns": 2}),
	)
	require.NoError(t, err)

	assert.Equal(t, "What about Azure?", fake.seen.Message)
	assert.Len(t, fake.seen.History, 2, "recent_turns: 2 must send two prior turns")
	assert.Equal(t, "turn two", fake.seen.History[0].Text)
	assert.Equal(t, "reply two", fake.seen.History[1].Text)
	assert.Equal(t, "Helps users operate AltairaLabs products.", fake.seen.Policy.Description)
	assert.Equal(t, []string{"Omnia and PromptKit"}, fake.seen.Policy.Allowed)
	assert.Equal(t, classify.SmallTalkAllow, fake.seen.Policy.SmallTalk)
}

func TestTopicPolicy_RecentTurnsZeroSendsNoHistory(t *testing.T) {
	fake := &fakeTopicClassifier{decision: classify.TopicDeny}
	h := &handlers.TopicPolicyHandler{}

	_, err := h.Eval(
		ctxWithTopic(t, fake),
		topicEvalCtx("What about Azure?", types.Message{Role: "user", Content: "turn one"}),
		topicParams(map[string]any{"recent_turns": 0}),
	)
	require.NoError(t, err)
	assert.Empty(t, fake.seen.History, "recent_turns: 0 means the current message only")
}

func TestTopicPolicy_ValidateParams(t *testing.T) {
	h := &handlers.TopicPolicyHandler{}

	cases := []struct {
		name     string
		params   map[string]any
		wantErr  bool
		contains string
	}{
		{name: "valid", params: topicParams(nil)},
		{
			name:     "unknown key",
			params:   topicParams(map[string]any{"dissallowed": []any{"politics"}}),
			wantErr:  true,
			contains: "dissallowed",
		},
		{
			name:     "missing description",
			params:   map[string]any{"allowed": []any{"x"}},
			wantErr:  true,
			contains: "description",
		},
		{
			name:     "missing allowed",
			params:   map[string]any{"description": "d"},
			wantErr:  true,
			contains: "allowed",
		},
		{
			name:     "empty allowed",
			params:   topicParams(map[string]any{"allowed": []any{}}),
			wantErr:  true,
			contains: "allowed",
		},
		{
			name:     "blank allowed entry",
			params:   topicParams(map[string]any{"allowed": []any{"  "}}),
			wantErr:  true,
			contains: "allowed",
		},
		{
			name:     "bad small_talk",
			params:   topicParams(map[string]any{"small_talk": "maybe"}),
			wantErr:  true,
			contains: "small_talk",
		},
		{
			name:     "bad on_unknown",
			params:   topicParams(map[string]any{"on_unknown": "block"}),
			wantErr:  true,
			contains: "on_unknown",
		},
		{
			name:     "bad on_deny",
			params:   topicParams(map[string]any{"on_deny": "redirect"}),
			wantErr:  true,
			contains: "on_deny",
		},
		{
			name:     "negative recent_turns",
			params:   topicParams(map[string]any{"recent_turns": -1}),
			wantErr:  true,
			contains: "recent_turns",
		},
		{
			name:     "threshold param",
			params:   topicParams(map[string]any{"min_score": 0.5}),
			wantErr:  true,
			contains: "assertion",
		},
		// The cases below are supplemental to the brief: they exist to reach
		// the 80% file coverage gate on topic_policy_params.go rather than to
		// pin new behavior the brief called out.
		{
			name:     "bad on_error",
			params:   topicParams(map[string]any{"on_error": "sometimes"}),
			wantErr:  true,
			contains: "on_error",
		},
		{
			name:     "description wrong type",
			params:   map[string]any{"description": 123, "allowed": []any{"x"}},
			wantErr:  true,
			contains: "description",
		},
		{
			name:     "classifier_id wrong type",
			params:   topicParams(map[string]any{"classifier_id": 123}),
			wantErr:  true,
			contains: "classifier_id",
		},
		{
			name:     "disallowed wrong type",
			params:   topicParams(map[string]any{"disallowed": "politics"}),
			wantErr:  true,
			contains: "disallowed",
		},
		{
			name:     "disallowed item wrong type",
			params:   topicParams(map[string]any{"disallowed": []any{123}}),
			wantErr:  true,
			contains: "disallowed",
		},
		{
			name:     "examples not a map",
			params:   topicParams(map[string]any{"examples": "nope"}),
			wantErr:  true,
			contains: "examples",
		},
		{
			name: "examples unknown key",
			params: topicParams(map[string]any{
				"examples": map[string]any{"maybe": []any{"x"}},
			}),
			wantErr:  true,
			contains: "examples",
		},
		{
			name: "examples valid",
			params: topicParams(map[string]any{
				"examples": map[string]any{
					"allowed":    []any{"a"},
					"disallowed": []any{"b"},
				},
			}),
		},
		{
			name:   "allowed as []string",
			params: topicParams(map[string]any{"allowed": []string{"Omnia and PromptKit"}}),
		},
		{
			name:   "valid with no optional params set (fallback defaults)",
			params: map[string]any{"description": "d", "allowed": []any{"x"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := h.ValidateParams(tc.params)
			if !tc.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.contains)
		})
	}
}

// TestTopicPolicy_DigestIgnoresCosmeticsButNotWording pins what policy_digest
// claims to be. It answers "which policy governed this?" and nothing more: list
// order and whitespace must not move it, a wording change must.
func TestTopicPolicy_DigestIgnoresCosmeticsButNotWording(t *testing.T) {
	fake := &fakeTopicClassifier{decision: classify.TopicAllow}
	h := &handlers.TopicPolicyHandler{}

	digestFor := func(allowed []any) string {
		res, err := h.Eval(
			ctxWithTopic(t, fake), topicEvalCtx("hi"),
			topicParams(map[string]any{"allowed": allowed}),
		)
		require.NoError(t, err)
		digest, ok := res.Details["policy_digest"].(string)
		require.True(t, ok, "policy_digest must be present and a string")
		return digest
	}

	base := digestFor([]any{"Omnia and PromptKit", "licensing"})
	assert.Equal(t, base, digestFor([]any{"licensing", "  Omnia and PromptKit  "}),
		"reordering and whitespace are cosmetic and must not change the digest")
	assert.NotEqual(t, base, digestFor([]any{"Omnia and PromptKit", "licensing and support"}),
		"a wording change is a different policy and must change the digest")
	assert.True(t, strings.HasPrefix(base, "sha256:"))
}

func TestTopicPolicy_DefaultsDirectionToInput(t *testing.T) {
	defaults, ok := evals.ParamDefaults["topic_policy"]
	require.True(t, ok, "topic_policy must declare defaults")
	assert.Equal(t, "input", defaults["direction"],
		"a topic guardrail that only inspects output never blocks the call it exists to prevent")
}
