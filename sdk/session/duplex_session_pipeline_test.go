package session

import (
	"context"
	"testing"
	"time"

	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBidirectionalSession_PipelineMode tests the Pipeline mode
func TestBidirectionalSession_PipelineMode(t *testing.T) {
	ctx := context.Background()

	t.Run("executes pipeline when chunk is sent", func(t *testing.T) {
		// Create pipeline with mock provider, recording what it is sent
		provider := &requestRecordingProvider{Provider: mock.NewProviderWithRepository("mock", "mock-model", false,
			mock.NewInMemoryMockRepository("Hello from mock provider"))}

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

		// Send a text chunk that ends the turn: the provider stage fires on the
		// turn boundary, not on every chunk.
		ctx := context.Background()
		err = session.SendChunk(ctx, &providers.StreamChunk{
			Content:  "test message",
			Metadata: map[string]interface{}{"end_of_stream": true},
		})
		require.NoError(t, err)

		responseChan := session.Response()
		require.NotNil(t, responseChan)

		// Read until the pipeline finishes the turn and closes the channel. The
		// bound only fails a pipeline that never answers; a passing run never
		// waits on it.
		var reply string
		deadline := time.After(2 * time.Second)
	read:
		for {
			select {
			case chunk, ok := <-responseChan:
				if !ok {
					break read
				}
				require.NoError(t, chunk.Error)
				if chunk.Delta != "" {
					reply += chunk.Delta
				} else {
					reply += chunk.Content
				}
			case <-deadline:
				t.Fatalf("pipeline never finished the turn; got %q", reply)
			}
		}
		assert.Contains(t, reply, "Hello from mock provider", "the provider's reply must reach the caller")
		// The user's text is what the provider was asked: exactly one request,
		// carrying it as the user message.
		assert.Equal(t, [][]types.Message{{{Role: "user", Content: "test message"}}}, provider.recorded(),
			"the provider must receive the user's text as the user message")

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
			sessionCtx:   context.Background(),
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

	// The pipeline runs the calls when the session has a registry: the turn
	// is neither run here nor forwarded, and the call's name is kept for a
	// result submitted by ID alone.
	t.Run("registry: the turn is not run or forwarded", func(t *testing.T) {
		s := makeSession(t, tools.NewRegistry())
		elem := &stage.StreamElement{
			EndOfStream: true,
			Message: &types.Message{
				ToolCalls: []types.MessageToolCall{
					{ID: "c1", Name: "sync_tool", Args: json.RawMessage(`{}`)},
				},
			},
		}

		require.NoError(t, s.handleToolCalls(context.Background(), elem))

		select {
		case inputElem := <-s.stageInput:
			t.Fatalf("unexpected element on stageInput: %+v", inputElem)
		case chunk := <-s.streamOutput:
			t.Fatalf("unexpected chunk on streamOutput: %+v", chunk)
		default:
		}
		assert.Equal(t, "sync_tool", s.takeCallName("c1"))
	})
}
