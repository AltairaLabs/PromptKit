//go:build e2e

package sdk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// =============================================================================
// E2E Test Configuration
//
// Configures which providers and capabilities to test. Tests automatically
// skip if required API keys aren't available or provider doesn't support
// the capability being tested.
//
// Environment Variables:
//   - OPENAI_API_KEY: Enable OpenAI provider tests
//   - ANTHROPIC_API_KEY: Enable Anthropic provider tests
//   - GEMINI_API_KEY or GOOGLE_API_KEY: Enable Gemini provider tests
//   - OLLAMA_HOST_URL: Enable Ollama provider tests (e.g., "http://localhost:11434")
//   - E2E_PROVIDERS: Comma-separated list to limit providers (e.g., "openai,anthropic")
//   - E2E_SKIP_PROVIDERS: Comma-separated list to skip (e.g., "gemini")
//   - E2E_CONFIG: Path to JSON config file for advanced settings
//
// =============================================================================

// Capability represents a provider capability for matrix testing.
type Capability string

const (
	CapText      Capability = "text"       // Basic text conversation
	CapStreaming Capability = "streaming"  // Streaming responses
	CapVision    Capability = "vision"     // Image understanding
	CapAudio     Capability = "audio"      // Audio input/output
	CapVideo     Capability = "video"      // Video understanding
	CapTools     Capability = "tools"      // Tool/function calling
	CapJSON      Capability = "json"       // JSON mode output
	CapRealtime  Capability = "realtime"   // Real-time streaming (WebSocket)
)

// ProviderConfig defines a provider's capabilities and configuration.
type ProviderConfig struct {
	// ID is the provider identifier (e.g., "openai", "anthropic")
	ID string

	// EnvKey is the primary environment variable name for the API key
	EnvKey string

	// AltEnvKeys are alternative environment variable names (checked if EnvKey is not set)
	AltEnvKeys []string

	// DefaultModel is the model to use for tests
	DefaultModel string

	// Capabilities lists what this provider supports
	Capabilities []Capability

	// VisionModel is the model to use for vision tests (if different)
	VisionModel string

	// RealtimeModel is the model for realtime tests (if supported)
	RealtimeModel string
}

// HasCapability checks if the provider supports a capability.
func (p *ProviderConfig) HasCapability(cap Capability) bool {
	for _, c := range p.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// IsAvailable checks if the provider's API key is set.
func (p *ProviderConfig) IsAvailable() bool {
	if os.Getenv(p.EnvKey) != "" {
		return true
	}
	for _, key := range p.AltEnvKeys {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

// DefaultProviders returns the built-in provider configurations.
func DefaultProviders() []ProviderConfig {
	return []ProviderConfig{
		{
			ID:           "openai",
			EnvKey:       "OPENAI_API_KEY",
			DefaultModel: "gpt-4o-mini",
			VisionModel:  "gpt-4o",
			Capabilities: []Capability{
				CapText, CapStreaming, CapVision, CapTools, CapJSON,
			},
		},
		{
			ID:            "openai-realtime",
			EnvKey:        "OPENAI_API_KEY",
			DefaultModel:  "gpt-4o-mini",
			RealtimeModel: "gpt-4o-realtime-preview",
			Capabilities: []Capability{
				CapRealtime, // Audio is only via realtime API, not predict
			},
		},
		{
			ID:           "anthropic",
			EnvKey:       "ANTHROPIC_API_KEY",
			DefaultModel: "claude-sonnet-4-20250514",
			VisionModel:  "claude-sonnet-4-20250514",
			Capabilities: []Capability{
				CapText, CapStreaming, CapVision, CapTools, CapJSON,
			},
		},
		{
			ID:           "gemini",
			EnvKey:       "GEMINI_API_KEY",
			AltEnvKeys:   []string{"GOOGLE_API_KEY"},
			DefaultModel: "gemini-2.0-flash",
			VisionModel:  "gemini-2.0-flash",
			Capabilities: []Capability{
				CapText, CapStreaming, CapVision, CapAudio, CapVideo, CapTools, CapJSON,
			},
		},
		{
			ID:            "gemini-realtime",
			EnvKey:        "GEMINI_API_KEY",
			AltEnvKeys:    []string{"GOOGLE_API_KEY"},
			DefaultModel:  "gemini-2.0-flash",
			RealtimeModel: "gemini-2.0-flash-live-001",
			Capabilities: []Capability{
				CapRealtime, CapAudio, CapVideo,
			},
		},
		{
			ID:           "ollama",
			EnvKey:       "OLLAMA_HOST_URL", // Set to Ollama base URL (e.g., "http://localhost:11434")
			DefaultModel: "llava:7b",        // Default to llava for vision tests
			VisionModel:  "llava:7b",
			Capabilities: []Capability{
				CapText, CapStreaming, CapVision,
				// Note: llava:7b does not support tools
			},
		},
		{
			ID:           "ollama-tools",
			EnvKey:       "OLLAMA_HOST_URL", // Set to Ollama base URL (e.g., "http://localhost:11434")
			DefaultModel: "llama3.2:3b",     // llama3.2 supports function calling
			Capabilities: []Capability{
				CapText, CapStreaming, CapTools,
			},
		},
		{
			ID:           "mock",
			EnvKey:       "", // Always available
			DefaultModel: "mock-model",
			Capabilities: []Capability{
				CapText, CapStreaming, CapTools,
			},
		},
	}
}

// E2EConfig holds the complete e2e test configuration.
type E2EConfig struct {
	// Providers to test (filtered by availability and env vars)
	Providers []ProviderConfig

	// TestTimeout is the default timeout for tests (seconds)
	TestTimeout int `json:"test_timeout"`

	// Verbose enables verbose logging
	Verbose bool `json:"verbose"`

	// SkipSlowTests skips tests marked as slow
	SkipSlowTests bool `json:"skip_slow_tests"`
}

// LoadE2EConfig loads configuration from environment and optional config file.
func LoadE2EConfig() *E2EConfig {
	cfg := &E2EConfig{
		Providers:   []ProviderConfig{},
		TestTimeout: 30,
	}

	// Load from config file if specified
	if configPath := os.Getenv("E2E_CONFIG"); configPath != "" {
		if data, err := os.ReadFile(configPath); err == nil {
			json.Unmarshal(data, cfg)
		}
	}

	// Get provider filters from env
	includeProviders := parseEnvList("E2E_PROVIDERS")
	excludeProviders := parseEnvList("E2E_SKIP_PROVIDERS")

	// Filter providers
	for _, p := range DefaultProviders() {
		// Check if explicitly included/excluded
		if len(includeProviders) > 0 && !contains(includeProviders, p.ID) {
			continue
		}
		if contains(excludeProviders, p.ID) {
			continue
		}

		// Check availability (mock is always available)
		if p.ID != "mock" && !p.IsAvailable() {
			continue
		}

		cfg.Providers = append(cfg.Providers, p)
	}

	return cfg
}

// ProvidersWithCapability returns providers that support the given capability.
func (c *E2EConfig) ProvidersWithCapability(cap Capability) []ProviderConfig {
	var result []ProviderConfig
	for _, p := range c.Providers {
		if p.HasCapability(cap) {
			result = append(result, p)
		}
	}
	return result
}

// parseEnvList parses a comma-separated environment variable.
func parseEnvList(key string) []string {
	val := os.Getenv(key)
	if val == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// =============================================================================
// Test Matrix Helpers
// =============================================================================

// RunForProviders runs a test function for each provider with the required capability.
// Skips providers that don't have the capability or aren't available.
func RunForProviders(t *testing.T, cap Capability, testFn func(t *testing.T, provider ProviderConfig)) {
	t.Helper()

	cfg := LoadE2EConfig()
	providers := cfg.ProvidersWithCapability(cap)

	if len(providers) == 0 {
		t.Skipf("No providers available with capability: %s", cap)
	}

	for _, p := range providers {
		p := p // capture
		t.Run(p.ID, func(t *testing.T) {
			t.Parallel()
			testFn(t, p)
		})
	}
}

// RunForProvidersSerial runs tests serially (for tests that can't be parallelized).
func RunForProvidersSerial(t *testing.T, cap Capability, testFn func(t *testing.T, provider ProviderConfig)) {
	t.Helper()

	cfg := LoadE2EConfig()
	providers := cfg.ProvidersWithCapability(cap)

	if len(providers) == 0 {
		t.Skipf("No providers available with capability: %s", cap)
	}

	for _, p := range providers {
		t.Run(p.ID, func(t *testing.T) {
			testFn(t, p)
		})
	}
}

// SkipIfNoProvider skips the test if the specified provider isn't available.
func SkipIfNoProvider(t *testing.T, providerID string) {
	t.Helper()

	cfg := LoadE2EConfig()
	for _, p := range cfg.Providers {
		if p.ID == providerID {
			return
		}
	}
	t.Skipf("Provider %s not available (missing API key or excluded)", providerID)
}

// RequireCapability skips if no providers have the capability.
func RequireCapability(t *testing.T, cap Capability) []ProviderConfig {
	t.Helper()

	cfg := LoadE2EConfig()
	providers := cfg.ProvidersWithCapability(cap)

	if len(providers) == 0 {
		t.Skipf("No providers available with capability: %s", cap)
	}

	return providers
}
