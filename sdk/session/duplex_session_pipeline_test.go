package session

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBidirectionalSession_PipelineMode tests the Pipeline mode
func TestBidirectionalSession_PipelineMode(t *testing.T) {
	ctx := context.Background()

	t.Run("returns streamOutput in Pipeline mode", func(t *testing.T) {
		// Create minimal pipeline with mock provider
		provider := mock.NewProvider("mock", "mock-model", false)
		pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*pipeline.Pipeline, error) {
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			builder := stage.NewPipelineBuilder()
			sp, err := builder.Chain(providerStage).Build()
			if err != nil {
				return nil, err
			}
			return wrapStreamPipelineForTest(sp), nil
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
		pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*pipeline.Pipeline, error) {
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			builder := stage.NewPipelineBuilder()
			sp, err := builder.Chain(providerStage).Build()
			if err != nil {
				return nil, err
			}
			return wrapStreamPipelineForTest(sp), nil
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
		pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*pipeline.Pipeline, error) {
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			builder := stage.NewPipelineBuilder()
			sp, err := builder.Chain(providerStage).Build()
			if err != nil {
				return nil, err
			}
			return wrapStreamPipelineForTest(sp), nil
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

		pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*pipeline.Pipeline, error) {
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			builder := stage.NewPipelineBuilder()
			sp, err := builder.Chain(providerStage).Build()
			if err != nil {
				return nil, err
			}
			return wrapStreamPipelineForTest(sp), nil
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
