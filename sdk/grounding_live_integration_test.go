//go:build integration

package sdk_test

// End-to-end coverage for ambient grounding (#1958) against real providers.
//
// The mock-provider tests assert the rendered system prompt, which is the right
// oracle for the ordering bug — but a prompt that looks correct at the provider
// boundary is not proof a model received and used it. These tests close that
// gap: the corpus holds a fact no model can know, and the answer is checked for
// it.
//
// Each case runs twice. Without a retriever the model must NOT produce the
// fact — that negative control is what makes the positive meaningful, since a
// model guessing plausibly is indistinguishable from a model grounded.
//
// Run:
//
//	ANTHROPIC_API_KEY=... OPENAI_API_KEY=... GEMINI_API_KEY=... \
//	  go test -tags integration ./sdk/ -run TestLive_Grounding -v

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/memory/corpus"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// groundingPackJSON consumes memory_context in its system prompt and says
// nothing about the subject, so an ungrounded model has only its priors.
const groundingPackJSON = `{
	"id": "live-grounding",
	"version": "1.0.0",
	"description": "Live ambient grounding",
	"prompts": {
		"support": {
			"id": "support",
			"name": "Support",
			"system_template": "You are a support agent for Kestrel Tools. Answer only from the reference material below. If it does not cover the question, say you do not know.\n\nReference material:\n{{memory_context}}",
			"variables": []
		}
	}
}`

// The grounded fact. 37 days is deliberately implausible: no model returns it
// from priors, and a wrong-but-reasonable guess (14, 30) fails the assertion
// rather than passing by luck.
const (
	groundedAnswer    = "37"
	groundingQuestion = "How many days do I have to return a faulty drill?"
)

func groundingDocs() []corpus.Document {
	return []corpus.Document{
		{
			ID:    "returns",
			Title: "Returns policy",
			Text:  "Kestrel Tools accepts returns of faulty drills for 37 days from delivery.",
		},
		{
			ID:    "warranty",
			Title: "Warranty",
			Text:  "Kestrel Tools power tools carry a two year parts and labour warranty.",
		},
	}
}

func TestLive_GroundingReachesTheModel(t *testing.T) {
	for _, c := range liveSDKCases() {
		t.Run(c.name, func(t *testing.T) {
			requireLiveKey(t, c)

			grounded := askGroundingQuestion(t, c, true)
			t.Logf("grounded reply: %s", grounded)
			assert.Contains(t, grounded, groundedAnswer,
				"want the corpus fact in the reply; the model cannot know it otherwise")
		})
	}
}

// TestLive_GroundingIsAbsentWithoutARetriever is the negative control. If a
// model produces "37" here, the positive test above proves nothing.
func TestLive_GroundingIsAbsentWithoutARetriever(t *testing.T) {
	for _, c := range liveSDKCases() {
		t.Run(c.name, func(t *testing.T) {
			requireLiveKey(t, c)

			ungrounded := askGroundingQuestion(t, c, false)
			t.Logf("ungrounded reply: %s", ungrounded)
			assert.NotContains(t, ungrounded, groundedAnswer,
				"the fact is only available through the corpus")
		})
	}
}

// TestLive_GroundingFollowsTheConversation covers the multi-turn case: the
// system prompt used to render once per conversation, so a second turn was
// answered with the first turn's retrieval (#1959).
func TestLive_GroundingFollowsTheConversation(t *testing.T) {
	for _, c := range liveSDKCases() {
		t.Run(c.name, func(t *testing.T) {
			requireLiveKey(t, c)

			provider, err := providers.CreateProviderFromSpec(c.spec(c.model))
			require.NoError(t, err)

			dir := t.TempDir()
			packPath := dir + "/grounding.pack.json"
			require.NoError(t, os.WriteFile(packPath, []byte(groundingPackJSON), 0o644))

			conv, err := sdk.Open(packPath, "support",
				sdk.WithProvider(provider),
				sdk.WithSkipSchemaValidation(),
				sdk.WithRetriever(corpus.New(groundingDocs())),
			)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conv.Close() })

			first, err := conv.Send(context.Background(), groundingQuestion)
			require.NoError(t, err)
			t.Logf("turn 1: %s", strings.TrimSpace(first.Text()))
			require.Contains(t, first.Text(), groundedAnswer)

			// A different question, answered by the other document.
			second, err := conv.Send(context.Background(), "How long is the warranty?")
			require.NoError(t, err)
			t.Logf("turn 2: %s", strings.TrimSpace(second.Text()))
			assert.Contains(t, strings.ToLower(second.Text()), "two year",
				"turn two must be grounded in its own retrieval, not turn one's")
		})
	}
}

// askGroundingQuestion opens a conversation on the grounding pack, optionally
// wired to the corpus, and returns the model's reply.
func askGroundingQuestion(t *testing.T, c liveSDKCase, withRetriever bool) string {
	t.Helper()

	provider, err := providers.CreateProviderFromSpec(c.spec(c.model))
	require.NoError(t, err)

	dir := t.TempDir()
	packPath := dir + "/grounding.pack.json"
	require.NoError(t, os.WriteFile(packPath, []byte(groundingPackJSON), 0o644))

	opts := []sdk.Option{
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
	}
	if withRetriever {
		// No store, no scope: grounding is configured on its own.
		opts = append(opts, sdk.WithRetriever(corpus.New(groundingDocs())))
	}

	conv, err := sdk.Open(packPath, "support", opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), groundingQuestion)
	require.NoError(t, err)

	return strings.TrimSpace(resp.Text())
}
