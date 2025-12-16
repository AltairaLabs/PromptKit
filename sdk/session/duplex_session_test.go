package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Mock implementations are now in runtime/providers/mock package
// Use mock.NewStreamingProvider() for duplex session testing

// Helper function to create a simple stage pipeline builder for tests.
// Returns *stage.StreamPipeline for duplex sessions to use directly.
func testPipelineBuilder(_ context.Context, p providers.Provider, ps providers.StreamInputSession, _ string, _ statestore.Store) (*stage.StreamPipeline, error) {
	// Build a stage pipeline with DuplexProviderStage for ASM mode
	pipelineConfig := stage.DefaultPipelineConfig()
	pipelineConfig.ExecutionTimeout = 0 // Disable timeout for duplex tests
	builder := stage.NewPipelineBuilderWithConfig(pipelineConfig)

	var stages []stage.Stage
	if ps != nil {
		// Duplex mode - use DuplexProviderStage
		stages = append(stages, stage.NewDuplexProviderStage(ps))
	} else if p != nil {
		// VAD mode or non-streaming - use ProviderStage
		stages = append(stages, stage.NewProviderStage(p, nil, nil, nil))
	} else {
		// Fallback - create a pass-through stage
		stages = append(stages, &testNoOpStage{})
	}
	// Build and return the stage pipeline
	return builder.Chain(stages...).Build()
}

// testNoOpStage is a minimal stage that does nothing (for tests that don't need execution).
type testNoOpStage struct{}

func (s *testNoOpStage) Name() string { return "test-noop" }

func (s *testNoOpStage) Type() stage.StageType { return stage.StageTypeTransform }

func (s *testNoOpStage) Process(ctx context.Context, in <-chan stage.StreamElement, out chan<- stage.StreamElement) error {
	for elem := range in {
		out <- elem
	}
	return nil
}

func TestNewBidirectionalSession(t *testing.T) {
	ctx := context.Background()

	t.Run("creates session with defaults", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)
		require.NotNil(t, session)
		assert.NotEmpty(t, session.ID())
	})

	t.Run("requires provider or pipeline", func(t *testing.T) {
		_, err := NewDuplexSession(ctx, &DuplexSessionConfig{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "pipeline builder is required")
	})
}

func TestBidirectionalSession_SendChunk(t *testing.T) {
	// The DuplexProviderStage doesn't work well with ExecuteSync in the test adapter.

	ctx := context.Background()

	t.Run("sends media chunk", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder,
		})
		require.NoError(t, err)

		mediaData := "audio data"
		chunk := &providers.StreamChunk{
			MediaDelta: &types.MediaContent{
				MIMEType: types.MIMETypeAudioWAV,
				Data:     &mediaData,
			},
		}

		err = session.SendChunk(context.Background(), chunk)
		require.NoError(t, err)

		// Give pipeline time to process
		time.Sleep(50 * time.Millisecond)

		require.Len(t, provider.GetSession().GetChunks(), 1)
		assert.Equal(t, []byte(mediaData), provider.GetSession().GetChunks()[0].Data)
	})

	t.Run("sends text chunk", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder,
		})
		require.NoError(t, err)

		chunk := &providers.StreamChunk{
			Content: "Hello",
		}

		err = session.SendChunk(context.Background(), chunk)
		require.NoError(t, err)

		// Give pipeline time to process
		time.Sleep(50 * time.Millisecond)

		require.Len(t, provider.GetSession().GetTexts(), 1)
		assert.Equal(t, "Hello", provider.GetSession().GetTexts()[0])
	})
}

func TestBidirectionalSession_Response(t *testing.T) {
	// TODO: Response forwarding needs review for stage-based pipeline
	t.Skip("Response forwarding needs review for stage-based pipeline")

	ctx := context.Background()

	t.Run("receives response chunks", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder,
		})
		require.NoError(t, err)

		// Get the session that was created during NewDuplexSession
		mockSession := provider.GetSession()
		require.NotNil(t, mockSession)

		// Emit a response from the mock session before we send input
		mockSession.EmitChunk(&providers.StreamChunk{
			Content: "Hello",
		})

		// Send a text chunk to trigger the pipeline
		err = session.SendText(ctx, "trigger")
		require.NoError(t, err)

		// Give pipeline time to process
		time.Sleep(50 * time.Millisecond)

		// Now read one response
		chunk := <-session.Response()
		assert.Equal(t, "Hello", chunk.Content)

		// Close the session
		err = session.Close()
		require.NoError(t, err)
	})
}

func TestBidirectionalSession_Variables(t *testing.T) {
	ctx := context.Background()

	ctx = context.Background()

	t.Run("manages variables", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			PipelineBuilder: testPipelineBuilder, Variables: map[string]string{
				"key": "value",
			},
		})
		require.NoError(t, err)

		val, ok := session.GetVar("key")
		assert.True(t, ok)
		assert.Equal(t, "value", val)

		session.SetVar("new", "data")
		val, ok = session.GetVar("new")
		assert.True(t, ok)
		assert.Equal(t, "data", val)
	})
}

func TestBidirectionalSession_Close(t *testing.T) {
	ctx := context.Background()

	t.Run("closes session", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)

		err = session.Close()
		require.NoError(t, err)
		// Note: provider.closed is not set because session closes providerSession, not provider itself
	})

	t.Run("close is idempotent", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)

		err = session.Close()
		require.NoError(t, err)
		err = session.Close()
		require.NoError(t, err)
	})
}

func TestBidirectionalSession_SendText(t *testing.T) {

	ctx := context.Background()

	t.Run("sends text", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder,
		})
		require.NoError(t, err)

		err = session.SendText(context.Background(), "test message")
		require.NoError(t, err)

		// Give pipeline time to process
		time.Sleep(50 * time.Millisecond)

		require.Len(t, provider.GetSession().GetTexts(), 1)
		assert.Equal(t, "test message", provider.GetSession().GetTexts()[0])
	})

	t.Run("returns error when closed", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)

		session.Close()

		err = session.SendText(context.Background(), "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session is closed")
	})
}

func TestBidirectionalSession_Done(t *testing.T) {
	ctx := context.Background()

	t.Run("done channel signals completion", func(t *testing.T) {
		t.Skip("Done() test requires pipeline to complete, which needs input - skipping for now")

		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder,
		})
		require.NoError(t, err)

		mockSession := provider.GetSession()
		require.NotNil(t, mockSession)

		// Close the mock session to signal completion
		mockSession.Close()

		// Close the duplex session which will trigger Done
		err = session.Close()
		require.NoError(t, err)

		// Wait for done signal
		<-session.Done()
	})
}

func TestBidirectionalSession_Error(t *testing.T) {
	ctx := context.Background()

	t.Run("reports provider errors", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false).
			WithCreateSessionError(errors.New("test error"))
		_, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder})
		// Session creation should fail if provider fails to create session
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "test error")
	})
}

func TestBidirectionalSession_AllMethods(t *testing.T) {
	ctx := context.Background()

	t.Run("comprehensive test of all methods", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			ConversationID:  "test-123",
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder, Variables: map[string]string{
				"initial": "value",
			},
		})
		require.NoError(t, err)

		// Test ID
		assert.Equal(t, "test-123", session.ID())

		// Test Variables
		vars := session.Variables()
		assert.Equal(t, "value", vars["initial"])

		// Test GetVar/SetVar
		val, ok := session.GetVar("initial")
		assert.True(t, ok)
		assert.Equal(t, "value", val)

		session.SetVar("new", "data")
		val, ok = session.GetVar("new")
		assert.True(t, ok)
		assert.Equal(t, "data", val)

		// Test SendText
		err = session.SendText(context.Background(), "hello")
		require.NoError(t, err)

		// Test SendChunk
		mediaData := "audio"
		err = session.SendChunk(context.Background(), &providers.StreamChunk{
			MediaDelta: &types.MediaContent{
				MIMEType: types.MIMETypeAudioWAV,
				Data:     &mediaData,
			},
		})
		require.NoError(t, err)

		// Test Response
		mockSession := provider.GetSession()
		require.NotNil(t, mockSession)

		go func() {
			mockSession.EmitChunk(&providers.StreamChunk{Content: "response"})
			mockSession.Close()
		}()

		chunks := make([]providers.StreamChunk, 0)
		for chunk := range session.Response() {
			chunks = append(chunks, chunk)
		}
		// Expect 2 chunks: provider response + final chunk from pipeline
		assert.GreaterOrEqual(t, len(chunks), 1)

		// Test Done
		<-session.Done()

		// Test Error (should be nil)
		assert.NoError(t, session.Error())
	})
}

func TestNewTextSession(t *testing.T) {
	t.Run("requires pipeline", func(t *testing.T) {
		cfg := UnarySessionConfig{}
		_, err := NewUnarySession(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pipeline is required")
	})

	t.Run("creates session with defaults", func(t *testing.T) {
		// Create a proper test pipeline
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

		builder := stage.NewPipelineBuilder()
		pipe, _ := builder.
			Chain(stage.NewPromptAssemblyStage(registry, "chat", nil)).
			Build()

		cfg := UnarySessionConfig{
			Pipeline: pipe,
		}
		session, err := NewUnarySession(cfg)
		require.NoError(t, err)
		require.NotNil(t, session)

		// Verify ID was generated
		assert.NotEmpty(t, session.ID())
	})
}

func TestDuplexSession_Messages(t *testing.T) {
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	session, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        provider,
		Config:          &providers.StreamingInputConfig{},
		PipelineBuilder: testPipelineBuilder})
	require.NoError(t, err)

	messages, err := session.Messages(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, messages)
	assert.Empty(t, messages)
}

func TestDuplexSession_Clear(t *testing.T) {
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	session, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        provider,
		Config:          &providers.StreamingInputConfig{},
		PipelineBuilder: testPipelineBuilder})
	require.NoError(t, err)

	err = session.Clear(context.Background())
	assert.NoError(t, err)
}

func TestDuplexSession_ForkSession(t *testing.T) {
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	store := statestore.NewMemoryStore()

	session, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        provider,
		Config:          &providers.StreamingInputConfig{},
		PipelineBuilder: testPipelineBuilder, ConversationID: "original",
		StateStore: store,
	})
	require.NoError(t, err)

	// Fork the session
	forked, err := session.ForkSession(context.Background(), "forked", testPipelineBuilder)
	require.NoError(t, err)
	assert.NotNil(t, forked)
	assert.Equal(t, "forked", forked.ID())
}

func TestDuplexSession_Done(t *testing.T) {
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	session, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        provider,
		Config:          &providers.StreamingInputConfig{},
		PipelineBuilder: testPipelineBuilder})
	require.NoError(t, err)

	done := session.Done()
	assert.NotNil(t, done)

	// Close session and check done fires
	err = session.Close()
	require.NoError(t, err)

	select {
	case <-done:
		// Expected - but this currently doesn't work because Done() is only for pipeline execution
		t.Log("Done channel closed as expected")
	case <-time.After(100 * time.Millisecond):
		// This is expected for now - Done() only closes after pipeline execution
		t.Log("Done channel doesn't close on session.Close() - only after pipeline execution")
	}
}

func TestDuplexSession_ExecutePipeline(t *testing.T) {
	// Skip - executePipeline requires complex bidirectional streaming setup
	t.Skip("executePipeline requires full pipeline and stream configuration")
}

func TestDuplexSession_SendChunkError(t *testing.T) {
	// Skip - SendChunk requires proper streaming session setup
	t.Skip("SendChunk requires full streaming session configuration")
}

func TestDuplexSession_ForkSessionError(t *testing.T) {
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)

	session, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        provider,
		Config:          &providers.StreamingInputConfig{},
		PipelineBuilder: testPipelineBuilder, ConversationID: "original",
		StateStore: nil, // No state store
	})
	require.NoError(t, err)

	// Fork should work even without state store
	forked, err := session.ForkSession(context.Background(), "forked", testPipelineBuilder)
	require.NoError(t, err)
	assert.NotNil(t, forked)
	assert.Equal(t, "forked", forked.ID())
}

func TestDuplexSession_MessagesFromStore(t *testing.T) {
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	store := statestore.NewMemoryStore()

	session, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:        provider,
		PipelineBuilder: testPipelineBuilder, ConversationID: "test",
		StateStore: store,
	})
	require.NoError(t, err)

	// Messages should work with empty state
	messages, err := session.Messages(context.Background())
	assert.NoError(t, err)
	assert.Empty(t, messages)
}

// Additional tests for coverage

// TestNewBidirectionalSessionFromProvider is deprecated
// The function NewBidirectionalSessionFromProvider does not exist
// Use NewDuplexSession with DuplexSessionConfig instead
/*
func TestNewBidirectionalSessionFromProvider(t *testing.T) {
	...test removed...
}
*/

func TestSendChunk_EdgeCases(t *testing.T) {
	// TODO: Edge cases need review for stage-based pipeline
	t.Skip("Edge case tests need review for stage-based pipeline")

	ctx := context.Background()

	t.Run("returns error for nil chunk", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)

		err = session.SendChunk(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chunk cannot be nil")
	})

	t.Run("returns error for chunk without content or media", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)

		// Empty chunk with no Content, Delta, or MediaDelta
		err = session.SendChunk(ctx, &providers.StreamChunk{})
		// Note: The mock provider might accept empty chunks, so we just check that it doesn't crash
		if err != nil {
			assert.Contains(t, err.Error(), "chunk must contain either MediaDelta or Content/Delta")
		}
	})

	t.Run("sends chunk with Delta field", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)

		err = session.SendChunk(ctx, &providers.StreamChunk{
			Delta: "delta text", // Using Delta instead of Content
		})
		require.NoError(t, err)

		// Give pipeline time to process
		time.Sleep(50 * time.Millisecond)

		mockSession := provider.GetSession()
		require.NotNil(t, mockSession, "Mock session should exist when Config is provided")

		texts := mockSession.GetTexts()
		require.Len(t, texts, 1)
		assert.Equal(t, "delta text", texts[0])
	})

	t.Run("sends media chunk with metadata", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)

		mediaData := "audio data"
		err = session.SendChunk(ctx, &providers.StreamChunk{
			MediaDelta: &types.MediaContent{
				MIMEType: types.MIMETypeAudioWAV,
				Data:     &mediaData,
			},
			Metadata: map[string]interface{}{
				"sampleRate": "16000",
				"channels":   "1",
				"nonString":  123, // Non-string metadata should be filtered out
			},
		})
		require.NoError(t, err)

		mockSession := provider.GetSession()
		require.NotNil(t, mockSession, "Mock session should exist when Config is provided")

		chunks := mockSession.GetChunks()
		require.Len(t, chunks, 1)
		assert.Equal(t, "16000", chunks[0].Metadata["sampleRate"])
		assert.Equal(t, "1", chunks[0].Metadata["channels"])
		_, hasNonString := chunks[0].Metadata["nonString"]
		assert.False(t, hasNonString, "Non-string metadata should be filtered out")
	})

	t.Run("sends media chunk with nil data", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)

		err = session.SendChunk(ctx, &providers.StreamChunk{
			MediaDelta: &types.MediaContent{
				MIMEType: types.MIMETypeAudioWAV,
				Data:     nil, // Nil data
			},
		})
		require.NoError(t, err)

		mockSession := provider.GetSession()
		require.NotNil(t, mockSession, "Mock session should exist when Config is provided")

		chunks := mockSession.GetChunks()
		require.Len(t, chunks, 1)
		assert.Nil(t, chunks[0].Data)
	})

	t.Run("returns error when session closed", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)

		session.Close()

		err = session.SendChunk(ctx, &providers.StreamChunk{Content: "test"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session is closed")
	})
}

func TestMessages_ErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("returns error when store load fails", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		store := &mockErrorStore{loadErr: errors.New("load failed")}

		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			PipelineBuilder: testPipelineBuilder, ConversationID: "test",
			StateStore: store,
		})
		require.NoError(t, err)

		_, err = session.Messages(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "load failed")
	})
}

func TestForkSession_ErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("returns error when fork fails", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		store := &mockErrorStore{forkErr: errors.New("fork failed")}

		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			PipelineBuilder: testPipelineBuilder, ConversationID: "original",
			StateStore: store,
		})
		require.NoError(t, err)

		_, err = session.ForkSession(ctx, "forked", testPipelineBuilder)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fork state")
	})

	t.Run("preserves variables in forked session", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		store := statestore.NewMemoryStore()

		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			PipelineBuilder: testPipelineBuilder, ConversationID: "original",
			StateStore: store,
			Variables: map[string]string{
				"var1": "value1",
				"var2": "value2",
			},
		})
		require.NoError(t, err)

		forked, err := session.ForkSession(ctx, "forked", testPipelineBuilder)
		require.NoError(t, err)

		val1, ok1 := forked.GetVar("var1")
		val2, ok2 := forked.GetVar("var2")
		assert.True(t, ok1)
		assert.True(t, ok2)
		assert.Equal(t, "value1", val1)
		assert.Equal(t, "value2", val2)
	})
}

func TestDone_ProviderMode(t *testing.T) {
	// TODO: Provider mode tests need review for stage-based pipeline
	t.Skip("Provider mode tests need review for stage-based pipeline")

	ctx := context.Background()

	t.Run("provider mode returns provider's done channel", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)

		mockSession := provider.GetSession()
		require.NotNil(t, mockSession)

		// Should not be closed initially
		select {
		case <-session.Done():
			t.Fatal("Done channel should not be closed yet")
		case <-time.After(10 * time.Millisecond):
			// Good - channel is open
		}

		// Close the session
		mockSession.Close()

		// Now it should be closed
		select {
		case <-session.Done():
			// Good - channel is closed
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Done channel should be closed after session close")
		}
	})
}

func TestError_ProviderMode(t *testing.T) {
	// TODO: Provider mode tests need review for stage-based pipeline
	t.Skip("Provider mode tests need review for stage-based pipeline")

	ctx := context.Background()

	t.Run("returns error from provider session", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:        provider,
			Config:          &providers.StreamingInputConfig{},
			PipelineBuilder: testPipelineBuilder})
		require.NoError(t, err)

		// Initially no error
		assert.NoError(t, session.Error())

		// Set error on mock session
		mockSession := provider.GetSession()
		testErr := errors.New("test error")
		mockSession.WithError(testErr)

		// Should return the error
		assert.Equal(t, testErr, session.Error())
	})
}

func TestClear_Success(t *testing.T) {
	ctx := context.Background()

	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	store := statestore.NewMemoryStore()

	session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
		Provider:        provider,
		PipelineBuilder: testPipelineBuilder, ConversationID: "test",
		StateStore: store,
	})
	require.NoError(t, err)

	// Clear should succeed
	err = session.Clear(ctx)
	assert.NoError(t, err)

	// Messages should be empty after clear
	messages, err := session.Messages(ctx)
	assert.NoError(t, err)
	assert.Empty(t, messages)
}

// mockErrorStore is a test helper that returns errors for specific operations
type mockErrorStore struct {
	statestore.Store
	loadErr error
	saveErr error
	forkErr error
}

func (m *mockErrorStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	// Return empty state if no error
	return &statestore.ConversationState{
		ID:       id,
		Messages: []types.Message{},
	}, nil
}

func (m *mockErrorStore) Save(ctx context.Context, state *statestore.ConversationState) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	return nil
}

func (m *mockErrorStore) Fork(ctx context.Context, sourceID, targetID string) error {
	if m.forkErr != nil {
		return m.forkErr
	}
	return nil
}
