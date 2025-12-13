package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/session"
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
	// Create a minimal textSession for tests - requires a pipeline
	// Since many tests don't actually execute, we create a minimal one
	minimalPipeline := &pipeline.Pipeline{} // Empty pipeline for tests that don't execute
	sess, err := session.NewTextSession(session.TextConfig{
		Variables: make(map[string]string),
		Pipeline:  minimalPipeline,
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create test session: %v", err))
	}
	return &Conversation{
		pack:           p,
		prompt:         &pack.Prompt{ID: "chat", SystemTemplate: "You are helpful."},
		promptName:     "chat",
		promptRegistry: p.ToPromptRegistry(),
		toolRegistry:   tools.NewRegistryWithRepository(p.ToToolRepository()),
		config:         &config{},
		handlers:       make(map[string]ToolHandler),
		textSession:    sess,
	}
}

func TestConversationSetVar(t *testing.T) {
	t.Skip("Skipping: SetVar requires textSession from full Open() initialization")
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

// testVariableProvider is a mock provider for testing
type testVariableProvider struct {
	name string
	vars map[string]string
	err  error
}

func (p *testVariableProvider) Name() string { return p.name }
func (p *testVariableProvider) Provide(ctx context.Context) (map[string]string, error) {
	return p.vars, p.err
}

func TestConversationGetVariablesWithProviders(t *testing.T) {
	t.Skip("Skipping: Variable providers are now handled by pipeline middleware, not SDK-level methods")
	// This test was verifying SDK-level variable provider resolution
	// Variable providers are now properly handled by VariableProviderMiddleware in the pipeline
	// Integration tests should verify this through full Open() + Send() flow
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
	ctx := context.Background()
	conv := newTestConversation()

	// Create a minimal text session
	store := statestore.NewMemoryStore()
	convID := "test-conv"

	// Create a dummy pipeline (not used for this test)
	// We'll create a minimal valid pipeline
	textSession, err := session.NewTextSession(session.TextConfig{
		ConversationID: convID,
		StateStore:     store,
		Pipeline:       &rtpipeline.Pipeline{}, // Minimal pipeline
	})
	require.NoError(t, err)
	conv.textSession = textSession

	// No state initially - should return empty array
	msgs := conv.Messages(ctx)
	assert.Empty(t, msgs)

	// Save state with messages
	state := &statestore.ConversationState{
		ID: convID,
		Messages: []types.Message{
			{Role: "user"},
			{Role: "assistant"},
		},
	}
	err = store.Save(ctx, state)
	require.NoError(t, err)

	// Now messages should be returned
	msgs = conv.Messages(ctx)
	assert.Len(t, msgs, 2)

	// Verify it's a copy
	msgs[0].Role = "modified"
	reloadedMsgs := conv.Messages(ctx)
	assert.Equal(t, "user", reloadedMsgs[0].Role)
}

func TestConversationClear(t *testing.T) {
	ctx := context.Background()
	conv := newTestConversation()

	// Create a minimal text session
	store := statestore.NewMemoryStore()
	convID := "test-conv"
	textSession, err := session.NewTextSession(session.TextConfig{
		ConversationID: convID,
		StateStore:     store,
		Pipeline:       &rtpipeline.Pipeline{},
	})
	require.NoError(t, err)
	conv.textSession = textSession

	// Add some state
	state := &statestore.ConversationState{
		ID:         convID,
		Messages:   []types.Message{{Role: "user"}},
		TokenCount: 100,
	}
	err = store.Save(ctx, state)
	require.NoError(t, err)

	// Clear it
	conv.Clear()

	// Verify messages are cleared
	msgs := conv.Messages(ctx)
	assert.Empty(t, msgs)
}

func TestConversationClearNilState(t *testing.T) {
	conv := newTestConversation()
	// Should not panic with nil state
	conv.Clear()
}

func TestConversationFork(t *testing.T) {
	ctx := context.Background()
	conv := newTestConversation()

	// Create a minimal text session
	store := statestore.NewMemoryStore()
	convID := "original"
	textSession, err := session.NewTextSession(session.TextConfig{
		ConversationID: convID,
		StateStore:     store,
		Pipeline:       &rtpipeline.Pipeline{},
	})
	require.NoError(t, err)
	conv.textSession = textSession

	conv.SetVar("name", "Alice")
	conv.OnTool("tool1", func(args map[string]any) (any, error) { return nil, nil })

	// Set up state in the session's store
	state := &statestore.ConversationState{
		ID:         convID,
		Messages:   []types.Message{{Role: "user"}},
		TokenCount: 50,
	}
	err = store.Save(ctx, state)
	require.NoError(t, err)

	fork := conv.Fork()

	// Verify fork has same data
	assert.Equal(t, "Alice", fork.GetVar("name"))
	fork.handlersMu.RLock()
	_, hasHandler := fork.handlers["tool1"]
	fork.handlersMu.RUnlock()
	assert.True(t, hasHandler)

	// Verify fork has independent ID
	assert.Contains(t, fork.ID(), "fork")

	// Fork should have copied internalStore and have the same messages
	assert.Len(t, fork.Messages(ctx), 1)

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
	t.Skip("Skipping: Send now requires full Open() initialization with textSession")
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
	t.Skip("Skipping: ID requires textSession from full Open() initialization")
	conv := newTestConversation()

	assert.Equal(t, "test-id-123", conv.ID())
}

func TestConversationIDAutoGenerated(t *testing.T) {
	t.Skip("Skipping: integration test requires full Open() initialization")
	// This test verifies that initInternalStateStore generates a conversation ID
	// when a conversation is created. The StateStore middleware requires a non-empty ID.
	conv := newTestConversation()

	// initInternalStateStore should set both the store and the ID
	initInternalStateStore(conv, &config{})

	assert.NotEmpty(t, conv.ID(), "conversation ID should be auto-generated")
	assert.NotNil(t, conv.textSession, "text session should be initialized")
}

func TestConversationStateStoreMiddlewareIntegration(t *testing.T) {
	t.Skip("Skipping: integration test requires full Open() initialization")
	// This test verifies that Send() works with StateStore middleware.
	// The StateStore middleware requires a valid (non-empty) conversation ID.
	conv := newTestConversation()
	initInternalStateStore(conv, &config{}) // This sets up both store and ID

	// Use existing mockStreamProvider (no extra setup needed)
	conv.provider = &mockStreamProvider{}

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Hello")
	require.NoError(t, err, "Send should not fail with valid conversation ID and StateStore")
	assert.NotNil(t, resp)
}

func TestConversationEventBus(t *testing.T) {
	conv := newTestConversation()
	conv.config.eventBus = events.NewEventBus()

	bus := conv.EventBus()
	assert.NotNil(t, bus)
}

func TestConversationToolRegistry(t *testing.T) {
	conv := newTestConversation()
	// Returns the actual tool registry
	assert.NotNil(t, conv.ToolRegistry())
}

func TestConversationStream(t *testing.T) {
	t.Skip("Skipping: Stream requires full Open() initialization with textSession")
	conv := newTestConversation()

	ch := conv.Stream(context.Background(), "hello")
	chunk := <-ch

	// Stream falls back to Send, which works without a provider
	// but returns an empty response
	assert.NoError(t, chunk.Error)
	assert.Equal(t, ChunkDone, chunk.Type)
}

func TestGetVariables(t *testing.T) {
	conv := newTestConversation()
	conv.SetVar("key1", "value1")
	conv.SetVar("key2", "value2")

	// GetVar works through the session now
	assert.Equal(t, "value1", conv.GetVar("key1"))
	assert.Equal(t, "value2", conv.GetVar("key2"))

	// Variables are managed by session - this tests the public API
	conv.SetVar("key1", "modified")
	assert.Equal(t, "modified", conv.GetVar("key1"))
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

func (m *mockStreamProvider) CreateStreamSession(_ context.Context, _ *providers.StreamingInputConfig) (providers.StreamInputSession, error) {
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
	complete  bool
	userSpeak bool
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
