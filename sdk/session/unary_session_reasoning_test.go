package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pipeline"
)

// reasoningSessionRepo answers with a reply its model reasons about first.
type reasoningSessionRepo struct{}

func (reasoningSessionRepo) GetTurn(context.Context, mock.ResponseParams) (*mock.Turn, error) {
	return &mock.Turn{Type: "text", ReasoningChunks: []string{"Think", "ing."}, StreamChunks: []string{"Hi."}}, nil
}

func (reasoningSessionRepo) GetResponse(context.Context, mock.ResponseParams) (string, error) {
	return "Hi.", nil
}

// streamReasoningTurn streams one reasoning turn through a session opened
// with streamReasoning, returning the chunks that carry reasoning or text.
func streamReasoningTurn(t *testing.T, streamReasoning bool) []reasoningChunkSeen {
	t.Helper()
	repo := memory.NewPromptRepository()
	repo.RegisterPrompt("chat", &prompt.Config{
		APIVersion: "promptkit.io/v1alpha1",
		Kind:       "Prompt",
		Spec:       prompt.Spec{TaskType: "chat", SystemTemplate: "You are helpful"},
	})
	pipe, err := pipeline.Build(&pipeline.Config{
		PromptRegistry: prompt.NewRegistryWithRepository(repo),
		TaskType:       "chat",
		Provider:       mock.NewProviderWithRepository("test", "test-model", false, reasoningSessionRepo{}),
	})
	require.NoError(t, err)
	session, err := NewUnarySession(UnarySessionConfig{Pipeline: pipe, StreamReasoning: streamReasoning})
	require.NoError(t, err)

	stream, err := session.ExecuteStream(context.Background(), "user", "Hello")
	require.NoError(t, err)

	var chunks []reasoningChunkSeen
	for c := range stream {
		require.NoError(t, c.Error)
		if c.Reasoning != "" || c.Delta != "" {
			chunks = append(chunks, reasoningChunkSeen{Reasoning: c.Reasoning, Delta: c.Delta})
		}
	}
	return chunks
}

type reasoningChunkSeen struct{ Reasoning, Delta string }

// With StreamReasoning, a streamed turn's reasoning reaches the caller as
// chunks carrying only Reasoning, ahead of the reply's text, never mixed into
// its Delta.
func TestUnarySession_StreamsReasoningApartFromTheText(t *testing.T) {
	assert.Equal(t, []reasoningChunkSeen{{Reasoning: "Think"}, {Reasoning: "ing."}, {Delta: "Hi."}},
		streamReasoningTurn(t, true))
}

// By default the stream carries the reply's text alone.
func TestUnarySession_DoesNotStreamReasoningByDefault(t *testing.T) {
	assert.Equal(t, []reasoningChunkSeen{{Delta: "Hi."}}, streamReasoningTurn(t, false))
}
