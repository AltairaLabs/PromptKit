package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Mock implementations are now in runtime/providers/mock package
// Use mock.NewStreamingProvider() for duplex session testing

func TestNewBidirectionalSession(t *testing.T) {
	ctx := context.Background()

	t.Run("creates session with defaults", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)
		require.NotNil(t, session)
		assert.NotEmpty(t, session.ID())
	})

	t.Run("requires provider or pipeline", func(t *testing.T) {
		_, err := NewDuplexSession(ctx, &DuplexSessionConfig{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "either Pipeline or Provider is required")
	})
}

func TestBidirectionalSession_SendChunk(t *testing.T) {
	ctx := context.Background()

	ctx = context.Background()

	t.Run("sends media chunk", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
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

		require.Len(t, provider.GetSession().GetChunks(), 1)
		assert.Equal(t, []byte(mediaData), provider.GetSession().GetChunks()[0].Data)
	})

	t.Run("sends text chunk", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		chunk := &providers.StreamChunk{
			Content: "Hello",
		}

		err = session.SendChunk(context.Background(), chunk)
		require.NoError(t, err)

		require.Len(t, provider.GetSession().GetTexts(), 1)
		assert.Equal(t, "Hello", provider.GetSession().GetTexts()[0])
	})
}

func TestBidirectionalSession_Response(t *testing.T) {
	ctx := context.Background()

	t.Run("receives response chunks", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		// Get the session that was created during NewDuplexSession
		mockSession := provider.GetSession()
		require.NotNil(t, mockSession)

		go func() {
			mockSession.EmitChunk(&providers.StreamChunk{
				Content: "Hello",
			})
			mockSession.Close()
		}()

		chunks := make([]providers.StreamChunk, 0)
		for chunk := range session.Response() {
			chunks = append(chunks, chunk)
		}

		require.Len(t, chunks, 1)
		assert.Equal(t, "Hello", chunks[0].Content)
	})
}

func TestBidirectionalSession_Variables(t *testing.T) {
	ctx := context.Background()

	ctx = context.Background()

	t.Run("manages variables", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
			Variables: map[string]string{
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
			Provider: provider,
		})
		require.NoError(t, err)

		err = session.Close()
		require.NoError(t, err)
		// Note: provider.closed is not set because session closes providerSession, not provider itself
	})

	t.Run("close is idempotent", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
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
			Provider: provider,
		})
		require.NoError(t, err)

		err = session.SendText(context.Background(), "test message")
		require.NoError(t, err)

		require.Len(t, provider.GetSession().GetTexts(), 1)
		assert.Equal(t, "test message", provider.GetSession().GetTexts()[0])
	})

	t.Run("returns error when closed", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
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
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		mockSession := provider.GetSession()
		require.NotNil(t, mockSession)

		go mockSession.Close()

		<-session.Done()
	})
}

func TestBidirectionalSession_Error(t *testing.T) {
	ctx := context.Background()

	t.Run("reports provider errors", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false).
			WithCreateSessionError(errors.New("test error"))
		_, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
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
			ConversationID: "test-123",
			Provider:       provider,
			Variables: map[string]string{
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
		assert.Len(t, chunks, 1)

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
		cfg := UnarySessionConfig{
			Pipeline: &pipeline.Pipeline{},
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
		Provider: provider,
	})
	require.NoError(t, err)

	messages, err := session.Messages(context.Background())
	assert.NoError(t, err)
	assert.NotNil(t, messages)
	assert.Empty(t, messages)
}

func TestDuplexSession_Clear(t *testing.T) {
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	session, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider: provider,
	})
	require.NoError(t, err)

	err = session.Clear(context.Background())
	assert.NoError(t, err)
}

func TestDuplexSession_ForkSession(t *testing.T) {
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	store := statestore.NewMemoryStore()

	session, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:       provider,
		ConversationID: "original",
		StateStore:     store,
	})
	require.NoError(t, err)

	// Fork the session
	forked, err := session.ForkSession(context.Background(), "forked", nil, provider)
	require.NoError(t, err)
	assert.NotNil(t, forked)
	assert.Equal(t, "forked", forked.ID())
}

func TestDuplexSession_Done(t *testing.T) {
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	session, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider: provider,
	})
	require.NoError(t, err)

	done := session.Done()
	assert.NotNil(t, done)

	// Close session and check done fires
	session.Close()
	select {
	case <-done:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should close when session closes")
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
		Provider:       provider,
		ConversationID: "original",
		StateStore:     nil, // No state store
	})
	require.NoError(t, err)

	// Fork should work even without state store
	forked, err := session.ForkSession(context.Background(), "forked", nil, provider)
	require.NoError(t, err)
	assert.NotNil(t, forked)
	assert.Equal(t, "forked", forked.ID())
}

func TestDuplexSession_MessagesFromStore(t *testing.T) {
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	store := statestore.NewMemoryStore()

	session, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
		Provider:       provider,
		ConversationID: "test",
		StateStore:     store,
	})
	require.NoError(t, err)

	// Messages should work with empty state
	messages, err := session.Messages(context.Background())
	assert.NoError(t, err)
	assert.Empty(t, messages)
}

// Additional tests for coverage

func TestNewBidirectionalSessionFromProvider(t *testing.T) {
	ctx := context.Background()

	t.Run("creates session successfully", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		store := statestore.NewMemoryStore()

		session, err := NewBidirectionalSessionFromProvider(
			ctx,
			"test-session",
			store,
			provider,
			&providers.StreamingInputConfig{},
			map[string]string{"key": "value"},
		)
		require.NoError(t, err)
		require.NotNil(t, session)
		assert.Equal(t, "test-session", session.ID())

		val, ok := session.GetVar("key")
		assert.True(t, ok)
		assert.Equal(t, "value", val)
	})

	t.Run("generates conversation ID if empty", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)

		session, err := NewBidirectionalSessionFromProvider(
			ctx,
			"", // Empty conversation ID
			nil,
			provider,
			&providers.StreamingInputConfig{},
			nil,
		)
		require.NoError(t, err)
		require.NotNil(t, session)
		assert.NotEmpty(t, session.ID())
	})

	t.Run("returns error when provider session creation fails", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false).
			WithCreateSessionError(errors.New("session creation failed"))

		_, err := NewBidirectionalSessionFromProvider(
			ctx,
			"test",
			nil,
			provider,
			&providers.StreamingInputConfig{},
			nil,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session creation failed")
	})
}

func TestSendChunk_EdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("returns error for nil chunk", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		err = session.SendChunk(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chunk cannot be nil")
	})

	t.Run("returns error for chunk without content or media", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		// Empty chunk with no Content, Delta, or MediaDelta
		err = session.SendChunk(ctx, &providers.StreamChunk{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chunk must contain either MediaDelta or Content/Delta")
	})

	t.Run("sends chunk with Delta field", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		err = session.SendChunk(ctx, &providers.StreamChunk{
			Delta: "delta text", // Using Delta instead of Content
		})
		require.NoError(t, err)

		texts := provider.GetSession().GetTexts()
		require.Len(t, texts, 1)
		assert.Equal(t, "delta text", texts[0])
	})

	t.Run("sends media chunk with metadata", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
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

		chunks := provider.GetSession().GetChunks()
		require.Len(t, chunks, 1)
		assert.Equal(t, "16000", chunks[0].Metadata["sampleRate"])
		assert.Equal(t, "1", chunks[0].Metadata["channels"])
		_, hasNonString := chunks[0].Metadata["nonString"]
		assert.False(t, hasNonString, "Non-string metadata should be filtered out")
	})

	t.Run("sends media chunk with nil data", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		err = session.SendChunk(ctx, &providers.StreamChunk{
			MediaDelta: &types.MediaContent{
				MIMEType: types.MIMETypeAudioWAV,
				Data:     nil, // Nil data
			},
		})
		require.NoError(t, err)

		chunks := provider.GetSession().GetChunks()
		require.Len(t, chunks, 1)
		assert.Nil(t, chunks[0].Data)
	})

	t.Run("returns error when session closed", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
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
			Provider:       provider,
			ConversationID: "test",
			StateStore:     store,
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
			Provider:       provider,
			ConversationID: "original",
			StateStore:     store,
		})
		require.NoError(t, err)

		_, err = session.ForkSession(ctx, "forked", nil, provider)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to fork state")
	})

	t.Run("preserves variables in forked session", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		store := statestore.NewMemoryStore()

		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider:       provider,
			ConversationID: "original",
			StateStore:     store,
			Variables: map[string]string{
				"var1": "value1",
				"var2": "value2",
			},
		})
		require.NoError(t, err)

		forked, err := session.ForkSession(ctx, "forked", nil, provider)
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
	ctx := context.Background()

	t.Run("provider mode returns provider's done channel", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
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
	ctx := context.Background()

	t.Run("returns error from provider session", func(t *testing.T) {
		provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
		session, err := NewDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
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
		Provider:       provider,
		ConversationID: "test",
		StateStore:     store,
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
