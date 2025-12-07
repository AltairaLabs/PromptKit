package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
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

	// Context management
	tokenBudget        int
	truncationStrategy string

	// Validation behavior
	validationMode     ValidationMode
	disabledValidators []string
	strictValidation   bool

	// MCP configuration
	mcpServers []mcp.ServerConfig
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
