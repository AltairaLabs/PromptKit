package mock

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestNewMockStreamSession(t *testing.T) {
	session := NewMockStreamSession()
	require.NotNil(t, session)
	assert.NotNil(t, session.responses)
	assert.NotNil(t, session.doneCh)
	assert.False(t, session.autoRespond)
	assert.Equal(t, "Mock response", session.responseText)
}

func TestMockStreamSession_WithMethods(t *testing.T) {
	t.Run("WithAutoRespond", func(t *testing.T) {
		session := NewMockStreamSession().WithAutoRespond("test response")
		assert.True(t, session.autoRespond)
		assert.Equal(t, "test response", session.responseText)
	})

	t.Run("WithResponseChunks", func(t *testing.T) {
		chunks := []providers.StreamChunk{
			{Content: "chunk1"},
			{Content: "chunk2"},
		}
		session := NewMockStreamSession().WithResponseChunks(chunks)
		assert.Equal(t, chunks, session.responseChunks)
	})

	t.Run("WithSendChunkError", func(t *testing.T) {
		testErr := errors.New("send chunk error")
		session := NewMockStreamSession().WithSendChunkError(testErr)
		assert.Equal(t, testErr, session.sendChunkErr)
	})

	t.Run("WithSendTextError", func(t *testing.T) {
		testErr := errors.New("send text error")
		session := NewMockStreamSession().WithSendTextError(testErr)
		assert.Equal(t, testErr, session.sendTextErr)
	})

	t.Run("WithError", func(t *testing.T) {
		testErr := errors.New("session error")
		session := NewMockStreamSession().WithError(testErr)
		assert.Equal(t, testErr, session.err)
	})
}

func TestMockStreamSession_SendChunk(t *testing.T) {
	ctx := context.Background()

	t.Run("sends chunk successfully", func(t *testing.T) {
		session := NewMockStreamSession()
		chunk := &types.MediaChunk{
			Data:        []byte("test data"),
			SequenceNum: 1,
		}

		err := session.SendChunk(ctx, chunk)
		require.NoError(t, err)

		chunks := session.GetChunks()
		require.Len(t, chunks, 1)
		assert.Equal(t, chunk, chunks[0])
	})

	t.Run("returns error when session closed", func(t *testing.T) {
		session := NewMockStreamSession()
		session.Close()

		err := session.SendChunk(ctx, &types.MediaChunk{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session closed")
	})

	t.Run("returns configured error", func(t *testing.T) {
		testErr := errors.New("send error")
		session := NewMockStreamSession().WithSendChunkError(testErr)

		err := session.SendChunk(ctx, &types.MediaChunk{})
		assert.Equal(t, testErr, err)
	})
}

func TestMockStreamSession_SendText(t *testing.T) {
	ctx := context.Background()

	t.Run("sends text successfully", func(t *testing.T) {
		session := NewMockStreamSession()

		err := session.SendText(ctx, "hello")
		require.NoError(t, err)

		texts := session.GetTexts()
		require.Len(t, texts, 1)
		assert.Equal(t, "hello", texts[0])
	})

	t.Run("returns error when session closed", func(t *testing.T) {
		session := NewMockStreamSession()
		session.Close()

		err := session.SendText(ctx, "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session closed")
	})

	t.Run("returns configured error", func(t *testing.T) {
		testErr := errors.New("send text error")
		session := NewMockStreamSession().WithSendTextError(testErr)

		err := session.SendText(ctx, "test")
		assert.Equal(t, testErr, err)
	})
}

func TestMockStreamSession_Response(t *testing.T) {
	session := NewMockStreamSession()
	responseChan := session.Response()
	assert.NotNil(t, responseChan)
	// The channel is not nil and is usable
}

func TestMockStreamSession_Close(t *testing.T) {
	t.Run("closes successfully", func(t *testing.T) {
		session := NewMockStreamSession()

		err := session.Close()
		assert.NoError(t, err)

		// Verify channels are closed
		select {
		case <-session.Done():
			// Good - done channel is closed
		case <-time.After(10 * time.Millisecond):
			t.Fatal("Done channel should be closed")
		}

		_, ok := <-session.Response()
		assert.False(t, ok, "Response channel should be closed")
	})

	t.Run("close is idempotent", func(t *testing.T) {
		session := NewMockStreamSession()

		err := session.Close()
		assert.NoError(t, err)

		err = session.Close()
		assert.NoError(t, err)
	})
}

func TestMockStreamSession_Error(t *testing.T) {
	t.Run("returns nil by default", func(t *testing.T) {
		session := NewMockStreamSession()
		assert.NoError(t, session.Error())
	})

	t.Run("returns configured error", func(t *testing.T) {
		testErr := errors.New("test error")
		session := NewMockStreamSession().WithError(testErr)
		assert.Equal(t, testErr, session.Error())
	})
}

func TestMockStreamSession_Done(t *testing.T) {
	session := NewMockStreamSession()
	doneChan := session.Done()
	assert.NotNil(t, doneChan)

	// Should not be closed initially
	select {
	case <-doneChan:
		t.Fatal("Done channel should not be closed yet")
	case <-time.After(10 * time.Millisecond):
		// Good
	}

	// Close the session
	session.Close()

	// Now it should be closed
	select {
	case <-doneChan:
		// Good
	case <-time.After(10 * time.Millisecond):
		t.Fatal("Done channel should be closed after Close()")
	}
}

func TestMockStreamSession_EmitChunk(t *testing.T) {
	t.Run("emits chunk successfully", func(t *testing.T) {
		session := NewMockStreamSession()

		chunk := &providers.StreamChunk{Content: "test"}
		session.EmitChunk(chunk)

		received := <-session.Response()
		assert.Equal(t, "test", received.Content)
	})

	t.Run("does not emit when closed", func(t *testing.T) {
		session := NewMockStreamSession()
		session.Close()

		// This should not panic or block
		chunk := &providers.StreamChunk{Content: "test"}
		session.EmitChunk(chunk)
	})
}

func TestMockStreamSession_GetChunks(t *testing.T) {
	session := NewMockStreamSession()

	chunk1 := &types.MediaChunk{Data: []byte("data1")}
	chunk2 := &types.MediaChunk{Data: []byte("data2")}

	session.SendChunk(context.Background(), chunk1)
	session.SendChunk(context.Background(), chunk2)

	chunks := session.GetChunks()
	require.Len(t, chunks, 2)
	assert.Equal(t, chunk1, chunks[0])
	assert.Equal(t, chunk2, chunks[1])
}

func TestMockStreamSession_GetTexts(t *testing.T) {
	session := NewMockStreamSession()

	session.SendText(context.Background(), "text1")
	session.SendText(context.Background(), "text2")

	texts := session.GetTexts()
	require.Len(t, texts, 2)
	assert.Equal(t, "text1", texts[0])
	assert.Equal(t, "text2", texts[1])
}

func TestNewStreamingProvider(t *testing.T) {
	provider := NewStreamingProvider("test-id", "test-model", false)
	require.NotNil(t, provider)
	assert.Equal(t, "test-id", provider.ID())
	assert.NotNil(t, provider.sessions)
}

func TestNewStreamingProviderWithRepository(t *testing.T) {
	repo := NewInMemoryMockRepository("test response")
	provider := NewStreamingProviderWithRepository("test-id", "test-model", false, repo)
	require.NotNil(t, provider)
	assert.Equal(t, "test-id", provider.ID())
	assert.NotNil(t, provider.sessions)
}

func TestStreamingProvider_WithCreateSessionError(t *testing.T) {
	provider := NewStreamingProvider("test-id", "test-model", false)
	testErr := errors.New("session creation error")

	provider = provider.WithCreateSessionError(testErr)
	assert.Equal(t, testErr, provider.createSessionErr)
}

func TestStreamingProvider_CreateStreamSession(t *testing.T) {
	ctx := context.Background()

	t.Run("creates session successfully", func(t *testing.T) {
		provider := NewStreamingProvider("test-id", "test-model", false)

		session, err := provider.CreateStreamSession(ctx, &providers.StreamingInputConfig{})
		require.NoError(t, err)
		require.NotNil(t, session)

		// Should be tracked
		assert.Len(t, provider.sessions, 1)
	})

	t.Run("returns configured error", func(t *testing.T) {
		testErr := errors.New("creation failed")
		provider := NewStreamingProvider("test-id", "test-model", false).
			WithCreateSessionError(testErr)

		_, err := provider.CreateStreamSession(ctx, &providers.StreamingInputConfig{})
		assert.Equal(t, testErr, err)
	})

	t.Run("tracks multiple sessions", func(t *testing.T) {
		provider := NewStreamingProvider("test-id", "test-model", false)

		session1, err := provider.CreateStreamSession(ctx, &providers.StreamingInputConfig{})
		require.NoError(t, err)
		require.NotNil(t, session1)

		session2, err := provider.CreateStreamSession(ctx, &providers.StreamingInputConfig{})
		require.NoError(t, err)
		require.NotNil(t, session2)

		assert.Len(t, provider.sessions, 2)
	})
}

func TestStreamingProvider_SupportsStreamInput(t *testing.T) {
	provider := NewStreamingProvider("test-id", "test-model", false)
	mediaTypes := provider.SupportsStreamInput()

	require.Len(t, mediaTypes, 2)
	assert.Contains(t, mediaTypes, types.ContentTypeAudio)
	assert.Contains(t, mediaTypes, types.ContentTypeVideo)
}

func TestStreamingProvider_GetStreamingCapabilities(t *testing.T) {
	provider := NewStreamingProvider("test-id", "test-model", false)
	caps := provider.GetStreamingCapabilities()

	require.Len(t, caps.SupportedMediaTypes, 2)
	assert.Contains(t, caps.SupportedMediaTypes, types.ContentTypeAudio)
	assert.Contains(t, caps.SupportedMediaTypes, types.ContentTypeVideo)

	require.NotNil(t, caps.Audio)
	assert.Contains(t, caps.Audio.SupportedEncodings, "pcm")
	assert.Contains(t, caps.Audio.SupportedEncodings, "opus")
	assert.Contains(t, caps.Audio.SupportedSampleRates, 16000)
	assert.Contains(t, caps.Audio.SupportedChannels, 1)

	require.NotNil(t, caps.Video)
	assert.Contains(t, caps.Video.SupportedEncodings, "h264")
	assert.Contains(t, caps.Video.SupportedEncodings, "vp8")
	require.Len(t, caps.Video.SupportedResolutions, 2)
}

func TestStreamingProvider_GetSession(t *testing.T) {
	ctx := context.Background()
	provider := NewStreamingProvider("test-id", "test-model", false)

	t.Run("returns nil when no sessions", func(t *testing.T) {
		session := provider.GetSession()
		assert.Nil(t, session)
	})

	t.Run("returns most recent session", func(t *testing.T) {
		session1, err := provider.CreateStreamSession(ctx, &providers.StreamingInputConfig{})
		require.NoError(t, err)

		session2, err := provider.CreateStreamSession(ctx, &providers.StreamingInputConfig{})
		require.NoError(t, err)

		lastSession := provider.GetSession()
		assert.Equal(t, session2, lastSession)
		assert.NotEqual(t, session1, lastSession)
	})
}

func TestStreamingProvider_GetSessions(t *testing.T) {
	ctx := context.Background()
	provider := NewStreamingProvider("test-id", "test-model", false)

	t.Run("returns empty slice initially", func(t *testing.T) {
		sessions := provider.GetSessions()
		assert.Empty(t, sessions)
	})

	t.Run("returns all sessions", func(t *testing.T) {
		session1, err := provider.CreateStreamSession(ctx, &providers.StreamingInputConfig{})
		require.NoError(t, err)

		session2, err := provider.CreateStreamSession(ctx, &providers.StreamingInputConfig{})
		require.NoError(t, err)

		sessions := provider.GetSessions()
		require.Len(t, sessions, 2)
		assert.Equal(t, session1, sessions[0])
		assert.Equal(t, session2, sessions[1])
	})
}
