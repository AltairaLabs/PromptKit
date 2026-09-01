// Package provider provides internal provider detection and initialization.
package provider

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/ollama"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
)

// Default provider settings
const (
	defaultTemperature = 0.7
	defaultTopP        = 1.0
	defaultMaxTokens   = 4096
)

// Provider name constants
const (
	providerGemini    = "gemini"
	providerOpenAI    = "openai"
	providerAnthropic = "anthropic"
	providerOllama    = "ollama"
)

// Default model per provider, used when the caller supplies no model and the
// provider is auto-detected from the environment.
// Each is the vendor's current balance-intelligence-and-cost tier, matching
// what the previous defaults were when they were chosen. Callers who want a
// frontier or budget model pass one explicitly.
const (
	// Replaces gpt-4o, which OpenAI now lists as deprecated.
	defaultOpenAIModel = "gpt-5.6-terra"
	// Replaces claude-sonnet-4-20250514, which is deprecated. claude-sonnet-5
	// is its documented drop-in successor; pass a model to opt into
	// claude-opus-5.
	defaultAnthropicModel = "claude-sonnet-5"
	// Replaces gemini-1.5-pro, long retired. gemini-3.6-flash is GA and the
	// current speed/intelligence balance; there is no current Pro-tier ID.
	defaultGeminiModel = "gemini-3.6-flash"
)

// Environment variables consulted during provider auto-detection.
const (
	envOpenAIKey    = "OPENAI_API_KEY"
	envAnthropicKey = "ANTHROPIC_API_KEY"
	envGoogleKey    = "GOOGLE_API_KEY"
	envGeminiKey    = "GEMINI_API_KEY"
)

// Info contains detected provider information.
type Info struct {
	// Name is the provider identifier (e.g., "openai", "anthropic", "gemini").
	Name string

	// APIKey is the API key for the provider.
	APIKey string

	// Model is the default model to use if none is specified.
	Model string
}

// Detect attempts to detect a provider from environment variables and create it.
// If apiKey is provided, it uses that instead of environment detection.
// If model is provided, it overrides the default model and may determine the provider.
// Returns the provider or an error if none can be detected.
func Detect(apiKey, model string) (providers.Provider, error) {
	// If model is specified, try to infer provider from model name first
	if model != "" {
		if providerName := inferProviderFromModel(model); providerName != "" {
			// Try to get API key for the inferred provider
			info := detectInfoForProvider(providerName)
			if info != nil {
				info.Model = model
				if apiKey != "" {
					info.APIKey = apiKey
				}
				return createProvider(info)
			}
		}
	}

	// Fall back to environment variable detection
	info := detectInfo()
	if info == nil && apiKey == "" {
		return nil, fmt.Errorf("no provider detected: set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GOOGLE_API_KEY")
	}

	// If apiKey provided but no provider-specific env var detected, default to OpenAI.
	// Log a warning so the caller knows this is an implicit assumption.
	if info == nil {
		slog.Warn("no provider detected from environment; defaulting to OpenAI "+defaultOpenAIModel,
			"hint", "set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GOOGLE_API_KEY to be explicit")
		info = &Info{Name: providerOpenAI, APIKey: apiKey, Model: defaultOpenAIModel}
	}

	// Override with provided values
	if apiKey != "" {
		info.APIKey = apiKey
	}
	if model != "" {
		info.Model = model
	}

	return createProvider(info)
}

// inferProviderFromModel attempts to determine the provider from the model name.
// Returns empty string if provider cannot be inferred.
func inferProviderFromModel(model string) string {
	modelLower := strings.ToLower(model)
	switch {
	case strings.HasPrefix(modelLower, "gemini"):
		return providerGemini
	case strings.HasPrefix(modelLower, "gpt"),
		strings.HasPrefix(modelLower, "o1"),
		strings.HasPrefix(modelLower, "o3"):
		return providerOpenAI
	case strings.HasPrefix(modelLower, "claude"):
		return providerAnthropic
	case strings.Contains(model, ":"):
		// Ollama models typically use "name:tag" format (e.g., "llava:7b", "llama2:latest")
		return providerOllama
	default:
		return ""
	}
}

// detectInfoForProvider returns provider info for a specific provider name.
func detectInfoForProvider(providerName string) *Info {
	// Ollama doesn't require API keys - just check OLLAMA_HOST env var
	if providerName == providerOllama {
		// OLLAMA_HOST indicates Ollama is configured; defaults to localhost if not set
		// Return info if model was explicitly specified (caller will set model)
		return &Info{
			Name:   providerOllama,
			APIKey: "", // Ollama doesn't use API keys
			Model:  "", // Will be set by caller
		}
	}

	envKeys := map[string][]string{
		providerOpenAI:    {envOpenAIKey},
		providerAnthropic: {envAnthropicKey},
		providerGemini:    {envGoogleKey, envGeminiKey},
	}

	keys, ok := envKeys[providerName]
	if !ok {
		return nil
	}

	for _, keyEnv := range keys {
		if key := os.Getenv(keyEnv); key != "" {
			return &Info{
				Name:   providerName,
				APIKey: key,
				Model:  "", // Will be set by caller
			}
		}
	}

	return nil
}

// detectInfo checks environment for provider API keys.
func detectInfo() *Info {
	// Check providers in priority order
	checks := []struct {
		name     string
		keyEnv   string
		defModel string
	}{
		{providerOpenAI, envOpenAIKey, defaultOpenAIModel},
		{providerAnthropic, envAnthropicKey, defaultAnthropicModel},
		{providerGemini, envGoogleKey, defaultGeminiModel},
		{providerGemini, envGeminiKey, defaultGeminiModel},
	}

	for _, c := range checks {
		if key := os.Getenv(c.keyEnv); key != "" {
			return &Info{
				Name:   c.name,
				APIKey: key,
				Model:  c.defModel,
			}
		}
	}

	return nil
}

// createProvider creates a runtime provider from info.
func createProvider(info *Info) (providers.Provider, error) {
	defaults := providers.ProviderDefaults{
		Temperature: defaultTemperature,
		TopP:        defaultTopP,
		MaxTokens:   defaultMaxTokens,
		// DisablePromptCaching is false (caching on by default).
		// SDK callers that need to disable caching should use a provider file
		// or WithLLMProvider option with prompt_caching: false in the config.
		DisablePromptCaching: false,
	}

	switch strings.ToLower(info.Name) {
	case "openai":
		return openai.NewToolProvider(
			"openai",
			info.Model,
			"https://api.openai.com/v1",
			defaults,
			false,
			nil,
			nil,
		), nil
	case "anthropic":
		return claude.NewToolProvider(
			"anthropic",
			info.Model,
			"https://api.anthropic.com",
			defaults,
			false,
		), nil
	case "gemini", "google":
		return gemini.NewToolProvider(
			"gemini",
			info.Model,
			"https://generativelanguage.googleapis.com/v1beta",
			defaults,
			false,
		), nil
	case "ollama":
		// Use OLLAMA_HOST_URL env var or default to localhost:11434
		baseURL := os.Getenv("OLLAMA_HOST_URL")
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		return ollama.NewToolProvider(
			"ollama",
			info.Model,
			baseURL,
			defaults,
			false,
			nil,
		), nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", info.Name)
	}
}
