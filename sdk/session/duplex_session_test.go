package session

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// mockProviderSession implements providers.StreamInputSession for testing
type mockProviderSession struct {
	chunks     chan providers.StreamChunk
	done       chan struct{}
	err        error
	sendChunks []types.MediaChunk
	sendTexts  []string
	closed     bool
	mu         sync.Mutex
}

func newMockProviderSession() *mockProviderSession {
	return &mockProviderSession{
		chunks: make(chan providers.StreamChunk, 10),
		done:   make(chan struct{}),
	}
}

func (m *mockProviderSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("session closed")
	}
	m.sendChunks = append(m.sendChunks, *chunk)
	return nil
}

func (m *mockProviderSession) SendText(ctx context.Context, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("session closed")
	}
	m.sendTexts = append(m.sendTexts, text)
	return nil
}

func (m *mockProviderSession) Response() <-chan providers.StreamChunk {
	return m.chunks
}

func (m *mockProviderSession) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	close(m.chunks)
	close(m.done)
	return nil
}

func (m *mockProviderSession) Error() error {
	return m.err
}

func (m *mockProviderSession) Done() <-chan struct{} {
	return m.done
}

func (m *mockProviderSession) emitChunk(chunk providers.StreamChunk) {
	m.chunks <- chunk
}

// mockStreamInputProvider implements providers.StreamInputSupport for testing
type mockStreamInputProvider struct {
	session *mockProviderSession
	closed  bool
	err     error
}

func newMockStreamInputProvider() *mockStreamInputProvider {
	return &mockStreamInputProvider{
		session: newMockProviderSession(),
	}
}

func (m *mockStreamInputProvider) CreateStreamSession(ctx context.Context, request *providers.StreamingInputConfig) (providers.StreamInputSession, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.session, nil
}

// Implement other Provider methods as stubs
func (m *mockStreamInputProvider) ID() string {
	return "mock-provider"
}

func (m *mockStreamInputProvider) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, errors.New("not implemented")
}

func (m *mockStreamInputProvider) PredictStream(ctx context.Context, req providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func (m *mockStreamInputProvider) SupportsStreaming() bool {
	return true
}

func (m *mockStreamInputProvider) ShouldIncludeRawOutput() bool {
	return false
}

func (m *mockStreamInputProvider) CalculateCost(input, output, cached int) types.CostInfo {
	return types.CostInfo{}
}

func (m *mockStreamInputProvider) Close() error {
	m.closed = true
	if m.session != nil {
		return m.session.Close()
	}
	return nil
}

func (m *mockStreamInputProvider) SupportsStreamInput() []string {
	return []string{types.ContentTypeAudio}
}

func (m *mockStreamInputProvider) GetStreamingCapabilities() providers.StreamingCapabilities {
	return providers.StreamingCapabilities{
		SupportedMediaTypes: []string{types.ContentTypeAudio},
	}
}

func (m *mockStreamInputProvider) Name() string {
	return "mock"
}

func TestNewBidirectionalSession(t *testing.T) {
	ctx := context.Background()

	t.Run("creates session with defaults", func(t *testing.T) {
		provider := newMockStreamInputProvider()
		session, err := newDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)
		require.NotNil(t, session)
		assert.NotEmpty(t, session.ID())
		assert.NotNil(t, session.StateStore())
	})

	t.Run("requires provider or pipeline", func(t *testing.T) {
		_, err := newDuplexSession(ctx, &DuplexSessionConfig{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "either Pipeline or Provider is required")
	})
}

func TestBidirectionalSession_SendChunk(t *testing.T) {
	ctx := context.Background()

	ctx = context.Background()

	t.Run("sends media chunk", func(t *testing.T) {
		provider := newMockStreamInputProvider()
		session, err := newDuplexSession(ctx, &DuplexSessionConfig{
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

		require.Len(t, provider.session.sendChunks, 1)
		assert.Equal(t, []byte(mediaData), provider.session.sendChunks[0].Data)
	})

	t.Run("sends text chunk", func(t *testing.T) {
		provider := newMockStreamInputProvider()
		session, err := newDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		chunk := &providers.StreamChunk{
			Content: "Hello",
		}

		err = session.SendChunk(context.Background(), chunk)
		require.NoError(t, err)

		require.Len(t, provider.session.sendTexts, 1)
		assert.Equal(t, "Hello", provider.session.sendTexts[0])
	})
}

func TestBidirectionalSession_Response(t *testing.T) {
	ctx := context.Background()

	t.Run("receives response chunks", func(t *testing.T) {
		provider := newMockStreamInputProvider()
		session, err := newDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		go func() {
			provider.session.emitChunk(providers.StreamChunk{
				Content: "Hello",
			})
			provider.Close()
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
		provider := newMockStreamInputProvider()
		session, err := newDuplexSession(ctx, &DuplexSessionConfig{
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
		provider := newMockStreamInputProvider()
		session, err := newDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		err = session.Close()
		require.NoError(t, err)
		// Note: provider.closed is not set because session closes providerSession, not provider itself
	})

	t.Run("close is idempotent", func(t *testing.T) {
		provider := newMockStreamInputProvider()
		session, err := newDuplexSession(ctx, &DuplexSessionConfig{
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
		provider := newMockStreamInputProvider()
		session, err := newDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		err = session.SendText(context.Background(), "test message")
		require.NoError(t, err)

		require.Len(t, provider.session.sendTexts, 1)
		assert.Equal(t, "test message", provider.session.sendTexts[0])
	})

	t.Run("returns error when closed", func(t *testing.T) {
		provider := newMockStreamInputProvider()
		session, err := newDuplexSession(ctx, &DuplexSessionConfig{
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
		provider := newMockStreamInputProvider()
		session, err := newDuplexSession(ctx, &DuplexSessionConfig{
			Provider: provider,
		})
		require.NoError(t, err)

		go provider.Close()

		<-session.Done()
	})
}

func TestBidirectionalSession_Error(t *testing.T) {
	ctx := context.Background()

	t.Run("reports provider errors", func(t *testing.T) {
		provider := newMockStreamInputProvider()
		provider.err = errors.New("test error")
		_, err := newDuplexSession(ctx, &DuplexSessionConfig{
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
		provider := newMockStreamInputProvider()
		session, err := newDuplexSession(ctx, &DuplexSessionConfig{
			ConversationID: "test-123",
			Provider:       provider,
			Variables: map[string]string{
				"initial": "value",
			},
		})
		require.NoError(t, err)

		// Test ID
		assert.Equal(t, "test-123", session.ID())

		// Test StateStore
		assert.NotNil(t, session.StateStore())

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
		go func() {
			provider.session.emitChunk(providers.StreamChunk{Content: "response"})
			provider.Close()
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
		cfg := TextConfig{}
		_, err := NewTextSession(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pipeline is required")
	})

	t.Run("creates session with defaults", func(t *testing.T) {
		cfg := TextConfig{
			Pipeline: &pipeline.Pipeline{},
		}
		session, err := NewTextSession(cfg)
		require.NoError(t, err)
		require.NotNil(t, session)

		// Verify ID was generated
		assert.NotEmpty(t, session.ID())

		// Verify StateStore was initialized
		assert.NotNil(t, session.StateStore())
	})
}
