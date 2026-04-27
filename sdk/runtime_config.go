package sdk

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	pkgconfig "github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/sandbox"
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	// Side-effect imports register embedding-provider factories so
	// CreateEmbeddingProviderFromSpec can resolve declarative entries.
	// Chat-provider factories register through other SDK paths.
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/ollama"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	_ "github.com/AltairaLabs/PromptKit/runtime/providers/voyageai"
	"github.com/AltairaLabs/PromptKit/runtime/selection"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/stt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// resolveSandboxes builds a map from declared sandbox names to ready
// Sandbox instances by looking up each mode in the process-wide factory
// registry. Factories are expected to have been registered via
// sandbox.RegisterFactory, direct's init, or sdk.WithSandboxFactory
// before RuntimeConfig is applied.
func resolveSandboxes(specs map[string]*pkgconfig.SandboxConfig) (map[string]sandbox.Sandbox, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make(map[string]sandbox.Sandbox, len(specs))
	for name, sb := range specs {
		if sb == nil {
			continue
		}
		factory, err := sandbox.LookupFactory(sb.Mode)
		if err != nil {
			return nil, fmt.Errorf("sandbox %q: %w", name, err)
		}
		inst, err := factory(name, sb.Config)
		if err != nil {
			return nil, fmt.Errorf("building sandbox %q: %w", name, err)
		}
		out[name] = inst
	}
	return out, nil
}

// applyExecHooks creates exec hook adapters from RuntimeConfig hook bindings
// and appends them to the appropriate hook slices in the SDK config.
// Resolved sandboxes (from spec.Sandboxes) are looked up by name when a
// binding sets its "sandbox:" field; unknown names are rejected.
func applyExecHooks(c *config, hookBindings map[string]*pkgconfig.ExecHook, resolved map[string]sandbox.Sandbox) error {
	for name, binding := range hookBindings {
		if binding == nil {
			continue
		}
		var sb sandbox.Sandbox
		if binding.Sandbox != "" {
			var ok bool
			sb, ok = resolved[binding.Sandbox]
			if !ok {
				return fmt.Errorf("hook %q references undeclared sandbox %q", name, binding.Sandbox)
			}
		}
		cfg := &hooks.ExecHookConfig{
			Name:      name,
			Command:   binding.Command,
			Args:      binding.Args,
			Env:       binding.Env,
			TimeoutMs: binding.TimeoutMs,
			Phases:    binding.Phases,
			Mode:      binding.Mode,
			Sandbox:   sb,
		}
		switch binding.Hook {
		case "provider":
			c.providerHooks = append(c.providerHooks, hooks.NewExecProviderHook(cfg))
		case "tool":
			c.toolHooks = append(c.toolHooks, hooks.NewExecToolHook(cfg))
		case "session":
			c.sessionHooks = append(c.sessionHooks, hooks.NewExecSessionHook(cfg))
		case "eval":
			// Eval hooks are observational and have no phases or modes,
			// so we only forward the fields that ExecEvalHook uses.
			c.evalHooks = append(c.evalHooks, evals.NewExecEvalHook(&evals.ExecEvalHookConfig{
				Name:      name,
				Command:   binding.Command,
				Args:      binding.Args,
				Env:       binding.Env,
				TimeoutMs: binding.TimeoutMs,
				Sandbox:   sb,
			}))
		}
	}
	return nil
}

// WithRuntimeConfig loads a RuntimeConfig YAML file and applies its settings
// to the SDK conversation. This provides a declarative alternative to
// programmatic configuration via individual With* options.
//
// RuntimeConfig sections are applied in order: providers, MCP servers,
// state store, logging, tools. Programmatic options applied after
// WithRuntimeConfig take precedence.
//
// Example:
//
//	conv, err := sdk.Open("./agent.pack.json", "chat",
//	    sdk.WithRuntimeConfig("./runtime.yaml"),
//	)
func WithRuntimeConfig(path string) Option {
	return func(c *config) error {
		rc, err := pkgconfig.LoadRuntimeConfig(path)
		if err != nil {
			return fmt.Errorf("loading runtime config: %w", err)
		}
		return applyRuntimeConfig(c, &rc.Spec)
	}
}

// applyRuntimeConfig applies a RuntimeConfigSpec to the SDK config struct.
//
// # Security: Trust Boundary
//
// Runtime config files are a trust boundary. They may specify MCP server
// commands, exec tool commands, and hook commands that are executed as
// subprocesses without sandboxing or validation. Runtime config files MUST
// come from trusted sources. Untrusted config files should never be loaded
// without review, as they can execute arbitrary commands with the privileges
// of the host process.
func applyRuntimeConfig(c *config, spec *pkgconfig.RuntimeConfigSpec) error {
	// Apply provider (use first provider if configured and no provider already set)
	if len(spec.Providers) > 0 && c.getAgentProvider() == nil {
		prov, err := createProviderFromConfig(&spec.Providers[0])
		if err != nil {
			return fmt.Errorf("creating provider from runtime config: %w", err)
		}
		registerAgentProvider(c, prov)
	}

	// Apply embedding providers (declarative). The first declared
	// entry doubles as the default RAG retrievalProvider when one
	// isn't already set programmatically.
	if err := applyEmbeddingProviders(c, spec.EmbeddingProviders); err != nil {
		return fmt.Errorf("applying embedding providers from runtime config: %w", err)
	}

	// Apply TTS / STT providers (declarative). First declared entry
	// becomes the default ttsService / sttService when one isn't set
	// programmatically via WithTTS / WithVADMode.
	if err := applyTTSProviders(c, spec.TTSProviders); err != nil {
		return fmt.Errorf("applying TTS providers from runtime config: %w", err)
	}
	if err := applySTTProviders(c, spec.STTProviders); err != nil {
		return fmt.Errorf("applying STT providers from runtime config: %w", err)
	}

	// Apply MCP servers
	for _, mcpCfg := range spec.MCPServers {
		serverCfg := mcp.ServerConfig{
			Name:       mcpCfg.Name,
			Command:    mcpCfg.Command,
			Args:       mcpCfg.Args,
			Env:        mcpCfg.Env,
			WorkingDir: mcpCfg.WorkingDir,
			TimeoutMs:  mcpCfg.TimeoutMs,
		}
		if mcpCfg.ToolFilter != nil {
			serverCfg.ToolFilter = &mcp.ToolFilter{
				Allowlist: mcpCfg.ToolFilter.Allowlist,
				Blocklist: mcpCfg.ToolFilter.Blocklist,
			}
		}
		c.mcpServers = append(c.mcpServers, serverCfg)
	}

	// Apply state store (only if not already set)
	if spec.StateStore != nil && c.stateStore == nil {
		store, err := createStateStoreFromConfig(spec.StateStore)
		if err != nil {
			return fmt.Errorf("creating state store from runtime config: %w", err)
		}
		c.stateStore = store
	}

	// Apply logging (only if not already set)
	if spec.Logging != nil && c.logger == nil {
		c.logger = buildLoggerFromSpec(spec.Logging)
	}

	// Apply exec tool configs from RuntimeConfig tools map
	applyExecToolConfigs(c, spec.Tools)

	// Apply exec eval handlers from RuntimeConfig evals map
	applyExecEvalHandlers(c, spec.Evals)

	// Resolve declared sandboxes, then apply exec hooks (which may
	// reference them by name).
	resolved, err := resolveSandboxes(spec.Sandboxes)
	if err != nil {
		return fmt.Errorf("resolving sandboxes from runtime config: %w", err)
	}
	if err := applyExecHooks(c, spec.Hooks, resolved); err != nil {
		return fmt.Errorf("applying exec hooks from runtime config: %w", err)
	}

	// Resolve declared selectors (exec-backed) and wire the skills
	// binding. Programmatic selectors registered via WithSelector are
	// already in c.selectors and take precedence over exec entries of
	// the same name.
	if err := applySelectors(c, spec.Selectors, resolved); err != nil {
		return fmt.Errorf("applying selectors from runtime config: %w", err)
	}
	if spec.Skills != nil && spec.Skills.Selector != "" {
		c.skillsSelectorName = spec.Skills.Selector
	}
	if spec.ToolSelector != "" {
		c.toolSelectorName = spec.ToolSelector
	}

	// Init all selectors with the shared SelectorContext now that
	// embedding providers are resolved. Programmatic selectors that
	// don't need an embedding provider can ignore the context; the
	// reference cosine selector uses it to skip building its own.
	if err := initSelectors(c); err != nil {
		return fmt.Errorf("initializing selectors: %w", err)
	}

	return nil
}

// initSelectors calls Init on every registered selector with a
// SelectorContext that exposes the configured RAG embedding provider
// (when one is set). Init failure aborts config application — a
// selector that can't initialize would fail every Send anyway, so
// surfacing the error at config-load time gives a faster signal.
func initSelectors(c *config) error {
	if len(c.selectors) == 0 {
		return nil
	}
	ctx := selection.SelectorContext{Embeddings: c.retrievalProvider}
	for name, sel := range c.selectors {
		if err := sel.Init(ctx); err != nil {
			return fmt.Errorf("selector %q: %w", name, err)
		}
	}
	return nil
}

// applySelectors builds exec-backed selector instances from the spec
// and merges them into c.selectors. Names already present in
// c.selectors (from WithSelector) are left untouched.
func applySelectors(c *config, specs map[string]*pkgconfig.SelectorConfig,
	sandboxes map[string]sandbox.Sandbox,
) error {
	if len(specs) == 0 {
		return nil
	}
	if c.selectors == nil {
		c.selectors = make(map[string]selection.Selector, len(specs))
	}
	for name, sel := range specs {
		if sel == nil {
			continue
		}
		if _, programmatic := c.selectors[name]; programmatic {
			continue
		}
		var sb sandbox.Sandbox
		if sel.Sandbox != "" {
			var ok bool
			sb, ok = sandboxes[sel.Sandbox]
			if !ok {
				return fmt.Errorf("selector %q references undeclared sandbox %q", name, sel.Sandbox)
			}
		}
		c.selectors[name] = selection.NewExecClient(selection.ExecClientConfig{
			Name:      name,
			Command:   sel.Command,
			Args:      sel.Args,
			Env:       sel.Env,
			TimeoutMs: sel.TimeoutMs,
			Sandbox:   sb,
		})
	}
	return nil
}

// applyEmbeddingProviders builds embedding-provider instances from
// the spec and stores them on the SDK config keyed by ID. The first
// declared entry doubles as the default RAG retrievalProvider when
// one isn't already set programmatically (matching the chat-provider
// "first wins, programmatic-takes-precedence" pattern).
func applyEmbeddingProviders(c *config, specs []pkgconfig.EmbeddingProviderConfig) error {
	if len(specs) == 0 {
		return nil
	}
	if c.embeddingProviders == nil {
		c.embeddingProviders = make(map[string]providers.EmbeddingProvider, len(specs))
	}
	for i := range specs {
		ep := &specs[i]
		id := ep.ID
		if id == "" {
			id = ep.Type
		}
		if _, exists := c.embeddingProviders[id]; exists {
			return fmt.Errorf("embedding provider %q: duplicate ID", id)
		}
		cred, err := providers.ResolveEmbeddingCredential(context.Background(), ep.Type, "", ep.Credential)
		if err != nil {
			return fmt.Errorf("embedding provider %q: resolving credential: %w", id, err)
		}
		instance, err := providers.CreateEmbeddingProviderFromSpec(providers.EmbeddingProviderSpec{
			ID:               id,
			Type:             ep.Type,
			Model:            ep.Model,
			BaseURL:          ep.BaseURL,
			Credential:       cred,
			AdditionalConfig: ep.AdditionalConfig,
		})
		if err != nil {
			return fmt.Errorf("embedding provider %q: %w", id, err)
		}
		c.embeddingProviders[id] = instance
		c.embeddingProviderIDs = append(c.embeddingProviderIDs, id)
	}
	// Default RAG provider: first declared, unless one is already wired.
	if c.retrievalProvider == nil && len(c.embeddingProviderIDs) > 0 {
		c.retrievalProvider = c.embeddingProviders[c.embeddingProviderIDs[0]]
	}
	return nil
}

// applyTTSProviders builds TTS instances from the spec and stores
// them by ID. The first declared entry doubles as the default
// ttsService when one isn't already set programmatically (matching
// the embedding-provider precedence pattern).
//
//nolint:dupl // applySTTProviders is structurally identical but operates on a different type.
func applyTTSProviders(c *config, specs []pkgconfig.TTSProviderConfig) error {
	if len(specs) == 0 {
		return nil
	}
	if c.ttsProviders == nil {
		c.ttsProviders = make(map[string]tts.Service, len(specs))
	}
	for i := range specs {
		tp := &specs[i]
		id := tp.ID
		if id == "" {
			id = tp.Type
		}
		if _, exists := c.ttsProviders[id]; exists {
			return fmt.Errorf("TTS provider %q: duplicate ID", id)
		}
		cred, err := tts.ResolveCredential(context.Background(), tp.Type, "", tp.Credential)
		if err != nil {
			return fmt.Errorf("TTS provider %q: resolving credential: %w", id, err)
		}
		instance, err := tts.CreateFromSpec(tts.ProviderSpec{
			ID:               id,
			Type:             tp.Type,
			Model:            tp.Model,
			BaseURL:          tp.BaseURL,
			Credential:       cred,
			AdditionalConfig: tp.AdditionalConfig,
		})
		if err != nil {
			return fmt.Errorf("TTS provider %q: %w", id, err)
		}
		c.ttsProviders[id] = instance
		c.ttsProviderIDs = append(c.ttsProviderIDs, id)
	}
	if c.ttsService == nil && len(c.ttsProviderIDs) > 0 {
		c.ttsService = c.ttsProviders[c.ttsProviderIDs[0]]
	}
	return nil
}

// applySTTProviders mirrors applyTTSProviders for STT.
//
//nolint:dupl // applyTTSProviders is structurally identical but operates on a different type.
func applySTTProviders(c *config, specs []pkgconfig.STTProviderConfig) error {
	if len(specs) == 0 {
		return nil
	}
	if c.sttProviders == nil {
		c.sttProviders = make(map[string]stt.Service, len(specs))
	}
	for i := range specs {
		sp := &specs[i]
		id := sp.ID
		if id == "" {
			id = sp.Type
		}
		if _, exists := c.sttProviders[id]; exists {
			return fmt.Errorf("STT provider %q: duplicate ID", id)
		}
		cred, err := stt.ResolveCredential(context.Background(), sp.Type, "", sp.Credential)
		if err != nil {
			return fmt.Errorf("STT provider %q: resolving credential: %w", id, err)
		}
		instance, err := stt.CreateFromSpec(stt.ProviderSpec{
			ID:               id,
			Type:             sp.Type,
			Model:            sp.Model,
			BaseURL:          sp.BaseURL,
			Credential:       cred,
			AdditionalConfig: sp.AdditionalConfig,
		})
		if err != nil {
			return fmt.Errorf("STT provider %q: %w", id, err)
		}
		c.sttProviders[id] = instance
		c.sttProviderIDs = append(c.sttProviderIDs, id)
	}
	if c.sttService == nil && len(c.sttProviderIDs) > 0 {
		c.sttService = c.sttProviders[c.sttProviderIDs[0]]
	}
	return nil
}

// applyExecToolConfigs stores exec tool configurations from RuntimeConfig.
// These are applied to tool descriptors during pipeline construction.
func applyExecToolConfigs(c *config, toolSpecs map[string]*pkgconfig.ToolSpec) {
	for name, ts := range toolSpecs {
		if ts == nil || ts.ExecConfig == nil {
			continue
		}
		if c.execToolConfigs == nil {
			c.execToolConfigs = make(map[string]*tools.ExecConfig)
		}
		c.execToolConfigs[name] = &tools.ExecConfig{
			Command:   ts.ExecConfig.Command,
			Runtime:   ts.ExecConfig.Runtime,
			Args:      ts.ExecConfig.Args,
			Env:       ts.ExecConfig.Env,
			TimeoutMs: ts.ExecConfig.TimeoutMs,
		}
	}
}

// applyExecEvalHandlers creates exec eval handlers from RuntimeConfig eval bindings
// and registers them in the eval registry.
func applyExecEvalHandlers(c *config, evalBindings map[string]*pkgconfig.ExecBinding) {
	for typeName, binding := range evalBindings {
		if binding == nil {
			continue
		}
		handler := handlers.NewExecEvalHandler(&handlers.ExecEvalConfig{
			TypeName:  typeName,
			Command:   binding.Command,
			Args:      binding.Args,
			Env:       binding.Env,
			TimeoutMs: binding.TimeoutMs,
		})
		// Register in eval registry (create if needed)
		if c.evalRegistry == nil {
			c.evalRegistry = evals.NewEvalTypeRegistry()
		}
		c.evalRegistry.Register(handler)
	}
}

// buildLoggerFromSpec creates a *slog.Logger from a LoggingConfigSpec.
func buildLoggerFromSpec(spec *pkgconfig.LoggingConfigSpec) *slog.Logger {
	level := slogLevel(spec.DefaultLevel)

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if spec.Format == pkgconfig.LogFormatJSON {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	// Add common fields as attributes
	if len(spec.CommonFields) > 0 {
		attrs := make([]slog.Attr, 0, len(spec.CommonFields))
		for k, v := range spec.CommonFields {
			attrs = append(attrs, slog.String(k, v))
		}
		handler = handler.WithAttrs(attrs)
	}

	return slog.New(handler)
}

// slogTraceOffset is how far below slog.LevelDebug the "trace" level sits.
// slog has no built-in trace level, so we use Debug-4 (matching OpenTelemetry conventions).
const slogTraceOffset = 4

// slogLevel converts a string log level to slog.Level.
func slogLevel(level string) slog.Level {
	switch level {
	case pkgconfig.LogLevelTrace:
		return slog.LevelDebug - slogTraceOffset
	case pkgconfig.LogLevelDebug:
		return slog.LevelDebug
	case pkgconfig.LogLevelWarn:
		return slog.LevelWarn
	case pkgconfig.LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// createProviderFromConfig converts a config.Provider to a providers.Provider.
func createProviderFromConfig(p *pkgconfig.Provider) (providers.Provider, error) {
	ctx := context.Background()

	// Resolve credential
	resolverCfg := credentials.ResolverConfig{
		ProviderType:     p.Type,
		CredentialConfig: p.Credential,
	}

	var platformCfg *providers.PlatformConfig
	var platform string
	if p.Platform != nil {
		platform = p.Platform.Type
		platformCfg = &providers.PlatformConfig{
			Type:             p.Platform.Type,
			Region:           p.Platform.Region,
			Project:          p.Platform.Project,
			Endpoint:         p.Platform.Endpoint,
			AdditionalConfig: p.Platform.AdditionalConfig,
		}
		resolverCfg.PlatformConfig = p.Platform
	}

	cred, err := credentials.Resolve(ctx, resolverCfg)
	if err != nil {
		return nil, fmt.Errorf("resolving credentials for provider %s: %w", p.ID, err)
	}

	spec := providers.ProviderSpec{
		ID:                p.ID,
		Type:              p.Type,
		Model:             p.Model,
		BaseURL:           p.BaseURL,
		Headers:           p.Headers,
		IncludeRawOutput:  p.IncludeRawOutput,
		AdditionalConfig:  p.AdditionalConfig,
		Credential:        cred,
		Platform:          platform,
		PlatformConfig:    platformCfg,
		UnsupportedParams: p.UnsupportedParams,
		Defaults: providers.ProviderDefaults{
			Temperature: p.Defaults.Temperature,
			TopP:        p.Defaults.TopP,
			MaxTokens:   p.Defaults.MaxTokens,
			Pricing: providers.Pricing{
				InputCostPer1K:  p.Pricing.InputCostPer1K,
				OutputCostPer1K: p.Pricing.OutputCostPer1K,
			},
		},
	}
	return providers.CreateProviderFromSpec(spec)
}

// createStateStoreFromConfig creates a statestore.Store from a StateStoreConfig.
func createStateStoreFromConfig(cfg *pkgconfig.StateStoreConfig) (statestore.Store, error) {
	switch cfg.Type {
	case "", "memory":
		return statestore.NewMemoryStore(), nil
	case "redis":
		if cfg.Redis == nil {
			return nil, fmt.Errorf("redis configuration is required when type is 'redis'")
		}
		client := redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Address,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.Database,
		})
		var opts []statestore.RedisOption
		if cfg.Redis.TTL != "" {
			ttl, err := time.ParseDuration(cfg.Redis.TTL)
			if err != nil {
				return nil, fmt.Errorf("invalid TTL duration %q: %w", cfg.Redis.TTL, err)
			}
			opts = append(opts, statestore.WithTTL(ttl))
		}
		if cfg.Redis.Prefix != "" {
			opts = append(opts, statestore.WithPrefix(cfg.Redis.Prefix))
		}
		return statestore.NewRedisStore(client, opts...), nil
	default:
		return nil, fmt.Errorf("unsupported state store type: %s", cfg.Type)
	}
}
