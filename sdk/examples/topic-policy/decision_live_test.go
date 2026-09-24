package main

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// The same guardrail, served by typed-decision backends instead of NemoGuard:
// the pack is unchanged, only the host's provider binding differs.
//
// SystemOne runs against any server speaking the protocol, e.g. a local simple-jev:
//
//	SYSTEMONE_BASE_URL=http://127.0.0.1:8766/v1 SYSTEMONE_MODEL=Qwen/Qwen3.5-4B go test -run SystemOne ./...
//
// or real Jev through Vercel AI Gateway:
//
//	SYSTEMONE_BASE_URL=https://ai-gateway.vercel.sh/typesafe/v1 SYSTEMONE_MODEL=typesafe-ai/jev \
//	SYSTEMONE_CREDENTIAL_ENV=AI_GATEWAY_API_KEY go test -run SystemOne ./...
//
// Logprobs runs against OpenAI: LOGPROBS_LIVE=1 OPENAI_API_KEY=... go test -run Logprobs ./...
func openSystemOneDemo(t *testing.T) (*sdk.Conversation, *countingProvider) {
	t.Helper()
	baseURL := os.Getenv("SYSTEMONE_BASE_URL")
	if baseURL == "" {
		t.Skip("SYSTEMONE_BASE_URL not set")
	}
	spec := sdk.ProviderSpec{
		ID: "topic-control", Type: "systemone", BaseURL: baseURL, Model: os.Getenv("SYSTEMONE_MODEL"),
		AdditionalConfig: map[string]any{"timeout_seconds": 300},
	}
	// A hosted endpoint names the env var holding its key, e.g. AI_GATEWAY_API_KEY.
	if env := os.Getenv("SYSTEMONE_CREDENTIAL_ENV"); env != "" {
		spec.Credential = &credentials.CredentialConfig{CredentialEnv: env}
	}
	return openDecisionDemo(t, spec)
}

func openLogprobsDemo(t *testing.T) (*sdk.Conversation, *countingProvider) {
	t.Helper()
	if os.Getenv("LOGPROBS_LIVE") == "" || os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("set LOGPROBS_LIVE=1 and OPENAI_API_KEY")
	}
	return openDecisionDemo(t, sdk.ProviderSpec{ID: "topic-control", Type: "openai", Model: "gpt-4.1-mini"})
}

func openDecisionDemo(t *testing.T, spec sdk.ProviderSpec) (*sdk.Conversation, *countingProvider) {
	t.Helper()
	provider := &countingProvider{
		Provider: mock.NewProviderWithRepository("mock", "mock-model", false,
			mock.NewInMemoryMockRepository("Here's what I can tell you about that.")),
	}
	conv, err := sdk.Open(packPath, "support",
		sdk.WithProvider(provider),
		sdk.WithInferenceProvider(spec),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv, provider
}

func TestSystemOneLive_InScopeTurnReachesTheAgent(t *testing.T) {
	conv, provider := openSystemOneDemo(t)

	resp, err := conv.Send(context.Background(), "Can Omnia run on OpenShift?")

	require.NoError(t, err)
	assert.Equal(t, 1, provider.callCount())
	assert.Equal(t, "Here's what I can tell you about that.", resp.Text())
}

func TestSystemOneLive_OutOfScopeTurnNeverReachesTheAgent(t *testing.T) {
	conv, provider := openSystemOneDemo(t)

	resp, err := conv.Send(context.Background(), "Who should I vote for?")

	require.NoError(t, err)
	assert.Equal(t, 0, provider.callCount())
	assert.Equal(t,
		"I can only help with AltairaLabs products — Omnia, PromptKit, licensing and support.",
		resp.Text())
}

func TestLogprobsLive_InScopeTurnReachesTheAgent(t *testing.T) {
	conv, provider := openLogprobsDemo(t)

	resp, err := conv.Send(context.Background(), "Can Omnia run on OpenShift?")

	require.NoError(t, err)
	assert.Equal(t, 1, provider.callCount())
	assert.Equal(t, "Here's what I can tell you about that.", resp.Text())
}

func TestLogprobsLive_OutOfScopeTurnNeverReachesTheAgent(t *testing.T) {
	conv, provider := openLogprobsDemo(t)

	resp, err := conv.Send(context.Background(), "Who should I vote for?")

	require.NoError(t, err)
	assert.Equal(t, 0, provider.callCount())
	assert.Equal(t,
		"I can only help with AltairaLabs products — Omnia, PromptKit, licensing and support.",
		resp.Text())
}
