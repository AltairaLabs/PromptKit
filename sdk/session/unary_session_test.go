package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pipeline"
)

func TestNewUnarySession(t *testing.T) {
	t.Run("creates session successfully", func(t *testing.T) {
		pipe := createTestPipeline(t)

		cfg := UnarySessionConfig{
			Pipeline: pipe,
		}

		session, err := NewUnarySession(cfg)
		require.NoError(t, err)
		assert.NotNil(t, session)
		assert.NotEmpty(t, session.ID())
	})

	t.Run("requires pipeline", func(t *testing.T) {
		cfg := UnarySessionConfig{}

		session, err := NewUnarySession(cfg)
		assert.Error(t, err)
		assert.Nil(t, session)
		assert.Contains(t, err.Error(), "pipeline is required")
	})

	t.Run("uses provided conversation ID", func(t *testing.T) {
		pipe := createTestPipeline(t)

		cfg := UnarySessionConfig{
			ConversationID: "test-conversation",
			Pipeline:       pipe,
		}

		session, err := NewUnarySession(cfg)
		require.NoError(t, err)
		assert.Equal(t, "test-conversation", session.ID())
	})

	t.Run("uses provided state store", func(t *testing.T) {
		pipe := createTestPipeline(t)
		store := statestore.NewMemoryStore()

		cfg := UnarySessionConfig{
			Pipeline:   pipe,
			StateStore: store,
		}

		session, err := NewUnarySession(cfg)
		require.NoError(t, err)
		assert.NotNil(t, session)
	})

	t.Run("initializes with metadata", func(t *testing.T) {
		pipe := createTestPipeline(t)

		cfg := UnarySessionConfig{
			Pipeline: pipe,
			UserID:   "user123",
			Metadata: map[string]interface{}{
				"key": "value",
			},
		}

		session, err := NewUnarySession(cfg)
		require.NoError(t, err)
		assert.NotNil(t, session)
	})

	t.Run("initializes with variables", func(t *testing.T) {
		pipe := createTestPipeline(t)

		cfg := UnarySessionConfig{
			Pipeline: pipe,
			Variables: map[string]string{
				"var1": "value1",
			},
		}

		session, err := NewUnarySession(cfg)
		require.NoError(t, err)
		assert.NotNil(t, session)
	})
}

func TestUnarySession_Execute(t *testing.T) {
	t.Run("executes successfully", func(t *testing.T) {
		pipe := createTestPipeline(t)

		cfg := UnarySessionConfig{
			Pipeline: pipe,
		}

		session, err := NewUnarySession(cfg)
		require.NoError(t, err)

		ctx := context.Background()
		result, err := session.Execute(ctx, "user", "test message")
		require.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestUnarySession_ExecuteWithMessage(t *testing.T) {
	t.Run("executes with message", func(t *testing.T) {
		pipe := createTestPipeline(t)

		cfg := UnarySessionConfig{
			Pipeline: pipe,
		}

		session, err := NewUnarySession(cfg)
		require.NoError(t, err)

		ctx := context.Background()
		msg := types.Message{
			Role:    "user",
			Content: "test",
		}

		result, err := session.ExecuteWithMessage(ctx, msg)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestUnarySession_SetVar(t *testing.T) {
	t.Run("sets and gets variable", func(t *testing.T) {
		pipe := createTestPipeline(t)

		cfg := UnarySessionConfig{
			Pipeline: pipe,
		}

		session, err := NewUnarySession(cfg)
		require.NoError(t, err)

		session.SetVar("key", "value")
		val, ok := session.GetVar("key")
		assert.True(t, ok)
		assert.Equal(t, "value", val)
	})
}

func TestUnarySession_Variables(t *testing.T) {
	t.Run("gets all variables", func(t *testing.T) {
		pipe := createTestPipeline(t)

		cfg := UnarySessionConfig{
			Pipeline: pipe,
			Variables: map[string]string{
				"var1": "value1",
			},
		}

		session, err := NewUnarySession(cfg)
		require.NoError(t, err)

		vars := session.Variables()
		assert.Contains(t, vars, "var1")
	})
}

func TestUnarySession_Messages(t *testing.T) {
	t.Run("gets messages successfully", func(t *testing.T) {
		pipe := createTestPipeline(t)

		cfg := UnarySessionConfig{
			ConversationID: "test-conv",
			Pipeline:       pipe,
		}

		session, err := NewUnarySession(cfg)
		require.NoError(t, err)

		ctx := context.Background()
		messages, err := session.Messages(ctx)
		require.NoError(t, err)
		assert.NotNil(t, messages)
	})
}

func TestUnarySession_Clear(t *testing.T) {
	t.Run("clears successfully", func(t *testing.T) {
		pipe := createTestPipeline(t)

		cfg := UnarySessionConfig{
			Pipeline: pipe,
		}

		session, err := NewUnarySession(cfg)
		require.NoError(t, err)

		ctx := context.Background()
		err = session.Clear(ctx)
		require.NoError(t, err)
	})
}

// Helper function to create a test pipeline
func createTestPipeline(t *testing.T) *rtpipeline.Pipeline {
	t.Helper()

	// Create prompt registry
	repo := memory.NewPromptRepository()
	repo.RegisterPrompt("chat", &prompt.Config{
		APIVersion: "promptkit.io/v1alpha1",
		Kind:       "Prompt",
		Spec: prompt.Spec{
			TaskType:       "chat",
			SystemTemplate: "You are helpful",
		},
	})
	registry := prompt.NewRegistryWithRepository(repo)

	// Create pipeline
	cfg := &pipeline.Config{
		PromptRegistry: registry,
		TaskType:       "chat",
		Provider:       mock.NewProvider("test", "test-model", false),
	}

	pipe, err := pipeline.Build(cfg)
	require.NoError(t, err)
	return pipe
}

func TestUnarySession_ExecuteStream(t *testing.T) {
	pipe := createTestPipeline(t)

	cfg := UnarySessionConfig{
		Pipeline: pipe,
	}

	session, err := NewUnarySession(cfg)
	require.NoError(t, err)

	// Execute stream
	stream, err := session.ExecuteStream(context.Background(), "user", "Hello")
	require.NoError(t, err)
	assert.NotNil(t, stream)

	// Consume stream
	var chunks []providers.StreamChunk
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}

	// Should have received chunks
	assert.NotEmpty(t, chunks)
}

func TestUnarySession_ExecuteStreamWithMessage(t *testing.T) {
	pipe := createTestPipeline(t)

	cfg := UnarySessionConfig{
		Pipeline: pipe,
	}

	session, err := NewUnarySession(cfg)
	require.NoError(t, err)

	// Create message
	msg := types.Message{Role: "user", Content: "Test message"}

	// Execute stream with message
	stream, err := session.ExecuteStreamWithMessage(context.Background(), msg)
	require.NoError(t, err)
	assert.NotNil(t, stream)

	// Consume stream
	var chunks []providers.StreamChunk
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}

	// Should have received chunks
	assert.NotEmpty(t, chunks)
}

func TestUnarySession_ForkSession(t *testing.T) {
	pipe := createTestPipeline(t)

	cfg := UnarySessionConfig{
		ConversationID: "original-session",
		Pipeline:       pipe,
	}

	session, err := NewUnarySession(cfg)
	require.NoError(t, err)

	// Execute to create some state
	_, err = session.Execute(context.Background(), "user", "Hello")
	require.NoError(t, err)

	// Fork the session
	forkedSession, err := session.ForkSession(context.Background(), "forked-session", pipe)
	require.NoError(t, err)
	assert.NotNil(t, forkedSession)

	// Verify forked session has same messages
	origMessages, err := session.Messages(context.Background())
	require.NoError(t, err)

	forkedMessages, err := forkedSession.Messages(context.Background())
	require.NoError(t, err)

	assert.Equal(t, len(origMessages), len(forkedMessages))
}
