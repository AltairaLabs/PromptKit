package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pipeline"
)

// TestProcessStreamElements_PopulatesFinalResultCost guards the streaming-cost
// phantom: the streaming path must fold assistant-message CostInfo into the
// terminal chunk's FinalResult, so the streaming Response reports real cost/
// tokens instead of zero (the non-streaming Send() path was always correct).
func TestProcessStreamElements_PopulatesFinalResultCost(t *testing.T) {
	stageChan := make(chan stage.StreamElement, 2)
	chunkChan := make(chan providers.StreamChunk, 8)

	msg := types.Message{
		Role:     "assistant",
		Content:  "hi",
		CostInfo: &types.CostInfo{InputTokens: 11, OutputTokens: 7, TotalCost: 0.02},
	}
	stageChan <- stage.StreamElement{Message: &msg}
	close(stageChan)

	processStreamElements(context.Background(), stageChan, chunkChan)
	close(chunkChan)

	var final *rtpipeline.ExecutionResult
	for chunk := range chunkChan {
		if r, ok := chunk.FinalResult.(*rtpipeline.ExecutionResult); ok {
			final = r
		}
	}

	require.NotNil(t, final, "terminal chunk must carry a FinalResult with cost data")
	assert.Equal(t, 11, final.CostInfo.InputTokens, "input tokens must survive to the streaming Response")
	assert.Equal(t, 7, final.CostInfo.OutputTokens)
	assert.InEpsilon(t, 0.02, final.CostInfo.TotalCost, 1e-9)
}

// TestStreamProcessor_AccumulateMessage_PreservesBreakdown guards the
// session roll-up against the lossy hand-rolled-field-sum pattern: it must
// route through base.AggregateCost so Quantities/Breakdown survive (and sum
// correctly across messages), not just the flat headline fields.
func TestStreamProcessor_AccumulateMessage_PreservesBreakdown(t *testing.T) {
	p := &streamProcessor{}

	msg1 := &types.Message{
		Role: streamRoleAssistant,
		CostInfo: &types.CostInfo{
			Quantities: map[string]float64{base.UnitReasoningToken: 25},
			Breakdown: []types.CostLineItem{
				{Provider: "g", Capability: "inference", Unit: base.UnitReasoningToken, Quantity: 25, USD: 0.02},
			},
		},
	}
	msg2 := &types.Message{
		Role: streamRoleAssistant,
		CostInfo: &types.CostInfo{
			Quantities: map[string]float64{base.UnitReasoningToken: 10},
			Breakdown: []types.CostLineItem{
				{Provider: "g", Capability: "inference", Unit: base.UnitReasoningToken, Quantity: 10, USD: 0.01},
			},
		},
	}

	p.accumulateMessage(msg1)
	p.accumulateMessage(msg2)

	require.NotNil(t, p.finalResult)
	assert.Equal(t, 35.0, p.finalResult.CostInfo.Quantities[base.UnitReasoningToken],
		"quantities must sum across accumulated messages")
	require.Len(t, p.finalResult.CostInfo.Breakdown, 1, "matching lines must merge into a single breakdown row")
	assert.InEpsilon(t, 35.0, p.finalResult.CostInfo.Breakdown[0].Quantity, 1e-9)
	assert.InEpsilon(t, 0.03, p.finalResult.CostInfo.Breakdown[0].USD, 1e-9)
	assert.InEpsilon(t, 0.03, p.finalResult.CostInfo.TotalCost, 1e-9)
}

// TestStreamProcessor_AccumulateMessage_TotalCostOnlyNotDropped guards the
// same phantom as TestProcessStreamElements_PopulatesFinalResultCost but for
// a headline that carries no token buckets at all (the imagen/replay shape:
// a non-canonical Quantities unit plus a flat TotalCost, no Breakdown, no
// Input/Output/CachedCostUSD). accumulateMessage routes through
// base.AggregateCost, so this must survive the roll-up instead of silently
// zeroing out.
func TestStreamProcessor_AccumulateMessage_TotalCostOnlyNotDropped(t *testing.T) {
	p := &streamProcessor{}

	msg := &types.Message{
		Role: streamRoleAssistant,
		CostInfo: &types.CostInfo{
			Quantities: map[string]float64{"image": 1},
			TotalCost:  0.04,
		},
	}

	p.accumulateMessage(msg)

	require.NotNil(t, p.finalResult)
	assert.InEpsilon(t, 0.04, p.finalResult.CostInfo.TotalCost, 1e-9,
		"a TotalCost-only message with no buckets/Breakdown must not aggregate to $0")
}

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

	t.Run("reads history from the provided state store", func(t *testing.T) {
		store := statestore.NewMemoryStore()
		session, err := NewUnarySession(UnarySessionConfig{
			ConversationID: "conv-store",
			Pipeline:       createStoredTestPipeline(t, store, "conv-store"),
			StateStore:     store,
		})
		require.NoError(t, err)

		_, err = session.Execute(context.Background(), "user", "remember me")
		require.NoError(t, err)

		// The session reads the store it was given, which the pipeline wrote.
		msgs, err := session.Messages(context.Background())
		require.NoError(t, err)
		assert.Equal(t, []string{"user:remember me", "assistant:" + testReply}, transcript(msgs))
	})

	t.Run("seeds metadata into the state store", func(t *testing.T) {
		store := statestore.NewMemoryStore()
		_, err := NewUnarySession(UnarySessionConfig{
			ConversationID: "conv-meta",
			Pipeline:       createTestPipeline(t),
			StateStore:     store,
			UserID:         "user123",
			Metadata:       map[string]any{"key": "value"},
		})
		require.NoError(t, err)

		meta, err := store.LoadMetadata(context.Background(), "conv-meta")
		require.NoError(t, err)
		assert.Equal(t, "value", meta["key"])
	})

	t.Run("initializes with variables", func(t *testing.T) {
		session, err := NewUnarySession(UnarySessionConfig{
			Pipeline:  createTestPipeline(t),
			Variables: map[string]string{"var1": "value1"},
		})
		require.NoError(t, err)

		val, ok := session.GetVar("var1")
		assert.True(t, ok)
		assert.Equal(t, "value1", val)
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
		require.NotNil(t, result.Response)
		assert.Equal(t, "assistant", result.Response.Role)
		assert.Equal(t, testReply, result.Response.Content)
		assert.Equal(t, []string{"user:test message", "assistant:" + testReply}, transcript(result.Messages))
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
		require.NotNil(t, result.Response)
		assert.Equal(t, testReply, result.Response.Content)
		assert.Equal(t, []string{"user:test", "assistant:" + testReply}, transcript(result.Messages))
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

		// A conversation nothing has been written to yet reads as empty, not
		// as an error and not as nil.
		ctx := context.Background()
		messages, err := session.Messages(ctx)
		require.NoError(t, err)
		require.NotNil(t, messages)
		assert.Empty(t, messages)
	})
}

func TestUnarySession_Clear(t *testing.T) {
	t.Run("clear empties the persisted history", func(t *testing.T) {
		store := statestore.NewMemoryStore()
		session, err := NewUnarySession(UnarySessionConfig{
			ConversationID: "conv-clear",
			Pipeline:       createStoredTestPipeline(t, store, "conv-clear"),
			StateStore:     store,
		})
		require.NoError(t, err)

		ctx := context.Background()
		_, err = session.Execute(ctx, "user", "forget me")
		require.NoError(t, err)
		before, err := session.Messages(ctx)
		require.NoError(t, err)
		require.Len(t, before, 2, "test premise: the turn was persisted")

		require.NoError(t, session.Clear(ctx))
		after, err := session.Messages(ctx)
		require.NoError(t, err)
		assert.Empty(t, after)
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

// testReply is what the test pipeline's mock provider answers.
const testReply = "Mock response from test model test-model"

// transcript renders messages as role:text for exact comparison.
func transcript(msgs []types.Message) []string {
	out := make([]string, 0, len(msgs))
	for i := range msgs {
		out = append(out, msgs[i].Role+":"+msgs[i].GetContent())
	}
	return out
}

// createStoredTestPipeline is createTestPipeline persisting to store under convID.
func createStoredTestPipeline(t *testing.T, store statestore.Store, convID string) *stage.StreamPipeline {
	t.Helper()
	return buildTestPipeline(t, store, convID)
}

// Helper function to create a test pipeline
func createTestPipeline(t *testing.T) *stage.StreamPipeline {
	t.Helper()
	return buildTestPipeline(t, nil, "")
}

func buildTestPipeline(t *testing.T, store statestore.Store, convID string) *stage.StreamPipeline {
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
		StateStore:     store,
		ConversationID: convID,
	}

	pipe, err := pipeline.Build(cfg)
	require.NoError(t, err)
	return pipe
}

// assertStreamEndsWithReply drains stream and checks its terminal chunk: a
// "stop" finish carrying the provider's reply as the final result.
func assertStreamEndsWithReply(t *testing.T, stream <-chan providers.StreamChunk) {
	t.Helper()
	var chunks []providers.StreamChunk
	for chunk := range stream {
		require.NoError(t, chunk.Error)
		chunks = append(chunks, chunk)
	}
	require.NotEmpty(t, chunks)
	last := chunks[len(chunks)-1]
	require.NotNil(t, last.FinishReason)
	assert.Equal(t, "stop", *last.FinishReason)
	final, ok := last.FinalResult.(*rtpipeline.ExecutionResult)
	require.True(t, ok, "the terminal chunk carries the turn's result")
	require.NotNil(t, final.Response)
	assert.Equal(t, testReply, final.Response.Content)
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

	assertStreamEndsWithReply(t, stream)
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

	assertStreamEndsWithReply(t, stream)
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
	require.NotNil(t, result.Response)
	assert.Equal(t, testReply, result.Response.Content)
	// The tool result is part of the resumed turn, ahead of the reply.
	require.NotEmpty(t, result.Messages)
	assert.Equal(t, "tool", result.Messages[0].Role)
	require.NotNil(t, result.Messages[0].ToolResult)
	assert.Equal(t, "call-1", result.Messages[0].ToolResult.ID)
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

	// Send a text element, then a pending_tools marker element
	text := "I need your location."
	stageChan <- stage.StreamElement{Text: &text}
	pendingElem := stage.StreamElement{}
	pendingElem.Meta.PendingTools = pendingTools
	stageChan <- pendingElem
	close(stageChan)

	// Use convertStreamOutput which properly wraps processStreamElements with close
	chunkChan := convertStreamOutput(context.Background(), stageChan)

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

	chunkChan := convertStreamOutput(context.Background(), stageChan)

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

// shortCanceledSendTimeout shrinks the post-cancel send wait for a test.
func shortCanceledSendTimeout(t *testing.T) {
	t.Helper()
	prev := canceledChunkSendTimeout
	canceledChunkSendTimeout = 5 * time.Millisecond
	t.Cleanup(func() { canceledChunkSendTimeout = prev })
}

// TestStreamProcessor_SendChunk_CanceledConsumerGone checks a canceled stream
// with nobody reading gives up rather than blocking, and stops sending.
func TestStreamProcessor_SendChunk_CanceledConsumerGone(t *testing.T) {
	shortCanceledSendTimeout(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	chunkChan := make(chan providers.StreamChunk) // unbuffered, never read
	p := &streamProcessor{ctx: ctx, chunkChan: chunkChan}

	p.sendChunk(&providers.StreamChunk{Delta: "test"})
	assert.True(t, p.consumerGone, "an unread send after cancellation must mark the consumer gone")
}

// TestStreamProcessor_SendChunk_CanceledStillDelivers checks a caller that
// canceled but keeps reading still receives the chunk.
func TestStreamProcessor_SendChunk_CanceledStillDelivers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	chunkChan := make(chan providers.StreamChunk)
	p := &streamProcessor{ctx: ctx, chunkChan: chunkChan}

	got := make(chan providers.StreamChunk, 1)
	go func() { got <- <-chunkChan }()
	p.sendChunk(&providers.StreamChunk{Delta: "test"})

	assert.Equal(t, "test", (<-got).Delta)
	assert.False(t, p.consumerGone)
}

func TestStreamProcessor_ProcessElement_Error(t *testing.T) {
	chunkChan := make(chan providers.StreamChunk, 10)
	p := &streamProcessor{ctx: context.Background(), chunkChan: chunkChan}

	testErr := errors.New("stream error")
	p.processElement(&stage.StreamElement{Error: testErr})

	chunk := <-chunkChan
	assert.Equal(t, testErr, chunk.Error)
	require.NotNil(t, chunk.FinishReason)
	assert.Equal(t, "error", *chunk.FinishReason)
}

// TestProcessStreamElements_CanceledDeliversErrorAndPartialReply checks the
// elements a canceled turn emits last — its partial reply and its error —
// still reach a caller that keeps reading.
func TestProcessStreamElements_CanceledDeliversErrorAndPartialReply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	partial := &types.Message{Role: "assistant", Content: "half", FinishReason: types.FinishReasonInterrupted}
	stageChan := make(chan stage.StreamElement, 3)
	stageChan <- stage.StreamElement{Message: partial}
	stageChan <- stage.StreamElement{Error: context.Canceled}
	close(stageChan)

	chunkChan := make(chan providers.StreamChunk)
	go func() {
		defer close(chunkChan)
		processStreamElements(ctx, stageChan, chunkChan)
	}()

	var chunks []providers.StreamChunk
	for c := range chunkChan {
		chunks = append(chunks, c)
	}
	require.Len(t, chunks, 2, "want the error chunk and the final chunk")
	assert.ErrorIs(t, chunks[0].Error, context.Canceled)
	final, ok := chunks[1].FinalResult.(*rtpipeline.ExecutionResult)
	require.True(t, ok, "final chunk carries no result")
	require.Len(t, final.Messages, 1)
	assert.True(t, final.Messages[0].IsInterrupted())
}

// TestProcessStreamElements_CanceledNoReaderReturns checks an abandoned stream
// still drains its input and returns instead of hanging.
func TestProcessStreamElements_CanceledNoReaderReturns(t *testing.T) {
	shortCanceledSendTimeout(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stageChan := make(chan stage.StreamElement, 5)
	text := "hello"
	stageChan <- stage.StreamElement{Text: &text}
	stageChan <- stage.StreamElement{Text: &text}
	close(stageChan)

	chunkChan := make(chan providers.StreamChunk) // never read
	processStreamElements(ctx, stageChan, chunkChan)
	assert.Empty(t, stageChan, "input must be drained")
}
