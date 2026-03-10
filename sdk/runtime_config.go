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
	"github.com/AltairaLabs/PromptKit/runtime/mcp"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
)

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
func applyRuntimeConfig(c *config, spec *pkgconfig.RuntimeConfigSpec) error {
	// Apply provider (use first provider if configured and no provider already set)
	if len(spec.Providers) > 0 && c.provider == nil {
		prov, err := createProviderFromConfig(&spec.Providers[0])
		if err != nil {
			return fmt.Errorf("creating provider from runtime config: %w", err)
		}
		c.provider = prov
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

	return nil
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
		ID:               p.ID,
		Type:             p.Type,
		Model:            p.Model,
		BaseURL:          p.BaseURL,
		IncludeRawOutput: p.IncludeRawOutput,
		AdditionalConfig: p.AdditionalConfig,
		Credential:       cred,
		Platform:         platform,
		PlatformConfig:   platformCfg,
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
