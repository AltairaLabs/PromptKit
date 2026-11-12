package mock_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// writeMockResponses writes a mock file answering "scripted" to every turn.
func writeMockResponses(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mock-responses.yaml"),
		[]byte("defaultResponse: scripted\n"), 0o600))
}

// reply creates the mock provider from spec and returns its answer to one turn.
func reply(t *testing.T, spec providers.ProviderSpec) string {
	t.Helper()
	p, err := providers.CreateProviderFromSpec(spec)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	resp, err := p.Predict(context.Background(), providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	return resp.Content
}

func mockSpec(baseDir, path string) providers.ProviderSpec {
	return providers.ProviderSpec{ID: "mock", Type: "mock", Model: "m", BaseDir: baseDir,
		AdditionalConfig: map[string]interface{}{"mock_config": path}}
}

// A relative mock_config resolves against BaseDir, whatever the working
// directory is.
func TestMockConfig_RelativePathResolvesAgainstBaseDir(t *testing.T) {
	kit := t.TempDir()
	writeMockResponses(t, kit)
	t.Chdir(t.TempDir())
	assert.Equal(t, "scripted", reply(t, mockSpec(kit, "mock-responses.yaml")))
}

// Without BaseDir, a relative path resolves against the working directory,
// as before.
func TestMockConfig_RelativePathWithoutBaseDirUsesTheWorkingDirectory(t *testing.T) {
	cwd := t.TempDir()
	writeMockResponses(t, cwd)
	t.Chdir(cwd)
	assert.Equal(t, "scripted", reply(t, mockSpec("", "mock-responses.yaml")))
}

// An absolute path ignores BaseDir.
func TestMockConfig_AbsolutePathIgnoresBaseDir(t *testing.T) {
	kit := t.TempDir()
	writeMockResponses(t, kit)
	assert.Equal(t, "scripted", reply(t, mockSpec(t.TempDir(), filepath.Join(kit, "mock-responses.yaml"))))
}

// A mock_config that does not load is an error, naming the path it tried,
// rather than a warning and generic replies.
func TestMockConfig_UnloadableFileIsAnError(t *testing.T) {
	kit := t.TempDir()
	_, err := providers.CreateProviderFromSpec(mockSpec(kit, "missing.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), filepath.Join(kit, "missing.yaml"))
}
