package sdk

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	rtprompt "github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/internal/provider"
	"github.com/AltairaLabs/PromptKit/sdk/session"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
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
		resolvedStore:  sdktools.NewResolvedStore(),
	}

	// Apply default variables from prompt BEFORE initializing session
	// This ensures defaults are available when creating the session
	applyDefaultVariables(conv, prompt)

	// Initialize capabilities (auto-inferred + explicit)
	allCaps := mergeCapabilities(cfg.capabilities, inferCapabilities(p))
	allCaps = ensureA2ACapability(allCaps, cfg)
	wireA2AConfig(allCaps, cfg)
	for _, cap := range allCaps {
		if err := cap.Init(CapabilityContext{Pack: p, PromptName: promptName}); err != nil {
			return nil, fmt.Errorf("capability %q init failed: %w", cap.Name(), err)
		}
	}
	conv.capabilities = allCaps

	// Initialize event bus BEFORE building pipeline so it can be wired up
	initEventBus(cfg)

	// Initialize internal memory store for conversation history
	// This is used by StateStoreLoad/Save middleware in the pipeline
	if err := initInternalStateStore(conv, cfg); err != nil {
		return nil, err
	}

	// Initialize eval middleware
	conv.evalMW = newEvalMiddleware(conv)

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
		resolvedStore:  sdktools.NewResolvedStore(),
	}

	// Apply default variables from prompt BEFORE initializing session
	applyDefaultVariables(conv, prompt)

	// Initialize capabilities (auto-inferred + explicit)
	allCaps := mergeCapabilities(cfg.capabilities, inferCapabilities(p))
	allCaps = ensureA2ACapability(allCaps, cfg)
	wireA2AConfig(allCaps, cfg)
	for _, cap := range allCaps {
		if err := cap.Init(CapabilityContext{Pack: p, PromptName: promptName}); err != nil {
			return nil, fmt.Errorf("capability %q init failed: %w", cap.Name(), err)
		}
	}
	conv.capabilities = allCaps

	// Initialize event bus BEFORE building pipeline so it can be wired up
	initEventBus(cfg)

	// Initialize duplex session
	if err := initDuplexSession(conv, cfg, streamProvider); err != nil {
		return nil, err
	}

	// Initialize eval middleware
	conv.evalMW = newEvalMiddleware(conv)

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

	// Platform-based provider creation (Bedrock, Vertex, Azure)
	if cfg.platform != nil {
		p, err := resolvePlatformProvider(cfg)
		if err != nil {
			return nil, err
		}
		cfg.provider = p
		return p, nil
	}

	detected, err := provider.Detect(cfg.apiKey, cfg.model)
	if err != nil {
		return nil, fmt.Errorf("failed to detect provider: %w", err)
	}
	cfg.provider = detected
	return detected, nil
}

// Platform type constants.
const (
	platformTypeBedrock = "bedrock"
	platformTypeVertex  = "vertex"
	platformTypeAzure   = "azure"
)

// resolvePlatformProvider creates a provider for a cloud platform (Bedrock, Vertex, Azure).
// The platform determines the credential; the provider type and model are specified
// explicitly since platforms host models from multiple providers.
func resolvePlatformProvider(cfg *config) (providers.Provider, error) {
	pc := cfg.platform
	ctx := context.Background()

	// Resolve platform credential
	cred, err := resolvePlatformCredential(ctx, pc)
	if err != nil {
		return nil, err
	}

	model := pc.model
	provType := pc.providerType

	// Map model through Bedrock model ID mapping if applicable
	if pc.platformType == platformTypeBedrock {
		if bedrockID, ok := credentials.BedrockModelMapping[model]; ok {
			model = bedrockID
		}
	}

	// Determine base URL from platform + provider combination
	baseURL := platformBaseURL(pc, provType)

	spec := providers.ProviderSpec{
		ID:         provType,
		Type:       provType,
		Model:      model,
		BaseURL:    baseURL,
		Credential: cred,
		Platform:   pc.platformType,
		PlatformConfig: &providers.PlatformConfig{
			Type:     pc.platformType,
			Region:   pc.region,
			Project:  pc.project,
			Endpoint: pc.endpoint,
		},
		Defaults: providers.ProviderDefaults{
			Temperature: defaultTemperature,
			TopP:        1.0,
			MaxTokens:   defaultMaxTokens,
		},
	}

	return providers.CreateProviderFromSpec(spec)
}

// resolvePlatformCredential creates the appropriate cloud credential for a platform.
func resolvePlatformCredential(ctx context.Context, pc *platformConfig) (providers.Credential, error) {
	switch pc.platformType {
	case platformTypeBedrock:
		cred, err := credentials.NewAWSCredential(ctx, pc.region)
		if err != nil {
			return nil, fmt.Errorf("bedrock credentials: %w", err)
		}
		return cred, nil
	case platformTypeVertex:
		cred, err := credentials.NewGCPCredential(ctx, pc.project, pc.region)
		if err != nil {
			return nil, fmt.Errorf("vertex credentials: %w", err)
		}
		return cred, nil
	case platformTypeAzure:
		cred, err := credentials.NewAzureCredential(ctx, pc.endpoint)
		if err != nil {
			return nil, fmt.Errorf("azure credentials: %w", err)
		}
		return cred, nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s", pc.platformType)
	}
}

// platformBaseURL returns the base URL for a platform + provider combination.
// The endpoint can be overridden via WithPlatformEndpoint.
func platformBaseURL(pc *platformConfig, provType string) string {
	if pc.endpoint != "" {
		return pc.endpoint
	}

	switch pc.platformType {
	case platformTypeBedrock:
		return credentials.BedrockEndpoint(pc.region)
	case platformTypeVertex:
		return vertexBaseURL(pc, provType)
	case platformTypeAzure:
		return pc.endpoint // Azure requires an explicit endpoint
	default:
		return ""
	}
}

// vertexBaseURL returns the Vertex AI base URL, which varies by provider.
func vertexBaseURL(pc *platformConfig, provType string) string {
	switch provType {
	case "claude":
		return credentials.VertexEndpoint(pc.project, pc.region)
	case "gemini":
		return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models",
			pc.region, pc.project, pc.region)
	default:
		// Generic Vertex endpoint for other providers
		return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s",
			pc.region, pc.project, pc.region)
	}
}

// initEventBus initializes the conversation's event bus.
// If an event store is configured, it is attached to the bus for persistence.
func initEventBus(cfg *config) {
	if cfg.eventBus == nil {
		cfg.eventBus = events.NewEventBus()
	}
	// Attach event store if configured (and not already attached)
	if cfg.eventStore != nil && cfg.eventBus.Store() == nil {
		cfg.eventBus.WithStore(cfg.eventStore)
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
	// No streaming provider/config for unary mode
	pipeline, err := conv.buildPipelineWithParams(store, conversationID, nil, nil)
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

// Note: loadSystemInstruction was removed - system prompt now comes from pipeline
// PromptAssemblyStage adds system_prompt to element metadata, and DuplexProviderStage
// reads it when creating the session lazily

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
	// Note: DuplexProviderStage creates session lazily using system_prompt from element metadata
	pipelineBuilder := func(
		ctx context.Context,
		provider providers.Provider,
		streamProvider providers.StreamInputSupport,
		streamConfig *providers.StreamingInputConfig,
		convID string,
		stateStore statestore.Store,
	) (*stage.StreamPipeline, error) {
		// Build stage pipeline directly (not wrapped) for duplex sessions
		return conv.buildStreamPipelineWithParams(stateStore, convID, streamProvider, streamConfig)
	}

	// Mode is determined by cfg.streamingConfig:
	// - If set: ASM mode (creates provider session, continuous streaming)
	// - If nil: VAD mode (no provider session, turn-based streaming)
	// Note: System instruction is NOT pre-loaded here anymore - it comes from pipeline
	// PromptAssemblyStage adds system_prompt to element metadata
	streamConfig := cfg.streamingConfig

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

// ensureA2ACapability adds an A2ACapability if the config has a bridge
// but no A2ACapability was already inferred or explicit.
func ensureA2ACapability(caps []Capability, cfg *config) []Capability {
	if cfg.a2aBridge == nil {
		return caps
	}
	for _, cap := range caps {
		if cap.Name() == "a2a" {
			return caps
		}
	}
	return append(caps, NewA2ACapability())
}

// wireA2AConfig threads config values into any A2ACapability before Init.
func wireA2AConfig(caps []Capability, cfg *config) {
	for _, cap := range caps {
		a2aCap, ok := cap.(*A2ACapability)
		if !ok {
			continue
		}
		if cfg.agentEndpointResolver != nil && a2aCap.endpointResolver == nil {
			a2aCap.endpointResolver = cfg.agentEndpointResolver
		}
		if cfg.localAgentExecutor != nil && a2aCap.localExecutor == nil {
			a2aCap.localExecutor = cfg.localAgentExecutor
		}
		if cfg.a2aBridge != nil && a2aCap.bridge == nil {
			a2aCap.bridge = cfg.a2aBridge
		}
	}
}

// packToRuntimePack converts the SDK internal pack types to a runtime
// prompt.Pack. Only the fields needed by agentcard.GenerateAgentCards are
// populated (Agents and Prompts).
func packToRuntimePack(p *pack.Pack) *rtprompt.Pack {
	rp := &rtprompt.Pack{
		ID:      p.ID,
		Version: p.Version,
	}

	// Convert prompts
	if len(p.Prompts) > 0 {
		rp.Prompts = make(map[string]*rtprompt.PackPrompt, len(p.Prompts))
		for name, pr := range p.Prompts {
			rp.Prompts[name] = &rtprompt.PackPrompt{
				Name:        pr.Name,
				Description: pr.Description,
			}
		}
	}

	// Convert agents
	if p.Agents != nil {
		rp.Agents = &rtprompt.AgentsConfig{
			Entry:   p.Agents.Entry,
			Members: make(map[string]*rtprompt.AgentDef, len(p.Agents.Members)),
		}
		for name, def := range p.Agents.Members {
			rp.Agents.Members[name] = &rtprompt.AgentDef{
				Description: def.Description,
				Tags:        def.Tags,
				InputModes:  def.InputModes,
				OutputModes: def.OutputModes,
			}
		}
	}

	return rp
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
