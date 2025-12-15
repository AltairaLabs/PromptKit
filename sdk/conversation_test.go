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
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
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
	sess, err := session.NewUnarySession(session.UnarySessionConfig{
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
		mode:           UnaryMode,
		unarySession:   sess,
	}
}

func TestConversationSetVar(t *testing.T) {
	t.Skip("Skipping: SetVar requires textSession from full Open() initialization")
	conv := newTestConversation()

	conv.SetVar("name", "Alice")
	val, _ := conv.GetVar("name")
	assert.Equal(t, "Alice", val)

	conv.SetVar("name", "Bob")
	val, _ = conv.GetVar("name")
	assert.Equal(t, "Bob", val)
}

func TestConversationSetVars(t *testing.T) {
	conv := newTestConversation()

	conv.SetVars(map[string]any{
		"name": "Alice",
		"age":  30,
		"tier": "premium",
	})

	val1, _ := conv.GetVar("name")
	val2, _ := conv.GetVar("age")
	val3, _ := conv.GetVar("tier")
	assert.Equal(t, "Alice", val1)
	assert.Equal(t, "30", val2)
	assert.Equal(t, "premium", val3)
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

	name, _ := conv.GetVar("name")
	value, _ := conv.GetVar("value")
	assert.Equal(t, "TestUser", name)
	assert.Equal(t, "123", value)
}

func TestConversationGetVarNotSet(t *testing.T) {
	conv := newTestConversation()
	val, exists := conv.GetVar("nonexistent")
	assert.Equal(t, "", val)
	assert.False(t, exists)
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
	provider := mock.NewProvider("test", "test-model", false)
	pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
		// Return a minimal stage pipeline with provider stage for test
		providerStage := stage.NewProviderStage(provider, nil, nil, nil)
		return stage.NewPipelineBuilder().Chain(providerStage).Build()
	}
	duplexSession, err := session.NewDuplexSession(ctx, &session.DuplexSessionConfig{
		ConversationID:  convID,
		StateStore:      store,
		PipelineBuilder: pipelineBuilder,
		Provider:        provider,
	})
	require.NoError(t, err)
	conv.mode = DuplexMode
	conv.duplexSession = duplexSession

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
	provider := mock.NewProvider("test", "test-model", false)
	pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
		// Return a minimal stage pipeline with provider stage for test
		providerStage := stage.NewProviderStage(provider, nil, nil, nil)
		return stage.NewPipelineBuilder().Chain(providerStage).Build()
	}
	duplexSession, err := session.NewDuplexSession(ctx, &session.DuplexSessionConfig{
		ConversationID:  convID,
		StateStore:      store,
		PipelineBuilder: pipelineBuilder,
		Provider:        provider,
	})
	require.NoError(t, err)
	conv.mode = DuplexMode
	conv.duplexSession = duplexSession

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
	provider := mock.NewProvider("test", "test-model", false)
	pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
		// Return a minimal stage pipeline with provider stage for test
		providerStage := stage.NewProviderStage(provider, nil, nil, nil)
		return stage.NewPipelineBuilder().Chain(providerStage).Build()
	}
	duplexSession, err := session.NewDuplexSession(ctx, &session.DuplexSessionConfig{
		ConversationID:  convID,
		StateStore:      store,
		PipelineBuilder: pipelineBuilder,
		Provider:        provider,
	})
	require.NoError(t, err)
	conv.mode = DuplexMode
	conv.duplexSession = duplexSession

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
	forkVal, ok := fork.GetVar("name")
	assert.True(t, ok)
	assert.Equal(t, "Alice", forkVal)
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
	val, _ := conv.GetVar("name")
	forkVal, _ = fork.GetVar("name")
	assert.Equal(t, "Alice", val)
	assert.Equal(t, "Bob", forkVal)
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
	assert.NotNil(t, conv.unarySession, "unary session should be initialized")
	assert.Equal(t, UnaryMode, conv.mode, "should be in unary mode")
}

func TestConversationStateStoreMiddlewareIntegration(t *testing.T) {
	t.Skip("Skipping: integration test requires full Open() initialization")
	// This test verifies that Send() works with StateStore middleware.
	// The StateStore middleware requires a valid (non-empty) conversation ID.
	conv := newTestConversation()
	initInternalStateStore(conv, &config{}) // This sets up both store and ID

	// Use existing mockStreamProvider (no extra setup needed)
	conv.config.provider = &mockStreamProvider{}

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
	val1, _ := conv.GetVar("key1")
	val2, _ := conv.GetVar("key2")
	assert.Equal(t, "value1", val1)
	assert.Equal(t, "value2", val2)

	// Variables are managed by session - this tests the public API
	conv.SetVar("key1", "modified")
	val1, _ = conv.GetVar("key1")
	assert.Equal(t, "modified", val1)
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
// Duplex Mode Tests
// =============================================================================

func TestDuplexMethodsInUnaryMode(t *testing.T) {
	conv := newTestConversation()
	ctx := context.Background()

	// All duplex methods should return errors in unary mode
	err := conv.SendChunk(ctx, &providers.StreamChunk{Content: "test"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplex mode")

	err = conv.SendText(ctx, "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplex mode")

	_, err = conv.Response()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplex mode")

	_, err = conv.Done()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplex mode")

	err = conv.SessionError()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplex mode")
}

func TestDuplexMethodsWhenClosed(t *testing.T) {
	conv := newTestConversation()
	conv.mode = DuplexMode
	conv.closed = true
	ctx := context.Background()

	// All duplex methods should return ErrConversationClosed
	err := conv.SendChunk(ctx, &providers.StreamChunk{Content: "test"})
	assert.Equal(t, ErrConversationClosed, err)

	err = conv.SendText(ctx, "test")
	assert.Equal(t, ErrConversationClosed, err)

	_, err = conv.Response()
	assert.Equal(t, ErrConversationClosed, err)

	_, err = conv.Done()
	assert.Equal(t, ErrConversationClosed, err)

	err = conv.SessionError()
	assert.Equal(t, ErrConversationClosed, err)
}

func TestGetBaseSessionUnary(t *testing.T) {
	conv := newTestConversation()
	store := statestore.NewMemoryStore()
	provider := mock.NewProvider("test", "test-model", false)
	pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
		// Return a minimal stage pipeline with provider stage for test
		providerStage := stage.NewProviderStage(provider, nil, nil, nil)
		return stage.NewPipelineBuilder().Chain(providerStage).Build()
	}
	duplexSession, err := session.NewDuplexSession(context.Background(), &session.DuplexSessionConfig{
		ConversationID:  "test",
		StateStore:      store,
		PipelineBuilder: pipelineBuilder,
		Provider:        provider,
	})
	require.NoError(t, err)
	conv.mode = DuplexMode
	conv.duplexSession = duplexSession

	baseSession := conv.getBaseSession()
	assert.NotNil(t, baseSession)
	assert.Equal(t, "test", baseSession.ID())
}

func TestGetBaseSessionDuplex(t *testing.T) {
	conv := newTestConversation()
	conv.mode = DuplexMode

	// Create a duplex session
	ctx := context.Background()
	store := statestore.NewMemoryStore()
	provider := mock.NewProvider("test", "test-model", false)
	pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
		// Return a minimal stage pipeline with provider stage for test
		providerStage := stage.NewProviderStage(provider, nil, nil, nil)
		return stage.NewPipelineBuilder().Chain(providerStage).Build()
	}
	duplexSession, err := session.NewDuplexSession(ctx, &session.DuplexSessionConfig{
		ConversationID:  "test-duplex",
		StateStore:      store,
		PipelineBuilder: pipelineBuilder,
		Provider:        provider,
	})
	require.NoError(t, err)
	conv.duplexSession = duplexSession

	baseSession := conv.getBaseSession()
	assert.NotNil(t, baseSession)
	assert.Equal(t, "test-duplex", baseSession.ID())
}

func TestMessagesWithDifferentModes(t *testing.T) {
	ctx := context.Background()

	t.Run("UnaryMode", func(t *testing.T) {
		conv := newTestConversation()
		store := statestore.NewMemoryStore()
		provider := mock.NewProvider("test", "test-model", false)
		pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
			// Return a minimal stage pipeline with provider stage for test
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			return stage.NewPipelineBuilder().Chain(providerStage).Build()
		}
		duplexSession, err := session.NewDuplexSession(ctx, &session.DuplexSessionConfig{
			ConversationID:  "test",
			StateStore:      store,
			PipelineBuilder: pipelineBuilder,
			Provider:        provider,
		})
		require.NoError(t, err)
		conv.mode = DuplexMode
		conv.duplexSession = duplexSession

		// Save a message
		state := &statestore.ConversationState{
			ID:       "test",
			Messages: []types.Message{{Role: "user", Content: "hello"}},
		}
		err = store.Save(ctx, state)
		require.NoError(t, err)

		messages := conv.Messages(ctx)
		assert.Len(t, messages, 1)
		assert.Equal(t, "hello", messages[0].Content)
	})

	t.Run("DuplexMode", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = DuplexMode

		store := statestore.NewMemoryStore()
		provider := mock.NewProvider("test", "test-model", false)
		pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
			// Return a minimal stage pipeline with provider stage for test
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			return stage.NewPipelineBuilder().Chain(providerStage).Build()
		}
		duplexSession, err := session.NewDuplexSession(ctx, &session.DuplexSessionConfig{
			ConversationID:  "test-duplex",
			StateStore:      store,
			PipelineBuilder: pipelineBuilder,
			Provider:        provider,
		})
		require.NoError(t, err)
		conv.duplexSession = duplexSession

		// Save a message
		state := &statestore.ConversationState{
			ID:       "test-duplex",
			Messages: []types.Message{{Role: "assistant", Content: "hi"}},
		}
		err = store.Save(ctx, state)
		require.NoError(t, err)

		messages := conv.Messages(ctx)
		assert.Len(t, messages, 1)
		assert.Equal(t, "hi", messages[0].Content)
	})
}

func TestClearWithDifferentModes(t *testing.T) {
	ctx := context.Background()

	t.Run("UnaryMode", func(t *testing.T) {
		conv := newTestConversation()
		store := statestore.NewMemoryStore()
		provider := mock.NewProvider("test", "test-model", false)
		pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
			// Return a minimal stage pipeline with provider stage for test
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			return stage.NewPipelineBuilder().Chain(providerStage).Build()
		}
		duplexSession, err := session.NewDuplexSession(ctx, &session.DuplexSessionConfig{
			ConversationID:  "test",
			StateStore:      store,
			PipelineBuilder: pipelineBuilder,
			Provider:        provider,
		})
		require.NoError(t, err)
		conv.mode = DuplexMode
		conv.duplexSession = duplexSession

		// Add messages
		state := &statestore.ConversationState{
			ID:       "test",
			Messages: []types.Message{{Role: "user", Content: "hello"}},
		}
		err = store.Save(ctx, state)
		require.NoError(t, err)

		// Clear
		err = conv.Clear()
		assert.NoError(t, err)

		// Verify cleared
		messages := conv.Messages(ctx)
		assert.Empty(t, messages)
	})

	t.Run("DuplexMode", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = DuplexMode

		store := statestore.NewMemoryStore()
		provider := mock.NewProvider("test", "test-model", false)
		pipelineBuilder := func(ctx context.Context, p providers.Provider, ps providers.StreamInputSession, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
			// Return a minimal stage pipeline with provider stage for test
			providerStage := stage.NewProviderStage(provider, nil, nil, nil)
			return stage.NewPipelineBuilder().Chain(providerStage).Build()
		}
		duplexSession, err := session.NewDuplexSession(ctx, &session.DuplexSessionConfig{
			ConversationID:  "test-duplex",
			StateStore:      store,
			PipelineBuilder: pipelineBuilder,
			Provider:        provider,
		})
		require.NoError(t, err)
		conv.duplexSession = duplexSession

		// Add messages
		state := &statestore.ConversationState{
			ID:       "test-duplex",
			Messages: []types.Message{{Role: "assistant", Content: "hi"}},
		}
		err = store.Save(ctx, state)
		require.NoError(t, err)

		// Clear should close duplex session first
		err = conv.Clear()
		assert.NoError(t, err)

		// Verify cleared
		messages := conv.Messages(ctx)
		assert.Empty(t, messages)
	})
}

// =============================================================================
// Send and Pipeline Execution Tests
// =============================================================================

// testMockProvider is now provided by runtime/providers/mock package
// Use mock.NewProvider() or mock.NewProviderWithRepository() for testing

// errorMockRepository returns an error for GetTurn and GetResponse calls
type errorMockRepository struct {
	err error
}

func (e *errorMockRepository) GetResponse(ctx context.Context, params mock.ResponseParams) (string, error) {
	return "", e.err
}

func (e *errorMockRepository) GetTurn(ctx context.Context, params mock.ResponseParams) (*mock.Turn, error) {
	return nil, e.err
}

func TestSendWithMockProvider(t *testing.T) {
	ctx := context.Background()

	// Create a conversation with a mock provider
	repo := mock.NewInMemoryMockRepository("Hello! How can I help you?")
	mockProv := mock.NewProviderWithRepository("test-mock", "test-model", false, repo)
	store := statestore.NewMemoryStore()

	p := &pack.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {
				ID:             "chat",
				SystemTemplate: "You are helpful.",
			},
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

	// Build pipeline and create session
	pipeline, err := conv.buildPipelineWithParams(store, "test-conv", nil)
	require.NoError(t, err)

	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "test-conv",
		StateStore:     store,
		Pipeline:       pipeline,
	})
	require.NoError(t, err)
	conv.unarySession = unarySession

	// Test Send
	resp, err := conv.Send(ctx, "Hello")
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "Hello! How can I help you?", resp.Text())
	assert.Greater(t, resp.Duration().Nanoseconds(), int64(0))
}

func TestSendWithProviderError(t *testing.T) {
	// TODO: Error propagation through stage-based pipeline needs review
	t.Skip("Error propagation through stage-based pipeline needs review")

	ctx := context.Background()

	// Create a conversation with a failing mock provider
	// Use a repository that returns an error
	repo := &errorMockRepository{err: fmt.Errorf("provider error")}
	mockProv := mock.NewProviderWithRepository("test-mock", "test-model", false, repo)
	store := statestore.NewMemoryStore()

	p := &pack.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {
				ID:             "chat",
				SystemTemplate: "You are helpful.",
			},
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

	// Build pipeline and create session
	pipeline, err := conv.buildPipelineWithParams(store, "test-conv", nil)
	require.NoError(t, err)

	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "test-conv",
		StateStore:     store,
		Pipeline:       pipeline,
	})
	require.NoError(t, err)
	conv.unarySession = unarySession

	// Test Send with error
	_, err = conv.Send(ctx, "Hello")
	assert.Error(t, err)
}

func TestSendInDuplexMode(t *testing.T) {
	ctx := context.Background()
	conv := newTestConversation()
	conv.mode = DuplexMode

	_, err := conv.Send(ctx, "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unary mode")
}

func TestSendWhenClosed(t *testing.T) {
	ctx := context.Background()
	conv := newTestConversation()
	conv.closed = true

	_, err := conv.Send(ctx, "test")
	assert.Equal(t, ErrConversationClosed, err)
}

func TestBuildResponse(t *testing.T) {
	conv := newTestConversation()

	t.Run("WithMessage", func(t *testing.T) {
		result := &rtpipeline.ExecutionResult{
			Response: &rtpipeline.Response{
				Role:    "assistant",
				Content: "Test response",
			},
			CostInfo: types.CostInfo{
				InputTokens:  10,
				OutputTokens: 20,
				TotalCost:    0.03,
			},
		}

		resp := conv.buildResponse(result, time.Now())
		assert.NotNil(t, resp)
		assert.Equal(t, "Test response", resp.Text())
		assert.NotNil(t, resp.message.CostInfo)
		assert.Equal(t, 0.03, resp.message.CostInfo.TotalCost)
	})

	t.Run("WithToolCalls", func(t *testing.T) {
		result := &rtpipeline.ExecutionResult{
			Response: &rtpipeline.Response{
				Role:    "assistant",
				Content: "Let me check that",
				ToolCalls: []types.MessageToolCall{
					{ID: "call1", Name: "get_weather", Args: json.RawMessage(`{"city": "SF"}`)},
				},
			},
		}

		resp := conv.buildResponse(result, time.Now())
		assert.NotNil(t, resp)
		assert.Len(t, resp.toolCalls, 1)
		assert.Equal(t, "get_weather", resp.toolCalls[0].Name)
	})

	t.Run("WithValidations", func(t *testing.T) {
		result := &rtpipeline.ExecutionResult{
			Response: &rtpipeline.Response{
				Role:    "assistant",
				Content: "Response",
			},
			Messages: []types.Message{
				{
					Role:    "assistant",
					Content: "Response",
					Validations: []types.ValidationResult{
						{ValidatorType: "test-validator", Passed: true},
					},
				},
			},
		}

		resp := conv.buildResponse(result, time.Now())
		assert.NotNil(t, resp)
		assert.Len(t, resp.validations, 1)
		assert.True(t, resp.validations[0].Passed)
	})

	t.Run("NilResponse", func(t *testing.T) {
		result := &rtpipeline.ExecutionResult{
			Response: nil,
		}

		resp := conv.buildResponse(result, time.Now())
		assert.NotNil(t, resp)
		assert.Nil(t, resp.message)
	})
}

func TestSendWithOptions(t *testing.T) {
	ctx := context.Background()
	repo := mock.NewInMemoryMockRepository("Response")
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

	pipeline, err := conv.buildPipelineWithParams(store, "test-conv", nil)
	require.NoError(t, err)

	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "test-conv",
		StateStore:     store,
		Pipeline:       pipeline,
	})
	require.NoError(t, err)
	conv.unarySession = unarySession

	// Test Send works with options
	resp, err := conv.Send(ctx, "test")
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestForkDuplexMode(t *testing.T) {
	// Skip this test as it requires complex duplex session setup
	t.Skip("Duplex Fork requires full bidirectional streaming setup")
}

func TestClose(t *testing.T) {
	conv := newTestConversation()

	err := conv.Close()
	assert.NoError(t, err)
	assert.True(t, conv.closed)

	// Second close should be no-op
	err = conv.Close()
	assert.NoError(t, err)
}

func TestCloseWithProvider(t *testing.T) {
	mockProv := mock.NewProvider("test-mock", "test-model", false)
	conv := newTestConversation()
	conv.config.provider = mockProv

	err := conv.Close()
	assert.NoError(t, err)
	assert.True(t, conv.closed)
}

func TestSendMultipleErrorCases(t *testing.T) {
	ctx := context.Background()

	t.Run("nil message", func(t *testing.T) {
		conv := newTestConversation()
		_, err := conv.Send(ctx, nil)
		assert.Error(t, err)
	})
}

func TestMessagesErrorCases(t *testing.T) {
	t.Run("unary mode success", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = UnaryMode
		messages := conv.Messages(context.Background())
		assert.NotNil(t, messages)
	})
}

func TestSetVarsFromEnv(t *testing.T) {
	conv := newTestConversation()

	// Set environment variables
	os.Setenv("PROMPTKIT_VAR1", "value1")
	os.Setenv("PROMPTKIT_VAR2", "value2")
	os.Setenv("OTHER_VAR", "should_not_be_set")
	defer os.Unsetenv("PROMPTKIT_VAR1")
	defer os.Unsetenv("PROMPTKIT_VAR2")
	defer os.Unsetenv("OTHER_VAR")

	conv.SetVarsFromEnv("PROMPTKIT_")

	val1, ok1 := conv.GetVar("var1")
	val2, ok2 := conv.GetVar("var2")
	_, ok3 := conv.GetVar("other_var")

	assert.True(t, ok1)
	assert.Equal(t, "value1", val1)
	assert.True(t, ok2)
	assert.Equal(t, "value2", val2)
	assert.False(t, ok3)
}

func TestBuildPipelineWithParameters(t *testing.T) {
	store := statestore.NewMemoryStore()

	// Test with prompt parameters set
	maxTokens := 2000
	temperature := 0.7
	p := &pack.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {
				ID:             "chat",
				SystemTemplate: "You are helpful.",
				Parameters: &pack.Parameters{
					MaxTokens:   &maxTokens,
					Temperature: &temperature,
				},
			},
		},
	}

	conv := &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: p.ToPromptRegistry(),
		toolRegistry:   tools.NewRegistry(),
		config:         &config{provider: mock.NewProvider("test-mock", "test-model", false)},
		mode:           UnaryMode,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
	}

	pipeline, err := conv.buildPipelineWithParams(store, "test-conv", nil)
	assert.NoError(t, err)
	assert.NotNil(t, pipeline)
}

func TestSendWithDifferentModes(t *testing.T) {
	ctx := context.Background()

	t.Run("duplex mode returns error", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = DuplexMode

		_, err := conv.Send(ctx, "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Send() only available in unary mode")
	})

	t.Run("closed conversation returns error", func(t *testing.T) {
		conv := newTestConversation()
		conv.closed = true

		_, err := conv.Send(ctx, "test")
		assert.Equal(t, ErrConversationClosed, err)
	})
}

func TestCloseWithErrors(t *testing.T) {
	t.Run("duplex mode close", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = DuplexMode

		err := conv.Close()
		assert.NoError(t, err) // No session to close, so no error
		assert.True(t, conv.closed)
	})

	t.Run("already closed returns nil", func(t *testing.T) {
		conv := newTestConversation()
		conv.closed = true

		err := conv.Close()
		assert.NoError(t, err) // Safe to call multiple times
	})
}

func TestForkErrorHandling(t *testing.T) {
	t.Run("fork preserves handlers", func(t *testing.T) {
		conv := newTestConversation()
		conv.OnTool("test_tool", func(args map[string]any) (any, error) {
			return "result", nil
		})

		forked := conv.Fork()

		conv.handlersMu.RLock()
		_, origHas := conv.handlers["test_tool"]
		conv.handlersMu.RUnlock()

		forked.handlersMu.RLock()
		_, forkHas := forked.handlers["test_tool"]
		forked.handlersMu.RUnlock()

		assert.True(t, origHas)
		assert.True(t, forkHas)
	})
}

// =============================================================================
