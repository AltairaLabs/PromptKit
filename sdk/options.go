package sdk

import (
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
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

// Video streaming default configuration constants.
const (
	defaultVideoStreamFPS     = 1.0
	defaultVideoStreamQuality = 85
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

	// RAG context window (hot window size for long conversations)
	contextWindow int

	// Embedding-based retrieval for RAG context
	retrievalProvider providers.EmbeddingProvider
	retrievalTopK     int

	// Auto-summarization for RAG context
	summarizeProvider  providers.Provider
	summarizeThreshold int
	summarizeBatchSize int

	// Validation behavior
	validationMode       ValidationMode
	disabledValidators   []string
	strictValidation     bool
	skipSchemaValidation bool

	// MCP configuration
	mcpServers []mcp.ServerConfig

	// A2A tool bridge for remote agent tools
	a2aBridge *a2a.ToolBridge

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

	// Video streaming configuration for realtime video in duplex sessions
	// When set, enables frame rate limiting and preprocessing for video/image streams
	videoStreamConfig *VideoStreamConfig

	// ResponseFormat configures the LLM response format (JSON mode)
	// When set, the provider will request responses in the specified format
	responseFormat *providers.ResponseFormat

	// Multi-agent endpoint resolution
	agentEndpointResolver EndpointResolver

	// Local agent executor for in-process multi-agent routing
	localAgentExecutor *LocalAgentExecutor

	// Eval configuration
	evalDispatcher    evals.EvalDispatcher
	evalRegistry      *evals.EvalTypeRegistry
	evalResultWriters []evals.ResultWriter
	judgeProvider     handlers.JudgeProvider

	// Workflow context carry-forward (used by OpenWorkflow)
	contextCarryForward bool

	// Platform configuration (bedrock, vertex, azure)
	platform *platformConfig

	// Platform capabilities (workflow, a2a, memory, etc.)
	capabilities []Capability
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

// CredentialOption configures credentials for a provider.
type CredentialOption interface {
	applyCredential(*credentialConfig)
}

// credentialConfig holds credential configuration.
type credentialConfig struct {
	apiKey         string
	credentialFile string
	credentialEnv  string
}

type credentialOptionFunc func(*credentialConfig)

func (f credentialOptionFunc) applyCredential(c *credentialConfig) {
	f(c)
}

// WithCredentialAPIKey sets an explicit API key.
func WithCredentialAPIKey(key string) CredentialOption {
	return credentialOptionFunc(func(c *credentialConfig) {
		c.apiKey = key
	})
}

// WithCredentialFile sets a credential file path.
func WithCredentialFile(path string) CredentialOption {
	return credentialOptionFunc(func(c *credentialConfig) {
		c.credentialFile = path
	})
}

// WithCredentialEnv sets an environment variable name for the credential.
func WithCredentialEnv(envVar string) CredentialOption {
	return credentialOptionFunc(func(c *credentialConfig) {
		c.credentialEnv = envVar
	})
}

// PlatformOption configures a platform for a provider.
type PlatformOption interface {
	applyPlatform(*platformConfig)
}

// platformConfig holds platform configuration.
type platformConfig struct {
	platformType string
	providerType string // provider factory name (e.g., "claude", "openai", "gemini")
	model        string
	region       string
	project      string
	endpoint     string
}

type platformOptionFunc func(*platformConfig)

func (f platformOptionFunc) applyPlatform(c *platformConfig) {
	f(c)
}

// WithPlatformRegion sets the cloud region.
func WithPlatformRegion(region string) PlatformOption {
	return platformOptionFunc(func(c *platformConfig) {
		c.region = region
	})
}

// WithPlatformProject sets the cloud project (for Vertex).
func WithPlatformProject(project string) PlatformOption {
	return platformOptionFunc(func(c *platformConfig) {
		c.project = project
	})
}

// WithPlatformEndpoint sets a custom endpoint URL.
func WithPlatformEndpoint(endpoint string) PlatformOption {
	return platformOptionFunc(func(c *platformConfig) {
		c.endpoint = endpoint
	})
}

// WithBedrock configures AWS Bedrock as the hosting platform.
// The providerType specifies the provider factory (e.g., "claude", "openai")
// and model is the model identifier. This uses the AWS SDK default credential
// chain (IRSA, instance profile, env vars).
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithBedrock("us-west-2", "claude", "claude-sonnet-4-20250514"),
//	)
func WithBedrock(region, providerType, model string, opts ...PlatformOption) Option {
	return func(c *config) error {
		pc := &platformConfig{
			platformType: "bedrock",
			providerType: providerType,
			model:        model,
			region:       region,
		}
		for _, opt := range opts {
			opt.applyPlatform(pc)
		}
		c.platform = pc
		return nil
	}
}

// WithVertex configures Google Cloud Vertex AI as the hosting platform.
// The providerType specifies the provider factory (e.g., "claude", "gemini")
// and model is the model identifier. This uses Application Default Credentials
// (Workload Identity, gcloud auth, etc.).
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithVertex("us-central1", "my-project", "gemini", "gemini-2.0-flash"),
//	)
func WithVertex(region, project, providerType, model string, opts ...PlatformOption) Option {
	return func(c *config) error {
		pc := &platformConfig{
			platformType: "vertex",
			providerType: providerType,
			model:        model,
			region:       region,
			project:      project,
		}
		for _, opt := range opts {
			opt.applyPlatform(pc)
		}
		c.platform = pc
		return nil
	}
}

// WithAzure configures Azure AI services as the hosting platform.
// The providerType specifies the provider factory (e.g., "openai") and model
// is the model identifier. This uses the Azure SDK default credential chain
// (Managed Identity, Azure CLI, etc.).
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithAzure("https://my-resource.openai.azure.com", "openai", "gpt-4o"),
//	)
func WithAzure(endpoint, providerType, model string, opts ...PlatformOption) Option {
	return func(c *config) error {
		pc := &platformConfig{
			platformType: "azure",
			providerType: providerType,
			model:        model,
			endpoint:     endpoint,
		}
		for _, opt := range opts {
			opt.applyPlatform(pc)
		}
		c.platform = pc
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

// WithContextWindow sets the hot window size for RAG context assembly.
//
// When set to a positive value, the pipeline uses ContextAssemblyStage and
// IncrementalSaveStage instead of loading all history on every turn. This
// dramatically reduces I/O for long conversations by only loading the most
// recent N messages.
//
// Requires a state store (WithStateStore). The store's MessageReader and
// MessageAppender interfaces are used when available, with automatic fallback
// to full Load/Save when they're not.
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithStateStore(store),
//	    sdk.WithContextWindow(20), // Keep last 20 messages in hot window
//	)
func WithContextWindow(recentMessages int) Option {
	return func(c *config) error {
		c.contextWindow = recentMessages
		return nil
	}
}

// WithContextRetrieval enables semantic search for relevant older messages.
//
// When configured alongside WithContextWindow, the pipeline uses the embedding
// provider to find messages outside the hot window that are semantically similar
// to the current user message. These retrieved messages are inserted chronologically
// between summaries and the hot window.
//
// Requires WithContextWindow to be set.
//
//	embProvider, _ := openai.NewEmbeddingProvider()
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithStateStore(store),
//	    sdk.WithContextWindow(20),
//	    sdk.WithContextRetrieval(embProvider, 5), // Retrieve top 5 relevant messages
//	)
func WithContextRetrieval(embeddingProvider providers.EmbeddingProvider, topK int) Option {
	return func(c *config) error {
		if embeddingProvider == nil {
			return fmt.Errorf("WithContextRetrieval: embeddingProvider must not be nil")
		}
		if topK <= 0 {
			return fmt.Errorf("WithContextRetrieval: topK must be positive, got %d", topK)
		}
		c.retrievalProvider = embeddingProvider
		c.retrievalTopK = topK
		return nil
	}
}

// WithAutoSummarize enables automatic summarization of old conversation turns.
//
// When the message count exceeds the threshold, the oldest unsummarized batch
// of messages is compressed into a summary using the provided LLM provider.
// Summaries are prepended to the context as system messages.
//
// A separate, cheaper provider can be used for summarization (e.g., a smaller model).
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithStateStore(store),
//	    sdk.WithContextWindow(20),
//	    sdk.WithAutoSummarize(summaryProvider, 100, 50), // Summarize after 100 msgs, 50 at a time
//	)
func WithAutoSummarize(provider providers.Provider, threshold, batchSize int) Option {
	return func(c *config) error {
		if provider == nil {
			return fmt.Errorf("WithAutoSummarize: provider must not be nil")
		}
		if threshold <= 0 {
			return fmt.Errorf("WithAutoSummarize: threshold must be positive, got %d", threshold)
		}
		if batchSize <= 0 {
			return fmt.Errorf("WithAutoSummarize: batchSize must be positive, got %d", batchSize)
		}
		c.summarizeProvider = provider
		c.summarizeThreshold = threshold
		c.summarizeBatchSize = batchSize
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

// WithContextCarryForward enables context carry-forward for workflow transitions.
//
// When enabled, transitioning to a new state injects a summary of the previous
// state's conversation into the new conversation via the {{workflow_context}}
// template variable. This provides continuity across workflow states.
//
// Default: disabled (each state gets a fresh conversation).
//
//	wc, _ := sdk.OpenWorkflow("./support.pack.json",
//	    sdk.WithContextCarryForward(),
//	)
func WithContextCarryForward() Option {
	return func(c *config) error {
		c.contextCarryForward = true
		return nil
	}
}

// WithCapability adds an explicit platform capability.
//
// Capabilities provide namespaced tools that are automatically injected into
// conversations. Most capabilities are auto-inferred from pack structure
// (e.g., workflow capability from pack.Workflow). Use this for explicit
// configuration or custom capabilities.
//
//	conv, _ := sdk.Open("./assistant.pack.json", "chat",
//	    sdk.WithCapability(sdk.NewWorkflowCapability()),
//	)
func WithCapability(capability Capability) Option {
	return func(c *config) error {
		c.capabilities = append(c.capabilities, capability)
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

// WithA2ATools registers tools from an A2A [a2a.ToolBridge] so the LLM can
// call remote A2A agents as tools.
//
// The bridge must have already discovered agents via [a2a.ToolBridge.RegisterAgent].
// Each agent skill becomes a tool with Mode "a2a" in the tool registry.
//
// Example:
//
//	client := a2a.NewClient("https://agent.example.com")
//	bridge := a2a.NewToolBridge(client)
//	bridge.RegisterAgent(ctx)
//
//	conv, _ := sdk.Open("./assistant.pack.json", "assistant",
//	    sdk.WithA2ATools(bridge),
//	)
func WithA2ATools(bridge *a2a.ToolBridge) Option {
	return func(c *config) error {
		c.a2aBridge = bridge
		return nil
	}
}

// WithAgentEndpoints configures endpoint resolution for multi-agent tool routing.
//
// When a pack has an agents section, prompts can reference other agent members
// as tools. This option tells the SDK how to resolve agent names to A2A
// endpoint URLs so that tool calls are routed to the correct agent.
//
// Example with a single gateway:
//
//	conv, _ := sdk.Open("./multiagent.pack.json", "orchestrator",
//	    sdk.WithAgentEndpoints(&sdk.StaticEndpointResolver{
//	        BaseURL: "http://localhost:9000",
//	    }),
//	)
//
// Example with per-agent endpoints:
//
//	conv, _ := sdk.Open("./multiagent.pack.json", "orchestrator",
//	    sdk.WithAgentEndpoints(&sdk.MapEndpointResolver{
//	        Endpoints: map[string]string{
//	            "summarizer": "http://summarizer:9001",
//	            "translator": "http://translator:9002",
//	        },
//	    }),
//	)
func WithAgentEndpoints(resolver EndpointResolver) Option {
	return func(c *config) error {
		c.agentEndpointResolver = resolver
		return nil
	}
}

// withLocalAgentExecutor sets a local agent executor for in-process routing.
// This is unexported because it's only used internally by OpenMultiAgent.
func withLocalAgentExecutor(exec *LocalAgentExecutor) Option {
	return func(c *config) error {
		c.localAgentExecutor = exec
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

// WithResponseFormat configures the LLM response format for JSON mode output.
// This instructs the model to return responses in the specified format.
//
// For simple JSON object output:
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithResponseFormat(&providers.ResponseFormat{
//	        Type: providers.ResponseFormatJSON,
//	    }),
//	)
//
// For structured JSON output with a schema:
//
//	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithResponseFormat(&providers.ResponseFormat{
//	        Type:       providers.ResponseFormatJSONSchema,
//	        JSONSchema: schema,
//	        SchemaName: "person",
//	        Strict:     true,
//	    }),
//	)
func WithResponseFormat(format *providers.ResponseFormat) Option {
	return func(c *config) error {
		c.responseFormat = format
		return nil
	}
}

// WithJSONMode is a convenience option that enables simple JSON output mode.
// The model will return valid JSON objects but without schema enforcement.
// Use WithResponseFormat for more control including schema validation.
//
// Example:
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithJSONMode(),
//	)
//	resp, _ := conv.Send(ctx, "List 3 colors as JSON")
//	// Response: {"colors": ["red", "green", "blue"]}
func WithJSONMode() Option {
	return func(c *config) error {
		c.responseFormat = &providers.ResponseFormat{
			Type: providers.ResponseFormatJSON,
		}
		return nil
	}
}

// VideoStreamConfig configures realtime video/image streaming for duplex sessions.
// This enables webcam feeds, screen sharing, and continuous frame analysis.
type VideoStreamConfig struct {
	// TargetFPS is the target frame rate for streaming.
	// Frames exceeding this rate will be dropped.
	// Default: 1.0 (one frame per second, suitable for most LLM vision scenarios)
	TargetFPS float64

	// MaxWidth is the maximum frame width in pixels.
	// Frames larger than this are resized. 0 means no limit.
	// Default: 0 (no resizing)
	MaxWidth int

	// MaxHeight is the maximum frame height in pixels.
	// Frames larger than this are resized. 0 means no limit.
	// Default: 0 (no resizing)
	MaxHeight int

	// Quality is the JPEG compression quality (1-100) for frame encoding.
	// Higher values = better quality, larger size.
	// Default: 85
	Quality int

	// EnableResize enables automatic frame resizing when dimensions exceed limits.
	// Default: true (resizing enabled when MaxWidth/MaxHeight are set)
	EnableResize bool
}

// DefaultVideoStreamConfig returns sensible defaults for video streaming.
func DefaultVideoStreamConfig() *VideoStreamConfig {
	return &VideoStreamConfig{
		TargetFPS:    defaultVideoStreamFPS,
		MaxWidth:     0,
		MaxHeight:    0,
		Quality:      defaultVideoStreamQuality,
		EnableResize: true,
	}
}

// WithStreamingVideo enables realtime video/image streaming for duplex sessions.
// This is used for webcam feeds, screen sharing, and continuous frame analysis.
//
// The FrameRateLimitStage is added to the pipeline when TargetFPS > 0, dropping
// frames to maintain the target frame rate for LLM processing.
//
// Example with defaults (1 FPS):
//
//	session, _ := sdk.OpenDuplex("./assistant.pack.json", "vision-chat",
//	    sdk.WithStreamingVideo(nil), // Use default settings
//	)
//
// Example with custom config:
//
//	session, _ := sdk.OpenDuplex("./assistant.pack.json", "vision-chat",
//	    sdk.WithStreamingVideo(&sdk.VideoStreamConfig{
//	        TargetFPS:  2.0,      // 2 frames per second
//	        MaxWidth:   1280,     // Resize large frames
//	        MaxHeight:  720,
//	        Quality:    80,
//	    }),
//	)
//
// Sending frames:
//
//	for frame := range webcam.Frames() {
//	    session.SendFrame(ctx, &session.ImageFrame{
//	        Data:      frame.JPEG(),
//	        MIMEType:  "image/jpeg",
//	        Timestamp: time.Now(),
//	    })
//	}
func WithStreamingVideo(cfg *VideoStreamConfig) Option {
	return func(c *config) error {
		if cfg == nil {
			cfg = DefaultVideoStreamConfig()
		}
		c.videoStreamConfig = cfg
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

// WithAudioData attaches audio from raw bytes.
//
//	resp, _ := conv.Send(ctx, "Transcribe this audio",
//	    sdk.WithAudioData(audioBytes, "audio/mp3"),
//	)
func WithAudioData(data []byte, mimeType string) SendOption {
	return func(c *sendConfig) error {
		c.parts = append(c.parts, audioDataPart{data: data, mimeType: mimeType})
		return nil
	}
}

// WithVideoFile attaches a video from a file path.
//
//	resp, _ := conv.Send(ctx, "Describe this video",
//	    sdk.WithVideoFile("/path/to/video.mp4"),
//	)
func WithVideoFile(path string) SendOption {
	return func(c *sendConfig) error {
		c.parts = append(c.parts, videoFilePart{path: path})
		return nil
	}
}

// WithVideoData attaches a video from raw bytes.
//
//	resp, _ := conv.Send(ctx, "Describe this video",
//	    sdk.WithVideoData(videoBytes, "video/mp4"),
//	)
func WithVideoData(data []byte, mimeType string) SendOption {
	return func(c *sendConfig) error {
		c.parts = append(c.parts, videoDataPart{data: data, mimeType: mimeType})
		return nil
	}
}

// WithFile attaches a file with the given name and content.
//
// Deprecated: Use WithDocumentFile or WithDocumentData instead for proper document handling.
// This function is kept for backward compatibility but should not be used for new code
// as it cannot properly handle binary files.
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

// WithDocumentFile attaches a document from a file path (PDF, Word, markdown, etc.).
//
//	resp, _ := conv.Send(ctx, "Analyze this document",
//	    sdk.WithDocumentFile("contract.pdf"),
//	)
func WithDocumentFile(path string) SendOption {
	return func(c *sendConfig) error {
		c.parts = append(c.parts, documentFilePart{path: path})
		return nil
	}
}

// WithDocumentData attaches a document from raw data with the specified MIME type.
//
//	resp, _ := conv.Send(ctx, "Review this PDF",
//	    sdk.WithDocumentData(pdfBytes, types.MIMETypePDF),
//	)
func WithDocumentData(data []byte, mimeType string) SendOption {
	return func(c *sendConfig) error {
		c.parts = append(c.parts, documentDataPart{data: data, mimeType: mimeType})
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

type audioDataPart struct {
	data     []byte
	mimeType string
}

type videoFilePart struct {
	path string
}

type videoDataPart struct {
	data     []byte
	mimeType string
}

type filePart struct {
	name string
	data []byte
}
type documentFilePart struct {
	path string
}

type documentDataPart struct {
	data     []byte
	mimeType string
}

// WithEvalDispatcher configures the eval dispatcher for running evals.
//
// The dispatcher controls how evals execute:
//   - InProcDispatcher: runs evals in-process (simplest, synchronous)
//   - EventDispatcher: publishes eval events to an event bus (async)
//   - NoOpDispatcher: disables eval execution at the SDK level
//
// Example:
//
//	registry := evals.NewEvalTypeRegistry()
//	runner := evals.NewEvalRunner(registry)
//	dispatcher := evals.NewInProcDispatcher(runner, nil)
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithEvalDispatcher(dispatcher),
//	)
func WithEvalDispatcher(d evals.EvalDispatcher) Option {
	return func(c *config) error {
		c.evalDispatcher = d
		return nil
	}
}

// WithEvalRegistry provides a custom eval type registry.
//
// Use this to register custom eval type handlers beyond the built-in ones.
// If not set, the default registry with all built-in handlers is used.
func WithEvalRegistry(r *evals.EvalTypeRegistry) Option {
	return func(c *config) error {
		c.evalRegistry = r
		return nil
	}
}

// WithResultWriters configures where eval results are sent.
//
// Multiple writers can be provided; they are composed into a CompositeResultWriter.
//
// Example:
//
//	mc := evals.NewMetricCollector()
//	metricWriter := evals.NewMetricResultWriter(mc, defs)
//
//	conv, _ := sdk.Open("./chat.pack.json", "assistant",
//	    sdk.WithEvalDispatcher(dispatcher),
//	    sdk.WithResultWriters(metricWriter),
//	)
func WithResultWriters(writers ...evals.ResultWriter) Option {
	return func(c *config) error {
		c.evalResultWriters = append(c.evalResultWriters, writers...)
		return nil
	}
}

// WithJudgeProvider configures the LLM judge provider for judge-based evals.
//
// If not set, an SDKJudgeProvider is created automatically using the
// conversation's provider.
func WithJudgeProvider(jp handlers.JudgeProvider) Option {
	return func(c *config) error {
		c.judgeProvider = jp
		return nil
	}
}
