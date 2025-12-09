package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	intpipeline "github.com/AltairaLabs/PromptKit/sdk/internal/pipeline"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConversation() *Conversation {
	p := &pack.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {ID: "chat", SystemTemplate: "You are helpful."},
		},
	}
	return &Conversation{
		pack:           p,
		prompt:         &pack.Prompt{ID: "chat", SystemTemplate: "You are helpful."},
		promptName:     "chat",
		promptRegistry: p.ToPromptRegistry(),
		toolRegistry:   tools.NewRegistryWithRepository(p.ToToolRepository()),
		config:         &config{},
		variables:      make(map[string]string),
		handlers:       make(map[string]ToolHandler),
	}
}

func TestConversationSetVar(t *testing.T) {
	conv := newTestConversation()

	conv.SetVar("name", "Alice")
	assert.Equal(t, "Alice", conv.GetVar("name"))

	conv.SetVar("name", "Bob")
	assert.Equal(t, "Bob", conv.GetVar("name"))
}

func TestConversationSetVars(t *testing.T) {
	conv := newTestConversation()

	conv.SetVars(map[string]any{
		"name": "Alice",
		"age":  30,
		"tier": "premium",
	})

	assert.Equal(t, "Alice", conv.GetVar("name"))
	assert.Equal(t, "30", conv.GetVar("age"))
	assert.Equal(t, "premium", conv.GetVar("tier"))
}

func TestConversationSetVarsFromEnv(t *testing.T) {
	conv := newTestConversation()

	// Set test env vars
	_ = os.Setenv("TEST_SDK_NAME", "TestUser")
	_ = os.Setenv("TEST_SDK_VALUE", "123")
	defer func() {
		_ = os.Unsetenv("TEST_SDK_NAME")
		_ = os.Unsetenv("TEST_SDK_VALUE")
	}()

	conv.SetVarsFromEnv("TEST_SDK_")

	assert.Equal(t, "TestUser", conv.GetVar("name"))
	assert.Equal(t, "123", conv.GetVar("value"))
}

func TestConversationGetVarNotSet(t *testing.T) {
	conv := newTestConversation()
	assert.Equal(t, "", conv.GetVar("nonexistent"))
}

func TestConversationOnTool(t *testing.T) {
	conv := newTestConversation()

	called := false
	conv.OnTool("test_tool", func(args map[string]any) (any, error) {
		called = true
		return "result", nil
	})

	// Verify handler was registered
	conv.handlersMu.RLock()
	handler, ok := conv.handlers["test_tool"]
	conv.handlersMu.RUnlock()

	assert.True(t, ok)
	assert.NotNil(t, handler)

	// Call the handler
	result, err := handler(map[string]any{})
	assert.NoError(t, err)
	assert.Equal(t, "result", result)
	assert.True(t, called)
}

func TestConversationOnToolCtx(t *testing.T) {
	conv := newTestConversation()

	var receivedCtx context.Context
	conv.OnToolCtx("ctx_tool", func(ctx context.Context, args map[string]any) (any, error) {
		receivedCtx = ctx
		return "ctx_result", nil
	})

	// Verify handler was registered (wrapped)
	conv.handlersMu.RLock()
	handler, ok := conv.handlers["ctx_tool"]
	conv.handlersMu.RUnlock()

	assert.True(t, ok)
	result, err := handler(map[string]any{})
	assert.NoError(t, err)
	assert.Equal(t, "ctx_result", result)
	assert.NotNil(t, receivedCtx)
}

func TestConversationOnTools(t *testing.T) {
	conv := newTestConversation()

	conv.OnTools(map[string]ToolHandler{
		"tool1": func(args map[string]any) (any, error) { return "r1", nil },
		"tool2": func(args map[string]any) (any, error) { return "r2", nil },
	})

	conv.handlersMu.RLock()
	assert.Len(t, conv.handlers, 2)
	_, ok1 := conv.handlers["tool1"]
	_, ok2 := conv.handlers["tool2"]
	conv.handlersMu.RUnlock()

	assert.True(t, ok1)
	assert.True(t, ok2)
}

func TestConversationMessages(t *testing.T) {
	conv := newTestConversation()

	// No state - should return nil
	assert.Nil(t, conv.Messages())

	// With state
	conv.state = &statestore.ConversationState{
		Messages: []types.Message{
			{Role: "user"},
			{Role: "assistant"},
		},
	}

	msgs := conv.Messages()
	assert.Len(t, msgs, 2)

	// Verify it's a copy
	msgs[0].Role = "modified"
	assert.Equal(t, "user", conv.state.Messages[0].Role)
}

func TestConversationClear(t *testing.T) {
	conv := newTestConversation()
	conv.state = &statestore.ConversationState{
		Messages:   []types.Message{{Role: "user"}},
		TokenCount: 100,
	}

	conv.Clear()

	assert.Nil(t, conv.state.Messages)
	assert.Equal(t, 0, conv.state.TokenCount)
}

func TestConversationClearNilState(t *testing.T) {
	conv := newTestConversation()
	// Should not panic with nil state
	conv.Clear()
}

func TestConversationFork(t *testing.T) {
	conv := newTestConversation()
	conv.SetVar("name", "Alice")
	conv.OnTool("tool1", func(args map[string]any) (any, error) { return nil, nil })
	conv.state = &statestore.ConversationState{
		ID:         "original",
		Messages:   []types.Message{{Role: "user"}},
		TokenCount: 50,
	}

	fork := conv.Fork()

	// Verify fork has same data
	assert.Equal(t, "Alice", fork.GetVar("name"))
	fork.handlersMu.RLock()
	_, hasHandler := fork.handlers["tool1"]
	fork.handlersMu.RUnlock()
	assert.True(t, hasHandler)

	// Verify fork state is independent
	assert.Contains(t, fork.state.ID, "fork")
	assert.Len(t, fork.Messages(), 1)

	// Modify fork - original should be unchanged
	fork.SetVar("name", "Bob")
	assert.Equal(t, "Alice", conv.GetVar("name"))
	assert.Equal(t, "Bob", fork.GetVar("name"))
}

func TestConversationClose(t *testing.T) {
	conv := newTestConversation()

	err := conv.Close()
	assert.NoError(t, err)
	assert.True(t, conv.closed)

	// Second close should be no-op
	err = conv.Close()
	assert.NoError(t, err)
}

func TestConversationSendWhenClosed(t *testing.T) {
	conv := newTestConversation()
	_ = conv.Close()

	_, err := conv.Send(context.Background(), "hello")
	assert.Error(t, err)
	assert.Equal(t, ErrConversationClosed, err)
}

func TestConversationSendMessageTypes(t *testing.T) {
	conv := newTestConversation()

	t.Run("string message", func(t *testing.T) {
		// Without a provider, Send should still work but return empty response
		resp, err := conv.Send(context.Background(), "hello")
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("types.Message", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		msg.AddTextPart("hello")
		resp, err := conv.Send(context.Background(), msg)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("invalid type", func(t *testing.T) {
		_, err := conv.Send(context.Background(), 123)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be string or *types.Message")
	})
}

func TestConversationID(t *testing.T) {
	conv := newTestConversation()
	conv.id = "test-id-123"

	assert.Equal(t, "test-id-123", conv.ID())
}

func TestConversationEventBus(t *testing.T) {
	conv := newTestConversation()
	conv.eventBus = events.NewEventBus()

	bus := conv.EventBus()
	assert.NotNil(t, bus)
}

func TestConversationToolRegistry(t *testing.T) {
	conv := newTestConversation()
	// Returns the actual tool registry
	assert.NotNil(t, conv.ToolRegistry())
}

func TestConversationStream(t *testing.T) {
	conv := newTestConversation()

	ch := conv.Stream(context.Background(), "hello")
	chunk := <-ch

	// Stream falls back to Send, which works without a provider
	// but returns an empty response
	assert.NoError(t, chunk.Error)
	assert.Equal(t, ChunkDone, chunk.Type)
}

func TestApplyContentParts(t *testing.T) {
	conv := newTestConversation()
	autoDetail := "auto"

	t.Run("image URL part", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		parts := []any{
			imageURLPart{url: "https://example.com/image.jpg", detail: &autoDetail},
		}

		err := conv.applyContentParts(msg, parts)
		assert.NoError(t, err)
		assert.Len(t, msg.Parts, 1)
	})

	t.Run("image data part", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		parts := []any{
			imageDataPart{data: []byte("fake-image-data"), mimeType: "image/png", detail: &autoDetail},
		}

		err := conv.applyContentParts(msg, parts)
		assert.NoError(t, err)
		assert.Len(t, msg.Parts, 1)
	})

	t.Run("file part", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		parts := []any{
			filePart{name: "test.txt", data: []byte("file content")},
		}

		err := conv.applyContentParts(msg, parts)
		assert.NoError(t, err)
		assert.Len(t, msg.Parts, 1)
	})

	t.Run("unknown part type", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		parts := []any{
			struct{ unknown string }{unknown: "type"},
		}

		err := conv.applyContentParts(msg, parts)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown content part type")
	})

	t.Run("empty parts", func(t *testing.T) {
		msg := &types.Message{Role: "user"}
		err := conv.applyContentParts(msg, []any{})
		assert.NoError(t, err)
		assert.Len(t, msg.Parts, 0)
	})
}

func TestBuildToolRegistry(t *testing.T) {
	t.Run("returns registry from pack", func(t *testing.T) {
		conv := newTestConversation()
		registry := conv.buildToolRegistry()
		// Registry is always created (from pack), even with no handlers
		assert.NotNil(t, registry)
	})

	t.Run("handler registers executor", func(t *testing.T) {
		conv := newTestConversation()
		conv.pack.Tools = map[string]*pack.Tool{
			"test_tool": {
				Name:        "test_tool",
				Description: "A test tool",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"input": map[string]any{"type": "string"},
					},
				},
			},
		}
		// Reinitialize toolRegistry with the new tool
		conv.toolRegistry = tools.NewRegistryWithRepository(conv.pack.ToToolRepository())

		conv.OnTool("test_tool", func(args map[string]any) (any, error) {
			return "result", nil
		})

		registry := conv.buildToolRegistry()
		assert.NotNil(t, registry)

		// Verify tool is in registry
		tool, err := registry.GetTool("test_tool")
		assert.NoError(t, err)
		assert.Equal(t, "test_tool", tool.Name)
	})
}

func TestGetVariables(t *testing.T) {
	conv := newTestConversation()
	conv.SetVar("key1", "value1")
	conv.SetVar("key2", "value2")

	vars := conv.getVariables()

	assert.Equal(t, "value1", vars["key1"])
	assert.Equal(t, "value2", vars["key2"])

	// Verify it's a copy
	vars["key1"] = "modified"
	assert.Equal(t, "value1", conv.GetVar("key1"))
}

func TestApplyPromptParameters(t *testing.T) {
	t.Run("no parameters", func(t *testing.T) {
		conv := newTestConversation()
		conv.prompt.Parameters = nil

		cfg := &intpipeline.Config{
			MaxTokens:   100,
			Temperature: 0.5,
		}

		conv.applyPromptParameters(cfg)
		assert.Equal(t, 100, cfg.MaxTokens)
		assert.Equal(t, float32(0.5), cfg.Temperature)
	})

	t.Run("with max tokens", func(t *testing.T) {
		conv := newTestConversation()
		maxTokens := 2048
		conv.prompt.Parameters = &pack.Parameters{
			MaxTokens: &maxTokens,
		}

		cfg := &intpipeline.Config{
			MaxTokens:   100,
			Temperature: 0.5,
		}

		conv.applyPromptParameters(cfg)
		assert.Equal(t, 2048, cfg.MaxTokens)
		assert.Equal(t, float32(0.5), cfg.Temperature)
	})

	t.Run("with temperature", func(t *testing.T) {
		conv := newTestConversation()
		temp := 0.9
		conv.prompt.Parameters = &pack.Parameters{
			Temperature: &temp,
		}

		cfg := &intpipeline.Config{
			MaxTokens:   100,
			Temperature: 0.5,
		}

		conv.applyPromptParameters(cfg)
		assert.Equal(t, 100, cfg.MaxTokens)
		assert.Equal(t, float32(0.9), cfg.Temperature)
	})
}

func TestAddMessageToHistory(t *testing.T) {
	conv := newTestConversation()

	msg := &types.Message{Role: "user"}
	msg.AddTextPart("Hello")

	conv.addMessageToHistory(msg)

	assert.NotNil(t, conv.state)
	assert.Len(t, conv.state.Messages, 1)
	assert.Equal(t, "user", conv.state.Messages[0].Role)
}

func TestEncodeBase64(t *testing.T) {
	data := []byte("hello world")
	encoded := encodeBase64(data)
	assert.Equal(t, "aGVsbG8gd29ybGQ=", encoded)
}

func TestHandlerAdapter(t *testing.T) {
	t.Run("name returns handler name", func(t *testing.T) {
		adapter := &handlerAdapter{
			name:    "test_handler",
			handler: func(args map[string]any) (any, error) { return nil, nil },
		}
		assert.Equal(t, "test_handler", adapter.Name())
	})

	t.Run("execute calls handler", func(t *testing.T) {
		called := false
		adapter := &handlerAdapter{
			name: "test",
			handler: func(args map[string]any) (any, error) {
				called = true
				return map[string]string{"result": "success"}, nil
			},
		}

		result, err := adapter.Execute(nil, []byte(`{"input": "test"}`))
		assert.NoError(t, err)
		assert.True(t, called)
		assert.Contains(t, string(result), "success")
	})

	t.Run("execute returns handler error", func(t *testing.T) {
		adapter := &handlerAdapter{
			name: "test",
			handler: func(args map[string]any) (any, error) {
				return nil, errors.New("handler error")
			},
		}

		_, err := adapter.Execute(nil, []byte(`{}`))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "handler error")
	})

	t.Run("execute returns parse error", func(t *testing.T) {
		adapter := &handlerAdapter{
			name:    "test",
			handler: func(args map[string]any) (any, error) { return nil, nil },
		}

		_, err := adapter.Execute(nil, []byte(`invalid json`))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse")
	})
}

func TestCheckClosed(t *testing.T) {
	t.Run("returns nil when not closed", func(t *testing.T) {
		conv := newTestConversation()
		err := conv.checkClosed()
		assert.NoError(t, err)
	})

	t.Run("returns error when closed", func(t *testing.T) {
		conv := newTestConversation()
		conv.closed = true
		err := conv.checkClosed()
		assert.Equal(t, ErrConversationClosed, err)
	})
}

func TestOnToolHTTP(t *testing.T) {
	t.Run("registers HTTP handler", func(t *testing.T) {
		conv := newTestConversation()
		cfg := sdktools.NewHTTPToolConfig("https://api.example.com/test",
			sdktools.WithMethod("POST"),
		)
		conv.OnToolHTTP("http_tool", cfg)

		conv.handlersMu.RLock()
		_, exists := conv.handlers["http_tool"]
		conv.handlersMu.RUnlock()

		assert.True(t, exists)
	})
}

func TestOnToolExecutor(t *testing.T) {
	t.Run("registers custom executor", func(t *testing.T) {
		conv := newTestConversation()
		// Add a tool to the pack so the executor can find it
		conv.pack = &pack.Pack{
			Tools: map[string]*pack.Tool{
				"custom_tool": {
					Name:        "custom_tool",
					Description: "Test tool",
					Parameters: map[string]any{
						"type":       "object",
						"properties": map[string]any{},
					},
				},
			},
		}

		executor := &mockExecutor{
			name:   "custom",
			result: []byte(`{"status": "ok"}`),
		}
		conv.OnToolExecutor("custom_tool", executor)

		conv.handlersMu.RLock()
		handler, exists := conv.handlers["custom_tool"]
		conv.handlersMu.RUnlock()

		assert.True(t, exists)

		// Test the handler
		result, err := handler(map[string]any{"input": "test"})
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("returns error if tool not in pack", func(t *testing.T) {
		conv := newTestConversation()
		conv.pack = &pack.Pack{} // Empty pack

		executor := &mockExecutor{
			name:   "custom",
			result: []byte(`{}`),
		}
		conv.OnToolExecutor("missing_tool", executor)

		conv.handlersMu.RLock()
		handler := conv.handlers["missing_tool"]
		conv.handlersMu.RUnlock()

		_, err := handler(map[string]any{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in pack")
	})
}

// mockExecutor is a test executor
type mockExecutor struct {
	name   string
	result []byte
	err    error
}

func (m *mockExecutor) Name() string { return m.name }

func (m *mockExecutor) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	return m.result, m.err
}

func TestLocalExecutorExecute(t *testing.T) {
	t.Run("successful execution", func(t *testing.T) {
		conv := newTestConversation()
		conv.OnTool("add", func(args map[string]any) (any, error) {
			a := args["a"].(float64)
			b := args["b"].(float64)
			return map[string]float64{"sum": a + b}, nil
		})

		// Get the localExecutor from the toolRegistry
		executor := &localExecutor{
			handlers: conv.handlers,
		}

		descriptor := &tools.ToolDescriptor{Name: "add"}
		args := json.RawMessage(`{"a": 1, "b": 2}`)

		result, err := executor.Execute(descriptor, args)
		assert.NoError(t, err)

		var parsed map[string]float64
		err = json.Unmarshal(result, &parsed)
		assert.NoError(t, err)
		assert.Equal(t, float64(3), parsed["sum"])
	})

	t.Run("handler not found", func(t *testing.T) {
		executor := &localExecutor{
			handlers: make(map[string]ToolHandler),
		}

		descriptor := &tools.ToolDescriptor{Name: "unknown"}
		args := json.RawMessage(`{}`)

		_, err := executor.Execute(descriptor, args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no handler registered")
	})

	t.Run("invalid args JSON", func(t *testing.T) {
		conv := newTestConversation()
		conv.OnTool("test", func(args map[string]any) (any, error) {
			return "ok", nil
		})

		executor := &localExecutor{
			handlers: conv.handlers,
		}

		descriptor := &tools.ToolDescriptor{Name: "test"}
		args := json.RawMessage(`{invalid json}`)

		_, err := executor.Execute(descriptor, args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse tool arguments")
	})

	t.Run("handler returns error", func(t *testing.T) {
		conv := newTestConversation()
		conv.OnTool("failing", func(args map[string]any) (any, error) {
			return nil, errors.New("handler failed")
		})

		executor := &localExecutor{
			handlers: conv.handlers,
		}

		descriptor := &tools.ToolDescriptor{Name: "failing"}
		args := json.RawMessage(`{}`)

		_, err := executor.Execute(descriptor, args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "handler failed")
	})
}

// =============================================================================
// Mock Types for Audio/TTS Testing
// =============================================================================

// audioMockStreamSession implements providers.StreamInputSession for testing
type audioMockStreamSession struct {
	closed   bool
	closeErr error
	chunks   chan providers.StreamChunk
}

func newAudioMockStreamSession() *audioMockStreamSession {
	return &audioMockStreamSession{
		chunks: make(chan providers.StreamChunk, 10),
	}
}

func (m *audioMockStreamSession) SendChunk(_ context.Context, _ *types.MediaChunk) error {
	if m.closed {
		return audio.ErrSessionClosed
	}
	return nil
}

func (m *audioMockStreamSession) SendText(_ context.Context, _ string) error {
	if m.closed {
		return audio.ErrSessionClosed
	}
	return nil
}

func (m *audioMockStreamSession) Response() <-chan providers.StreamChunk {
	return m.chunks
}

func (m *audioMockStreamSession) Close() error {
	m.closed = true
	close(m.chunks)
	return m.closeErr
}

func (m *audioMockStreamSession) Error() error { return nil }
func (m *audioMockStreamSession) Done() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

// mockStreamProvider implements providers.StreamInputSupport for testing
type mockStreamProvider struct {
	session    providers.StreamInputSession
	sessionErr error
}

func (m *mockStreamProvider) ID() string { return "mock-stream" }
func (m *mockStreamProvider) Predict(_ context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, nil
}
func (m *mockStreamProvider) PredictStream(_ context.Context, _ providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, nil
}
func (m *mockStreamProvider) SupportsStreaming() bool      { return true }
func (m *mockStreamProvider) ShouldIncludeRawOutput() bool { return false }
func (m *mockStreamProvider) Close() error                 { return nil }
func (m *mockStreamProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

func (m *mockStreamProvider) CreateStreamSession(_ context.Context, _ *providers.StreamInputRequest) (providers.StreamInputSession, error) {
	if m.sessionErr != nil {
		return nil, m.sessionErr
	}
	if m.session == nil {
		return newAudioMockStreamSession(), nil
	}
	return m.session, nil
}

func (m *mockStreamProvider) SupportsStreamInput() []string {
	return []string{types.ContentTypeAudio}
}

func (m *mockStreamProvider) GetStreamingCapabilities() providers.StreamingCapabilities {
	return providers.StreamingCapabilities{
		SupportedMediaTypes:  []string{types.ContentTypeAudio},
		BidirectionalSupport: true,
	}
}

// mockNonStreamProvider implements providers.Provider but not StreamInputSupport
type mockNonStreamProvider struct{}

func (m *mockNonStreamProvider) ID() string { return "mock-non-stream" }
func (m *mockNonStreamProvider) Predict(_ context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, nil
}
func (m *mockNonStreamProvider) PredictStream(_ context.Context, _ providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, nil
}
func (m *mockNonStreamProvider) SupportsStreaming() bool      { return false }
func (m *mockNonStreamProvider) ShouldIncludeRawOutput() bool { return false }
func (m *mockNonStreamProvider) Close() error                 { return nil }
func (m *mockNonStreamProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

// mockTTSReadCloser is a mock io.ReadCloser
type mockTTSReadCloser struct {
	*bytes.Reader
	closed bool
}

func newMockTTSReadCloser(data []byte) *mockTTSReadCloser {
	return &mockTTSReadCloser{Reader: bytes.NewReader(data)}
}

func (m *mockTTSReadCloser) Close() error {
	m.closed = true
	return nil
}

// convMockTTSService implements tts.Service for testing
type convMockTTSService struct {
	synthData []byte
	synthErr  error
	name      string
}

func newConvMockTTSService() *convMockTTSService {
	return &convMockTTSService{
		synthData: []byte("audio-data"),
		name:      "mock-tts",
	}
}

func (m *convMockTTSService) Name() string { return m.name }

func (m *convMockTTSService) Synthesize(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
	if m.synthErr != nil {
		return nil, m.synthErr
	}
	return newMockTTSReadCloser(m.synthData), nil
}

func (m *convMockTTSService) SupportedVoices() []tts.Voice {
	return []tts.Voice{{ID: "voice-1", Name: "Test Voice"}}
}

func (m *convMockTTSService) SupportedFormats() []tts.AudioFormat {
	return []tts.AudioFormat{tts.FormatMP3}
}

// convMockStreamingTTSService implements tts.StreamingService for testing
type convMockStreamingTTSService struct {
	convMockTTSService
	streamChunks []tts.AudioChunk
	streamErr    error
}

func newConvMockStreamingTTSService() *convMockStreamingTTSService {
	return &convMockStreamingTTSService{
		convMockTTSService: convMockTTSService{
			synthData: []byte("audio-data"),
			name:      "mock-streaming-tts",
		},
		streamChunks: []tts.AudioChunk{
			{Data: []byte("chunk1"), Index: 0},
			{Data: []byte("chunk2"), Index: 1, Final: true},
		},
	}
}

func (m *convMockStreamingTTSService) SynthesizeStream(_ context.Context, _ string, _ tts.SynthesisConfig) (<-chan tts.AudioChunk, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan tts.AudioChunk, len(m.streamChunks))
	for _, chunk := range m.streamChunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

// convMockVAD implements audio.VADAnalyzer for testing
type convMockVAD struct {
	state   audio.VADState
	stateCh chan audio.VADEvent
}

func newConvMockVAD() *convMockVAD {
	return &convMockVAD{
		state:   audio.VADStateQuiet,
		stateCh: make(chan audio.VADEvent, 10),
	}
}

func (m *convMockVAD) Name() string { return "mock-vad" }

func (m *convMockVAD) Analyze(_ context.Context, _ []byte) (float64, error) {
	return 0.0, nil
}

func (m *convMockVAD) State() audio.VADState {
	return m.state
}

func (m *convMockVAD) OnStateChange() <-chan audio.VADEvent {
	return m.stateCh
}

func (m *convMockVAD) Reset() {}

// convMockTurnDetector implements audio.TurnDetector for testing
type convMockTurnDetector struct {
	complete    bool
	userSpeak   bool
}

func (m *convMockTurnDetector) Name() string { return "mock-turn-detector" }

func (m *convMockTurnDetector) ProcessAudio(_ context.Context, _ []byte) (bool, error) {
	return m.complete, nil
}

func (m *convMockTurnDetector) ProcessVADState(_ context.Context, _ audio.VADState) (bool, error) {
	return m.complete, nil
}

func (m *convMockTurnDetector) IsUserSpeaking() bool {
	return m.userSpeak
}

func (m *convMockTurnDetector) Reset() {}

// =============================================================================
// Tests for OpenAudioSession
// =============================================================================

func TestConversation_OpenAudioSession(t *testing.T) {
	t.Run("success with stream provider", func(t *testing.T) {
		conv := newTestConversation()
		conv.provider = &mockStreamProvider{}
		conv.config = &config{}

		session, err := conv.OpenAudioSession(context.Background())
		require.NoError(t, err)
		require.NotNil(t, session)
		_ = session.Close()
	})

	t.Run("error when conversation closed", func(t *testing.T) {
		conv := newTestConversation()
		conv.provider = &mockStreamProvider{}
		conv.config = &config{}
		conv.closed = true

		_, err := conv.OpenAudioSession(context.Background())
		assert.ErrorIs(t, err, ErrConversationClosed)
	})

	t.Run("error when provider does not support streaming", func(t *testing.T) {
		conv := newTestConversation()
		conv.provider = &mockNonStreamProvider{}
		conv.config = &config{}

		_, err := conv.OpenAudioSession(context.Background())
		assert.ErrorIs(t, err, ErrProviderNotStreamCapable)
	})

	t.Run("error when stream session creation fails", func(t *testing.T) {
		conv := newTestConversation()
		conv.provider = &mockStreamProvider{
			sessionErr: errors.New("session creation failed"),
		}
		conv.config = &config{}

		_, err := conv.OpenAudioSession(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session creation failed")
	})

	t.Run("applies session options", func(t *testing.T) {
		conv := newTestConversation()
		conv.provider = &mockStreamProvider{}
		conv.config = &config{}

		mockVAD := newConvMockVAD()
		mockTD := &convMockTurnDetector{}

		session, err := conv.OpenAudioSession(
			context.Background(),
			WithSessionVAD(mockVAD),
			WithSessionTurnDetector(mockTD),
			WithInterruptionStrategy(audio.InterruptionImmediate),
			WithAutoCompleteTurn(),
		)
		require.NoError(t, err)
		require.NotNil(t, session)
		_ = session.Close()
	})

	t.Run("uses conversation-level turn detector when not overridden", func(t *testing.T) {
		conv := newTestConversation()
		conv.provider = &mockStreamProvider{}
		mockTD := &convMockTurnDetector{}
		conv.config = &config{
			turnDetector: mockTD,
		}

		session, err := conv.OpenAudioSession(context.Background())
		require.NoError(t, err)
		require.NotNil(t, session)
		_ = session.Close()
	})

	t.Run("builds system message from prompt", func(t *testing.T) {
		conv := newTestConversation()
		conv.provider = &mockStreamProvider{}
		conv.config = &config{}
		conv.prompt = &pack.Prompt{
			ID:             "test",
			SystemTemplate: "You are a {{role}} assistant.",
		}
		conv.SetVar("role", "helpful")

		session, err := conv.OpenAudioSession(context.Background())
		require.NoError(t, err)
		require.NotNil(t, session)
		_ = session.Close()
	})
}

// =============================================================================
// Tests for SpeakResponse
// =============================================================================

func TestConversation_SpeakResponse(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		conv := newTestConversation()
		conv.config = &config{
			ttsService: newConvMockTTSService(),
		}

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "Hello, world!",
			},
		}

		reader, err := conv.SpeakResponse(context.Background(), resp)
		require.NoError(t, err)
		require.NotNil(t, reader)

		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, []byte("audio-data"), data)

		_ = reader.Close()
	})

	t.Run("error when conversation closed", func(t *testing.T) {
		conv := newTestConversation()
		conv.config = &config{
			ttsService: newConvMockTTSService(),
		}
		conv.closed = true

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "Hello",
			},
		}

		_, err := conv.SpeakResponse(context.Background(), resp)
		assert.ErrorIs(t, err, ErrConversationClosed)
	})

	t.Run("error when TTS not configured", func(t *testing.T) {
		conv := newTestConversation()
		conv.config = &config{}

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "Hello",
			},
		}

		_, err := conv.SpeakResponse(context.Background(), resp)
		assert.ErrorIs(t, err, ErrNoTTSConfigured)
	})

	t.Run("error when response text is empty", func(t *testing.T) {
		conv := newTestConversation()
		conv.config = &config{
			ttsService: newConvMockTTSService(),
		}

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "",
			},
		}

		_, err := conv.SpeakResponse(context.Background(), resp)
		assert.ErrorIs(t, err, tts.ErrEmptyText)
	})

	t.Run("error when TTS synthesis fails", func(t *testing.T) {
		mockTTS := newConvMockTTSService()
		mockTTS.synthErr = errors.New("synthesis failed")

		conv := newTestConversation()
		conv.config = &config{
			ttsService: mockTTS,
		}

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "Hello",
			},
		}

		_, err := conv.SpeakResponse(context.Background(), resp)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "synthesis failed")
	})

	t.Run("applies TTS options", func(t *testing.T) {
		conv := newTestConversation()
		conv.config = &config{
			ttsService: newConvMockTTSService(),
		}

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "Hello",
			},
		}

		reader, err := conv.SpeakResponse(
			context.Background(),
			resp,
			WithTTSVoice("custom-voice"),
			WithTTSFormat(tts.FormatWAV),
			WithTTSSpeed(1.5),
			WithTTSPitch(2.0),
			WithTTSLanguage("en-US"),
			WithTTSModel("tts-1-hd"),
		)
		require.NoError(t, err)
		require.NotNil(t, reader)
		_ = reader.Close()
	})
}

// =============================================================================
// Tests for SpeakResponseStream
// =============================================================================

func TestConversation_SpeakResponseStream(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		conv := newTestConversation()
		conv.config = &config{
			ttsService: newConvMockStreamingTTSService(),
		}

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "Hello, world!",
			},
		}

		chunks, err := conv.SpeakResponseStream(context.Background(), resp)
		require.NoError(t, err)
		require.NotNil(t, chunks)

		var received []tts.AudioChunk
		for chunk := range chunks {
			received = append(received, chunk)
		}

		assert.Len(t, received, 2)
		assert.Equal(t, []byte("chunk1"), received[0].Data)
		assert.Equal(t, []byte("chunk2"), received[1].Data)
		assert.True(t, received[1].Final)
	})

	t.Run("error when conversation closed", func(t *testing.T) {
		conv := newTestConversation()
		conv.config = &config{
			ttsService: newConvMockStreamingTTSService(),
		}
		conv.closed = true

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "Hello",
			},
		}

		_, err := conv.SpeakResponseStream(context.Background(), resp)
		assert.ErrorIs(t, err, ErrConversationClosed)
	})

	t.Run("error when TTS not configured", func(t *testing.T) {
		conv := newTestConversation()
		conv.config = &config{}

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "Hello",
			},
		}

		_, err := conv.SpeakResponseStream(context.Background(), resp)
		assert.ErrorIs(t, err, ErrNoTTSConfigured)
	})

	t.Run("error when TTS does not support streaming", func(t *testing.T) {
		conv := newTestConversation()
		conv.config = &config{
			ttsService: newConvMockTTSService(), // Non-streaming TTS
		}

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "Hello",
			},
		}

		_, err := conv.SpeakResponseStream(context.Background(), resp)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not support streaming")
	})

	t.Run("error when response text is empty", func(t *testing.T) {
		conv := newTestConversation()
		conv.config = &config{
			ttsService: newConvMockStreamingTTSService(),
		}

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "",
			},
		}

		_, err := conv.SpeakResponseStream(context.Background(), resp)
		assert.ErrorIs(t, err, tts.ErrEmptyText)
	})

	t.Run("error when streaming synthesis fails", func(t *testing.T) {
		mockTTS := newConvMockStreamingTTSService()
		mockTTS.streamErr = errors.New("streaming synthesis failed")

		conv := newTestConversation()
		conv.config = &config{
			ttsService: mockTTS,
		}

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "Hello",
			},
		}

		_, err := conv.SpeakResponseStream(context.Background(), resp)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "streaming synthesis failed")
	})

	t.Run("applies TTS options", func(t *testing.T) {
		conv := newTestConversation()
		conv.config = &config{
			ttsService: newConvMockStreamingTTSService(),
		}

		resp := &Response{
			message: &types.Message{
				Role:    "assistant",
				Content: "Hello",
			},
		}

		chunks, err := conv.SpeakResponseStream(
			context.Background(),
			resp,
			WithTTSVoice("custom-voice"),
			WithTTSSpeed(1.2),
		)
		require.NoError(t, err)
		require.NotNil(t, chunks)

		// Drain the channel
		for range chunks {
		}
	})
}

// =============================================================================
// Tests for buildSystemMessage
// =============================================================================

func TestConversation_buildSystemMessage(t *testing.T) {
	t.Run("empty when no prompt", func(t *testing.T) {
		conv := newTestConversation()
		conv.prompt = nil

		msg := conv.buildSystemMessage()
		assert.Empty(t, msg)
	})

	t.Run("empty when no system template", func(t *testing.T) {
		conv := newTestConversation()
		conv.prompt = &pack.Prompt{
			ID:             "test",
			SystemTemplate: "",
		}

		msg := conv.buildSystemMessage()
		assert.Empty(t, msg)
	})

	t.Run("returns template without variables", func(t *testing.T) {
		conv := newTestConversation()
		conv.prompt = &pack.Prompt{
			ID:             "test",
			SystemTemplate: "You are a helpful assistant.",
		}

		msg := conv.buildSystemMessage()
		assert.Equal(t, "You are a helpful assistant.", msg)
	})

	t.Run("substitutes variables with double braces", func(t *testing.T) {
		conv := newTestConversation()
		conv.prompt = &pack.Prompt{
			ID:             "test",
			SystemTemplate: "You are a {{role}} assistant that helps with {{task}}.",
		}
		conv.SetVar("role", "friendly")
		conv.SetVar("task", "coding")

		msg := conv.buildSystemMessage()
		assert.Equal(t, "You are a friendly assistant that helps with coding.", msg)
	})

	t.Run("substitutes variables with spaced braces", func(t *testing.T) {
		conv := newTestConversation()
		conv.prompt = &pack.Prompt{
			ID:             "test",
			SystemTemplate: "You are a {{ role }} assistant.",
		}
		conv.SetVar("role", "helpful")

		msg := conv.buildSystemMessage()
		assert.Equal(t, "You are a helpful assistant.", msg)
	})

	t.Run("handles missing variables gracefully", func(t *testing.T) {
		conv := newTestConversation()
		conv.prompt = &pack.Prompt{
			ID:             "test",
			SystemTemplate: "You are a {{role}} assistant.",
		}
		// role variable not set

		msg := conv.buildSystemMessage()
		assert.Equal(t, "You are a {{role}} assistant.", msg)
	})

	t.Run("handles multiple occurrences of same variable", func(t *testing.T) {
		conv := newTestConversation()
		conv.prompt = &pack.Prompt{
			ID:             "test",
			SystemTemplate: "{{name}} is a {{role}}. {{name}} helps users.",
		}
		conv.SetVar("name", "Claude")
		conv.SetVar("role", "assistant")

		msg := conv.buildSystemMessage()
		assert.Equal(t, "Claude is a assistant. Claude helps users.", msg)
	})
}
