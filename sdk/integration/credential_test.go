package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// TestCredential_FromEnvVar verifies that WithCredential(WithCredentialEnv(...))
// resolves the API key from an environment variable and successfully opens a
// conversation.
func TestCredential_FromEnvVar(t *testing.T) {
	t.Setenv("TEST_PROMPTKIT_KEY", "test-api-key-from-env")

	packPath := writePackFile(t, minimalPackJSON)
	provider := mock.NewProvider("mock-test", "mock-model", false)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithCredential(
			sdk.WithCredentialEnv("TEST_PROMPTKIT_KEY"),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	// Verify the conversation works end-to-end
	resp, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text())
}

// TestCredential_FromFile verifies that WithCredential(WithCredentialFile(...))
// reads the API key from a file.
func TestCredential_FromFile(t *testing.T) {
	// Write a credential file
	credDir := t.TempDir()
	credFile := filepath.Join(credDir, "api-key.txt")
	require.NoError(t, os.WriteFile(credFile, []byte("test-api-key-from-file\n"), 0o600))

	packPath := writePackFile(t, minimalPackJSON)
	provider := mock.NewProvider("mock-test", "mock-model", false)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithCredential(
			sdk.WithCredentialFile(credFile),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text())
}

// TestCredential_DirectAPIKey verifies WithCredential(WithCredentialAPIKey(...)).
func TestCredential_DirectAPIKey(t *testing.T) {
	packPath := writePackFile(t, minimalPackJSON)
	provider := mock.NewProvider("mock-test", "mock-model", false)

	conv, err := sdk.Open(packPath, "chat",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithCredential(
			sdk.WithCredentialAPIKey("test-direct-key"),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "Hello")
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Text())
}

// TestCredential_EnvVarNotSet verifies that Open fails gracefully when the
// environment variable is not set.
func TestCredential_EnvVarNotSet(t *testing.T) {
	packPath := writePackFile(t, minimalPackJSON)

	_, err := sdk.Open(packPath, "chat",
		sdk.WithSkipSchemaValidation(),
		sdk.WithCredential(
			sdk.WithCredentialEnv("NONEXISTENT_PROMPTKIT_KEY_12345"),
		),
	)
	// Should fail at credential resolution or provider detection
	assert.Error(t, err)
}

// TestCredential_FileNotFound verifies that Open fails gracefully when the
// credential file doesn't exist.
func TestCredential_FileNotFound(t *testing.T) {
	packPath := writePackFile(t, minimalPackJSON)

	_, err := sdk.Open(packPath, "chat",
		sdk.WithSkipSchemaValidation(),
		sdk.WithCredential(
			sdk.WithCredentialFile("/nonexistent/path/creds.txt"),
		),
	)
	assert.Error(t, err)
}
