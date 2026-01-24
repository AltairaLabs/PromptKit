package credentials

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve_ExplicitAPIKey(t *testing.T) {
	cfg := ResolverConfig{
		ProviderType: "openai",
		CredentialConfig: &config.CredentialConfig{
			APIKey: "sk-test-key",
		},
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	assert.Equal(t, "api_key", cred.Type())

	// Verify it's an APIKeyCredential with the correct value
	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "sk-test-key", akc.APIKey())
}

func TestResolve_CredentialFile(t *testing.T) {
	// Create a temporary credential file
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "api_key.txt")
	err := os.WriteFile(credFile, []byte("sk-file-key\n"), 0600)
	require.NoError(t, err)

	cfg := ResolverConfig{
		ProviderType: "openai",
		CredentialConfig: &config.CredentialConfig{
			CredentialFile: credFile,
		},
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "sk-file-key", akc.APIKey())
}

func TestResolve_CredentialEnv(t *testing.T) {
	// Set a custom environment variable
	envVar := "TEST_PROMPTKIT_API_KEY"
	t.Setenv(envVar, "sk-env-key")

	cfg := ResolverConfig{
		ProviderType: "openai",
		CredentialConfig: &config.CredentialConfig{
			CredentialEnv: envVar,
		},
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "sk-env-key", akc.APIKey())
}

func TestResolve_CredentialEnv_NotSet(t *testing.T) {
	cfg := ResolverConfig{
		ProviderType: "openai",
		CredentialConfig: &config.CredentialConfig{
			CredentialEnv: "NONEXISTENT_ENV_VAR_12345",
		},
	}

	_, err := Resolve(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not set")
}

func TestResolve_DefaultEnvVars(t *testing.T) {
	// Set default OpenAI env var
	t.Setenv("OPENAI_API_KEY", "sk-default-key")

	cfg := ResolverConfig{
		ProviderType: "openai",
		// No explicit credential config
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "sk-default-key", akc.APIKey())
}

func TestResolve_ClaudeDefaultEnvVars(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-anthropic-key")

	cfg := ResolverConfig{
		ProviderType: "claude",
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "sk-anthropic-key", akc.APIKey())
}

func TestResolve_GeminiDefaultEnvVars(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "gemini-key")

	cfg := ResolverConfig{
		ProviderType: "gemini",
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "gemini-key", akc.APIKey())
}

func TestResolve_NoCredential(t *testing.T) {
	// Clear any environment variables that might be set
	for _, envVar := range DefaultEnvVars["openai"] {
		t.Setenv(envVar, "")
	}

	cfg := ResolverConfig{
		ProviderType: "openai",
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	// Should return NoOpCredential
	assert.Equal(t, "none", cred.Type())
}

func TestResolve_PriorityOrder(t *testing.T) {
	// Set up all three sources
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "api_key.txt")
	err := os.WriteFile(credFile, []byte("sk-file-key"), 0600)
	require.NoError(t, err)

	t.Setenv("TEST_CRED_ENV", "sk-env-key")
	t.Setenv("OPENAI_API_KEY", "sk-default-key")

	// Test 1: api_key takes precedence
	cfg := ResolverConfig{
		ProviderType: "openai",
		CredentialConfig: &config.CredentialConfig{
			APIKey:         "sk-explicit-key",
			CredentialFile: credFile,
			CredentialEnv:  "TEST_CRED_ENV",
		},
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "sk-explicit-key", akc.APIKey())

	// Test 2: credential_file takes precedence over credential_env
	cfg = ResolverConfig{
		ProviderType: "openai",
		CredentialConfig: &config.CredentialConfig{
			CredentialFile: credFile,
			CredentialEnv:  "TEST_CRED_ENV",
		},
	}

	cred, err = Resolve(context.Background(), cfg)
	require.NoError(t, err)
	akc, ok = cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "sk-file-key", akc.APIKey())

	// Test 3: credential_env takes precedence over default
	cfg = ResolverConfig{
		ProviderType: "openai",
		CredentialConfig: &config.CredentialConfig{
			CredentialEnv: "TEST_CRED_ENV",
		},
	}

	cred, err = Resolve(context.Background(), cfg)
	require.NoError(t, err)
	akc, ok = cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "sk-env-key", akc.APIKey())
}

func TestAPIKeyCredential_Apply(t *testing.T) {
	cred := NewAPIKeyCredential("sk-test-key")

	req, err := http.NewRequest("POST", "https://api.example.com", nil)
	require.NoError(t, err)

	err = cred.Apply(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, "Bearer sk-test-key", req.Header.Get("Authorization"))
}

func TestAPIKeyCredential_CustomHeader(t *testing.T) {
	cred := NewAPIKeyCredential("sk-test-key",
		WithHeaderName("X-API-Key"),
		WithPrefix(""),
	)

	req, err := http.NewRequest("POST", "https://api.example.com", nil)
	require.NoError(t, err)

	err = cred.Apply(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, "sk-test-key", req.Header.Get("X-API-Key"))
}

func TestNoOpCredential_Apply(t *testing.T) {
	cred := &NoOpCredential{}

	req, err := http.NewRequest("POST", "https://api.example.com", nil)
	require.NoError(t, err)

	err = cred.Apply(context.Background(), req)
	require.NoError(t, err)

	// No headers should be added
	assert.Empty(t, req.Header.Get("Authorization"))
}

func TestResolve_UnknownProviderType(t *testing.T) {
	// Unknown provider should get default Bearer auth
	cfg := ResolverConfig{
		ProviderType: "unknown-provider",
		CredentialConfig: &config.CredentialConfig{
			APIKey: "sk-test-key",
		},
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)

	// Verify it uses default Bearer auth
	req, err := http.NewRequest("POST", "https://api.example.com", nil)
	require.NoError(t, err)
	err = akc.Apply(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "Bearer sk-test-key", req.Header.Get("Authorization"))
}

func TestResolve_CredentialFile_RelativePath(t *testing.T) {
	// Create a temporary directory and credential file
	tmpDir := t.TempDir()
	credFile := "api_key.txt"
	err := os.WriteFile(filepath.Join(tmpDir, credFile), []byte("sk-relative-key"), 0600)
	require.NoError(t, err)

	cfg := ResolverConfig{
		ProviderType: "openai",
		CredentialConfig: &config.CredentialConfig{
			CredentialFile: credFile,
		},
		ConfigDir: tmpDir,
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "sk-relative-key", akc.APIKey())
}

func TestResolve_CredentialFile_NotFound(t *testing.T) {
	cfg := ResolverConfig{
		ProviderType: "openai",
		CredentialConfig: &config.CredentialConfig{
			CredentialFile: "/nonexistent/path/to/file.txt",
		},
	}

	_, err := Resolve(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read credential file")
}

func TestResolve_FallbackDefaultEnvVar(t *testing.T) {
	// Set the second choice env var (OPENAI_TOKEN instead of OPENAI_API_KEY)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_TOKEN", "sk-fallback-key")

	cfg := ResolverConfig{
		ProviderType: "openai",
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "sk-fallback-key", akc.APIKey())
}

func TestResolve_ClaudeHeaderConfig(t *testing.T) {
	cfg := ResolverConfig{
		ProviderType: "claude",
		CredentialConfig: &config.CredentialConfig{
			APIKey: "sk-claude-key",
		},
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)

	// Claude uses X-API-Key header without prefix
	req, err := http.NewRequest("POST", "https://api.anthropic.com", nil)
	require.NoError(t, err)
	err = akc.Apply(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "sk-claude-key", req.Header.Get("X-API-Key"))
}

func TestResolve_ImagenDefaultEnvVars(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "imagen-key")

	cfg := ResolverConfig{
		ProviderType: "imagen",
	}

	cred, err := Resolve(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, cred)

	akc, ok := cred.(*APIKeyCredential)
	require.True(t, ok)
	assert.Equal(t, "imagen-key", akc.APIKey())
}
