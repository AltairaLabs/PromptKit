package handlers_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// fakeTopicClassifier records what it was asked and answers with the label
// distribution a backend would return for a canned decision, or fails.
type fakeTopicClassifier struct {
	decision string // "allow", "deny", or anything else for no usable label
	err      error
	seen     inference.Request
	calls    int
}

func (f *fakeTopicClassifier) Infer(_ context.Context, req inference.Request) (inference.Response, error) {
	f.calls++
	f.seen = req
	if f.err != nil {
		return inference.Response{}, f.err
	}
	switch f.decision {
	case "allow":
		return inference.Response{Raw: f.decision, Scores: []inference.LabelScore{
			{Label: "on-topic", Score: 0.9}, {Label: "off-topic", Score: 0.1}}}, nil
	case "deny":
		return inference.Response{Raw: f.decision, Scores: []inference.LabelScore{
			{Label: "off-topic", Score: 0.8}, {Label: "on-topic", Score: 0.2}}}, nil
	}
	return inference.Response{Raw: f.decision, Scores: []inference.LabelScore{{Label: "maybe", Score: 1}}}, nil
}

// message is the judged message: the last input the handler sent.
func (f *fakeTopicClassifier) message() string {
	if len(f.seen.Inputs) == 0 {
		return ""
	}
	return f.seen.Inputs[len(f.seen.Inputs)-1].GetContent()
}

// history is every input before the judged message.
func (f *fakeTopicClassifier) history() []types.Message {
	if len(f.seen.Inputs) == 0 {
		return nil
	}
	return f.seen.Inputs[:len(f.seen.Inputs)-1]
}

func ctxWithTopic(t *testing.T, p inference.Provider) context.Context {
	t.Helper()
	reg := inference.NewRegistry()
	require.NoError(t, reg.Register("topic-control", p))
	return inference.WithRegistry(context.Background(), reg)
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
	fake := &fakeTopicClassifier{decision: "allow"}
	h := &handlers.TopicPolicyHandler{}

	res, err := h.Eval(ctxWithTopic(t, fake), topicEvalCtx("Can Omnia run on OpenShift?"), topicParams(nil))
	require.NoError(t, err)
	require.NotNil(t, res.Score)

	assert.InDelta(t, 1.0, *res.Score, 0.0001)
	assert.Equal(t, "allow", res.Details["decision"])
	assert.InDelta(t, 0.9, res.Details["confidence"], 0.0001,
		"confidence is the probability the backend gave the chosen label")
	assert.Nil(t, res.Value, "eval primitives never set Value")
}

func TestTopicPolicy_DenyScoresZero(t *testing.T) {
	fake := &fakeTopicClassifier{decision: "deny"}
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
			fake := &fakeTopicClassifier{decision: "unknown"}
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
	fake := &fakeTopicClassifier{decision: "allow"}
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

	assert.Equal(t, "What about Azure?", fake.message())
	assert.Len(t, fake.history(), 2, "recent_turns: 2 must send two prior turns")
	assert.Equal(t, "turn two", fake.history()[0].Content)
	assert.Equal(t, "reply two", fake.history()[1].Content)
	assert.Equal(t, "user", fake.seen.Inputs[len(fake.seen.Inputs)-1].Role, "the judged message goes last, as the user")
	assert.Contains(t, fake.seen.Prompt, "Helps users operate AltairaLabs products.")
	assert.Contains(t, fake.seen.Prompt, "- Omnia and PromptKit")
	assert.Contains(t, fake.seen.Prompt, "Small talk — greetings, thanks and pleasantries — is on-topic.")
	assert.True(t, strings.HasSuffix(fake.seen.Prompt, `You must respond with "on-topic" or "off-topic".`),
		"every backend gets NemoGuard's trained prompt, ending with its required closing sentence")
	assert.Equal(t, []string{"on-topic", "off-topic"}, fake.seen.Labels)
}

// TestTopicPolicy_ExcludesItsOwnBlockedTurnFromHistory — a denied turn is
// persisted with the validator's message as the assistant reply and
// FinishReason "safety". Replaying that to the classifier would present the
// guardrail's own output as the agent's voice, spend the anaphora window on a
// refusal with no subject in it, and disclose prior denials.
//
// The real agent turn either side must survive, or this would be indis-
// tinguishable from dropping assistant history altogether.
func TestTopicPolicy_ExcludesItsOwnBlockedTurnFromHistory(t *testing.T) {
	fake := &fakeTopicClassifier{decision: "allow"}
	h := &handlers.TopicPolicyHandler{}

	history := []types.Message{
		{Role: "user", Content: "Can Omnia run on OpenShift?"},
		{Role: "assistant", Content: "Yes, on 4.14 and later.", FinishReason: types.FinishReasonStop},
		{Role: "user", Content: "Who should I vote for?"},
		{
			Role:         "assistant",
			Content:      "I can only help with AltairaLabs products.",
			FinishReason: types.FinishReasonSafety,
		},
	}
	_, err := h.Eval(
		ctxWithTopic(t, fake),
		topicEvalCtx("What about Azure?", history...),
		topicParams(map[string]any{"recent_turns": 4}),
	)
	require.NoError(t, err)

	var texts []string
	for _, turn := range fake.history() {
		texts = append(texts, turn.Content)
	}
	assert.NotContains(t, texts, "I can only help with AltairaLabs products.",
		"the guardrail's own substituted reply is not the agent's voice")
	assert.Equal(t,
		[]string{"Can Omnia run on OpenShift?", "Yes, on 4.14 and later.", "Who should I vote for?"},
		texts,
		"every genuine turn survives, including the user message that was denied")
}

// TestTopicPolicy_BlockedTurnDoesNotConsumeTheWindow — the filter runs before
// the recent_turns slice, so a blocked turn does not evict real history. With
// the order reversed, recent_turns: 2 here would yield a single turn.
func TestTopicPolicy_BlockedTurnDoesNotConsumeTheWindow(t *testing.T) {
	fake := &fakeTopicClassifier{decision: "allow"}
	h := &handlers.TopicPolicyHandler{}

	history := []types.Message{
		{Role: "user", Content: "Can Omnia run on OpenShift?"},
		{Role: "assistant", Content: "Yes, on 4.14 and later.", FinishReason: types.FinishReasonStop},
		{Role: "user", Content: "Who should I vote for?"},
		{Role: "assistant", Content: "Blocked.", FinishReason: types.FinishReasonSafety},
	}
	_, err := h.Eval(
		ctxWithTopic(t, fake),
		topicEvalCtx("What about Azure?", history...),
		topicParams(map[string]any{"recent_turns": 2}),
	)
	require.NoError(t, err)

	var texts []string
	for _, turn := range fake.history() {
		texts = append(texts, turn.Content)
	}
	assert.Equal(t, []string{"Yes, on 4.14 and later.", "Who should I vote for?"}, texts,
		"the window holds the last two REAL turns; filtering before slicing is what "+
			"keeps the blocked turn from costing one of them")
}

func TestTopicPolicy_RecentTurnsZeroSendsNoHistory(t *testing.T) {
	fake := &fakeTopicClassifier{decision: "deny"}
	h := &handlers.TopicPolicyHandler{}

	_, err := h.Eval(
		ctxWithTopic(t, fake),
		topicEvalCtx("What about Azure?", types.Message{Role: "user", Content: "turn one"}),
		topicParams(map[string]any{"recent_turns": 0}),
	)
	require.NoError(t, err)
	assert.Empty(t, fake.history(), "recent_turns: 0 means the current message only")
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
			params:   topicParams(map[string]any{"provider": 123}),
			wantErr:  true,
			contains: "provider",
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
	fake := &fakeTopicClassifier{decision: "allow"}
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

// TestTopicPolicy_NonTextTurnIsUnknown pins the media-only turn. A user message
// whose Parts carry only an image has GetContent() == "", and the handler used
// to hand that empty string to the classifier as the message under judgment —
// asking "is ” on topic?", whose likeliest answer is yes. The guardrail then
// no-ops on exactly the traffic no text check can see. An unjudgable turn is
// unknown, so it resolves through on_unknown; the classifier is never called.
func TestTopicPolicy_NonTextTurnIsUnknown(t *testing.T) {
	imageOnly := types.Message{
		Role:  "user",
		Parts: []types.ContentPart{{Type: "image", Media: &types.MediaContent{MIMEType: "image/png"}}},
	}

	for _, tc := range []struct {
		onUnknown string
		want      float64
	}{
		{"deny", 0.0},
		{"allow", 1.0},
	} {
		t.Run(tc.onUnknown, func(t *testing.T) {
			fake := &fakeTopicClassifier{decision: "allow"}
			h := &handlers.TopicPolicyHandler{}

			res, err := h.Eval(
				ctxWithTopic(t, fake),
				&evals.EvalContext{Messages: []types.Message{imageOnly}, ContentScope: evals.ContentScopeCurrent},
				topicParams(map[string]any{"on_unknown": tc.onUnknown}),
			)
			require.NoError(t, err)
			require.NotNil(t, res.Score)

			assert.InDelta(t, tc.want, *res.Score, 0.0001)
			assert.Equal(t, "unknown", res.Details["decision"])
			assert.Equal(t, 0, fake.calls,
				"an unjudgable turn must not be sent to the classifier as the empty string")
			assert.Contains(t, res.Details["reason"], "no judgable text")
		})
	}
}

// TestTopicPolicy_ToolMessagesDoNotEvictHistory is the design's own worked
// example. The agent answers using four tool calls, then the user asks an
// anaphoric follow-up. History was sliced to the last recent_turns MESSAGES and
// only then filtered by role, so four tool results evicted every real turn and
// "What about Azure?" reached the classifier bare — and was denied. Filtering
// before slicing is what makes recent_turns count conversational turns.
func TestTopicPolicy_ToolMessagesDoNotEvictHistory(t *testing.T) {
	fake := &fakeTopicClassifier{decision: "allow"}
	h := &handlers.TopicPolicyHandler{}

	history := []types.Message{
		{Role: "user", Content: "Can Omnia run on OpenShift?"},
		{Role: "assistant", Content: "Yes — here is how."},
		{Role: "tool", Content: "{\"docs\": 1}"},
		{Role: "tool", Content: "{\"docs\": 2}"},
		{Role: "tool", Content: "{\"docs\": 3}"},
		{Role: "tool", Content: "{\"docs\": 4}"},
	}
	_, err := h.Eval(
		ctxWithTopic(t, fake),
		topicEvalCtx("What about Azure?", history...),
		topicParams(map[string]any{"recent_turns": 4}),
	)
	require.NoError(t, err)

	require.Len(t, fake.history(), 2,
		"tool messages must be filtered out before the window is applied, not after")
	assert.Equal(t, "Can Omnia run on OpenShift?", fake.history()[0].Content)
	assert.Equal(t, "Yes — here is how.", fake.history()[1].Content)
	assert.Equal(t, "What about Azure?", fake.message())
}

// TestTopicPolicy_WindowCountsConversationalTurnsOnly is the other half: once
// tool messages are filtered out, recent_turns still bounds what is sent.
func TestTopicPolicy_WindowCountsConversationalTurnsOnly(t *testing.T) {
	fake := &fakeTopicClassifier{decision: "allow"}
	h := &handlers.TopicPolicyHandler{}

	history := []types.Message{
		{Role: "user", Content: "turn one"},
		{Role: "tool", Content: "noise"},
		{Role: "assistant", Content: "reply one"},
		{Role: "tool", Content: "noise"},
		{Role: "user", Content: "turn two"},
		{Role: "tool", Content: "noise"},
		{Role: "assistant", Content: "reply two"},
	}
	_, err := h.Eval(
		ctxWithTopic(t, fake),
		topicEvalCtx("What about Azure?", history...),
		topicParams(map[string]any{"recent_turns": 2}),
	)
	require.NoError(t, err)

	require.Len(t, fake.history(), 2)
	assert.Equal(t, "turn two", fake.history()[0].Content)
	assert.Equal(t, "reply two", fake.history()[1].Content)
}
