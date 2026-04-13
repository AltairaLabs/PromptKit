package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	rtprompt "github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/skills"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/telemetry"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/internal/provider"
	"github.com/AltairaLabs/PromptKit/sdk/session"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
)

// loggerOnce guards logger.SetLogger to avoid data races when multiple
// goroutines call Open() concurrently with WithLogger.
var loggerOnce sync.Once

// setLoggerOnce sets the global logger exactly once. Subsequent calls with
// different loggers are silently ignored because the runtime logger is global
// and not safe to swap after initialization. If multiple conversations need
// independent loggers, configure logging at the application level instead.
func setLoggerOnce(l *slog.Logger) {
	loggerOnce.Do(func() {
		logger.SetLogger(l)
	})
}

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
//   - Remote URL pointing at a .pack.json (future)
//
// The promptName must match a prompt ID defined in the pack's "prompts" section.
func Open(packPath, promptName string, opts ...Option) (*Conversation, error) {
	conv, _, err := initConversation(packPath, promptName, opts)
	if err != nil {
		logger.Error("conversation open failed",
			"pack", packPath, "prompt", promptName, "error", err)
		return nil, err
	}

	// Initialize internal memory store for conversation history
	// This is used by StateStoreLoad/Save middleware in the pipeline
	if err := initInternalStateStore(conv, conv.config); err != nil {
		return nil, err
	}

	// Finalize conversation (eval middleware, MCP, session start hooks)
	if err := finalizeConversation(conv, conv.config); err != nil {
		return nil, err
	}

	// Register with shutdown manager if configured
	if conv.config.shutdownManager != nil {
		if err := conv.config.shutdownManager.Register(conv.ID(), conv); err != nil {
			_ = conv.Close()
			return nil, fmt.Errorf("failed to register with shutdown manager: %w", err)
		}
	}

	logger.Info("conversation opened",
		"id", conv.ID(), "pack", packPath, "prompt", promptName)
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
	conv, prov, err := initConversation(packPath, promptName, opts)
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

	// Initialize duplex session
	if err := initDuplexSession(conv, conv.config, streamProvider); err != nil {
		return nil, err
	}

	// Finalize conversation (eval middleware, MCP, session start hooks)
	if err := finalizeConversation(conv, conv.config); err != nil {
		return nil, err
	}

	// Register with shutdown manager if configured
	if conv.config.shutdownManager != nil {
		if err := conv.config.shutdownManager.Register(conv.ID(), conv); err != nil {
			_ = conv.Close()
			return nil, fmt.Errorf("failed to register with shutdown manager: %w", err)
		}
	}

	return conv, nil
}

// initConversation performs the common initialization shared by Open and OpenDuplex.
// It applies options, loads the pack, resolves the provider, creates the Conversation
// struct, initializes capabilities, the event bus, and the hook registry.
// Returns the partially initialized conversation and the resolved provider.
func initConversation(
	packPath, promptName string, opts []Option,
) (*Conversation, providers.Provider, error) {
	// Apply options to build configuration
	cfg, err := applyOptions(promptName, opts)
	if err != nil {
		return nil, nil, err
	}

	// Set custom logger before any logging occurs — only once to avoid
	// data races when multiple goroutines call Open() concurrently.
	if cfg.logger != nil {
		setLoggerOnce(cfg.logger)
	}

	// Load and validate pack
	p, prompt, err := loadAndValidatePack(packPath, promptName, cfg)
	if err != nil {
		return nil, nil, err
	}

	// Resolve provider and store in config
	prov, err := resolveProvider(cfg)
	if err != nil {
		return nil, nil, err
	}

	// Use caller-provided tool registry or create a new one from the pack.
	toolReg := cfg.toolRegistry
	if toolReg == nil {
		toolReg = tools.NewRegistryWithRepository(p.ToToolRepository())
	}

	// Create conversation
	conv := &Conversation{
		pack:           p,
		prompt:         prompt,
		promptName:     promptName,
		promptRegistry: p.ToPromptRegistry(), // Create registry for PromptAssemblyMiddleware
		toolRegistry:   toolReg,
		config:         cfg,
		handlers:       make(map[string]ToolHandler),
		ctxHandlers:    make(map[string]ToolHandlerCtx),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewPendingStore(),
		resolvedStore:  sdktools.NewResolvedStore(),
	}

	// Apply default variables from prompt BEFORE initializing session
	// This ensures defaults are available when creating the session
	applyDefaultVariables(conv, prompt)

	// Auto-convert pack validators to provider hooks (before building hook registry)
	convertPackValidatorsToHooks(prompt, cfg)

	// Initialize capabilities (auto-inferred + explicit)
	allCaps := mergeCapabilities(cfg.capabilities, inferCapabilities(p))
	allCaps = ensureA2ACapability(allCaps, cfg)
	allCaps = ensureSkillsCapability(allCaps, cfg)
	wireA2AConfig(allCaps, cfg)
	wireSkillsConfig(allCaps, cfg)
	capCtx := newCapabilityContext(p, promptName, cfg)
	for _, cap := range allCaps {
		logger.Info("initializing capability", "capability", cap.Name())
		if err := cap.Init(capCtx); err != nil {
			return nil, nil, fmt.Errorf("capability %q init failed: %w", cap.Name(), err)
		}
	}
	if len(allCaps) > 0 {
		names := make([]string, len(allCaps))
		for i, c := range allCaps {
			names[i] = c.Name()
		}
		logger.Info("capabilities initialized", "capabilities", names, "count", len(allCaps))
	}
	conv.capabilities = allCaps

	// Initialize event bus BEFORE building pipeline so it can be wired up
	initEventBus(cfg)

	// Build hook registry BEFORE building pipeline so it can be wired into the provider stage
	conv.hookRegistry = cfg.buildHookRegistry()
	conv.sessionHooks = newSessionHookDispatcher(conv.hookRegistry, conv.sessionInfo)

	return conv, prov, nil
}

// finalizeConversation completes the conversation setup after mode-specific
// initialization (unary or duplex). It wires eval middleware, MCP servers,
// and dispatches session start hooks.
func finalizeConversation(conv *Conversation, cfg *config) error {
	// Initialize eval middleware
	conv.evalMW = newEvalMiddleware(conv)

	// Initialize MCP registry if configured
	if err := initMCPRegistry(conv, cfg); err != nil {
		return err
	}

	// Dispatch session start hooks
	conv.sessionHooks.SessionStart(context.Background())

	return nil
}

// applyOptions applies the configuration options and validates cross-option constraints.
func applyOptions(promptName string, opts []Option) (*config, error) {
	cfg := &config{promptName: promptName}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	// Validate RAG context option dependencies
	if cfg.contextWindow > 0 && cfg.stateStore == nil {
		return nil, fmt.Errorf("WithContextWindow requires WithStateStore")
	}
	if cfg.retrievalProvider != nil && cfg.contextWindow <= 0 {
		return nil, fmt.Errorf("WithContextRetrieval requires WithContextWindow")
	}
	if cfg.summarizeProvider != nil && cfg.contextWindow <= 0 {
		return nil, fmt.Errorf("WithAutoSummarize requires WithContextWindow")
	}

	return cfg, nil
}

// packCache caches loaded+validated packs by absolute path. The *pack.Pack
// returned by pack.Load is immutable after construction, so sharing across
// goroutines is safe. This eliminates per-request file I/O, JSON parsing,
// and JSON schema compilation — the #1 CPU bottleneck under high concurrency.
var packCache sync.Map // map[string]*pack.Pack

// loadAndValidatePack loads the pack and validates the prompt exists.
// Packs are cached by absolute path so repeated Open() calls for the
// same pack file skip disk I/O and schema validation.
func loadAndValidatePack(packPath, promptName string, cfg *config) (*pack.Pack, *pack.Prompt, error) {
	absPath, err := resolvePackPath(packPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve pack path: %w", err)
	}

	// Check cache first.
	if cached, ok := packCache.Load(absPath); ok {
		p := cached.(*pack.Pack)
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

	// Cache miss — load from disk and validate.
	loadOpts := pack.LoadOptions{
		SkipSchemaValidation: cfg.skipSchemaValidation,
	}

	p, err := pack.Load(absPath, loadOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load pack: %w", err)
	}

	// Store in cache. If another goroutine raced us, that's fine —
	// both loaded the same file and the result is identical.
	packCache.Store(absPath, p)

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

	// Resolve credential config into an API key if set.
	// WithCredential takes precedence over WithAPIKey.
	if cfg.credential != nil {
		key, err := resolveCredentialAPIKey(cfg.credential)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve credential: %w", err)
		}
		if key != "" {
			cfg.apiKey = key
		}
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

// resolveCredentialAPIKey resolves a credentialConfig into an API key string.
// Priority: direct API key > environment variable > file.
func resolveCredentialAPIKey(cc *credentialConfig) (string, error) {
	// 1. Direct API key takes highest priority
	if cc.apiKey != "" {
		return cc.apiKey, nil
	}

	// 2. Environment variable
	if cc.credentialEnv != "" {
		if val := os.Getenv(cc.credentialEnv); val != "" {
			return val, nil
		}
		return "", fmt.Errorf("credential environment variable %q is not set or empty", cc.credentialEnv)
	}

	// 3. Credential file
	if cc.credentialFile != "" {
		data, err := os.ReadFile(cc.credentialFile)
		if err != nil {
			return "", fmt.Errorf("failed to read credential file %q: %w", cc.credentialFile, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	return "", nil
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
		return resolveBedrockCredential(ctx, pc)
	case platformTypeVertex:
		return resolveVertexCredential(ctx, pc)
	case platformTypeAzure:
		return resolveAzureCredential(ctx, pc)
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
		// Azure always requires an explicit endpoint via WithPlatformEndpoint.
		// When none is set, pc.endpoint is empty and provider creation will fail
		// with a clear error from the Azure credential/provider layer.
		return pc.endpoint
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
// If an event store is configured, it is subscribed to the bus for persistence.
// If a TracerProvider is configured, an OTel event listener is wired in.
func initEventBus(cfg *config) {
	if cfg.eventBus == nil {
		cfg.eventBus = events.NewEventBus()
	}
	// Subscribe event store for persistence if configured.
	if cfg.eventStore != nil {
		cfg.eventBus.SubscribeAll(cfg.eventStore.OnEvent)
	}
	// Wire OTel event listener if a TracerProvider is configured.
	if cfg.tracerProvider != nil {
		tracer := telemetry.Tracer(cfg.tracerProvider)
		listener := telemetry.NewOTelEventListener(tracer)
		cfg.eventBus.SubscribeAll(listener.OnEvent)
		cfg.otelListener = listener
	}
	// Wire unified metrics if a Collector is configured.
	if cfg.metricsCollector != nil {
		metricCtx := cfg.metricsCollector.Bind(cfg.metricsInstanceLabels)
		cfg.eventBus.SubscribeAll(metricCtx.OnEvent)
		cfg.metricContext = metricCtx
	}
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
		UserID:         cfg.userID,
		StateStore:     store,
		Pipeline:       pipeline,
		Metadata:       cfg.sessionMetadata,
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

	// Build HITL checker closure — reads asyncHandlers dynamically under lock
	// so handlers registered after OpenDuplex() are still consulted.
	asyncChecker := session.AsyncToolChecker(func(callID, name string, args map[string]any) *session.AsyncToolCheckResult {
		conv.asyncHandlersMu.RLock()
		checkFunc, isAsync := conv.asyncHandlers[name]
		conv.asyncHandlersMu.RUnlock()

		if !isAsync {
			return nil // Not an async tool — fall through to registry
		}

		checkResult := checkFunc(args)
		if !checkResult.IsPending() {
			// Check passed — execute the handler directly (it may not be in the registry
			// since OnToolAsync is often called after OpenDuplex builds the pipeline).
			conv.handlersMu.RLock()
			handler := conv.handlers[name]
			conv.handlersMu.RUnlock()
			if handler == nil {
				return nil // No handler — fall through to registry
			}
			result, execErr := handler(args)
			if execErr != nil {
				return &session.AsyncToolCheckResult{Handled: true, HandlerError: execErr}
			}
			resultJSON, marshalErr := json.Marshal(result)
			if marshalErr != nil {
				return &session.AsyncToolCheckResult{Handled: true, HandlerError: marshalErr}
			}
			return &session.AsyncToolCheckResult{Handled: true, HandlerResult: resultJSON}
		}

		// Tool requires human approval — create pending call using the provider's
		// callID so ResolveTool maps directly.
		pending := &sdktools.PendingToolCall{
			ID:        callID,
			Name:      name,
			Arguments: args,
			Reason:    checkResult.Reason,
			Message:   checkResult.Message,
		}

		// Set the handler for execution on resolve
		conv.handlersMu.RLock()
		handler := conv.handlers[name]
		conv.handlersMu.RUnlock()
		if handler != nil {
			pending.SetHandler(handler)
		}

		if err := conv.pendingStore.Add(pending); err != nil {
			return nil
		}

		return &session.AsyncToolCheckResult{
			ShouldWait: true,
			PendingInfo: &tools.PendingToolInfo{
				Reason:   checkResult.Reason,
				Message:  checkResult.Message,
				ToolName: name,
			},
		}
	})

	// Create duplex session with builder
	duplexSession, err := session.NewDuplexSession(context.Background(), &session.DuplexSessionConfig{
		ConversationID:   conversationID,
		UserID:           cfg.userID,
		StateStore:       store,
		PipelineBuilder:  pipelineBuilder,
		Provider:         streamProvider,
		Config:           streamConfig, // nil for VAD mode, set for ASM mode
		ToolRegistry:     conv.toolRegistry,
		AsyncToolChecker: asyncChecker,
		Metadata:         cfg.sessionMetadata,
		Variables:        initialVars,
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
	// Options are applied twice intentionally: first here to extract the state
	// store for loading, then again inside Open() for full initialization.
	// This is safe because options are idempotent config setters.
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
	opts = append(opts, WithConversationID(conversationID))
	conv, err := Open(packPath, promptName, opts...)
	if err != nil {
		return nil, err
	}

	// The session now has the correct conversation ID from WithConversationID option

	return conv, nil
}

// ensureA2ACapability adds an A2ACapability if the config has a bridge or
// builder-based agents but no A2ACapability was already inferred or explicit.
func ensureA2ACapability(caps []Capability, cfg *config) []Capability {
	if cfg.a2aBridge == nil && len(cfg.a2aAgents) == 0 {
		return caps
	}
	for _, cap := range caps {
		if cap.Name() == nsA2A {
			return caps
		}
	}
	return append(caps, NewA2ACapability())
}

// ensureSkillsCapability adds a SkillsCapability if config has skill sources
// but no SkillsCapability was already inferred or explicit.
func ensureSkillsCapability(caps []Capability, cfg *config) []Capability {
	if len(cfg.skillsDirs) == 0 && len(cfg.skillSources) == 0 {
		return caps
	}
	for _, cap := range caps {
		if cap.Name() == capabilityNameSkills {
			return caps
		}
	}
	sources := make([]skills.SkillSource, 0, len(cfg.skillsDirs)+len(cfg.skillSources))
	for _, dir := range cfg.skillsDirs {
		sources = append(sources, skills.SkillSource{Dir: dir})
	}
	sources = append(sources, cfg.skillSources...)
	return append(caps, NewSkillsCapability(sources))
}

// wireSkillsConfig threads config values into any SkillsCapability before Init.
func wireSkillsConfig(caps []Capability, cfg *config) {
	for _, cap := range caps {
		sc, ok := cap.(*SkillsCapability)
		if !ok {
			continue
		}
		if cfg.skillSelector != nil && sc.selector == nil {
			sc.selector = cfg.skillSelector
		}
		if cfg.maxActiveSkills > 0 && sc.maxActive == 0 {
			sc.maxActive = cfg.maxActiveSkills
		}
	}
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
		// Wire builder-based A2A agents as bridges.
		if len(cfg.a2aAgents) > 0 && len(a2aCap.agentBridges) == 0 {
			for _, agentCfg := range cfg.a2aAgents {
				var opts []a2a.ClientOption
				if agentCfg.config.Auth != nil {
					token := agentCfg.config.Auth.Token
					if token == "" && agentCfg.config.Auth.TokenEnv != "" {
						token = os.Getenv(agentCfg.config.Auth.TokenEnv)
					}
					if token != "" {
						opts = append(opts, a2a.WithAuth(agentCfg.config.Auth.Scheme, token))
					}
				}
				headers := resolveA2AHeaders(agentCfg.config)
				if len(headers) > 0 {
					opts = append(opts, a2a.WithHeaders(headers))
				}
				client := a2a.NewClient(agentCfg.url, opts...)
				bridge := a2a.NewToolBridgeWithConfig(client, agentCfg.config)
				a2aCap.agentBridges = append(a2aCap.agentBridges, bridge)
			}
		}
	}
}

// resolveA2AHeaders merges static headers with env-based headers for A2A config.
func resolveA2AHeaders(cfg *tools.A2AConfig) map[string]string {
	result := make(map[string]string, len(cfg.Headers)+len(cfg.HeadersFromEnv))
	for k, v := range cfg.Headers {
		result[k] = v
	}
	for _, spec := range cfg.HeadersFromEnv {
		if key, envVar, ok := strings.Cut(spec, "="); ok {
			if val := os.Getenv(envVar); val != "" {
				result[key] = val
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
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

// convertPackValidatorsToHooks auto-converts pack prompt validators into
// provider hooks, prepending them before any user-registered hooks.
// This enables pack-defined guardrails (e.g., banned_words, max_length) to
// run as enforcement hooks in the SDK pipeline.
//
// Validators are skipped (with a warning logged) when:
//   - Enabled is false.
//   - The validator type is not registered in the runtime registry.
//   - The validator's params are unusable by its handler (missing required keys, etc.).
func convertPackValidatorsToHooks(prompt *pack.Prompt, cfg *config) {
	if len(prompt.Validators) == 0 {
		return
	}
	var packHooks []hooks.ProviderHook
	for _, v := range prompt.Validators {
		if !v.Enabled {
			logger.Debug("Skipping disabled pack validator", "type", v.Type)
			continue
		}

		var opts []guardrails.GuardrailOption
		// Spec default for fail_on_violation is false (monitor-only). We enforce
		// only when the pack explicitly sets fail_on_violation: true.
		if v.FailOnViolation == nil || !*v.FailOnViolation {
			opts = append(opts, guardrails.WithMonitorOnly())
		}

		hook, err := guardrails.NewGuardrailHook(v.Type, v.Params, opts...)
		if err != nil {
			logger.Warn("Skipping unusable pack validator",
				"type", v.Type, "error", err)
			continue
		}
		packHooks = append(packHooks, hook)
	}
	// Prepend pack validators before user-registered hooks
	if len(packHooks) > 0 {
		cfg.providerHooks = append(packHooks, cfg.providerHooks...)
	}
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
