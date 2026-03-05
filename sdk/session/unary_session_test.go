package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
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
			Metadata: map[string]any{
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

func TestConvertExecutionResult(t *testing.T) {
	t.Run("copies Parts from stage response", func(t *testing.T) {
		parts := []types.ContentPart{
			types.NewTextPart("Hello"),
			types.NewTextPart("World"),
		}
		stageResult := &stage.ExecutionResult{
			Response: &stage.Response{
				Role:    "assistant",
				Content: "Hello World",
				Parts:   parts,
				ToolCalls: []types.MessageToolCall{
					{ID: "call1", Name: "test_tool"},
				},
			},
		}

		result := convertExecutionResult(stageResult)
		require.NotNil(t, result.Response)
		assert.Equal(t, "assistant", result.Response.Role)
		assert.Equal(t, "Hello World", result.Response.Content)
		assert.Len(t, result.Response.Parts, 2)
		assert.Equal(t, "text", result.Response.Parts[0].Type)
		assert.Equal(t, "text", result.Response.Parts[1].Type)
		assert.Len(t, result.Response.ToolCalls, 1)
	})

	t.Run("nil response", func(t *testing.T) {
		stageResult := &stage.ExecutionResult{
			Response: nil,
		}

		result := convertExecutionResult(stageResult)
		assert.Nil(t, result.Response)
	})
}

// Helper function to create a test pipeline
func createTestPipeline(t *testing.T) *stage.StreamPipeline {
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

func TestUnarySession_ResumeWithToolResults(t *testing.T) {
	pipe := createTestPipeline(t)

	cfg := UnarySessionConfig{
		ConversationID: "resume-session",
		Pipeline:       pipe,
	}

	sess, err := NewUnarySession(cfg)
	require.NoError(t, err)

	// First execute to get the conversation started
	_, err = sess.Execute(context.Background(), "user", "Hello")
	require.NoError(t, err)

	// Resume with tool results — sends tool result messages through pipeline
	toolResults := []types.Message{
		types.NewToolResultMessage(
			types.NewTextToolResult("call-1", "", `{"lat": 37.7749}`),
		),
	}

	result, err := sess.ResumeWithToolResults(context.Background(), toolResults)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Response)
}

func TestProcessStreamElements_PropagatesPendingTools(t *testing.T) {
	stageChan := make(chan stage.StreamElement, 5)

	pendingTools := []tools.PendingToolExecution{
		{
			CallID:   "call-1",
			ToolName: "get_location",
			Args:     map[string]any{"accuracy": "fine"},
			PendingInfo: &tools.PendingToolInfo{
				Message: "Allow location?",
			},
		},
	}

	// Send a text element, then a pending_tools element
	text := "I need your location."
	stageChan <- stage.StreamElement{Text: &text}
	stageChan <- stage.StreamElement{
		Metadata: map[string]any{
			"pending_tools": pendingTools,
		},
	}
	close(stageChan)

	// Use convertStreamOutput which properly wraps processStreamElements with close
	chunkChan := convertStreamOutput(stageChan)

	var chunks []providers.StreamChunk
	for chunk := range chunkChan {
		chunks = append(chunks, chunk)
	}

	// Should have text chunk + pending_tools chunk (no "stop" chunk)
	require.Len(t, chunks, 2)

	// First: text delta
	assert.Equal(t, "I need your location.", chunks[0].Delta)
	assert.Nil(t, chunks[0].FinishReason)

	// Second: pending_tools
	require.NotNil(t, chunks[1].FinishReason)
	assert.Equal(t, "pending_tools", *chunks[1].FinishReason)
	require.Len(t, chunks[1].PendingTools, 1)
	assert.Equal(t, "call-1", chunks[1].PendingTools[0].CallID)
	assert.Equal(t, "get_location", chunks[1].PendingTools[0].ToolName)
}

func TestProcessStreamElements_NoPendingTools(t *testing.T) {
	stageChan := make(chan stage.StreamElement, 5)

	text := "Hello world"
	stageChan <- stage.StreamElement{Text: &text}
	close(stageChan)

	chunkChan := convertStreamOutput(stageChan)

	var chunks []providers.StreamChunk
	for chunk := range chunkChan {
		chunks = append(chunks, chunk)
	}

	// Should have text chunk + "stop" final chunk
	require.Len(t, chunks, 2)
	assert.Equal(t, "Hello world", chunks[0].Delta)
	require.NotNil(t, chunks[1].FinishReason)
	assert.Equal(t, "stop", *chunks[1].FinishReason)
	assert.Empty(t, chunks[1].PendingTools)
}

func TestUnarySession_ResumeStreamWithToolResults(t *testing.T) {
	pipe := createTestPipeline(t)

	cfg := UnarySessionConfig{
		ConversationID: "resume-stream-session",
		Pipeline:       pipe,
	}

	sess, err := NewUnarySession(cfg)
	require.NoError(t, err)

	// First execute to get the conversation started
	_, err = sess.Execute(context.Background(), "user", "Hello")
	require.NoError(t, err)

	// Resume with tool results in streaming mode
	toolResults := []types.Message{
		types.NewToolResultMessage(
			types.NewTextToolResult("call-1", "", `{"lat": 37.7749}`),
		),
	}

	stream, err := sess.ResumeStreamWithToolResults(context.Background(), toolResults)
	require.NoError(t, err)
	assert.NotNil(t, stream)

	// Consume stream
	var chunks []providers.StreamChunk
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}

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
