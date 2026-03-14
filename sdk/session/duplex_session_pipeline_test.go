package session

import (
	"context"
	"testing"
	"time"

	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBidirectionalSession_PipelineMode tests the Pipeline mode
func TestBidirectionalSession_PipelineMode(t *testing.T) {
	ctx := context.Background()

	t.Run("returns streamOutput in Pipeline mode", func(t *testing.T) {
		// Create minimal pipeline with mock provider
		provider := mock.NewProvider("mock", "mock-model", false)
		pipelineBuilder := func(ctx context.Context, p providers.Provider, streamProvider providers.StreamInputSupport, streamConfig *providers.StreamingInputConfig, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			builder := stage.NewPipelineBuilder()
			return builder.Chain(providerStage).Build()
		}

		// Create session
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			PipelineBuilder: pipelineBuilder,
			Provider:        provider,
		})
		require.NoError(t, err)

		// Response should return streamOutput channel
		responseChan := session.Response()
		assert.NotNil(t, responseChan)

		// Close session
		err = session.Close()
		assert.NoError(t, err)
	})

	t.Run("Error() returns nil in Pipeline mode", func(t *testing.T) {
		// Create minimal pipeline
		provider := mock.NewProvider("mock", "mock-model", false)
		pipelineBuilder := func(ctx context.Context, p providers.Provider, streamProvider providers.StreamInputSupport, streamConfig *providers.StreamingInputConfig, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			builder := stage.NewPipelineBuilder()
			return builder.Chain(providerStage).Build()
		}

		// Create session
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			PipelineBuilder: pipelineBuilder,
			Provider:        provider,
		})
		require.NoError(t, err)

		// Error should return nil in Pipeline mode (errors sent as chunks)
		assert.Nil(t, session.Error())

		// Close session
		err = session.Close()
		assert.NoError(t, err)
	})

	t.Run("Close() closes streamInput in Pipeline mode", func(t *testing.T) {
		// Create minimal pipeline
		provider := mock.NewProvider("mock", "mock-model", false)
		pipelineBuilder := func(ctx context.Context, p providers.Provider, streamProvider providers.StreamInputSupport, streamConfig *providers.StreamingInputConfig, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			builder := stage.NewPipelineBuilder()
			return builder.Chain(providerStage).Build()
		}

		// Create session
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			PipelineBuilder: pipelineBuilder,
			Provider:        provider,
		})
		require.NoError(t, err)

		// Send a chunk
		ctx := context.Background()
		err = session.SendText(ctx, "test message")
		require.NoError(t, err)

		// Wait a bit for pipeline to start
		time.Sleep(100 * time.Millisecond)

		// Close should work
		err = session.Close()
		assert.NoError(t, err)

		// Second close should be idempotent
		err = session.Close()
		assert.NoError(t, err)
	})

	t.Run("executes pipeline when chunk is sent", func(t *testing.T) {
		// Create pipeline with mock provider
		provider := mock.NewProviderWithRepository("mock", "mock-model", false,
			mock.NewInMemoryMockRepository("Hello from mock provider"))

		pipelineBuilder := func(ctx context.Context, p providers.Provider, streamProvider providers.StreamInputSupport, streamConfig *providers.StreamingInputConfig, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			builder := stage.NewPipelineBuilder()
			return builder.Chain(providerStage).Build()
		}

		// Create session
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			PipelineBuilder: pipelineBuilder,
			Provider:        provider,
		})
		require.NoError(t, err)

		// Send a text chunk
		ctx := context.Background()
		err = session.SendText(ctx, "test message")
		require.NoError(t, err)

		// Get response channel
		responseChan := session.Response()
		require.NotNil(t, responseChan)

		// Wait for chunks (with timeout)
		timeout := time.After(2 * time.Second)
		receivedChunks := 0

		done := false
		for !done {
			select {
			case chunk, ok := <-responseChan:
				if !ok {
					// Channel closed
					done = true
					break
				}
				receivedChunks++
				t.Logf("Received chunk %d: delta=%s, finish=%v, error=%v", receivedChunks, chunk.Delta, chunk.FinishReason, chunk.Error)

				// Break if final chunk
				if chunk.FinishReason != nil || chunk.Error != nil {
					done = true
				}
			case <-timeout:
				t.Log("timeout waiting for response")
				done = true
			}
		}

		// We should have received at least one chunk
		// Note: The mock provider should send chunks
		t.Logf("Total chunks received: %d", receivedChunks)

		// Close session
		err = session.Close()
		assert.NoError(t, err)
	})
}

func TestDuplexSession_HandleToolCalls(t *testing.T) {
	// Helper to create a minimal duplexSession with channels for testing handleToolCalls.
	makeSession := func(t *testing.T, registry *tools.Registry) *duplexSession {
		t.Helper()
		return &duplexSession{
			id:           "test",
			toolRegistry: registry,
			stageInput:   make(chan stage.StreamElement, 10),
			streamOutput: make(chan providers.StreamChunk, 10),
		}
	}

	t.Run("no registry forwards element as-is", func(t *testing.T) {
		s := makeSession(t, nil)
		elem := &stage.StreamElement{
			EndOfStream: true,
			Message: &types.Message{
				ToolCalls: []types.MessageToolCall{
					{ID: "c1", Name: "some_tool", Args: json.RawMessage(`{}`)},
				},
			},
		}

		err := s.handleToolCalls(context.Background(), elem)
		require.NoError(t, err)

		// Should have forwarded the chunk to streamOutput
		select {
		case chunk := <-s.streamOutput:
			assert.Nil(t, chunk.Error)
		default:
			t.Fatal("expected chunk on streamOutput")
		}
	})

	t.Run("all completed sends results to stageInput", func(t *testing.T) {
		reg := makeRegistry(&syncTestExecutor{result: json.RawMessage(`{"ok":true}`)})
		registerTool(t, reg, "sync_tool", "test")

		s := makeSession(t, reg)
		elem := &stage.StreamElement{
			EndOfStream: true,
			Message: &types.Message{
				ToolCalls: []types.MessageToolCall{
					{ID: "c1", Name: "sync_tool", Args: json.RawMessage(`{}`)},
				},
			},
		}

		err := s.handleToolCalls(context.Background(), elem)
		require.NoError(t, err)

		// Tool results sent to stageInput
		select {
		case inputElem := <-s.stageInput:
			assert.NotNil(t, inputElem.Metadata)
			assert.NotNil(t, inputElem.Metadata["tool_responses"])
			assert.NotNil(t, inputElem.Metadata["tool_result_messages"])
		default:
			t.Fatal("expected tool results on stageInput")
		}

		// No pending tools → nothing on streamOutput
		select {
		case chunk := <-s.streamOutput:
			t.Fatalf("unexpected chunk on streamOutput: %+v", chunk)
		default:
			// expected
		}
	})

	t.Run("all pending surfaces pending tools to streamOutput", func(t *testing.T) {
		reg := makeRegistry(&pendingTestExecutor{})
		registerTool(t, reg, "client_tool", "pending")

		s := makeSession(t, reg)
		elem := &stage.StreamElement{
			EndOfStream: true,
			Message: &types.Message{
				ToolCalls: []types.MessageToolCall{
					{ID: "c1", Name: "client_tool", Args: json.RawMessage(`{"key":"val"}`)},
				},
			},
		}

		err := s.handleToolCalls(context.Background(), elem)
		require.NoError(t, err)

		// Pending tools surfaced on streamOutput
		select {
		case chunk := <-s.streamOutput:
			require.NotEmpty(t, chunk.PendingTools)
			assert.Equal(t, "c1", chunk.PendingTools[0].CallID)
			assert.Equal(t, "client_tool", chunk.PendingTools[0].ToolName)
		default:
			t.Fatal("expected pending tools chunk on streamOutput")
		}

		// Nothing on stageInput (no completed tools)
		select {
		case inputElem := <-s.stageInput:
			t.Fatalf("unexpected element on stageInput: %+v", inputElem)
		default:
			// expected
		}
	})

	t.Run("mixed sends completed to stageInput and pending to streamOutput", func(t *testing.T) {
		reg := makeRegistry(
			&syncTestExecutor{result: json.RawMessage(`{"done":true}`)},
			&pendingTestExecutor{},
		)
		registerTool(t, reg, "sync_tool", "test")
		registerTool(t, reg, "client_tool", "pending")

		s := makeSession(t, reg)
		elem := &stage.StreamElement{
			EndOfStream: true,
			Message: &types.Message{
				ToolCalls: []types.MessageToolCall{
					{ID: "c1", Name: "sync_tool", Args: json.RawMessage(`{}`)},
					{ID: "c2", Name: "client_tool", Args: json.RawMessage(`{}`)},
				},
			},
		}

		err := s.handleToolCalls(context.Background(), elem)
		require.NoError(t, err)

		// Completed tool results on stageInput
		select {
		case inputElem := <-s.stageInput:
			responses := inputElem.Metadata["tool_responses"].([]providers.ToolResponse)
			assert.Len(t, responses, 1)
			assert.Equal(t, "c1", responses[0].ToolCallID)
		default:
			t.Fatal("expected tool results on stageInput")
		}

		// Pending tools on streamOutput
		select {
		case chunk := <-s.streamOutput:
			require.Len(t, chunk.PendingTools, 1)
			assert.Equal(t, "c2", chunk.PendingTools[0].CallID)
		default:
			t.Fatal("expected pending tools chunk on streamOutput")
		}
	})
}
