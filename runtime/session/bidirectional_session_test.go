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

func TestNewBidirectionalSession(t *testing.T) {
	t.Run("creates session with defaults", func(t *testing.T) {
		provider := newMockProviderSession()
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ProviderSession: provider,
		})
		require.NoError(t, err)
		require.NotNil(t, session)
		assert.NotEmpty(t, session.ID())
		assert.NotNil(t, session.StateStore())
	})

	t.Run("requires provider session or pipeline", func(t *testing.T) {
		_, err := NewBidirectionalSession(&BidirectionalConfig{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "either Pipeline or ProviderSession is required")
	})
}

func TestBidirectionalSession_SendChunk(t *testing.T) {
	t.Run("sends media chunk", func(t *testing.T) {
		provider := newMockProviderSession()
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ProviderSession: provider,
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

		require.Len(t, provider.sendChunks, 1)
		assert.Equal(t, []byte(mediaData), provider.sendChunks[0].Data)
	})

	t.Run("sends text chunk", func(t *testing.T) {
		provider := newMockProviderSession()
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ProviderSession: provider,
		})
		require.NoError(t, err)

		chunk := &providers.StreamChunk{
			Content: "Hello",
		}

		err = session.SendChunk(context.Background(), chunk)
		require.NoError(t, err)

		require.Len(t, provider.sendTexts, 1)
		assert.Equal(t, "Hello", provider.sendTexts[0])
	})
}

func TestBidirectionalSession_Response(t *testing.T) {
	t.Run("receives response chunks", func(t *testing.T) {
		provider := newMockProviderSession()
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ProviderSession: provider,
		})
		require.NoError(t, err)

		go func() {
			provider.emitChunk(providers.StreamChunk{
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
	t.Run("manages variables", func(t *testing.T) {
		provider := newMockProviderSession()
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ProviderSession: provider,
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
	t.Run("closes session", func(t *testing.T) {
		provider := newMockProviderSession()
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ProviderSession: provider,
		})
		require.NoError(t, err)

		err = session.Close()
		require.NoError(t, err)
		assert.True(t, provider.closed)
	})

	t.Run("close is idempotent", func(t *testing.T) {
		provider := newMockProviderSession()
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ProviderSession: provider,
		})
		require.NoError(t, err)

		err = session.Close()
		require.NoError(t, err)
		err = session.Close()
		require.NoError(t, err)
	})
}

func TestBidirectionalSession_SendText(t *testing.T) {
	t.Run("sends text", func(t *testing.T) {
		provider := newMockProviderSession()
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ProviderSession: provider,
		})
		require.NoError(t, err)

		err = session.SendText(context.Background(), "test message")
		require.NoError(t, err)

		require.Len(t, provider.sendTexts, 1)
		assert.Equal(t, "test message", provider.sendTexts[0])
	})

	t.Run("returns error when closed", func(t *testing.T) {
		provider := newMockProviderSession()
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ProviderSession: provider,
		})
		require.NoError(t, err)

		session.Close()

		err = session.SendText(context.Background(), "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session is closed")
	})
}

func TestBidirectionalSession_Done(t *testing.T) {
	t.Run("done channel signals completion", func(t *testing.T) {
		provider := newMockProviderSession()
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ProviderSession: provider,
		})
		require.NoError(t, err)

		go provider.Close()

		<-session.Done()
	})
}

func TestBidirectionalSession_Error(t *testing.T) {
	t.Run("reports provider errors", func(t *testing.T) {
		provider := newMockProviderSession()
		provider.err = errors.New("test error")
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ProviderSession: provider,
		})
		require.NoError(t, err)

		err = session.Error()
		assert.Error(t, err)
		assert.Equal(t, "test error", err.Error())
	})
}

func TestBidirectionalSession_AllMethods(t *testing.T) {
	t.Run("comprehensive test of all methods", func(t *testing.T) {
		provider := newMockProviderSession()
		session, err := NewBidirectionalSession(&BidirectionalConfig{
			ConversationID:  "test-123",
			ProviderSession: provider,
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
			provider.emitChunk(providers.StreamChunk{Content: "response"})
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
