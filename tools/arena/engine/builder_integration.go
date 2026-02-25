package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	a2amock "github.com/AltairaLabs/PromptKit/runtime/a2a/mock"
	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers" // register default eval handlers
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/skills"

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
	adapterReg *adapters.Registry,
	a2aCleanup func(),
	toolReg *tools.Registry,
	err error,
) {
	// Initialize core registries
	providerRegistry = providers.NewRegistry()

	// Create MCP registry from config
	mcpRegistry, err = buildMCPRegistry(cfg)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("failed to create MCP registry: %w", err)
	}

	// Build prompt registry - validate and extract task type mappings
	promptRegistry, err = buildPromptRegistry(cfg)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("failed to build prompt registry: %w", err)
	}

	// Build tool registry with memory repository
	toolRegistry, toolErr := buildToolRegistry(cfg)
	if toolErr != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("failed to build tool registry: %w", toolErr)
	}

	// Build provider registry (must stay in engine to avoid circular import with config)
	for _, provider := range cfg.LoadedProviders {
		providerImpl, provErr := createProviderImpl(cfg.ConfigDir, provider)
		if provErr != nil {
			return nil, nil, nil, nil, nil, nil, nil, provErr
		}
		providerRegistry.Register(providerImpl)
	}

	// Discover and register MCP tools (if any MCP servers configured)
	if mcpErr := discoverAndRegisterMCPTools(mcpRegistry, toolRegistry); mcpErr != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("failed to discover MCP tools: %w", mcpErr)
	}

	// Build media storage service
	mediaStorage, storageErr := buildMediaStorage(cfg)
	if storageErr != nil {
		return nil, nil, nil, nil, nil, nil, nil, storageErr
	}

	// Discover and register A2A agent tools (if any A2A agents configured)
	a2aCleanupFn, a2aErr := startAndRegisterA2AAgents(cfg, toolRegistry)
	if a2aErr != nil {
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("failed to start A2A agents: %w", a2aErr)
	}

	// Discover and register skill tools (if pack has skills configured)
	if skillErr := discoverAndRegisterSkillTools(cfg, toolRegistry); skillErr != nil {
		if a2aCleanupFn != nil {
			a2aCleanupFn()
		}
		return nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("failed to register skill tools: %w", skillErr)
	}

	// Build pack eval hook — always create one because scenario turn
	// assertions dispatch through it even when there's no pack loaded.
	var packEvalHook *PackEvalHook
	if cfg.LoadedPack != nil {
		packEvalHook = buildPackEvalHook(cfg, cfg.SkipPackEvals, cfg.EvalTypeFilter)
	} else if !cfg.SkipPackEvals {
		registry := evals.NewEvalTypeRegistry()
		packEvalHook = NewPackEvalHook(registry, nil, false, cfg.EvalTypeFilter, "")
	}

	// Build conversation executor (engine-specific, stays here)
	conversationExecutor, adapterRegistry, err := newConversationExecutor(
		cfg, toolRegistry, promptRegistry, mediaStorage, providerRegistry, packEvalHook)
	if err != nil {
		if a2aCleanupFn != nil {
			a2aCleanupFn()
		}
		return nil, nil, nil, nil, nil, nil, nil, err
	}

	return providerRegistry, promptRegistry, mcpRegistry, conversationExecutor,
		adapterRegistry, a2aCleanupFn, toolRegistry, nil
}

// createProviderImpl converts config.Provider to providers.Provider.
// configDir is used to resolve relative paths in additional_config (e.g. mock_config).
func createProviderImpl(configDir string, provider *config.Provider) (providers.Provider, error) {
	// Resolve relative mock_config path against configDir
	if provider.Type == "mock" && provider.AdditionalConfig != nil {
		if mockCfg, ok := provider.AdditionalConfig["mock_config"].(string); ok &&
			mockCfg != "" && !filepath.IsAbs(mockCfg) && configDir != "" {
			provider.AdditionalConfig["mock_config"] = filepath.Join(configDir, mockCfg)
		}
	}

	// Resolve credential using the credential chain
	ctx := context.Background()
	resolverCfg := credentials.ResolverConfig{
		ProviderType:     provider.Type,
		CredentialConfig: provider.Credential,
	}

	// Handle platform configuration
	var platformConfig *providers.PlatformConfig
	var platform string
	if provider.Platform != nil {
		platform = provider.Platform.Type
		platformConfig = &providers.PlatformConfig{
			Type:             provider.Platform.Type,
			Region:           provider.Platform.Region,
			Project:          provider.Platform.Project,
			Endpoint:         provider.Platform.Endpoint,
			AdditionalConfig: provider.Platform.AdditionalConfig,
		}
		resolverCfg.PlatformConfig = provider.Platform
	}

	cred, err := credentials.Resolve(ctx, resolverCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve credentials for provider %s: %w", provider.ID, err)
	}

	spec := providers.ProviderSpec{
		ID:               provider.ID,
		Type:             provider.Type,
		Model:            provider.Model,
		BaseURL:          provider.BaseURL,
		IncludeRawOutput: provider.IncludeRawOutput,
		AdditionalConfig: provider.AdditionalConfig,
		Credential:       cred,
		Platform:         platform,
		PlatformConfig:   platformConfig,
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

			qualifiedName := fmt.Sprintf("mcp__%s__%s", serverName, mcpTool.Name)
			toolDesc := &tools.ToolDescriptor{
				Name:         qualifiedName,
				Description:  mcpTool.Description,
				InputSchema:  mcpTool.InputSchema,
				OutputSchema: mcpOutputSchema,
				Mode:         "mcp", // Mark as MCP tool for routing
			}
			if err := toolRegistry.Register(toolDesc); err != nil {
				return fmt.Errorf("failed to register MCP tool %s from server %s: %w", qualifiedName, serverName, err)
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
// Also returns the adapter registry for use in batch evaluation enumeration.
func newConversationExecutor(
	cfg *config.Config,
	toolRegistry *tools.Registry,
	promptRegistry *prompt.Registry,
	mediaStorage storage.MediaStorageService,
	providerRegistry *providers.Registry,
	packEvalHook *PackEvalHook,
) (ConversationExecutor, *adapters.Registry, error) {
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
			return nil, nil, err
		}
	}

	// Build default conversation executor
	defaultExecutor := NewDefaultConversationExecutor(
		scriptedExecutor,
		selfPlayExecutor,
		selfPlayRegistry,
		promptRegistry,
		packEvalHook,
	)

	// Build duplex conversation executor
	duplexExecutor := NewDuplexConversationExecutor(
		selfPlayRegistry,
		promptRegistry,
		toolRegistry,
		mediaStorage,
		packEvalHook,
	)

	// Build eval conversation executor components
	evalExecutor, adapterRegistry := buildEvalExecutor(promptRegistry, providerRegistry, packEvalHook)

	// Return composite executor that routes based on scenario configuration
	return NewCompositeConversationExecutor(defaultExecutor, duplexExecutor, evalExecutor), adapterRegistry, nil
}

// buildPackEvalHook creates a PackEvalHook from the loaded pack.
// It resolves pack-level and prompt-level evals, creates a registry with default handlers,
// and returns a hook ready for use in conversation executors.
func buildPackEvalHook(cfg *config.Config, skipEvals bool, evalTypeFilter []string) *PackEvalHook {
	pack := cfg.LoadedPack

	// Collect all eval defs from pack level
	allDefs := pack.Evals

	// Merge with prompt-level evals (prompt overrides by ID)
	for _, pp := range pack.Prompts {
		if len(pp.Evals) > 0 {
			allDefs = evals.ResolveEvals(allDefs, pp.Evals)
		}
	}

	// Always create a PackEvalHook even when pack has no evals,
	// because scenario turn assertions also dispatch through it.
	// Only return nil if there's no pack at all (handled by caller).

	// Create registry with default handlers (registered via init() in handlers package)
	registry := evals.NewEvalTypeRegistry()

	return NewPackEvalHook(registry, allDefs, skipEvals, evalTypeFilter, pack.ID)
}

// buildEvalExecutor creates the eval conversation executor with required registries.
// Returns both the executor and the adapter registry (for use in batch enumeration).
func buildEvalExecutor(
	promptRegistry *prompt.Registry,
	providerRegistry *providers.Registry,
	packEvalHook *PackEvalHook,
) (*EvalConversationExecutor, *adapters.Registry) {
	// Import adapters and assertions packages
	// We need to do this here to avoid circular dependencies
	adapterRegistry := adapters.NewRegistry()

	return NewEvalConversationExecutor(
		adapterRegistry,
		promptRegistry,
		providerRegistry,
		packEvalHook,
	), adapterRegistry
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

// discoverAndRegisterSkillTools discovers skills from the loaded pack and registers
// skill tool descriptors and executor in the tool registry.
func discoverAndRegisterSkillTools(cfg *config.Config, toolRegistry *tools.Registry) error {
	pack := cfg.LoadedPack
	if pack == nil || len(pack.Skills) == 0 {
		return nil
	}

	// Convert pack skill configs to runtime SkillSource
	sources := make([]skills.SkillSource, len(pack.Skills))
	for i, s := range pack.Skills {
		sources[i] = skills.SkillSource{
			Dir:          s.EffectiveDir(),
			Name:         s.Name,
			Description:  s.Description,
			Instructions: s.Instructions,
			Preload:      s.Preload,
		}
	}

	// Discover skills
	reg := skills.NewRegistry()
	if err := reg.Discover(sources); err != nil {
		return fmt.Errorf("skills discovery: %w", err)
	}

	// Create executor (no selector, no pack tool ceiling in arena)
	executor := skills.NewExecutor(skills.ExecutorConfig{Registry: reg})

	// Register tool descriptors + executor
	_ = toolRegistry.Register(skills.BuildSkillActivateDescriptor())
	_ = toolRegistry.Register(skills.BuildSkillDeactivateDescriptor())
	_ = toolRegistry.Register(skills.BuildSkillReadResourceDescriptor())
	toolRegistry.RegisterExecutor(skills.NewToolExecutor(executor))

	// Preload skills marked with preload: true
	for _, sk := range reg.PreloadedSkills() {
		_, _, _ = executor.Activate(sk.Name)
	}

	logger.Info("Discovered skills", "count", len(reg.List()))
	return nil
}

// a2aInitTimeout is the timeout for discovering and registering A2A agent tools.
const a2aInitTimeout = 30 * time.Second

// a2aRunningServer tracks a started mock A2A server and its URL.
type a2aRunningServer struct {
	server *a2amock.A2AServer
	url    string
}

// startAndRegisterA2AAgents starts mock A2A servers from config, discovers their
// tools, and registers them in the tool registry. Returns a cleanup function that
// shuts down all mock servers.
func startAndRegisterA2AAgents(cfg *config.Config, toolRegistry *tools.Registry) (func(), error) {
	if len(cfg.A2AAgents) == 0 {
		return nil, nil
	}

	servers, cleanup, err := startA2AServers(cfg.A2AAgents)
	if err != nil {
		return nil, err
	}

	toolRegistry.RegisterExecutor(a2a.NewExecutor())

	totalTools, err := discoverAndRegisterA2ATools(servers, toolRegistry)
	if err != nil {
		cleanup()
		return nil, err
	}

	if totalTools > 0 {
		logger.Info("Discovered A2A tools", "tools", totalTools, "agents", len(servers))
	}

	return cleanup, nil
}

// startA2AServers starts mock A2A servers for each agent config.
func startA2AServers(agents []config.A2AAgentConfig) ([]a2aRunningServer, func(), error) {
	var servers []a2aRunningServer
	cleanup := func() {
		for _, s := range servers {
			s.server.Close()
		}
	}

	for _, agentCfg := range agents {
		mockCfg := buildA2AMockConfig(&agentCfg)
		opts := a2amock.OptionsFromConfig(mockCfg)
		server := a2amock.NewA2AServer(&mockCfg.Card, opts...)
		url, err := server.Start()
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to start A2A mock server %q: %w", agentCfg.Name, err)
		}
		servers = append(servers, a2aRunningServer{server: server, url: url})
	}

	return servers, cleanup, nil
}

// buildA2AMockConfig converts an A2AAgentConfig to a mock.AgentConfig.
func buildA2AMockConfig(agentCfg *config.A2AAgentConfig) *a2amock.AgentConfig {
	card := a2a.AgentCard{
		Name:               agentCfg.Card.Name,
		Description:        agentCfg.Card.Description,
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
	}
	for _, skill := range agentCfg.Card.Skills {
		card.Skills = append(card.Skills, a2a.AgentSkill{
			ID:          skill.ID,
			Name:        skill.Name,
			Description: skill.Description,
			Tags:        skill.Tags,
		})
	}

	mockCfg := &a2amock.AgentConfig{Name: agentCfg.Name, Card: card}
	for _, rule := range agentCfg.Responses {
		mockRule := a2amock.RuleConfig{Skill: rule.Skill, Error: rule.Error}
		if rule.Match != nil {
			mockRule.Match = &a2amock.MatchConfig{
				Contains: rule.Match.Contains,
				Regex:    rule.Match.Regex,
			}
		}
		if rule.Response != nil {
			mockRule.Response = &a2amock.ResponseConfig{}
			for _, part := range rule.Response.Parts {
				mockRule.Response.Parts = append(mockRule.Response.Parts, a2amock.PartConfig{
					Text: part.Text,
				})
			}
		}
		mockCfg.Responses = append(mockCfg.Responses, mockRule)
	}
	return mockCfg
}

// discoverAndRegisterA2ATools discovers tools from running A2A servers and registers them.
func discoverAndRegisterA2ATools(servers []a2aRunningServer, toolRegistry *tools.Registry) (int, error) {
	initCtx, cancel := context.WithTimeout(context.Background(), a2aInitTimeout)
	defer cancel()

	totalTools := 0
	for _, s := range servers {
		client := a2a.NewClient(s.url)
		bridge := a2a.NewToolBridge(client)
		descriptors, err := bridge.RegisterAgent(initCtx)
		if err != nil {
			return 0, fmt.Errorf("failed to register A2A agent tools from %s: %w", s.url, err)
		}
		for _, td := range descriptors {
			if err := toolRegistry.Register(td); err != nil {
				return 0, fmt.Errorf("failed to register A2A tool %s: %w", td.Name, err)
			}
			totalTools++
		}
	}
	return totalTools, nil
}
