package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	mock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/stt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/v2/session"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConversation() *Conversation {
	p := &pack.Pack{Pack: packspec.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {ID: "chat", SystemTemplate: "You are helpful."},
		},
	}}
	// Create a minimal textSession for tests - requires a pipeline with at least one stage
	promptRegistry := pack.ToPromptRegistry(p)
	minimalPipeline, _ := stage.NewPipelineBuilder().
		Chain(stage.NewPromptAssemblyStageWithTurnState(promptRegistry, "chat", nil, stage.NewTurnState())).
		Build()

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
		promptRegistry: pack.ToPromptRegistry(p),
		toolRegistry:   tools.NewRegistryWithRepository(pack.ToToolRepository(p)),
		config:         &config{},
		handlers:       make(map[string]ToolHandler),
		mode:           UnaryMode,
		unarySession:   sess,
	}
}

func TestConversationGetVarNotSet(t *testing.T) {
	conv := newTestConversation()
	val, exists := conv.GetVar("nonexistent")
	assert.Equal(t, "", val)
	assert.False(t, exists)
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

func TestConversationVarsWhenClosed(t *testing.T) {
	conv := newTestConversation()
	conv.ctxHandlers = make(map[string]ToolHandlerCtx)
	conv.clientHandlers = make(map[string]ClientToolHandler)
	_ = conv.Close()

	// SetVar should silently return on closed conversation
	conv.SetVar("key", "value")
	// SetVars should silently return on closed conversation
	conv.SetVars(map[string]any{"key": "value"})
	// SetVarsFromEnv should silently return on closed conversation
	conv.SetVarsFromEnv("TEST_PREFIX_")
	// GetVar should return empty on closed conversation
	val, ok := conv.GetVar("key")
	assert.False(t, ok)
	assert.Empty(t, val)
}

func TestConversationClearWhenClosed(t *testing.T) {
	conv := newTestConversation()
	conv.ctxHandlers = make(map[string]ToolHandlerCtx)
	conv.clientHandlers = make(map[string]ClientToolHandler)
	_ = conv.Close()

	err := conv.Clear()
	assert.Error(t, err)
}

func TestOnToolHTTP(t *testing.T) {
	t.Run("registers HTTP handler", func(t *testing.T) {
		conv := newTestConversation()
		cfg := sdktools.NewHTTPToolConfig("https://api.example.com/test",
			sdktools.WithMethod("POST"),
		)
		conv.OnToolHTTP("http_tool", cfg)

		conv.handlersMu.RLock()
		_, exists := conv.ctxHandlers["http_tool"]
		conv.handlersMu.RUnlock()

		assert.True(t, exists)
	})
}

func TestOnToolExecutor(t *testing.T) {
	t.Run("registers custom executor", func(t *testing.T) {
		conv := newTestConversation()
		// Add a tool to the pack so the executor can find it
		conv.pack = &pack.Pack{Pack: packspec.Pack{
			Tools: map[string]*pack.Tool{
				"custom_tool": {
					Name:        "custom_tool",
					Description: "Test tool",
					Parameters:  &packspec.ToolParameters{Type: "object", Properties: map[string]map[string]any{}},
				},
			},
		}}

		executor := &mockExecutor{
			name:   "custom",
			result: []byte(`{"status": "ok"}`),
		}
		conv.OnToolExecutor("custom_tool", executor)

		conv.handlersMu.RLock()
		handler, exists := conv.ctxHandlers["custom_tool"]
		conv.handlersMu.RUnlock()

		assert.True(t, exists)

		// Test the handler — context is now propagated
		result, err := handler(context.Background(), map[string]any{"input": "test"})
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
		handler := conv.ctxHandlers["missing_tool"]
		conv.handlersMu.RUnlock()

		_, err := handler(context.Background(), map[string]any{})
		assert.ErrorIs(t, err, ErrToolNotInPack)
		assert.Equal(t, `tool "missing_tool" not found in pack`, err.Error(), "the text is unchanged")
	})
}

// mockExecutor is a test executor
type mockExecutor struct {
	name   string
	result []byte
	err    error
}

func (m *mockExecutor) Name() string { return m.name }

func (m *mockExecutor) Execute(_ context.Context, descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
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

		result, err := executor.Execute(context.Background(), descriptor, args)
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

		_, err := executor.Execute(context.Background(), descriptor, args)
		assert.ErrorIs(t, err, ErrToolNotRegistered)
		assert.Equal(t, "no handler registered for tool: unknown", err.Error(), "the text is unchanged")
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

		_, err := executor.Execute(context.Background(), descriptor, args)
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

		_, err := executor.Execute(context.Background(), descriptor, args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "handler failed")
	})
}

func TestLocalExecutorCtxHandler(t *testing.T) {
	t.Run("context-aware handler receives pipeline context", func(t *testing.T) {
		type ctxKey string
		var receivedCtx context.Context

		executor := &localExecutor{
			handlers: make(map[string]ToolHandler),
			ctxHandlers: map[string]ToolHandlerCtx{
				"search": func(ctx context.Context, args map[string]any) (any, error) {
					receivedCtx = ctx
					return "found", nil
				},
			},
		}

		pipelineCtx := context.WithValue(context.Background(), ctxKey("trace"), "abc123")
		descriptor := &tools.ToolDescriptor{Name: "search"}
		result, err := executor.Execute(pipelineCtx, descriptor, json.RawMessage(`{}`))

		assert.NoError(t, err)
		assert.Equal(t, `"found"`, string(result))
		assert.Equal(t, "abc123", receivedCtx.Value(ctxKey("trace")))
	})

	t.Run("ctxHandler takes priority over plain handler", func(t *testing.T) {
		executor := &localExecutor{
			handlers: map[string]ToolHandler{
				"search": func(args map[string]any) (any, error) {
					return "plain", nil
				},
			},
			ctxHandlers: map[string]ToolHandlerCtx{
				"search": func(ctx context.Context, args map[string]any) (any, error) {
					return "ctx-aware", nil
				},
			},
		}

		descriptor := &tools.ToolDescriptor{Name: "search"}
		result, err := executor.Execute(context.Background(), descriptor, json.RawMessage(`{}`))

		assert.NoError(t, err)
		assert.Equal(t, `"ctx-aware"`, string(result))
	})

	t.Run("falls back to plain handler when no ctxHandler", func(t *testing.T) {
		executor := &localExecutor{
			handlers: map[string]ToolHandler{
				"search": func(args map[string]any) (any, error) {
					return "plain", nil
				},
			},
			ctxHandlers: make(map[string]ToolHandlerCtx),
		}

		descriptor := &tools.ToolDescriptor{Name: "search"}
		result, err := executor.Execute(context.Background(), descriptor, json.RawMessage(`{}`))

		assert.NoError(t, err)
		assert.Equal(t, `"plain"`, string(result))
	})
}

func TestOnToolCtxStoresCtxHandler(t *testing.T) {
	conv := newTestConversation()

	var receivedCtx context.Context
	conv.OnToolCtx("search", func(ctx context.Context, args map[string]any) (any, error) {
		receivedCtx = ctx
		return "ok", nil
	})

	// ctxHandlers should have the handler, handlers should not
	assert.NotNil(t, conv.ctxHandlers["search"])
	assert.Nil(t, conv.handlers["search"])

	// Execute through localExecutor to verify context propagation
	type ctxKey string
	pipelineCtx := context.WithValue(context.Background(), ctxKey("span"), "test-span")
	executor := &localExecutor{handlers: conv.handlers, ctxHandlers: conv.ctxHandlers}
	result, err := executor.Execute(pipelineCtx, &tools.ToolDescriptor{Name: "search"}, json.RawMessage(`{}`))

	assert.NoError(t, err)
	assert.Equal(t, `"ok"`, string(result))
	assert.Equal(t, "test-span", receivedCtx.Value(ctxKey("span")))
}

// =============================================================================
// Mock Types for Audio/TTS Testing
// =============================================================================

// errMockSessionClosed is used by mock session for testing
var errMockSessionClosed = fmt.Errorf("session closed")

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
		return errMockSessionClosed
	}
	return nil
}

func (m *audioMockStreamSession) SendText(_ context.Context, _ string) error {
	if m.closed {
		return errMockSessionClosed
	}
	return nil
}

func (m *audioMockStreamSession) SendSystemContext(_ context.Context, _ string) error {
	if m.closed {
		return errMockSessionClosed
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

// BargeIn satisfies providers.StreamInputSession; this mock never signals barge-in.
func (m *audioMockStreamSession) BargeIn() <-chan struct{} { return nil }

// mockStreamProvider implements providers.StreamInputSupport for testing
type mockStreamProvider struct {
	base.Implementation
	session    providers.StreamInputSession
	sessionErr error
}

func (m *mockStreamProvider) ID() string    { return "mock-stream" }
func (m *mockStreamProvider) Model() string { return "mock-stream-model" }
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
type mockNonStreamProvider struct {
	base.Implementation
}

func (m *mockNonStreamProvider) ID() string    { return "mock-non-stream" }
func (m *mockNonStreamProvider) Model() string { return "mock-non-stream-model" }
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
	streamChunks []audio.Chunk
	streamErr    error
}

func newConvMockStreamingTTSService() *convMockStreamingTTSService {
	return &convMockStreamingTTSService{
		convMockTTSService: convMockTTSService{
			synthData: []byte("audio-data"),
			name:      "mock-streaming-tts",
		},
		streamChunks: []audio.Chunk{
			{Data: []byte("chunk1"), Index: 0},
			{Data: []byte("chunk2"), Index: 1, Final: true},
		},
	}
}

func (m *convMockStreamingTTSService) SynthesizeStream(_ context.Context, _ string, _ tts.SynthesisConfig) (<-chan audio.Chunk, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan audio.Chunk, len(m.streamChunks))
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

// convMockSTTService implements stt.Service for testing
type convMockSTTService struct{}

func (m *convMockSTTService) Name() string                        { return "mock-stt" }
func (m *convMockSTTService) Type() base.ProviderType             { return base.ProviderTypeSTT }
func (m *convMockSTTService) Pricing() *base.PricingDescriptor    { return nil }
func (m *convMockSTTService) Validate() error                     { return nil }
func (m *convMockSTTService) Init(_ context.Context) error        { return nil }
func (m *convMockSTTService) HealthCheck(_ context.Context) error { return nil }
func (m *convMockSTTService) Close() error                        { return nil }
func (m *convMockSTTService) Transcribe(_ context.Context, _ base.STTRequest) (base.STTResponse, error) {
	return base.STTResponse{Text: "hello"}, nil
}
func (m *convMockSTTService) TranscribeBytes(_ context.Context, _ []byte, _ stt.TranscriptionConfig) (string, error) {
	return "hello", nil
}
func (m *convMockSTTService) SupportedFormats() []string { return []string{"pcm"} }

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

	// Test SendFrame in unary mode
	err = conv.SendFrame(ctx, &session.ImageFrame{
		Data:     []byte("test"),
		MIMEType: "image/jpeg",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplex mode")

	// Test SendVideoChunk in unary mode
	err = conv.SendVideoChunk(ctx, &session.VideoChunk{
		Data:     []byte("test"),
		MIMEType: "video/h264",
	})
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

	// Test SendFrame when closed
	err = conv.SendFrame(ctx, &session.ImageFrame{
		Data:     []byte("test"),
		MIMEType: "image/jpeg",
	})
	assert.Equal(t, ErrConversationClosed, err)

	// Test SendVideoChunk when closed
	err = conv.SendVideoChunk(ctx, &session.VideoChunk{
		Data:     []byte("test"),
		MIMEType: "video/h264",
	})
	assert.Equal(t, ErrConversationClosed, err)
}

func TestGetBaseSessionUnary(t *testing.T) {
	conv := newTestConversation()
	store := statestore.NewMemoryStore()
	provider := mock.NewProvider("test", "test-model", false)
	pipelineBuilder := func(ctx context.Context, p providers.Provider, streamProvider providers.StreamInputSupport, streamConfig *providers.StreamingInputConfig, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
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
	pipelineBuilder := func(ctx context.Context, p providers.Provider, streamProvider providers.StreamInputSupport, streamConfig *providers.StreamingInputConfig, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
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
		pipelineBuilder := func(ctx context.Context, p providers.Provider, streamProvider providers.StreamInputSupport, streamConfig *providers.StreamingInputConfig, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
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
		pipelineBuilder := func(ctx context.Context, p providers.Provider, streamProvider providers.StreamInputSupport, streamConfig *providers.StreamingInputConfig, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
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
		pipelineBuilder := func(ctx context.Context, p providers.Provider, streamProvider providers.StreamInputSupport, streamConfig *providers.StreamingInputConfig, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
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
		pipelineBuilder := func(ctx context.Context, p providers.Provider, streamProvider providers.StreamInputSupport, streamConfig *providers.StreamingInputConfig, cid string, s statestore.Store) (*stage.StreamPipeline, error) {
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

	p := &pack.Pack{Pack: packspec.Pack{
		ID: "test-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {
				ID:             "chat",
				SystemTemplate: "You are helpful.",
			},
		},
	}}

	conv := &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: pack.ToPromptRegistry(p),
		toolRegistry:   tools.NewRegistry(),
		config:         &config{provider: mockProv},
		mode:           UnaryMode,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewMemoryPendingStore(),
	}

	// Build pipeline and create session
	pipeline, err := conv.buildPipelineWithParams(store, "test-conv", nil, nil)
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

func TestBuildContextSummary_EmptyHistory(t *testing.T) {
	conv := newTestConversation()
	// No messages have been exchanged, so the summary is empty.
	assert.Equal(t, "", buildContextSummary("greeting", conv))
}

func TestBuildContextSummary_WithHistory(t *testing.T) {
	ctx := context.Background()

	repo := mock.NewInMemoryMockRepository("Sure, happy to help!")
	mockProv := mock.NewProviderWithRepository("test-mock", "test-model", false, repo)
	store := statestore.NewMemoryStore()

	p := &pack.Pack{Pack: packspec.Pack{
		ID: "summary-pack",
		Prompts: map[string]*pack.Prompt{
			"chat": {ID: "chat", SystemTemplate: "You are helpful."},
		},
	}}

	conv := &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: pack.ToPromptRegistry(p),
		toolRegistry:   tools.NewRegistry(),
		config:         &config{provider: mockProv},
		mode:           UnaryMode,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewMemoryPendingStore(),
	}

	pipeline, err := conv.buildPipelineWithParams(store, "summary-conv", nil, nil)
	require.NoError(t, err)
	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "summary-conv",
		StateStore:     store,
		Pipeline:       pipeline,
	})
	require.NoError(t, err)
	conv.unarySession = unarySession

	_, err = conv.Send(ctx, "Can you help me?")
	require.NoError(t, err)

	summary := buildContextSummary("intake", conv)
	assert.Contains(t, summary, "[Previous state: intake")
	assert.Contains(t, summary, "Can you help me?")
	assert.Contains(t, summary, "Sure, happy to help!")
}

func TestTriggerStart(t *testing.T) {
	ctx := context.Background()

	t.Run("fails in unary mode", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = UnaryMode

		err := conv.TriggerStart(ctx, "Hello")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "duplex mode")
	})

	t.Run("fails when closed", func(t *testing.T) {
		conv := newTestConversation()
		conv.mode = DuplexMode
		conv.closed = true

		err := conv.TriggerStart(ctx, "Hello")
		assert.Equal(t, ErrConversationClosed, err)
	})
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

		resp := conv.buildResponse(context.Background(), result, time.Now())
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

		resp := conv.buildResponse(context.Background(), result, time.Now())
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

		resp := conv.buildResponse(context.Background(), result, time.Now())
		assert.NotNil(t, resp)
		assert.Len(t, resp.validations, 1)
		assert.True(t, resp.validations[0].Passed)
	})

	t.Run("WithParts", func(t *testing.T) {
		parts := []types.ContentPart{
			types.NewTextPart("Hello"),
			types.NewTextPart("World"),
		}
		result := &rtpipeline.ExecutionResult{
			Response: &rtpipeline.Response{
				Role:    "assistant",
				Content: "Hello",
				Parts:   parts,
			},
		}

		resp := conv.buildResponse(context.Background(), result, time.Now())
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.message)
		assert.Len(t, resp.message.Parts, 2)
		assert.Equal(t, "text", resp.message.Parts[0].Type)
		assert.Equal(t, "text", resp.message.Parts[1].Type)
	})

	t.Run("NilResponse", func(t *testing.T) {
		result := &rtpipeline.ExecutionResult{
			Response: nil,
		}

		resp := conv.buildResponse(context.Background(), result, time.Now())
		assert.NotNil(t, resp)
		assert.Nil(t, resp.message)
	})
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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be string or *types.Message")
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

	t.Run("a second Close is a no-op", func(t *testing.T) {
		conv := newTestConversation()
		capErr := errors.New("capability close error")
		conv.capabilities = []Capability{&closeErrorCapability{name: "cap", closeErr: capErr}}

		require.ErrorIs(t, conv.Close(), capErr, "the first Close closes the capabilities")
		assert.NoError(t, conv.Close(), "a second Close must not close the capabilities again")
	})

	t.Run("collects all errors from capabilities", func(t *testing.T) {
		conv := newTestConversation()
		errCap1 := errors.New("capability-1 close error")
		errCap2 := errors.New("capability-2 close error")
		conv.capabilities = []Capability{
			&closeErrorCapability{name: "cap1", closeErr: errCap1},
			&closeErrorCapability{name: "cap2", closeErr: errCap2},
		}

		err := conv.Close()
		require.Error(t, err)
		assert.True(t, errors.Is(err, errCap1), "should contain cap1 error")
		assert.True(t, errors.Is(err, errCap2), "should contain cap2 error")
		assert.True(t, conv.closed, "conversation should be marked closed")
	})
}

// closeErrorCapability is a test capability whose Close returns a configured error.
type closeErrorCapability struct {
	name     string
	closeErr error
}

func (c *closeErrorCapability) Name() string                    { return c.name }
func (c *closeErrorCapability) Init(_ CapabilityContext) error  { return nil }
func (c *closeErrorCapability) RegisterTools(_ *tools.Registry) {}
func (c *closeErrorCapability) Close() error                    { return c.closeErr }

func TestForkRegistryIsolation(t *testing.T) {
	conv := newTestConversation()

	// Register a tool in the original
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	_ = conv.toolRegistry.Register(&tools.ToolDescriptor{
		Name:        "original_tool",
		Description: "A tool in the original conversation",
		InputSchema: schema,
	})

	fork, err := conv.Fork()
	require.NoError(t, err)

	// Fork should have the original tool
	forkTools := fork.toolRegistry.GetTools()
	foundOriginal := false
	for _, td := range forkTools {
		if td.Name == "original_tool" {
			foundOriginal = true
			break
		}
	}
	assert.True(t, foundOriginal, "fork should have copied original_tool")

	// Register a new tool in the fork — should NOT appear in original
	_ = fork.toolRegistry.Register(&tools.ToolDescriptor{
		Name:        "fork_only_tool",
		Description: "A tool only in the fork",
		InputSchema: schema,
	})

	origTools := conv.toolRegistry.GetTools()
	for _, td := range origTools {
		if td.Name == "fork_only_tool" {
			t.Error("fork_only_tool should NOT appear in original registry")
		}
	}
}

func TestForkErrorHandling(t *testing.T) {
	t.Run("fork preserves handlers", func(t *testing.T) {
		conv := newTestConversation()
		conv.OnTool("test_tool", func(args map[string]any) (any, error) {
			return "result", nil
		})

		forked, err := conv.Fork()
		require.NoError(t, err)

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
// Session hook lifecycle tests
// =============================================================================

// recordingSessionHook records which lifecycle methods were called.
type recordingSessionHook struct {
	name         string
	startCalled  bool
	updateCalled bool
	endCalled    bool
	lastEvent    hooks.SessionEvent
}

func (h *recordingSessionHook) Name() string { return h.name }
func (h *recordingSessionHook) OnSessionStart(_ context.Context, event hooks.SessionEvent) error {
	h.startCalled = true
	h.lastEvent = event
	return nil
}
func (h *recordingSessionHook) OnSessionUpdate(_ context.Context, event hooks.SessionEvent) error {
	h.updateCalled = true
	h.lastEvent = event
	return nil
}
func (h *recordingSessionHook) OnSessionEnd(_ context.Context, event hooks.SessionEvent) error {
	h.endCalled = true
	h.lastEvent = event
	return nil
}

func TestSessionHookDispatcher_BuildEvent(t *testing.T) {
	t.Run("populates all fields from conversation state", func(t *testing.T) {
		conv := newTestConversation()
		conv.config.conversationID = "conv-456"
		conv.sessionHooks = newSessionHookDispatcher(nil, conv.sessionInfo)
		conv.sessionHooks.turns.Store(7)

		event := conv.sessionHooks.buildEvent()

		assert.Equal(t, conv.ID(), event.SessionID)
		assert.Equal(t, "conv-456", event.ConversationID)
		assert.Equal(t, 7, event.TurnIndex)
		assert.NotNil(t, event.Messages)
	})

	t.Run("handles nil sessions gracefully", func(t *testing.T) {
		conv := &Conversation{
			config: &config{conversationID: "conv-789"},
		}
		conv.sessionHooks = newSessionHookDispatcher(nil, conv.sessionInfo)
		conv.sessionHooks.turns.Store(2)

		event := conv.sessionHooks.buildEvent()

		assert.Equal(t, "", event.SessionID)
		assert.Nil(t, event.Messages)
		assert.Equal(t, "conv-789", event.ConversationID)
		assert.Equal(t, 2, event.TurnIndex)
	})
}

func TestSessionHookDispatcher_IncrementTurn(t *testing.T) {
	d := newSessionHookDispatcher(nil, nil)
	assert.Equal(t, 0, d.TurnIndex())

	d.IncrementTurn()
	assert.Equal(t, 1, d.TurnIndex())

	d.IncrementTurn()
	assert.Equal(t, 2, d.TurnIndex())
}

func TestMessageSizeValidation(t *testing.T) {
	t.Run("string message within limit", func(t *testing.T) {
		conv := newTestConversation()
		conv.config.maxMessageSize = 5
		msg, err := conv.buildUserMessage("hello")
		require.NoError(t, err, "a message exactly at the limit is accepted")
		assert.Equal(t, "user", msg.Role)
		assert.Equal(t, "hello", msg.GetContent())
	})

	t.Run("string message exceeds limit", func(t *testing.T) {
		conv := newTestConversation()
		conv.config.maxMessageSize = 5
		_, err := conv.buildUserMessage("hello world")
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrMessageTooLarge)
	})

	t.Run("typed message within limit", func(t *testing.T) {
		conv := newTestConversation()
		conv.config.maxMessageSize = 2
		userMsg := &types.Message{Role: "user", Content: "hi"}
		msg, err := conv.buildUserMessage(userMsg)
		require.NoError(t, err, "a message exactly at the limit is accepted")
		assert.Same(t, userMsg, msg, "a typed message is sent as given")
	})

	t.Run("typed message exceeds limit", func(t *testing.T) {
		conv := newTestConversation()
		conv.config.maxMessageSize = 5
		userMsg := &types.Message{Role: "user", Content: "hello world"}
		_, err := conv.buildUserMessage(userMsg)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrMessageTooLarge)
	})

	t.Run("multimodal message size counts parts", func(t *testing.T) {
		conv := newTestConversation()
		conv.config.maxMessageSize = 9
		text := "hello"
		userMsg := &types.Message{
			Role: "user",
			Parts: []types.ContentPart{
				{Type: types.ContentTypeText, Text: &text},
				{Type: types.ContentTypeText, Text: &text},
			},
		}
		_, err := conv.buildUserMessage(userMsg)
		require.ErrorIs(t, err, ErrMessageTooLarge, "two 5-byte parts are 10 bytes, over a 9-byte limit")

		conv.config.maxMessageSize = 10
		msg, err := conv.buildUserMessage(userMsg)
		require.NoError(t, err)
		assert.Same(t, userMsg, msg)
	})

	t.Run("default limit allows normal messages", func(t *testing.T) {
		conv := newTestConversation()
		// config.maxMessageSize is 0, so defaultMaxMessageSize (10MB) is used
		msg, err := conv.buildUserMessage(strings.Repeat("x", defaultMaxMessageSize))
		require.NoError(t, err, "a message of exactly the default limit is accepted")
		assert.Len(t, msg.GetContent(), defaultMaxMessageSize)

		_, err = conv.buildUserMessage(strings.Repeat("x", defaultMaxMessageSize+1))
		assert.ErrorIs(t, err, ErrMessageTooLarge, "one byte over the default limit is refused")
	})

	t.Run("error message includes sizes", func(t *testing.T) {
		conv := newTestConversation()
		conv.config.maxMessageSize = 5
		_, err := conv.buildUserMessage("hello world")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "11 bytes")
		assert.Contains(t, err.Error(), "5 bytes")
	})
}

func TestMessageContentSize(t *testing.T) {
	t.Run("text only via Content field", func(t *testing.T) {
		msg := &types.Message{Content: "hello"}
		assert.Equal(t, 5, messageContentSize(msg))
	})

	t.Run("text parts", func(t *testing.T) {
		text1 := "hello"
		text2 := " world"
		msg := &types.Message{
			Parts: []types.ContentPart{
				{Type: types.ContentTypeText, Text: &text1},
				{Type: types.ContentTypeText, Text: &text2},
			},
		}
		assert.Equal(t, 11, messageContentSize(msg))
	})

	t.Run("media parts count data length", func(t *testing.T) {
		data := "base64data"
		msg := &types.Message{
			Parts: []types.ContentPart{
				{Type: types.ContentTypeImage, Media: &types.MediaContent{Data: &data}},
			},
		}
		assert.Equal(t, 10, messageContentSize(msg))
	})

	t.Run("mixed parts", func(t *testing.T) {
		text := "hi"
		data := "imgdata"
		msg := &types.Message{
			Parts: []types.ContentPart{
				{Type: types.ContentTypeText, Text: &text},
				{Type: types.ContentTypeImage, Media: &types.MediaContent{Data: &data}},
			},
		}
		assert.Equal(t, 9, messageContentSize(msg))
	})

	t.Run("nil text pointer", func(t *testing.T) {
		msg := &types.Message{
			Parts: []types.ContentPart{
				{Type: types.ContentTypeText, Text: nil},
			},
		}
		assert.Equal(t, 0, messageContentSize(msg))
	})

	t.Run("media part without data", func(t *testing.T) {
		url := "https://example.com/img.jpg"
		msg := &types.Message{
			Parts: []types.ContentPart{
				{Type: types.ContentTypeImage, Media: &types.MediaContent{URL: &url}},
			},
		}
		assert.Equal(t, 0, messageContentSize(msg))
	})
}
