package sdk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/internal/provider"
)

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

	// Resolve provider
	prov, err := resolveProvider(cfg)
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
		provider:       prov,
		config:         cfg,
		variables:      make(map[string]string),
		handlers:       make(map[string]ToolHandler),
	}

	// Initialize event bus (use provided or create new)
	initEventBus(conv, cfg)

	// Initialize MCP registry if configured
	if err := initMCPRegistry(conv, cfg); err != nil {
		return nil, err
	}

	// Apply default variables from prompt
	applyDefaultVariables(conv, prompt)

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
func resolveProvider(cfg *config) (providers.Provider, error) {
	if cfg.provider != nil {
		return cfg.provider, nil
	}
	detected, err := provider.Detect(cfg.apiKey, cfg.model)
	if err != nil {
		return nil, fmt.Errorf("failed to detect provider: %w", err)
	}
	return detected, nil
}

// initEventBus initializes the conversation's event bus.
func initEventBus(conv *Conversation, cfg *config) {
	if cfg.eventBus != nil {
		conv.eventBus = cfg.eventBus
	} else {
		conv.eventBus = events.NewEventBus()
	}
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
func applyDefaultVariables(conv *Conversation, prompt *pack.Prompt) {
	for _, v := range prompt.Variables {
		if v.Default != "" {
			conv.variables[v.Name] = v.Default
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
	conv, err := Open(packPath, promptName, opts...)
	if err != nil {
		return nil, err
	}

	// Restore state
	conv.state = state
	conv.id = conversationID

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
