package credentials

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

// Platform type constants.
const (
	platformBedrock = "bedrock"
	platformVertex  = "vertex"
	platformAzure   = "azure"
)

// DefaultEnvVars maps provider types to their default environment variable names.
// This maintains backward compatibility with existing configurations.
var DefaultEnvVars = map[string][]string{
	"claude": {"ANTHROPIC_API_KEY", "CLAUDE_API_KEY"},
	"openai": {"OPENAI_API_KEY", "OPENAI_TOKEN"},
	"gemini": {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
	"imagen": {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
}

// ProviderHeaderConfig maps provider types to their API key header configuration.
var ProviderHeaderConfig = map[string]struct {
	HeaderName string
	Prefix     string
}{
	"claude": {HeaderName: "X-API-Key", Prefix: ""},
	"openai": {HeaderName: "Authorization", Prefix: "Bearer "},
	"gemini": {HeaderName: "", Prefix: ""}, // Gemini uses query param, not header
	"imagen": {HeaderName: "", Prefix: ""}, // Same as Gemini
}

// ResolverConfig holds configuration for credential resolution.
type ResolverConfig struct {
	// ProviderType is the provider type (claude, openai, gemini, etc.)
	ProviderType string

	// CredentialConfig is the explicit credential configuration from the provider.
	CredentialConfig *config.CredentialConfig

	// PlatformConfig is the platform configuration (bedrock, vertex, azure).
	PlatformConfig *config.PlatformConfig

	// ConfigDir is the base directory for resolving relative credential file paths.
	ConfigDir string
}

// Resolve resolves credentials according to the chain:
// 1. api_key (explicit value)
// 2. credential_file (read from file)
// 3. credential_env (read from environment variable)
// 4. default env vars for provider type
//
// For platform configurations (bedrock, vertex, azure), it returns the appropriate
// cloud credential type that uses the respective SDK's default credential chain.
func Resolve(ctx context.Context, cfg ResolverConfig) (Credential, error) {
	// Handle platform-specific credentials
	if cfg.PlatformConfig != nil && cfg.PlatformConfig.Type != "" {
		return resolvePlatformCredential(ctx, cfg)
	}

	// Handle API key credentials
	return resolveAPIKeyCredential(cfg)
}

// resolveAPIKeyCredential resolves API key credentials from various sources.
func resolveAPIKeyCredential(cfg ResolverConfig) (Credential, error) {
	apiKey, err := findAPIKey(cfg)
	if err != nil {
		return nil, err
	}

	// If no API key found, return a NoOp credential (some providers may not need auth)
	if apiKey == "" {
		return &NoOpCredential{}, nil
	}

	return createAPIKeyCredential(apiKey, cfg.ProviderType), nil
}

// findAPIKey searches for an API key in the resolution chain.
func findAPIKey(cfg ResolverConfig) (string, error) {
	// 1. Try explicit api_key
	if cfg.CredentialConfig != nil && cfg.CredentialConfig.APIKey != "" {
		return cfg.CredentialConfig.APIKey, nil
	}

	// 2. Try credential_file
	if cfg.CredentialConfig != nil && cfg.CredentialConfig.CredentialFile != "" {
		key, err := readCredentialFile(cfg.CredentialConfig.CredentialFile, cfg.ConfigDir)
		if err != nil {
			return "", fmt.Errorf("failed to read credential file: %w", err)
		}
		return key, nil
	}

	// 3. Try credential_env
	if cfg.CredentialConfig != nil && cfg.CredentialConfig.CredentialEnv != "" {
		key := os.Getenv(cfg.CredentialConfig.CredentialEnv)
		if key == "" {
			return "", fmt.Errorf("environment variable %s is not set", cfg.CredentialConfig.CredentialEnv)
		}
		return key, nil
	}

	// 4. Try default env vars for provider type
	return findDefaultEnvKey(cfg.ProviderType), nil
}

// findDefaultEnvKey looks for API keys in default environment variables.
func findDefaultEnvKey(providerType string) string {
	defaultVars, ok := DefaultEnvVars[providerType]
	if !ok {
		return ""
	}
	for _, envVar := range defaultVars {
		if key := os.Getenv(envVar); key != "" {
			return key
		}
	}
	return ""
}

// createAPIKeyCredential creates an API key credential with provider-specific config.
func createAPIKeyCredential(apiKey, providerType string) *APIKeyCredential {
	headerCfg, ok := ProviderHeaderConfig[providerType]
	if !ok {
		// Default to Bearer token in Authorization header
		headerCfg = struct {
			HeaderName string
			Prefix     string
		}{HeaderName: "Authorization", Prefix: "Bearer "}
	}

	opts := []APIKeyOption{WithHeaderName(headerCfg.HeaderName)}
	if headerCfg.Prefix != "" {
		opts = append(opts, WithPrefix(headerCfg.Prefix))
	} else {
		opts = append(opts, WithPrefix(""))
	}

	return NewAPIKeyCredential(apiKey, opts...)
}

// readCredentialFile reads an API key from a file.
func readCredentialFile(path, configDir string) (string, error) {
	// Handle relative paths
	if !strings.HasPrefix(path, "/") && configDir != "" {
		path = configDir + "/" + path
	}

	//nolint:gosec // G304: File path is from trusted configuration
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	// Trim whitespace and newlines
	return strings.TrimSpace(string(data)), nil
}

// MustResolve resolves credentials and panics on error.
// Use this only in initialization code where errors are unrecoverable.
func MustResolve(ctx context.Context, cfg ResolverConfig) Credential {
	cred, err := Resolve(ctx, cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve credentials: %v", err))
	}
	return cred
}
