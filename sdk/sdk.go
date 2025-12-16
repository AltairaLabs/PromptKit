package sdk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/internal/provider"
	"github.com/AltairaLabs/PromptKit/sdk/session"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
)

// debugSnippetMaxLen is the max length for debug log snippets.
const debugSnippetMaxLen = 200

// Open loads a pack file and creates a new conversation for the specified prompt.
//
// This is the primary entry point for SDK v2. It:
//   - Loads and parses the pack file
//   - Auto-detects the provider from environment (OPENAI_API_KEY, ANTHROPIC_API_KEY, etc.)
//   - Configures the runtime pipeline based on pack settings
//   - Creates an isolated conversation with its own state
//
// Basic usage:
//
//	conv, err := sdk.Open("./assistant.pack.json", "chat")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer conv.Close()
//
//	resp, _ := conv.Send(ctx, "Hello!")
//	fmt.Println(resp.Text())
//
// With options:
//
//	conv, err := sdk.Open("./assistant.pack.json", "chat",
//	    sdk.WithModel("gpt-4o"),
//	    sdk.WithAPIKey(os.Getenv("MY_KEY")),
//	    sdk.WithStateStore(redisStore),
//	)
//
// The packPath can be:
//   - Absolute path: "/path/to/assistant.pack.json"
//   - Relative path: "./packs/assistant.pack.json"
//   - URL: "https://example.com/packs/assistant.pack.json" (future)
//
// The promptName must match a prompt ID defined in the pack's "prompts" section.
func Open(packPath, promptName string, opts ...Option) (*Conversation, error) {
	// Apply options to build configuration
	cfg, err := applyOptions(promptName, opts)
	if err != nil {
		return nil, err
	}

	// Load and validate pack
	p, prompt, err := loadAndValidatePack(packPath, promptName, cfg)
	if err != nil {
		return nil, err
	}

	// Resolve provider and store in config
	_, err = resolveProvider(cfg)
	if err != nil {
		return nil, err
	}

	// Create conversation
	conv := &Conversation{
		pack:           p,
		prompt:         prompt,
		promptName:     promptName,
		promptRegistry: p.ToPromptRegistry(),                                  // Create registry for PromptAssemblyMiddleware
		toolRegistry:   tools.NewRegistryWithRepository(p.ToToolRepository()), // Create registry with pack tools
		config:         cfg,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewPendingStore(),
	}

	// Apply default variables from prompt BEFORE initializing session
	// This ensures defaults are available when creating the session
	applyDefaultVariables(conv, prompt)

	// Initialize internal memory store for conversation history
	// This is used by StateStoreLoad/Save middleware in the pipeline
	if err := initInternalStateStore(conv, cfg); err != nil {
		return nil, err
	}

	// Initialize event bus (use provided or create new)
	initEventBus(conv, cfg)

	// Initialize MCP registry if configured
	if err := initMCPRegistry(conv, cfg); err != nil {
		return nil, err
	}

	return conv, nil
}

// OpenDuplex loads a pack file and creates a new duplex streaming conversation for the specified prompt.
//
// This creates a conversation in duplex mode for bidirectional streaming interactions.
// Use this when you need real-time streaming input/output with the LLM.
//
// Basic usage:
//
//	conv, err := sdk.OpenDuplex("./assistant.pack.json", "chat")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer conv.Close()
//
//	// Send streaming input
//	go func() {
//	    conv.SendText(ctx, "Hello, ")
//	    conv.SendText(ctx, "how are you?")
//	}()
//
//	// Receive streaming output
//	respCh, _ := conv.Response()
//	for chunk := range respCh {
//	    fmt.Print(chunk.Content)
//	}
//
// The provider must support streaming input (implement providers.StreamInputSupport).
// Currently supported providers: Gemini with certain models.
func OpenDuplex(packPath, promptName string, opts ...Option) (*Conversation, error) {
	// Apply options to build configuration
	cfg, err := applyOptions(promptName, opts)
	if err != nil {
		return nil, err
	}

	// Load and validate pack
	p, prompt, err := loadAndValidatePack(packPath, promptName, cfg)
	if err != nil {
		return nil, err
	}

	// Resolve provider
	prov, err := resolveProvider(cfg)
	if err != nil {
		return nil, err
	}

	// Verify provider supports streaming input
	streamProvider, ok := prov.(providers.StreamInputSupport)
	if !ok {
		return nil, fmt.Errorf(
			"provider %T does not support duplex streaming (must implement providers.StreamInputSupport)",
			prov,
		)
	}

	// Create conversation
	conv := &Conversation{
		pack:           p,
		prompt:         prompt,
		promptName:     promptName,
		promptRegistry: p.ToPromptRegistry(),
		toolRegistry:   tools.NewRegistryWithRepository(p.ToToolRepository()),
		config:         cfg,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewPendingStore(),
	}

	// Apply default variables from prompt BEFORE initializing session
	applyDefaultVariables(conv, prompt)

	// Initialize duplex session
	if err := initDuplexSession(conv, cfg, streamProvider); err != nil {
		return nil, err
	}

	// Initialize event bus
	initEventBus(conv, cfg)

	// Initialize MCP registry if configured
	if err := initMCPRegistry(conv, cfg); err != nil {
		return nil, err
	}

	return conv, nil
}

// applyOptions applies the configuration options.
func applyOptions(promptName string, opts []Option) (*config, error) {
	cfg := &config{promptName: promptName}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}
	return cfg, nil
}

// loadAndValidatePack loads the pack and validates the prompt exists.
func loadAndValidatePack(packPath, promptName string, cfg *config) (*pack.Pack, *pack.Prompt, error) {
	absPath, err := resolvePackPath(packPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve pack path: %w", err)
	}

	// Build load options
	loadOpts := pack.LoadOptions{
		SkipSchemaValidation: cfg.skipSchemaValidation,
	}

	p, err := pack.Load(absPath, loadOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load pack: %w", err)
	}

	prompt, ok := p.Prompts[promptName]
	if !ok {
		available := make([]string, 0, len(p.Prompts))
		for name := range p.Prompts {
			available = append(available, name)
		}
		return nil, nil, fmt.Errorf("prompt %q not found in pack (available: %v)", promptName, available)
	}

	return p, prompt, nil
}

// resolveProvider auto-detects or uses the configured provider.
// Stores the resolved provider in cfg.provider for later use.
func resolveProvider(cfg *config) (providers.Provider, error) {
	if cfg.provider != nil {
		return cfg.provider, nil
	}
	detected, err := provider.Detect(cfg.apiKey, cfg.model)
	if err != nil {
		return nil, fmt.Errorf("failed to detect provider: %w", err)
	}
	cfg.provider = detected
	return detected, nil
}

// initEventBus initializes the conversation's event bus.
func initEventBus(conv *Conversation, cfg *config) {
	if cfg.eventBus == nil {
		cfg.eventBus = events.NewEventBus()
	}
	// EventBus is stored in config and accessed via c.config.eventBus
}

// initInternalStateStore initializes the internal state store for conversation history.
// If a state store is provided via options, use that. Otherwise, create a MemoryStore.
// This enables the StateStoreLoad/Save middleware to manage conversation history.
// Also generates a unique conversation ID if not already set.
// Finally, creates a TextSession wrapping a pre-configured pipeline.
func initInternalStateStore(conv *Conversation, cfg *config) error {
	var store statestore.Store
	if cfg.stateStore != nil {
		// User provided a state store (e.g., Redis for persistence)
		store = cfg.stateStore
	} else {
		// Create internal memory store for this conversation
		store = statestore.NewMemoryStore()
	}

	// Generate conversation ID if not already set via options
	conversationID := cfg.conversationID
	if conversationID == "" {
		conversationID = uuid.New().String()
	}

	// Get initial variables from config (includes prompt defaults)
	initialVars := cfg.initialVariables
	if initialVars == nil {
		initialVars = make(map[string]string)
	}

	// Build pipeline once during initialization (note: pipeline doesn't capture variables anymore)
	// No StreamInputSession for unary mode
	pipeline, err := conv.buildPipelineWithParams(store, conversationID, nil)
	if err != nil {
		return fmt.Errorf("failed to build pipeline: %w", err)
	}

	// Create text session wrapping the pipeline
	unarySession, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: conversationID,
		StateStore:     store,
		Pipeline:       pipeline,
		Variables:      initialVars,
	})
	if err != nil {
		return fmt.Errorf("failed to create unary session: %w", err)
	}

	conv.mode = UnaryMode
	conv.unarySession = unarySession
	return nil
}

// initDuplexSession initializes a duplex streaming session.
func initDuplexSession(conv *Conversation, cfg *config, streamProvider providers.StreamInputSupport) error {
	var store statestore.Store
	if cfg.stateStore != nil {
		store = cfg.stateStore
	} else {
		store = statestore.NewMemoryStore()
	}

	// Generate conversation ID if not already set via options
	conversationID := cfg.conversationID
	if conversationID == "" {
		conversationID = uuid.New().String()
	}

	// Get initial variables from config
	initialVars := cfg.initialVariables
	if initialVars == nil {
		initialVars = make(map[string]string)
	}

	// Create pipeline builder closure that captures conversation context
	// Returns *stage.StreamPipeline directly for duplex sessions
	pipelineBuilder := func(
		ctx context.Context,
		provider providers.Provider,
		providerSession providers.StreamInputSession,
		convID string,
		stateStore statestore.Store,
	) (*stage.StreamPipeline, error) {
		// Build stage pipeline directly (not wrapped) for duplex sessions
		return conv.buildStreamPipelineWithParams(stateStore, convID, providerSession)
	}

	// Mode is determined by cfg.streamingConfig:
	// - If set: ASM mode (creates provider session, continuous streaming)
	// - If nil: VAD mode (no provider session, turn-based streaming)
	var streamConfig *providers.StreamingInputConfig
	if cfg.streamingConfig != nil {
		streamConfig = cfg.streamingConfig

		// For ASM mode, load and set the system instruction from prompt registry
		// Gemini Live API requires system instruction in the setup message
		if streamConfig.SystemInstruction == "" && conv.promptRegistry != nil {
			logger.Debug("Loading system instruction with variables",
				"promptName", conv.promptName,
				"varCount", len(initialVars),
				"topic", initialVars["topic"])
			assembled := conv.promptRegistry.LoadWithVars(conv.promptName, initialVars, "")
			if assembled != nil && assembled.SystemPrompt != "" {
				streamConfig.SystemInstruction = assembled.SystemPrompt
				// Log first N chars of system prompt for debugging
				snippet := assembled.SystemPrompt
				if len(snippet) > debugSnippetMaxLen {
					snippet = snippet[:debugSnippetMaxLen] + "..."
				}
				logger.Debug("Set system instruction for ASM session",
					"promptName", conv.promptName,
					"length", len(assembled.SystemPrompt),
					"snippet", snippet)
			}
		}
	}

	// Create duplex session with builder
	duplexSession, err := session.NewDuplexSession(context.Background(), &session.DuplexSessionConfig{
		ConversationID:  conversationID,
		StateStore:      store,
		PipelineBuilder: pipelineBuilder,
		Provider:        streamProvider,
		Config:          streamConfig, // nil for VAD mode, set for ASM mode
		Variables:       initialVars,
	})
	if err != nil {
		return fmt.Errorf("failed to create duplex session: %w", err)
	}

	conv.mode = DuplexMode
	conv.duplexSession = duplexSession
	return nil
}

// initMCPRegistry initializes the MCP registry if servers are configured.
func initMCPRegistry(conv *Conversation, cfg *config) error {
	if len(cfg.mcpServers) == 0 {
		return nil
	}

	registry := mcp.NewRegistry()
	conv.mcpRegistry = registry

	for _, serverCfg := range cfg.mcpServers {
		if err := registry.RegisterServer(serverCfg); err != nil {
			_ = registry.Close()
			return fmt.Errorf("failed to register MCP server %q: %w", serverCfg.Name, err)
		}
	}
	return nil
}

// applyDefaultVariables sets default variable values from the prompt.
// Only sets defaults for variables that weren't already provided via WithVariables.
func applyDefaultVariables(conv *Conversation, prompt *pack.Prompt) {
	// This is called before session is created, so we need to track these
	// temporarily. The session will be initialized with these variables later.
	for _, v := range prompt.Variables {
		if v.Default != "" {
			// Store in a temporary map until session is created
			if conv.config != nil && conv.config.initialVariables == nil {
				conv.config.initialVariables = make(map[string]string)
			}
			if conv.config != nil {
				// Only set default if user didn't provide a value
				if _, exists := conv.config.initialVariables[v.Name]; !exists {
					conv.config.initialVariables[v.Name] = v.Default
				}
			}
		}
	}
}

// Resume loads an existing conversation from state storage.
//
// Use this to continue a conversation that was previously persisted:
//
//	store := statestore.NewRedisStore("redis://localhost:6379")
//	conv, err := sdk.Resume("session-123", "./chat.pack.json", "assistant",
//	    sdk.WithStateStore(store),
//	)
//	if errors.Is(err, sdk.ErrConversationNotFound) {
//	    // Start new conversation
//	    conv, _ = sdk.Open("./chat.pack.json", "assistant",
//	        sdk.WithStateStore(store),
//	        sdk.WithConversationID("session-123"),
//	    )
//	}
//
// Resume requires a state store to be configured. If no state store is provided,
// it returns [ErrNoStateStore].
func Resume(conversationID, packPath, promptName string, opts ...Option) (*Conversation, error) {
	// Ensure state store is provided
	cfg := &config{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	if cfg.stateStore == nil {
		return nil, ErrNoStateStore
	}

	// Try to load existing state
	ctx := context.Background()
	state, err := cfg.stateStore.Load(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("failed to load conversation state: %w", err)
	}
	if state == nil {
		return nil, ErrConversationNotFound
	}

	// Open conversation with the loaded state
	// Add WithConversationID to preserve the original ID
	optsWithID := append(opts, WithConversationID(conversationID))
	conv, err := Open(packPath, promptName, optsWithID...)
	if err != nil {
		return nil, err
	}

	// The session now has the correct conversation ID from WithConversationID option

	return conv, nil
}

// resolvePackPath converts a pack path to an absolute path.
func resolvePackPath(packPath string) (string, error) {
	// Handle absolute paths
	if filepath.IsAbs(packPath) {
		if _, err := os.Stat(packPath); err != nil {
			return "", fmt.Errorf("pack file not found: %s", packPath)
		}
		return packPath, nil
	}

	// Handle relative paths
	absPath, err := filepath.Abs(packPath)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(absPath); err != nil {
		return "", fmt.Errorf("pack file not found: %s", absPath)
	}

	return absPath, nil
}
