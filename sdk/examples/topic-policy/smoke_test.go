package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

func openDemo(t *testing.T) (*sdk.Conversation, *countingProvider) {
	t.Helper()
	provider := &countingProvider{
		Provider: mock.NewProviderWithRepository("mock", "mock-model", false,
			mock.NewInMemoryMockRepository("Here's what I can tell you about that.")),
	}
	conv, err := sdk.Open(packPath, "support",
		sdk.WithProvider(provider),
		sdk.WithClassifier("topic-control", keywordClassifier{}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv, provider
}

// TestInScopeTurnReachesTheAgent is the control for the test below: without it,
// a guardrail that blocked everything would pass the denial test.
func TestInScopeTurnReachesTheAgent(t *testing.T) {
	conv, provider := openDemo(t)

	resp, err := conv.Send(context.Background(), "Can Omnia run on OpenShift?")

	require.NoError(t, err)
	assert.Equal(t, 1, provider.callCount(), "an in-scope turn must reach the agent")
	assert.Equal(t, "Here's what I can tell you about that.", resp.Text())
}

// TestOutOfScopeTurnNeverReachesTheAgent is the property the whole feature
// exists for: the off-topic answer is not generated and then suppressed, it is
// never generated.
func TestOutOfScopeTurnNeverReachesTheAgent(t *testing.T) {
	conv, provider := openDemo(t)

	resp, err := conv.Send(context.Background(), "Who should I vote for?")

	require.NoError(t, err, "an enforced guardrail is not an error")
	assert.Equal(t, 0, provider.callCount(), "a denied turn must not reach the agent")
	assert.Equal(t,
		"I can only help with AltairaLabs products — Omnia, PromptKit, licensing and support.",
		resp.Text(),
		"the denied turn must carry the validator's own message, not the generic default")
}
