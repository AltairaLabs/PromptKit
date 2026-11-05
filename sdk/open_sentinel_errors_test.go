package sdk

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Open's documented sentinels must match with errors.Is, and wrapping them
// must not change the text callers already see.

func TestOpenSentinel_PackNotFound(t *testing.T) {
	for name, path := range map[string]string{
		"relative": "no-such-dir/missing.pack.json",
		"absolute": filepath.Join(t.TempDir(), "missing.pack.json"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Open(path, "chat", WithProvider(mock.NewProvider("m", "m", false)))
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrPackNotFound), "errors.Is(err, ErrPackNotFound): %v", err)
			assert.Contains(t, err.Error(), "pack file not found: ")
			assert.Contains(t, err.Error(), "missing.pack.json")
		})
	}
}

func TestOpenSentinel_PromptNotFound(t *testing.T) {
	_, err := Open(writeTestPack(t), "no-such-prompt",
		WithProvider(mock.NewProvider("m", "m", false)), WithSkipSchemaValidation())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPromptNotFound), "errors.Is(err, ErrPromptNotFound): %v", err)
	assert.Contains(t, err.Error(), `prompt "no-such-prompt" not found in pack (available: `)
}

func TestOpenSentinel_PromptNotFoundCached(t *testing.T) {
	packPath := writeTestPack(t)
	conv, err := Open(packPath, "chat", WithProvider(mock.NewProvider("m", "m", false)), WithSkipSchemaValidation())
	require.NoError(t, err)
	_ = conv.Close()

	// The second open is served from the pack cache.
	_, err = Open(packPath, "no-such-prompt",
		WithProvider(mock.NewProvider("m", "m", false)), WithSkipSchemaValidation())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPromptNotFound), "errors.Is(err, ErrPromptNotFound): %v", err)
}

func TestOpenSentinel_ProviderNotDetected(t *testing.T) {
	clearDetectableEnv(t)
	_, err := Open(writeTestPack(t), "chat", WithSkipSchemaValidation())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProviderNotDetected), "errors.Is(err, ErrProviderNotDetected): %v", err)
	assert.Contains(t, err.Error(),
		"failed to detect provider: no provider detected: set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GOOGLE_API_KEY")
}
