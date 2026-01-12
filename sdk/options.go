package sdk

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
	"github.com/AltairaLabs/PromptKit/runtime/variables"
)

// VAD mode default configuration constants.
const (
	defaultVADSilenceDuration   = 800 * time.Millisecond
	defaultVADMinSpeechDuration = 200 * time.Millisecond
	defaultVADMaxTurnDuration   = 30 * time.Second
	defaultVADSampleRate        = 16000
)

// config holds the configuration for a conversation.
// It is populated by Option functions passed to Open.
type config struct {
	// Prompt selection
	promptName string

	// Provider configuration
	provider providers.Provider
	apiKey   string
	model    string

	// State management
	stateStore     statestore.Store
	conversationID string

	// Tool registry (for power users)
	toolRegistry *tools.Registry

	// Event bus for observability
	eventBus *events.EventBus

	// Event store for session recording
	eventStore events.EventStore

	// Context management
	tokenBudget        int
	truncationStrategy string
	relevanceConfig    *RelevanceConfig

	// Validation behavior
	validationMode       ValidationMode
	disabledValidators   []string
	strictValidation     bool
	skipSchemaValidation bool

	// MCP configuration
	mcpServers []mcp.ServerConfig

	// Variable providers for dynamic variable resolution
	variableProviders []variables.Provider

	// Initial variables from prompt defaults
	initialVariables map[string]string

	// TTS configuration for Pipeline middleware
	ttsService tts.Service

	// Audio session configuration for Pipeline middleware
	turnDetector audio.TurnDetector

	// Streaming configuration for duplex mode
	// If set: ASM mode (audio streaming model with continuous bidirectional streaming)
	// If nil: VAD mode (voice activity detection with turn-based streaming)
	streamingConfig *providers.StreamingInputConfig

	// VAD mode configuration
	// When set, enables VAD pipeline: AudioTurnStage → STTStage → ProviderStage → TTSStage
	vadModeConfig *VADModeConfig

	// STT service for VAD mode
	sttService stt.Service

	// Image preprocessing configuration
	// When set, images are preprocessed (resized, optimized) before sending to provider
	imagePreprocessConfig *stage.ImagePreprocessConfig
}

// Option configures a Conversation.
type Option func(*config) error

// WithModel overrides the default model specified in the pack.
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithModel("gpt-4o"),
//	)
func WithModel(model string) Option {
	return func(c *config) error {
		c.model = model
		return nil
	}
}

// WithAPIKey provides an explicit API key instead of reading from environment.
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithAPIKey(os.Getenv("MY_CUSTOM_KEY")),
//	)
func WithAPIKey(key string) Option {
	return func(c *config) error {
		c.apiKey = key
		return nil
	}
}

// WithProvider uses a custom provider instance.
//
// This bypasses auto-detection and uses the provided provider directly.
// Use this for custom provider implementations or when you need full
// control over provider configuration.
//
//	provider := openai.NewProvider(openai.Config{...})
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithProvider(provider),
//	)
func WithProvider(p providers.Provider) Option {
	return func(c *config) error {
		c.provider = p
		return nil
	}
}

// WithStateStore configures persistent state storage.
//
// When configured, conversation state (messages, metadata) is automatically
// persisted after each turn and can be resumed later via [Resume].
//
//	store := statestore.NewRedisStore("redis://localhost:6379")
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithStateStore(store),
//	)
func WithStateStore(store statestore.Store) Option {
	return func(c *config) error {
		c.stateStore = store
		return nil
	}
}

// WithConversationID sets the conversation identifier.
//
// If not set, a unique ID is auto-generated. Set this when you want to
// use a specific ID for state persistence or tracking.
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithStateStore(store),
//	    sdk.WithConversationID("user-123-session-456"),
//	)
func WithConversationID(id string) Option {
	return func(c *config) error {
		c.conversationID = id
		return nil
	}
}

// WithToolRegistry provides a pre-configured tool registry.
//
// This is a power-user option for scenarios requiring direct registry access.
// Tool descriptors are still loaded from the pack; this allows providing
// custom executors or middleware.
//
//	registry := tools.NewRegistry()
//	registry.RegisterExecutor(&myCustomExecutor{})
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithToolRegistry(registry),
//	)
func WithToolRegistry(registry *tools.Registry) Option {
	return func(c *config) error {
		c.toolRegistry = registry
		return nil
	}
}

// WithEventBus provides a shared event bus for observability.
//
// When set, the conversation emits events to this bus. Use this to share
// an event bus across multiple conversations for centralized logging,
// metrics, or debugging.
//
//	bus := events.NewEventBus()
//	bus.SubscribeAll(myMetricsCollector)
//
//	conv1, _ := sdk.Open("./chat.pack.json", "assistant", sdk.WithEventBus(bus))
//	conv2, _ := sdk.Open("./chat.pack.json", "assistant", sdk.WithEventBus(bus))
func WithEventBus(bus *events.EventBus) Option {
	return func(c *config) error {
		c.eventBus = bus
		return nil
	}
}

// WithEventStore configures event persistence for session recording.
//
// When set, all events published through the conversation's event bus are
// automatically persisted to the store. This enables session replay and
// analysis.
//
// The event store is automatically attached to the event bus. If no event bus
// is provided via WithEventBus, a new one is created internally.
//
// Example with file-based storage:
//
//	store, _ := events.NewFileEventStore("/var/log/sessions")
//	defer store.Close()
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithEventStore(store),
//	)
//
// Example with shared bus and store:
//
//	store, _ := events.NewFileEventStore("/var/log/sessions")
//	bus := events.NewEventBus().WithStore(store)
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithEventBus(bus),
//	)
func WithEventStore(store events.EventStore) Option {
	return func(c *config) error {
		c.eventStore = store
		return nil
	}
}

// WithTokenBudget sets the maximum tokens for context (prompt + history).
//
// When the conversation history exceeds this budget, older messages are
// truncated according to the truncation strategy.
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithTokenBudget(8000),
//	)
func WithTokenBudget(tokens int) Option {
	return func(c *config) error {
		c.tokenBudget = tokens
		return nil
	}
}

// WithTruncation sets the truncation strategy for context management.
//
// Strategies:
//
//   - "sliding": Remove oldest messages first (default)
//
//   - "summarize": Summarize old messages before removing
//
//   - "relevance": Remove least relevant messages based on embedding similarity
//
//     conv, _ := sdk.Open("./chat.pack.json", "assistant",
//     sdk.WithTokenBudget(8000),
//     sdk.WithTruncation("summarize"),
//     )
func WithTruncation(strategy string) Option {
	return func(c *config) error {
		c.truncationStrategy = strategy
		return nil
	}
}

// RelevanceConfig configures embedding-based relevance truncation.
// Used when truncation strategy is "relevance".
type RelevanceConfig struct {
	// EmbeddingProvider generates embeddings for similarity scoring.
	// Required for relevance-based truncation.
	EmbeddingProvider providers.EmbeddingProvider

	// MinRecentMessages always keeps the N most recent messages regardless of relevance.
	// Default: 3
	MinRecentMessages int

	// AlwaysKeepSystemRole keeps all system role messages regardless of score.
	// Default: true
	AlwaysKeepSystemRole bool

	// SimilarityThreshold is the minimum score (0.0-1.0) to consider a message relevant.
	// Messages below this threshold are dropped first. Default: 0.0 (no threshold)
	SimilarityThreshold float64

	// QuerySource determines what text to compare messages against.
	// Values: "last_user" (default), "last_n", "custom"
	QuerySource string

	// LastNCount is the number of messages to use when QuerySource is "last_n".
	// Default: 3
	LastNCount int

	// CustomQuery is the query text when QuerySource is "custom".
	CustomQuery string
}

// WithRelevanceTruncation configures embedding-based relevance truncation.
//
// This automatically sets the truncation strategy to "relevance" and configures
// the embedding provider for semantic similarity scoring.
//
// Example with OpenAI embeddings:
//
//	embProvider, _ := openai.NewEmbeddingProvider()
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithTokenBudget(8000),
//	    sdk.WithRelevanceTruncation(&sdk.RelevanceConfig{
//	        EmbeddingProvider: embProvider,
//	        MinRecentMessages: 3,
//	        SimilarityThreshold: 0.3,
//	    }),
//	)
//
// Example with Gemini embeddings:
//
//	embProvider, _ := gemini.NewEmbeddingProvider()
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithTokenBudget(8000),
//	    sdk.WithRelevanceTruncation(&sdk.RelevanceConfig{
//	        EmbeddingProvider: embProvider,
//	    }),
//	)
func WithRelevanceTruncation(cfg *RelevanceConfig) Option {
	return func(c *config) error {
		c.truncationStrategy = "relevance"
		c.relevanceConfig = cfg
		return nil
	}
}

// ValidationMode controls how validation failures are handled.
type ValidationMode int

const (
	// ValidationModeError causes validation failures to return errors (default).
	ValidationModeError ValidationMode = iota

	// ValidationModeWarn logs validation failures but doesn't return errors.
	ValidationModeWarn

	// ValidationModeDisabled skips validation entirely.
	ValidationModeDisabled
)

// WithValidationMode sets how validation failures are handled.
//
//	// Suppress validation errors (useful for testing)
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithValidationMode(sdk.ValidationModeWarn),
//	)
func WithValidationMode(mode ValidationMode) Option {
	return func(c *config) error {
		c.validationMode = mode
		return nil
	}
}

// WithDisabledValidators disables specific validators by name.
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithDisabledValidators("max_length", "banned_words"),
//	)
func WithDisabledValidators(names ...string) Option {
	return func(c *config) error {
		c.disabledValidators = append(c.disabledValidators, names...)
		return nil
	}
}

// WithStrictValidation makes all validators fail on violation.
//
// Normally, validators respect their fail_on_violation setting from the pack.
// With strict validation, all validators will cause errors on failure.
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithStrictValidation(),
//	)
func WithStrictValidation() Option {
	return func(c *config) error {
		c.strictValidation = true
		return nil
	}
}

// WithSkipSchemaValidation disables JSON schema validation during pack loading.
//
// By default, packs are validated against the PromptPack JSON schema to ensure
// they are well-formed. Use this option to skip validation, for example when
// loading legacy packs or during development.
//
//	conv, _ := sdk.Open("./legacy.pack.json", "assistant",
//	    sdk.WithSkipSchemaValidation(),
//	)
func WithSkipSchemaValidation() Option {
	return func(c *config) error {
		c.skipSchemaValidation = true
		return nil
	}
}

// WithMCP adds an MCP (Model Context Protocol) server for tool execution.
//
// MCP servers provide external tools that can be called by the LLM.
// The server is started automatically when the conversation opens and
// stopped when the conversation is closed.
//
// Basic usage:
//
//	conv, _ := sdk.Open("./assistant.pack.json", "assistant",
//	    sdk.WithMCP("filesystem", "npx", "@modelcontextprotocol/server-filesystem", "/path"),
//	)
//
// With environment variables:
//
//	conv, _ := sdk.Open("./assistant.pack.json", "assistant",
//	    sdk.WithMCP("github", "npx", "@modelcontextprotocol/server-github").
//	        WithEnv("GITHUB_TOKEN", os.Getenv("GITHUB_TOKEN")),
//	)
//
// Multiple servers:
//
//	conv, _ := sdk.Open("./assistant.pack.json", "assistant",
//	    sdk.WithMCP("filesystem", "npx", "@modelcontextprotocol/server-filesystem", "/path"),
//	    sdk.WithMCP("memory", "npx", "@modelcontextprotocol/server-memory"),
//	)
func WithMCP(name, command string, args ...string) Option {
	return func(c *config) error {
		c.mcpServers = append(c.mcpServers, mcp.ServerConfig{
			Name:    name,
			Command: command,
			Args:    args,
		})
		return nil
	}
}

// MCPServerBuilder provides a fluent interface for configuring MCP servers.
type MCPServerBuilder struct {
	config mcp.ServerConfig
}

// NewMCPServer creates a new MCP server configuration builder.
//
//	server := sdk.NewMCPServer("github", "npx", "@modelcontextprotocol/server-github").
//	    WithEnv("GITHUB_TOKEN", os.Getenv("GITHUB_TOKEN"))
//
//	conv, _ := sdk.Open("./assistant.pack.json", "assistant",
//	    sdk.WithMCPServer(server),
//	)
func NewMCPServer(name, command string, args ...string) *MCPServerBuilder {
	return &MCPServerBuilder{
		config: mcp.ServerConfig{
			Name:    name,
			Command: command,
			Args:    args,
			Env:     make(map[string]string),
		},
	}
}

// WithEnv adds an environment variable to the MCP server.
func (b *MCPServerBuilder) WithEnv(key, value string) *MCPServerBuilder {
	b.config.Env[key] = value
	return b
}

// WithArgs appends additional arguments to the MCP server command.
func (b *MCPServerBuilder) WithArgs(args ...string) *MCPServerBuilder {
	b.config.Args = append(b.config.Args, args...)
	return b
}

// Build returns the configured server config.
func (b *MCPServerBuilder) Build() mcp.ServerConfig {
	return b.config
}

// WithMCPServer adds a pre-configured MCP server.
//
//	server := sdk.NewMCPServer("github", "npx", "@modelcontextprotocol/server-github").
//	    WithEnv("GITHUB_TOKEN", os.Getenv("GITHUB_TOKEN"))
//
//	conv, _ := sdk.Open("./assistant.pack.json", "assistant",
//	    sdk.WithMCPServer(server),
//	)
func WithMCPServer(builder *MCPServerBuilder) Option {
	return func(c *config) error {
		c.mcpServers = append(c.mcpServers, builder.Build())
		return nil
	}
}

// WithVariableProvider adds a variable provider for dynamic variable resolution.
//
// Variables are resolved before each Send() and merged with static variables.
// Later providers in the chain override earlier ones with the same key.
//
//	conv, _ := sdk.Open("./assistant.pack.json", "support",
//	    sdk.WithVariableProvider(variables.Time()),
//	    sdk.WithVariableProvider(variables.State()),
//	)
func WithVariableProvider(p variables.Provider) Option {
	return func(c *config) error {
		c.variableProviders = append(c.variableProviders, p)
		return nil
	}
}

// WithVariables sets initial variables for template substitution.
//
// These variables are available immediately when the conversation opens,
// before any messages are sent. Use this for variables that must be set
// before the first LLM call (e.g., in streaming/ASM mode).
//
// Variables set here override prompt defaults but can be further modified
// via conv.SetVar() for subsequent messages.
//
//	conv, _ := sdk.Open("./assistant.pack.json", "assistant",
//	    sdk.WithVariables(map[string]string{
//	        "user_name": "Alice",
//	        "language": "en",
//	    }),
//	)
func WithVariables(vars map[string]string) Option {
	return func(c *config) error {
		if c.initialVariables == nil {
			c.initialVariables = make(map[string]string)
		}
		for k, v := range vars {
			c.initialVariables[k] = v
		}
		return nil
	}
}

// WithTTS configures text-to-speech for the Pipeline.
//
// TTS is applied via Pipeline middleware during streaming responses.
//
//	conv, _ := sdk.Open("./assistant.pack.json", "voice",
//	    sdk.WithTTS(tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))),
//	)
func WithTTS(service tts.Service) Option {
	return func(c *config) error {
		c.ttsService = service
		return nil
	}
}

// WithTurnDetector configures turn detection for the Pipeline.
//
// Turn detectors determine when a user has finished speaking in audio sessions.
//
//	conv, _ := sdk.Open("./assistant.pack.json", "voice",
//	    sdk.WithTurnDetector(audio.NewSilenceDetector(500 * time.Millisecond)),
//	)
func WithTurnDetector(detector audio.TurnDetector) Option {
	return func(c *config) error {
		c.turnDetector = detector
		return nil
	}
}

// WithStreamingConfig configures streaming for duplex mode.
// When set, enables ASM (Audio Streaming Model) mode with continuous bidirectional streaming.
// When nil (default), uses VAD (Voice Activity Detection) mode with turn-based streaming.
//
// ASM mode is for models with native bidirectional audio support (e.g., gemini-2.0-flash-exp).
// VAD mode is for standard text-based models with audio transcription.
//
// Example for ASM mode:
//
//	conv, _ := sdk.OpenDuplex("./assistant.pack.json", "voice-chat",
//	    sdk.WithStreamingConfig(&providers.StreamingInputConfig{
//	        Type:       types.ContentTypeAudio,
//	        SampleRate: 16000,
//	        Channels:   1,
//	    }),
//	)
func WithStreamingConfig(streamingConfig *providers.StreamingInputConfig) Option {
	return func(c *config) error {
		c.streamingConfig = streamingConfig
		return nil
	}
}

// VADModeConfig configures VAD (Voice Activity Detection) mode for voice conversations.
// In VAD mode, the pipeline processes audio through:
// AudioTurnStage → STTStage → ProviderStage → TTSStage
//
// This enables voice conversations using standard text-based LLMs.
type VADModeConfig struct {
	// SilenceDuration is how long silence must persist to trigger turn complete.
	// Default: 800ms
	SilenceDuration time.Duration

	// MinSpeechDuration is minimum speech before turn can complete.
	// Default: 200ms
	MinSpeechDuration time.Duration

	// MaxTurnDuration is maximum turn length before forcing completion.
	// Default: 30s
	MaxTurnDuration time.Duration

	// SampleRate is the audio sample rate.
	// Default: 16000
	SampleRate int

	// Language is the language hint for STT (e.g., "en", "es").
	// Default: "en"
	Language string

	// Voice is the TTS voice to use.
	// Default: "alloy"
	Voice string

	// Speed is the TTS speech rate (0.5-2.0).
	// Default: 1.0
	Speed float64
}

// DefaultVADModeConfig returns sensible defaults for VAD mode.
func DefaultVADModeConfig() *VADModeConfig {
	return &VADModeConfig{
		SilenceDuration:   defaultVADSilenceDuration,
		MinSpeechDuration: defaultVADMinSpeechDuration,
		MaxTurnDuration:   defaultVADMaxTurnDuration,
		SampleRate:        defaultVADSampleRate,
		Language:          "en",
		Voice:             "alloy",
		Speed:             1.0,
	}
}

// WithVADMode configures VAD mode for voice conversations with standard text-based LLMs.
// VAD mode processes audio through a pipeline: Audio → VAD → STT → LLM → TTS → Audio
//
// This is an alternative to ASM mode (WithStreamingConfig) for providers without
// native audio streaming support.
//
// Example:
//
//	sttService := stt.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
//	ttsService := tts.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
//
//	conv, _ := sdk.OpenDuplex("./assistant.pack.json", "voice-chat",
//	    sdk.WithProvider(openai.NewProvider(openai.Config{...})),
//	    sdk.WithVADMode(sttService, ttsService, nil), // nil uses defaults
//	)
//
// With custom config:
//
//	conv, _ := sdk.OpenDuplex("./assistant.pack.json", "voice-chat",
//	    sdk.WithProvider(openai.NewProvider(openai.Config{...})),
//	    sdk.WithVADMode(sttService, ttsService, &sdk.VADModeConfig{
//	        SilenceDuration: 500 * time.Millisecond,
//	        Voice:           "nova",
//	    }),
//	)
func WithVADMode(sttService stt.Service, ttsService tts.Service, cfg *VADModeConfig) Option {
	return func(c *config) error {
		if cfg == nil {
			cfg = DefaultVADModeConfig()
		}
		c.vadModeConfig = cfg
		c.sttService = sttService
		c.ttsService = ttsService
		return nil
	}
}

// toAudioTurnConfig converts VADModeConfig to internal AudioTurnConfig.
func (v *VADModeConfig) toAudioTurnConfig(ih *audio.InterruptionHandler) stage.AudioTurnConfig {
	cfg := stage.DefaultAudioTurnConfig()
	if v.SilenceDuration > 0 {
		cfg.SilenceDuration = v.SilenceDuration
	}
	if v.MinSpeechDuration > 0 {
		cfg.MinSpeechDuration = v.MinSpeechDuration
	}
	if v.MaxTurnDuration > 0 {
		cfg.MaxTurnDuration = v.MaxTurnDuration
	}
	if v.SampleRate > 0 {
		cfg.SampleRate = v.SampleRate
	}
	cfg.InterruptionHandler = ih
	return cfg
}

// toSTTStageConfig converts VADModeConfig to internal STTStageConfig.
func (v *VADModeConfig) toSTTStageConfig() stage.STTStageConfig {
	cfg := stage.DefaultSTTStageConfig()
	if v.Language != "" {
		cfg.Language = v.Language
	}
	return cfg
}

// toTTSStageConfig converts VADModeConfig to internal TTSStageWithInterruptionConfig.
func (v *VADModeConfig) toTTSStageConfig(ih *audio.InterruptionHandler) stage.TTSStageWithInterruptionConfig {
	cfg := stage.DefaultTTSStageWithInterruptionConfig()
	if v.Voice != "" {
		cfg.Voice = v.Voice
	}
	if v.Speed > 0 {
		cfg.Speed = v.Speed
	}
	cfg.InterruptionHandler = ih
	return cfg
}

// WithImagePreprocessing enables automatic image preprocessing before sending to the LLM.
// This resizes large images to fit within provider limits, reducing token usage and preventing errors.
//
// The default configuration resizes images to max 1024x1024 with 85% quality.
//
// Example with defaults:
//
//	conv, _ := sdk.Open("./chat.pack.json", "vision-assistant",
//	    sdk.WithImagePreprocessing(nil), // Use default settings
//	)
//
// Example with custom config:
//
//	conv, _ := sdk.Open("./chat.pack.json", "vision-assistant",
//	    sdk.WithImagePreprocessing(&stage.ImagePreprocessConfig{
//	        Resize: stage.ImageResizeStageConfig{
//	            MaxWidth:  2048,
//	            MaxHeight: 2048,
//	            Quality:   90,
//	        },
//	        EnableResize: true,
//	    }),
//	)
func WithImagePreprocessing(cfg *stage.ImagePreprocessConfig) Option {
	return func(c *config) error {
		if cfg == nil {
			defaultCfg := stage.DefaultImagePreprocessConfig()
			cfg = &defaultCfg
		}
		c.imagePreprocessConfig = cfg
		return nil
	}
}

// WithAutoResize is a convenience option that enables image resizing with the specified dimensions.
// Use this for simple cases; use WithImagePreprocessing for full control.
//
// Example:
//
//	conv, _ := sdk.Open("./chat.pack.json", "vision-assistant",
//	    sdk.WithAutoResize(1024, 1024), // Max 1024x1024
//	)
func WithAutoResize(maxWidth, maxHeight int) Option {
	return func(c *config) error {
		cfg := stage.DefaultImagePreprocessConfig()
		cfg.Resize.MaxWidth = maxWidth
		cfg.Resize.MaxHeight = maxHeight
		c.imagePreprocessConfig = &cfg
		return nil
	}
}

// sendConfig holds configuration for a single Send call.
type sendConfig struct {
	parts []any // Additional content parts (images, audio, etc.)
}

// SendOption configures a single Send call.
type SendOption func(*sendConfig) error

// WithImageFile attaches an image from a file path.
//
//	resp, _ := conv.Send(ctx, "What's in this image?",
//	    sdk.WithImageFile("/path/to/image.jpg"),
//	)
func WithImageFile(path string, detail ...*string) SendOption {
	return func(c *sendConfig) error {
		var d *string
		if len(detail) > 0 {
			d = detail[0]
		}
		c.parts = append(c.parts, imageFilePart{path: path, detail: d})
		return nil
	}
}

// WithImageURL attaches an image from a URL.
//
//	resp, _ := conv.Send(ctx, "What's in this image?",
//	    sdk.WithImageURL("https://example.com/photo.jpg"),
//	)
func WithImageURL(url string, detail ...*string) SendOption {
	return func(c *sendConfig) error {
		var d *string
		if len(detail) > 0 {
			d = detail[0]
		}
		c.parts = append(c.parts, imageURLPart{url: url, detail: d})
		return nil
	}
}

// WithImageData attaches an image from raw bytes.
//
//	resp, _ := conv.Send(ctx, "What's in this image?",
//	    sdk.WithImageData(imageBytes, "image/png"),
//	)
func WithImageData(data []byte, mimeType string, detail ...*string) SendOption {
	return func(c *sendConfig) error {
		var d *string
		if len(detail) > 0 {
			d = detail[0]
		}
		c.parts = append(c.parts, imageDataPart{data: data, mimeType: mimeType, detail: d})
		return nil
	}
}

// WithAudioFile attaches audio from a file path.
//
//	resp, _ := conv.Send(ctx, "Transcribe this audio",
//	    sdk.WithAudioFile("/path/to/audio.mp3"),
//	)
func WithAudioFile(path string) SendOption {
	return func(c *sendConfig) error {
		c.parts = append(c.parts, audioFilePart{path: path})
		return nil
	}
}

// WithFile attaches a file with the given name and content.
//
//	resp, _ := conv.Send(ctx, "Analyze this data",
//	    sdk.WithFile("data.csv", csvBytes),
//	)
func WithFile(name string, data []byte) SendOption {
	return func(c *sendConfig) error {
		c.parts = append(c.parts, filePart{name: name, data: data})
		return nil
	}
}

// Internal types for content parts
type imageFilePart struct {
	path   string
	detail *string
}

type imageURLPart struct {
	url    string
	detail *string
}

type imageDataPart struct {
	data     []byte
	mimeType string
	detail   *string
}

type audioFilePart struct {
	path string
}

type filePart struct {
	name string
	data []byte
}
