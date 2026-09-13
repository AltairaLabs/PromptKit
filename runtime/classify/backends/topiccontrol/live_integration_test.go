package topiccontrol_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/classify/backends/topiccontrol"
)

// Live verification of the wire contract, skipped unless TOPIC_CONTROL_BASE_URL
// is set. The unit tests in this package assert against an httptest fake that we
// wrote to match our own reading of the model card, so they cannot detect the
// one failure that matters most: the real service answering in a shape
// ParseLabel does not recognize. Every unknown label resolves through
// on_unknown, which defaults to deny — so a wire mismatch does not leak
// off-topic traffic, it blocks every turn instead. Loud, but still an outage.
//
// Run it against any OpenAI-compatible endpoint:
//
//	# The purpose-built model (needs a self-hosted NIM; NVIDIA's hosted
//	# deployment has been returning TensorRT-LLM 500s — see #2003).
//	TOPIC_CONTROL_BASE_URL=http://localhost:8000/v1 \
//	  go -C runtime test ./classify/backends/topiccontrol/ -run Live -v
//
//	# A general instruction-following model, which verifies the renderer and
//	# the parser even when the purpose-built model is unavailable.
//	TOPIC_CONTROL_BASE_URL=https://integrate.api.nvidia.com/v1 \
//	TOPIC_CONTROL_API_KEY=$NVIDIA_API_KEY \
//	TOPIC_CONTROL_MODEL=meta/llama-3.2-11b-vision-instruct \
//	  go -C runtime test ./classify/backends/topiccontrol/ -run Live -v
//
// A reasoning model is NOT a valid backend here: it answers with its thinking
// rather than a bare label, every classification lands on unknown, and the
// guardrail blocks the conversation. If this test fails with raw output that
// starts like a chain of thought, that is the cause.
const (
	envBaseURL = "TOPIC_CONTROL_BASE_URL"
	envAPIKey  = "TOPIC_CONTROL_API_KEY"
	envModel   = "TOPIC_CONTROL_MODEL"
)

// liveRepeats is how many times each case runs. A single sample from a
// stochastic endpoint is not evidence, and the failure this test exists to
// catch — a model that mostly complies and occasionally editorializes — is
// invisible at one sample. Every repeat must agree, or the case fails.
const liveRepeats = 2

// liveTimeout bounds one classification generously; a cold NIM can be slow to
// answer its first request, and a shared hosted endpoint can stall under load.
// Passed to Config.Timeout rather than only to the context, because the
// client's own timeout caps the call and the shorter of the two always wins.
const liveTimeout = 60 * time.Second

func liveClient(t *testing.T) *topiccontrol.Client {
	t.Helper()

	baseURL := os.Getenv(envBaseURL)
	if baseURL == "" {
		t.Skipf("set %s to run the live wire-contract test (see the file's doc comment)", envBaseURL)
	}

	c, err := topiccontrol.New(topiccontrol.Config{
		BaseURL: baseURL,
		APIKey:  os.Getenv(envAPIKey),
		Model:   os.Getenv(envModel), // empty falls back to DefaultModel
		// The backend's 20s default is deliberately tight for the request path;
		// a live test against a shared endpoint should not fail over one slow
		// response, which is a different fact from a wrong answer.
		Timeout: liveTimeout,
	})
	require.NoError(t, err)
	return c
}

// livePolicy is the design document's worked example, kept verbatim so the
// multi-turn case below exercises the scenario the whole recent_turns
// mechanism exists for.
func livePolicy() classify.TopicPolicy {
	return classify.TopicPolicy{
		Description: "Helps users understand, evaluate, deploy and operate AltairaLabs products.",
		Allowed: []string{
			"AltairaLabs as a company",
			"Omnia, PromptKit, PromptPack and PromptArena",
			"product capabilities and architecture",
			"installation, configuration, deployment and operation",
			"licensing and support",
		},
		Disallowed: []string{
			"general-purpose software development",
			"politics",
			"entertainment",
		},
		SmallTalk: classify.SmallTalkAllow,
		Examples: classify.TopicExamples{
			Allowed:    []string{"Can Omnia run on OpenShift?"},
			Disallowed: []string{"Write me a Python game."},
		},
	}
}

// classifyLive runs one request liveRepeats times and returns the decision only
// when every attempt agreed. Disagreement is reported as a failure naming both
// answers, because an endpoint that flips between allow and deny on identical
// input is a finding, not a flake to paper over.
func classifyLive(
	t *testing.T, c *topiccontrol.Client, req classify.TopicRequest,
) classify.TopicDecision {
	t.Helper()

	var first classify.TopicResult
	for attempt := range liveRepeats {
		ctx, cancel := context.WithTimeout(context.Background(), liveTimeout)
		res, err := c.ClassifyTopic(ctx, req)
		cancel()
		require.NoErrorf(t, err, "attempt %d: live classifier call failed", attempt+1)

		t.Logf("attempt %d: decision=%s raw=%q", attempt+1, res.Decision, res.Raw)

		if attempt == 0 {
			first = res
			continue
		}
		require.Equalf(t, first.Decision, res.Decision,
			"the endpoint gave different answers for identical input (%q then %q) — "+
				"the model is not deterministic enough to gate a conversation on",
			first.Raw, res.Raw)
	}
	return first.Decision
}

// TestLive_ClassifiesAgainstTheRealService is the assertion the unit tests
// cannot make: a real model, asked with our real rendered policy, answers in a
// shape ParseLabel maps to the decision we expect.
//
// It asserts on the decision rather than on the raw string deliberately. The
// raw label is the backend's business; what the guardrail acts on is the
// decision, and a test pinned to the exact string would pass while the parser
// was broken.
func TestLive_ClassifiesAgainstTheRealService(t *testing.T) {
	c := liveClient(t)
	policy := livePolicy()

	cases := []struct {
		name    string
		message string
		want    classify.TopicDecision
	}{
		{"in scope", "Can Omnia run on OpenShift?", classify.TopicAllow},
		{"in scope, licensing", "What does a PromptKit support contract cover?", classify.TopicAllow},
		{"small talk is allowed by this policy", "Thanks, that helped!", classify.TopicAllow},
		{"out of scope, politics", "Who should I vote for?", classify.TopicDeny},
		{"out of scope, general development", "Write me a Python game.", classify.TopicDeny},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLive(t, c, classify.TopicRequest{Policy: policy, Message: tc.message})
			assert.Equalf(t, tc.want, got,
				"a live classifier disagreed with the policy on %q — either the model is "+
					"unsuitable, or RenderPolicy is not communicating the scope",
				tc.message)
		})
	}
}

// TestLive_ResolvesAnaphoraFromHistory covers the case the design calls out as
// the reason history is replayed at all: "What about Azure?" is meaningless in
// isolation and in scope given the turn before it.
//
// This is the case most likely to reveal a rendering problem. A model that
// judges the final message without reading the replayed turns denies it, and
// nothing else in the suite would notice.
func TestLive_ResolvesAnaphoraFromHistory(t *testing.T) {
	c := liveClient(t)

	got := classifyLive(t, c, classify.TopicRequest{
		Policy: livePolicy(),
		History: []classify.TopicTurn{
			{Role: "user", Text: "Can Omnia run on OpenShift?"},
			{Role: "assistant", Text: "Yes — on OpenShift 4.14 and later."},
		},
		Message: "What about Azure?",
	})

	assert.Equal(t, classify.TopicAllow, got,
		"an in-scope follow-up that only makes sense with history was denied — "+
			"the replayed turns are not reaching the model, or are not being read as context")
}

// TestLive_PriorContextDoesNotAuthorizeALaterSubject is the other half of the
// same rule, and the one that actually matters for safety. History must
// disambiguate a reference without licensing a new subject: several in-scope
// turns must not make an off-topic question acceptable.
//
// Without this case, a backend that simply answered "on-topic" whenever history
// was in scope would pass the anaphora test above and look correct.
func TestLive_PriorContextDoesNotAuthorizeALaterSubject(t *testing.T) {
	c := liveClient(t)

	got := classifyLive(t, c, classify.TopicRequest{
		Policy: livePolicy(),
		History: []classify.TopicTurn{
			{Role: "user", Text: "Can Omnia run on OpenShift?"},
			{Role: "assistant", Text: "Yes — on OpenShift 4.14 and later."},
			{Role: "user", Text: "And what does support cover?"},
			{Role: "assistant", Text: "Business-hours response with a 4-hour SLA."},
		},
		Message: "Great. Now recommend me a film for tonight.",
	})

	assert.Equal(t, classify.TopicDeny, got,
		"an off-topic message was allowed after in-scope history — prior context is "+
			"being read as authorization rather than as disambiguation")
}
