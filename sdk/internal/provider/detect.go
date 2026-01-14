// Package provider provides internal provider detection and initialization.
package provider

import (
	"fmt"
	"os"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/claude"
	"github.com/AltairaLabs/PromptKit/runtime/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
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

	// If apiKey provided but no info, default to OpenAI
	if info == nil {
		info = &Info{Name: "openai", APIKey: apiKey, Model: "gpt-4o"}
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
	default:
		return ""
	}
}

// detectInfoForProvider returns provider info for a specific provider name.
func detectInfoForProvider(providerName string) *Info {
	envKeys := map[string][]string{
		"openai":    {"OPENAI_API_KEY"},
		"anthropic": {"ANTHROPIC_API_KEY"},
		"gemini":    {"GOOGLE_API_KEY", "GEMINI_API_KEY"},
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
		{"openai", "OPENAI_API_KEY", "gpt-4o"},
		{"anthropic", "ANTHROPIC_API_KEY", "claude-sonnet-4-20250514"},
		{"gemini", "GOOGLE_API_KEY", "gemini-1.5-pro"},
		{"gemini", "GEMINI_API_KEY", "gemini-1.5-pro"},
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
	default:
		return nil, fmt.Errorf("unsupported provider: %s", info.Name)
	}
}
