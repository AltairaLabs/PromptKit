package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/session"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper: create a mock provider with custom streaming chunks
type customStreamMockProvider struct {
	*mock.Provider
	streamChunks []providers.StreamChunk
	streamErr    error
}

func newCustomStreamProvider(chunks []providers.StreamChunk) *customStreamMockProvider {
	return &customStreamMockProvider{
		Provider:     mock.NewProvider("test-mock", "test-model", false),
		streamChunks: chunks,
	}
}

func (m *customStreamMockProvider) PredictStream(_ context.Context, _ providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}

	ch := make(chan providers.StreamChunk, 10)
	go func() {
		defer close(ch)
		for _, chunk := range m.streamChunks {
			ch <- chunk
		}
	}()

	return ch, nil
}

// TestChunkTypeString is already defined in response_test.go

func TestStream(t *testing.T) {
	ctx := context.Background()
	repo := mock.NewInMemoryMockRepository("Streaming response")
	mockProv := mock.NewProviderWithRepository("test-mock", "test-model", false, repo)
	store := statestore.NewMemoryStore()

	p := &pack.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {ID: "chat", SystemTemplate: "System"},
		},
	}

	conv := &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: p.ToPromptRegistry(),
		toolRegistry:   tools.NewRegistry(),
		config:         &config{provider: mockProv},
		mode:           UnaryMode,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewPendingStore(),
	}

	pipeline, err := conv.buildPipelineWithParams(store, "test-conv", nil, nil)
	require.NoError(t, err)

	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "test-conv",
		StateStore:     store,
		Pipeline:       pipeline,
	})
	require.NoError(t, err)
	conv.unarySession = unarySession

	chunks := conv.Stream(ctx, "test message")

	var receivedChunks []StreamChunk
	for chunk := range chunks {
		receivedChunks = append(receivedChunks, chunk)
	}

	assert.NotEmpty(t, receivedChunks)
}

func TestBuildStreamMessage(t *testing.T) {
	conv := newTestConversation()

	t.Run("simple text message", func(t *testing.T) {
		msg, err := conv.buildStreamMessage("hello", nil)
		require.NoError(t, err)
		assert.NotNil(t, msg)
		assert.Equal(t, "user", msg.Role)
		assert.NotEmpty(t, msg.Parts)
	})

	t.Run("message with parts", func(t *testing.T) {
		msg, err := conv.buildStreamMessage("text", []SendOption{
			WithImageURL("http://example.com/image.jpg"),
		})
		require.NoError(t, err)
		assert.NotNil(t, msg)
	})
}

func TestAddContentParts(t *testing.T) {
	conv := newTestConversation()

	t.Run("empty parts", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		err := conv.addContentParts(msg, []any{})
		assert.NoError(t, err)
	})

	t.Run("with text parts", func(t *testing.T) {
		// Skip - requires proper ContentPart types
		t.Skip("requires proper ContentPart construction")
	})
}

func TestStreamWhenClosed(t *testing.T) {
	conv := newTestConversation()
	conv.closed = true

	chunks := conv.Stream(context.Background(), "test")

	var receivedChunks []StreamChunk
	for chunk := range chunks {
		receivedChunks = append(receivedChunks, chunk)
		if chunk.Error != nil {
			assert.Equal(t, ErrConversationClosed, chunk.Error)
		}
	}

	assert.NotEmpty(t, receivedChunks)
}

func TestStreamInDuplexMode(t *testing.T) {
	conv := newTestConversation()
	conv.mode = DuplexMode

	chunks := conv.Stream(context.Background(), "test")

	var receivedChunks []StreamChunk
	for chunk := range chunks {
		receivedChunks = append(receivedChunks, chunk)
		if chunk.Error != nil {
			assert.Contains(t, chunk.Error.Error(), "unary mode")
		}
	}

	assert.NotEmpty(t, receivedChunks)
}

func TestStreamRaw(t *testing.T) {
	ctx := context.Background()
	repo := mock.NewInMemoryMockRepository("Raw stream")
	mockProv := mock.NewProviderWithRepository("test-mock", "test-model", false, repo)
	store := statestore.NewMemoryStore()

	p := &pack.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {ID: "chat", SystemTemplate: "System"},
		},
	}

	conv := &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: p.ToPromptRegistry(),
		toolRegistry:   tools.NewRegistry(),
		config:         &config{provider: mockProv},
		mode:           UnaryMode,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewPendingStore(),
	}

	pipeline, err := conv.buildPipelineWithParams(store, "test-conv", nil, nil)
	require.NoError(t, err)

	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "test-conv",
		StateStore:     store,
		Pipeline:       pipeline,
	})
	require.NoError(t, err)
	conv.unarySession = unarySession

	chunks, err := conv.StreamRaw(ctx, "test message")
	require.NoError(t, err)
	assert.NotNil(t, chunks)
}

func TestEmitStreamChunk(t *testing.T) {
	conv := newTestConversation()
	out := make(chan StreamChunk, 10)

	providerChunk := providers.StreamChunk{
		Content: "test",
		Delta:   "test",
	}

	conv.emitStreamChunk(&providerChunk, out, &streamState{})

	close(out)
	chunks := []StreamChunk{}
	for chunk := range out {
		chunks = append(chunks, chunk)
	}

	assert.NotEmpty(t, chunks)
}

func TestStreamWithClosedConversation(t *testing.T) {
	conv := newTestConversation()
	conv.closed = true

	ctx := context.Background()
	chunks := conv.Stream(ctx, "test")

	chunk := <-chunks
	assert.Error(t, chunk.Error)
	assert.Equal(t, ErrConversationClosed, chunk.Error)

	// Channel should be closed
	_, ok := <-chunks
	assert.False(t, ok)
}

func TestStreamRawUnaryModeOnly(t *testing.T) {
	conv := newTestConversation()
	conv.mode = DuplexMode

	ctx := context.Background()
	ch, err := conv.StreamRaw(ctx, "test")
	assert.NoError(t, err) // StreamRaw doesn't check mode, Stream() does

	// First chunk from Stream should contain the error
	chunk := <-ch
	assert.Error(t, chunk.Error)
}

func TestStreamRawClosedConversation(t *testing.T) {
	conv := newTestConversation()
	conv.closed = true

	ctx := context.Background()
	ch, err := conv.StreamRaw(ctx, "test")
	assert.NoError(t, err) // StreamRaw doesn't check closed, Stream() does

	// First chunk from Stream should contain the error
	chunk := <-ch
	assert.Error(t, chunk.Error)
}

func TestBuildStreamMessageWithNil(t *testing.T) {
	conv := newTestConversation()

	_, err := conv.buildStreamMessage(nil, nil)
	assert.Error(t, err)
}

func TestStreamingWithMultipleChunks(t *testing.T) {
	// TODO: Streaming tests need review for stage-based pipeline
	t.Skip("Streaming tests need review for stage-based pipeline")

	ctx := context.Background()

	finishReason := "stop"
	// Create provider with multiple streaming chunks including tool calls and media
	mockProv := newCustomStreamProvider([]providers.StreamChunk{
		{Delta: "Hello ", Content: "Hello "},
		{Delta: "world", Content: "Hello world"},
		{
			Delta:   "!",
			Content: "Hello world!",
			ToolCalls: []types.MessageToolCall{
				{
					ID:   "call-1",
					Name: "get_weather",
					Args: json.RawMessage(`{"city":"SF"}`),
				},
			},
		},
		{
			Content: "Final",
			MediaDelta: &types.MediaContent{
				MIMEType: "audio/pcm",
				Data:     strPtr("base64data"),
			},
			FinishReason: &finishReason,
		},
	})

	store := statestore.NewMemoryStore()
	p := &pack.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {ID: "chat", SystemTemplate: "System"},
		},
	}

	conv := &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: p.ToPromptRegistry(),
		toolRegistry:   tools.NewRegistry(),
		config:         &config{provider: mockProv},
		mode:           UnaryMode,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewPendingStore(),
	}

	pipeline, err := conv.buildPipelineWithParams(store, "test-conv", nil, nil)
	require.NoError(t, err)

	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "test-conv",
		StateStore:     store,
		Pipeline:       pipeline,
	})
	require.NoError(t, err)
	conv.unarySession = unarySession

	// Stream and collect chunks
	chunks := conv.Stream(ctx, "test message")
	var receivedChunks []StreamChunk
	for chunk := range chunks {
		receivedChunks = append(receivedChunks, chunk)
	}

	// Verify we got text, tool call, and media chunks
	assert.NotEmpty(t, receivedChunks)
	hasText := false
	hasToolCall := false
	hasMedia := false
	for _, chunk := range receivedChunks {
		if chunk.Type == ChunkText && chunk.Text != "" {
			hasText = true
		}
		if chunk.Type == ChunkToolCall {
			hasToolCall = true
		}
		if chunk.Type == ChunkMedia {
			hasMedia = true
		}
	}
	assert.True(t, hasText, "Should have text chunks")
	assert.True(t, hasToolCall, "Should have tool call chunks")
	assert.True(t, hasMedia, "Should have media chunks")
}

// Helper to create string pointers
func strPtr(s string) *string {
	return &s
}

func TestAddContentPartsWithURL(t *testing.T) {
	conv := newTestConversation()
	msg := &types.Message{Role: "user"}

	// Test with image URL part
	detail := "high"
	err := conv.addContentParts(msg, []any{
		imageURLPart{url: "https://example.com/image.jpg", detail: &detail},
	})

	assert.NoError(t, err)
	// Parts are added to message
	assert.NotEmpty(t, msg.Parts)
}

func TestAddContentPartsWithImageData(t *testing.T) {
	conv := newTestConversation()
	msg := &types.Message{Role: "user"}

	// Test with image data part
	detail := "auto"
	err := conv.addContentParts(msg, []any{
		imageDataPart{
			data:     []byte("fake image data"),
			mimeType: "image/png",
			detail:   &detail,
		},
	})

	assert.NoError(t, err)
	assert.NotEmpty(t, msg.Parts)
}

func TestAddContentPartsWithFile(t *testing.T) {
	conv := newTestConversation()
	msg := &types.Message{Role: "user"}

	// Test with generic file part
	err := conv.addContentParts(msg, []any{
		filePart{
			name: "test.txt",
			data: []byte("file contents"),
		},
	})

	assert.NoError(t, err)
	assert.NotEmpty(t, msg.Parts)
	if len(msg.Parts) > 0 && msg.Parts[0].Text != nil {
		assert.Contains(t, *msg.Parts[0].Text, "test.txt")
		assert.Contains(t, *msg.Parts[0].Text, "file contents")
	}
}

func TestAddContentPartsUnknownType(t *testing.T) {
	conv := newTestConversation()
	msg := &types.Message{Role: "user"}

	// Test with unknown part type
	err := conv.addContentParts(msg, []any{
		"invalid part type",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown content part type")
}

func TestStreamingError(t *testing.T) {
	ctx := context.Background()

	// Create provider that returns error during streaming
	mockProv := newCustomStreamProvider(nil)
	mockProv.streamErr = errors.New("streaming failed")

	store := statestore.NewMemoryStore()
	p := &pack.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {ID: "chat", SystemTemplate: "System"},
		},
	}

	conv := &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: p.ToPromptRegistry(),
		toolRegistry:   tools.NewRegistry(),
		config:         &config{provider: mockProv},
		mode:           UnaryMode,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewPendingStore(),
	}

	pipeline, err := conv.buildPipelineWithParams(store, "test-conv", nil, nil)
	require.NoError(t, err)

	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "test-conv",
		StateStore:     store,
		Pipeline:       pipeline,
	})
	require.NoError(t, err)
	conv.unarySession = unarySession

	// Stream should return error
	chunks := conv.Stream(ctx, "test")
	chunk := <-chunks
	assert.Error(t, chunk.Error)
}
