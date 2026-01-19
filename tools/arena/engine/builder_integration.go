package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"

	// Import provider subpackages to register their factories
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/claude"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/imagen"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/ollama"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/replay"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/vllm"
	runtimestore "github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/tools/arena/adapters"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/selfplay"
	"github.com/AltairaLabs/PromptKit/tools/arena/statestore"
	"github.com/AltairaLabs/PromptKit/tools/arena/turnexecutors"
	"github.com/redis/go-redis/v9"
)

// BuildEngineComponents builds all engine components from a loaded Config object.
// This function creates and initializes:
// - MCP registry and tools (if configured)
// - Provider registry (for main assistant)
// - Prompt registry (if configured)
// - Tool registry (static + MCP tools)
// - Turn executor
// - Self-play provider registry (if enabled)
// - Self-play registry (if enabled)
// - Conversation executor
//
// This function is exported to enable programmatic creation of Arena engines
// without requiring file-based configuration. Users can construct a *config.Config
// programmatically and pass it to this function to get all required registries
// for use with NewEngine.
//
// Returns all components needed to construct an Engine, or an error if any component fails to build.
func BuildEngineComponents(cfg *config.Config) (
	providerRegistry *providers.Registry,
	promptRegistry *prompt.Registry,
	mcpRegistry *mcp.RegistryImpl,
	convExecutor ConversationExecutor,
	err error,
) {
	// Initialize core registries
	providerRegistry = providers.NewRegistry()

	// Create MCP registry from config
	mcpRegistry, err = buildMCPRegistry(cfg)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to create MCP registry: %w", err)
	}

	// Build prompt registry - validate and extract task type mappings
	promptRegistry, err = buildPromptRegistry(cfg)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to build prompt registry: %w", err)
	}

	// Build tool registry with memory repository
	toolRegistry, toolErr := buildToolRegistry(cfg)
	if toolErr != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to build tool registry: %w", toolErr)
	}

	// Build provider registry (must stay in engine to avoid circular import with config)
	for _, provider := range cfg.LoadedProviders {
		providerImpl, provErr := createProviderImpl(provider)
		if provErr != nil {
			return nil, nil, nil, nil, provErr
		}
		providerRegistry.Register(providerImpl)
	}

	// Discover and register MCP tools (if any MCP servers configured)
	if mcpErr := discoverAndRegisterMCPTools(mcpRegistry, toolRegistry); mcpErr != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to discover MCP tools: %w", mcpErr)
	}

	// Build media storage service
	mediaStorage, storageErr := buildMediaStorage(cfg)
	if storageErr != nil {
		return nil, nil, nil, nil, storageErr
	}

	// Build conversation executor (engine-specific, stays here)
	conversationExecutor, err := newConversationExecutor(cfg, toolRegistry, promptRegistry, mediaStorage, providerRegistry)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	return providerRegistry, promptRegistry, mcpRegistry, conversationExecutor, nil
}

// createProviderImpl converts config.Provider to providers.Provider.
func createProviderImpl(provider *config.Provider) (providers.Provider, error) {
	spec := providers.ProviderSpec{
		ID:               provider.ID,
		Type:             provider.Type,
		Model:            provider.Model,
		BaseURL:          provider.BaseURL,
		IncludeRawOutput: provider.IncludeRawOutput,
		AdditionalConfig: provider.AdditionalConfig, // Pass through additional config
		Defaults: providers.ProviderDefaults{
			Temperature: provider.Defaults.Temperature,
			TopP:        provider.Defaults.TopP,
			MaxTokens:   provider.Defaults.MaxTokens,
			Pricing: providers.Pricing{
				InputCostPer1K:  provider.Pricing.InputCostPer1K,
				OutputCostPer1K: provider.Pricing.OutputCostPer1K,
			},
		},
	}
	return providers.CreateProviderFromSpec(spec)
}

// discoverAndRegisterMCPTools discovers tools from MCP servers and registers them in the tool registry.
// This function must stay in engine package to avoid circular imports with tools package.
func discoverAndRegisterMCPTools(mcpRegistry *mcp.RegistryImpl, toolRegistry *tools.Registry) error {
	// Early return if no servers configured
	if mcpRegistry == nil {
		return nil
	}

	// Register MCP executor for routing tool calls
	mcpExecutor := tools.NewMCPExecutor(mcpRegistry)
	toolRegistry.RegisterExecutor(mcpExecutor)

	// Discover and register tools from all MCP servers
	initCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	serverTools, err := mcpRegistry.ListAllTools(initCtx)
	if err != nil {
		return fmt.Errorf("failed to list MCP tools: %w", err)
	}

	// Early return if no tools discovered
	if len(serverTools) == 0 {
		return nil
	}

	// Register each discovered tool in the tool registry
	totalTools := 0
	for serverName, mcpTools := range serverTools {
		for _, mcpTool := range mcpTools {
			// MCP tools return a ToolCallResponse with Content array
			// Define a generic output schema that matches this structure
			mcpOutputSchema := json.RawMessage(`{
				"type": "object",
				"properties": {
					"content": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"type": {"type": "string"},
								"text": {"type": "string"},
								"data": {"type": "string"},
								"mimeType": {"type": "string"},
								"uri": {"type": "string"}
							},
							"required": ["type"]
						}
					},
					"isError": {"type": "boolean"}
				},
				"required": ["content"]
			}`)

			toolDesc := &tools.ToolDescriptor{
				Name:         mcpTool.Name,
				Description:  mcpTool.Description,
				InputSchema:  mcpTool.InputSchema,
				OutputSchema: mcpOutputSchema,
				Mode:         "mcp", // Mark as MCP tool for routing
			}
			if err := toolRegistry.Register(toolDesc); err != nil {
				return fmt.Errorf("failed to register MCP tool %s from server %s: %w", mcpTool.Name, serverName, err)
			}
			totalTools++
		}
	}

	logger.Info("Discovered MCP tools", "tools", totalTools)
	return nil
}

// buildPromptRegistry creates a prompt registry from config.
// It uses a memory repository with pre-loaded prompt configs from the config layer.
// Returns nil if no prompt configs are loaded.
func buildPromptRegistry(cfg *config.Config) (*prompt.Registry, error) {
	if len(cfg.LoadedPromptConfigs) == 0 {
		return nil, nil
	}

	// Create memory repository
	memRepo := memory.NewPromptRepository()

	// Track task types for duplicate detection
	taskTypeToID := make(map[string]string)
	taskTypeToFile := make(map[string]string)

	// Register each pre-loaded prompt config
	for refID, promptData := range cfg.LoadedPromptConfigs {
		if promptData.Config == nil {
			continue
		}

		// Type assert the config (it was loaded as interface{} to avoid circular import)
		promptConfig, ok := promptData.Config.(*prompt.Config)
		if !ok {
			return nil, fmt.Errorf("prompt config %s (ID: %s) has invalid type", promptData.FilePath, refID)
		}

		// Validate task type is present
		if promptConfig.Spec.TaskType == "" {
			return nil, fmt.Errorf(
				"prompt config %s (ID: %s) is missing required spec.task_type field",
				promptData.FilePath, refID)
		}

		// Check for duplicate task types
		if existingFile, exists := taskTypeToFile[promptConfig.Spec.TaskType]; exists {
			existingID := taskTypeToID[promptConfig.Spec.TaskType]
			return nil, fmt.Errorf(
				"duplicate task_type '%s' found in multiple prompt configs:\n"+
					"  - %s (ID: %s)\n  - %s (ID: %s)\n"+
					"Each task_type must be unique across all prompt configs",
				promptConfig.Spec.TaskType,
				existingFile,
				existingID,
				promptData.FilePath,
				refID,
			)
		}

		// Save prompt to memory repository
		if err := memRepo.SavePrompt(promptConfig); err != nil {
			return nil, fmt.Errorf("failed to register prompt %s (ID: %s): %w", promptData.FilePath, refID, err)
		}

		taskTypeToFile[promptConfig.Spec.TaskType] = promptData.FilePath
		taskTypeToID[promptConfig.Spec.TaskType] = refID
	}

	// Create registry with memory repository
	logger.Info("Initialized prompt registry with memory repository", "configs", len(taskTypeToFile))
	return prompt.NewRegistryWithRepository(memRepo), nil
}

// buildToolRegistry creates a tool registry from config.
// It parses tool files and uses a memory repository with pre-loaded tool descriptors.
// Returns nil if no tools are loaded.
func buildToolRegistry(cfg *config.Config) (*tools.Registry, error) {
	// Create memory repository
	memRepo := memory.NewToolRepository()

	// Parse and register each tool from the loaded data
	for _, toolData := range cfg.LoadedTools {
		// Create a temporary registry to parse the tool
		tempRegistry := tools.NewRegistry()
		if err := tempRegistry.LoadToolFromBytes(toolData.FilePath, toolData.Data); err != nil {
			return nil, fmt.Errorf("failed to parse tool %s: %w", toolData.FilePath, err)
		}

		// Get the parsed tool descriptor(s) from temp registry
		loadedTools := tempRegistry.GetTools()
		for _, tool := range loadedTools {
			if err := memRepo.SaveTool(tool); err != nil {
				return nil, fmt.Errorf("failed to save tool %s to repository: %w", tool.Name, err)
			}
		}
	}

	// Create registry with memory repository
	toolRegistry := tools.NewRegistryWithRepository(memRepo)

	if len(cfg.LoadedTools) > 0 {
		logger.Info("Initialized tool registry with memory repository", "tools", len(cfg.LoadedTools))
	}

	return toolRegistry, nil
}

// buildMCPRegistry creates an MCP registry from config and registers all MCP servers.
// Returns nil if no MCP servers are configured.
func buildMCPRegistry(cfg *config.Config) (*mcp.RegistryImpl, error) {
	if len(cfg.MCPServers) == 0 {
		return nil, nil
	}

	registry := mcp.NewRegistry()

	for _, serverCfg := range cfg.MCPServers {
		mcpServerConfig := mcp.ServerConfig{
			Name:    serverCfg.Name,
			Command: serverCfg.Command,
			Args:    serverCfg.Args,
			Env:     serverCfg.Env,
		}
		if err := registry.RegisterServer(mcpServerConfig); err != nil {
			return nil, fmt.Errorf("failed to register MCP server %s: %w", serverCfg.Name, err)
		}
	}

	return registry, nil
}

// newConversationExecutor creates the conversation executor with self-play support if enabled.
// The mediaStorage parameter is passed through to turn executors to enable media externalization.
// The providerRegistry is used to reuse providers for selfplay instead of creating duplicates.
// This function stays in engine package as it creates engine-specific types.
//
// Returns a CompositeConversationExecutor that routes between:
// - DefaultConversationExecutor for standard turn-based conversations
// - DuplexConversationExecutor for bidirectional streaming scenarios (duplex mode)
func newConversationExecutor(
	cfg *config.Config,
	toolRegistry *tools.Registry,
	promptRegistry *prompt.Registry,
	mediaStorage storage.MediaStorageService,
	providerRegistry *providers.Registry,
) (ConversationExecutor, error) {
	// Build turn executors (always needed, even without self-play)
	pipelineExecutor := turnexecutors.NewPipelineExecutor(toolRegistry, mediaStorage)
	scriptedExecutor := turnexecutors.NewScriptedExecutor(pipelineExecutor)

	// Build self-play components if enabled
	var selfPlayExecutor *turnexecutors.SelfPlayExecutor
	var selfPlayRegistry *selfplay.Registry

	if cfg.SelfPlay != nil && cfg.SelfPlay.Enabled {
		var err error
		selfPlayRegistry, selfPlayExecutor, err = buildSelfPlayComponents(cfg, pipelineExecutor, providerRegistry)
		if err != nil {
			return nil, err
		}
	}

	// Build default conversation executor
	defaultExecutor := NewDefaultConversationExecutor(
		scriptedExecutor,
		selfPlayExecutor,
		selfPlayRegistry,
		promptRegistry,
	)

	// Build duplex conversation executor
	duplexExecutor := NewDuplexConversationExecutor(
		selfPlayRegistry,
		promptRegistry,
		toolRegistry,
		mediaStorage,
	)

	// Build eval conversation executor components
	evalExecutor := buildEvalExecutor(promptRegistry, providerRegistry)

	// Return composite executor that routes based on scenario configuration
	return NewCompositeConversationExecutor(defaultExecutor, duplexExecutor, evalExecutor), nil
}

// buildEvalExecutor creates the eval conversation executor with required registries.
func buildEvalExecutor(
	promptRegistry *prompt.Registry,
	providerRegistry *providers.Registry,
) *EvalConversationExecutor {
	// Import adapters and assertions packages
	// We need to do this here to avoid circular dependencies
	adapterRegistry := adapters.NewRegistry()
	assertionRegistry := assertions.NewArenaAssertionRegistry()
	convAssertionRegistry := assertions.NewConversationAssertionRegistry()

	return NewEvalConversationExecutor(
		adapterRegistry,
		assertionRegistry,
		convAssertionRegistry,
		promptRegistry,
		providerRegistry,
	)
}

// buildSelfPlayComponents creates the self-play registry and executor.
func buildSelfPlayComponents(
	cfg *config.Config,
	pipelineExecutor *turnexecutors.PipelineExecutor,
	mainProviderRegistry *providers.Registry,
) (*selfplay.Registry, *turnexecutors.SelfPlayExecutor, error) {
	// Build self-play provider registry and role mapping
	// Self-play roles now reference providers from the main provider list
	selfPlayProviderRegistry := providers.NewRegistry()
	selfPlayProviderMap := make(map[string]string) // roleID -> providerID

	for _, role := range cfg.SelfPlay.Roles {
		// Look up the provider config to validate it exists
		provider, exists := cfg.LoadedProviders[role.Provider]
		if !exists {
			return nil, nil, fmt.Errorf("self-play role %s references unknown provider %s", role.ID, role.Provider)
		}

		// Reuse provider from main registry instead of creating a duplicate
		providerImpl, found := mainProviderRegistry.Get(provider.ID)
		if !found {
			return nil, nil, fmt.Errorf("self-play role %s: provider %s not found in main registry", role.ID, role.Provider)
		}
		selfPlayProviderRegistry.Register(providerImpl)

		// Map role ID to provider ID for self-play registry
		selfPlayProviderMap[role.ID] = provider.ID
	}

	// Initialize self-play registry
	selfPlayRegistry := selfplay.NewRegistry(
		selfPlayProviderRegistry,
		selfPlayProviderMap,
		cfg.LoadedPersonas,
		cfg.SelfPlay.Roles,
	)

	logger.Info("Initialized self-play registry", "roles", len(cfg.SelfPlay.Roles), "personas", len(cfg.LoadedPersonas))

	// Build self-play executor - registry implements Provider interface directly
	selfPlayExecutor := turnexecutors.NewSelfPlayExecutor(pipelineExecutor, selfPlayRegistry)

	return selfPlayRegistry, selfPlayExecutor, nil
}

// buildStateStore creates a StateStore from configuration.
// Always returns a StateStore - defaults to ArenaStateStore if not configured.
// ArenaStateStore is used by default for Arena testing to capture telemetry
// (validation results, turn metrics, costs) for analysis.
func buildStateStore(cfg *config.Config) (runtimestore.Store, error) {
	if cfg.StateStore == nil || cfg.StateStore.Type == "" || cfg.StateStore.Type == "memory" {
		// Default to ArenaStateStore for telemetry capture
		return statestore.NewArenaStateStore(), nil
	}

	switch cfg.StateStore.Type {
	case "redis":
		if cfg.StateStore.Redis == nil {
			return nil, fmt.Errorf("redis configuration is required when type is 'redis'")
		}

		// Create Redis client
		client := redis.NewClient(&redis.Options{
			Addr:     cfg.StateStore.Redis.Address,
			Password: cfg.StateStore.Redis.Password,
			DB:       cfg.StateStore.Redis.Database,
		})

		// Parse TTL if specified
		var opts []runtimestore.RedisOption
		if cfg.StateStore.Redis.TTL != "" {
			ttl, err := time.ParseDuration(cfg.StateStore.Redis.TTL)
			if err != nil {
				return nil, fmt.Errorf("invalid TTL duration %q: %w", cfg.StateStore.Redis.TTL, err)
			}
			opts = append(opts, runtimestore.WithTTL(ttl))
		}

		// Set prefix if specified
		if cfg.StateStore.Redis.Prefix != "" {
			opts = append(opts, runtimestore.WithPrefix(cfg.StateStore.Redis.Prefix))
		}

		return runtimestore.NewRedisStore(client, opts...), nil

	default:
		return nil, fmt.Errorf("unsupported state store type: %s", cfg.StateStore.Type)
	}
}
