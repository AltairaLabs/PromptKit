package sdk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePackPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		setup   func() string
		wantErr bool
	}{
		{
			name:    "non-existent absolute file",
			path:    "/nonexistent/path/to/pack.yaml",
			wantErr: true,
		},
		{
			name:    "non-existent relative file",
			path:    "nonexistent/pack.yaml",
			wantErr: true,
		},
		{
			name: "valid yaml file",
			path: "", // Set in setup
			setup: func() string {
				dir := t.TempDir()
				file := filepath.Join(dir, "pack.yaml")
				os.WriteFile(file, []byte("name: test"), 0644)
				return file
			},
			wantErr: false,
		},
		{
			name: "valid yml file",
			path: "", // Set in setup
			setup: func() string {
				dir := t.TempDir()
				file := filepath.Join(dir, "pack.yml")
				os.WriteFile(file, []byte("name: test"), 0644)
				return file
			},
			wantErr: false,
		},
		{
			name: "valid json file",
			path: "", // Set in setup
			setup: func() string {
				dir := t.TempDir()
				file := filepath.Join(dir, "pack.json")
				os.WriteFile(file, []byte(`{"name": "test"}`), 0644)
				return file
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path
			if tt.setup != nil {
				path = tt.setup()
			}

			result, err := resolvePackPath(path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, result)
			}
		})
	}
}

func TestOpenErrors(t *testing.T) {
	// Test with non-existent path
	_, err := Open("/nonexistent/path/pack.yaml", "main")
	assert.Error(t, err)
}

func TestResumeErrors(t *testing.T) {
	// Test without state store
	_, err := Resume("conv-123", "/nonexistent/path/pack.yaml", "main")
	assert.Error(t, err)
}

func TestOpenWithValidPack(t *testing.T) {
	// Create a valid pack file (JSON only - that's what the loader supports)
	dir := t.TempDir()
	packFile := filepath.Join(dir, "test.pack.json")
	packContent := `{
		"name": "test-pack",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "You are a helpful assistant."
			},
			"other": {
				"system_template": "Another prompt."
			}
		}
	}`
	err := os.WriteFile(packFile, []byte(packContent), 0644)
	require.NoError(t, err)

	t.Run("prompt not found", func(t *testing.T) {
		// Set up fake API key
		os.Setenv("OPENAI_API_KEY", "test-key")
		defer os.Unsetenv("OPENAI_API_KEY")

		_, err := Open(packFile, "nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("option error", func(t *testing.T) {
		_, err := Open(packFile, "main", func(c *config) error {
			return assert.AnError
		})
		assert.Error(t, err)
	})

	t.Run("no provider configured", func(t *testing.T) {
		// Clear all API keys
		os.Unsetenv("OPENAI_API_KEY")
		os.Unsetenv("ANTHROPIC_API_KEY")
		os.Unsetenv("GOOGLE_API_KEY")
		os.Unsetenv("GEMINI_API_KEY")

		_, err := Open(packFile, "main")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "provider")
	})

	t.Run("success with API key", func(t *testing.T) {
		os.Setenv("OPENAI_API_KEY", "test-key")
		defer os.Unsetenv("OPENAI_API_KEY")

		conv, err := Open(packFile, "main")
		require.NoError(t, err)
		assert.NotNil(t, conv)
		assert.Equal(t, "main", conv.promptName)
	})

	t.Run("success with provided API key", func(t *testing.T) {
		os.Unsetenv("OPENAI_API_KEY")

		conv, err := Open(packFile, "main", WithAPIKey("my-api-key"))
		require.NoError(t, err)
		assert.NotNil(t, conv)
	})

	t.Run("success with model override", func(t *testing.T) {
		os.Setenv("OPENAI_API_KEY", "test-key")
		defer os.Unsetenv("OPENAI_API_KEY")

		conv, err := Open(packFile, "main", WithModel("gpt-4o"))
		require.NoError(t, err)
		assert.NotNil(t, conv)
	})
}

func TestOpenWithVariableDefaults(t *testing.T) {
	// Create a pack with variable defaults (JSON format)
	dir := t.TempDir()
	packFile := filepath.Join(dir, "vars.pack.json")
	packContent := `{
		"name": "vars-pack",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "Hello {{name}}",
				"variables": [
					{"name": "name", "default": "World"},
					{"name": "greeting", "default": "Hi"}
				]
			}
		}
	}`
	err := os.WriteFile(packFile, []byte(packContent), 0644)
	require.NoError(t, err)

	os.Setenv("OPENAI_API_KEY", "test-key")
	defer os.Unsetenv("OPENAI_API_KEY")

	conv, err := Open(packFile, "main")
	require.NoError(t, err)

	// Check that defaults were applied
	assert.Equal(t, "World", conv.GetVar("name"))
	assert.Equal(t, "Hi", conv.GetVar("greeting"))
}

func TestResumeWithStateStore(t *testing.T) {
	// Create a valid pack file (JSON format)
	dir := t.TempDir()
	packFile := filepath.Join(dir, "test.pack.json")
	packContent := `{
		"name": "test-pack",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "You are a helpful assistant."
			}
		}
	}`
	err := os.WriteFile(packFile, []byte(packContent), 0644)
	require.NoError(t, err)

	t.Run("no state store returns error", func(t *testing.T) {
		_, err := Resume("conv-123", packFile, "main")
		assert.Error(t, err)
	})
}

// mockStore implements statestore.Store for testing
type mockStore struct {
	conversations map[string]*statestore.ConversationState
	loadErr       error
}

func newMockStore() *mockStore {
	return &mockStore{
		conversations: make(map[string]*statestore.ConversationState),
	}
}

func (m *mockStore) Save(_ context.Context, state *statestore.ConversationState) error {
	m.conversations[state.ID] = state
	return nil
}

func (m *mockStore) Load(_ context.Context, id string) (*statestore.ConversationState, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	return m.conversations[id], nil
}

func TestResumeWithMockStateStore(t *testing.T) {
	// Create a valid pack file
	dir := t.TempDir()
	packFile := filepath.Join(dir, "test.pack.json")
	packContent := `{
		"name": "test-pack",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "You are a helpful assistant."
			}
		}
	}`
	err := os.WriteFile(packFile, []byte(packContent), 0644)
	require.NoError(t, err)

	os.Setenv("OPENAI_API_KEY", "test-key")
	defer os.Unsetenv("OPENAI_API_KEY")

	t.Run("conversation not found", func(t *testing.T) {
		store := newMockStore()
		_, err := Resume("nonexistent", packFile, "main", WithStateStore(store))
		assert.ErrorIs(t, err, ErrConversationNotFound)
	})

	t.Run("state load error", func(t *testing.T) {
		store := newMockStore()
		store.loadErr = errors.New("storage failure")
		_, err := Resume("conv-123", packFile, "main", WithStateStore(store))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "storage failure")
	})

	t.Run("successful resume", func(t *testing.T) {
		store := newMockStore()
		// Save a conversation state
		store.conversations["conv-123"] = &statestore.ConversationState{
			ID: "conv-123",
		}

		conv, err := Resume("conv-123", packFile, "main", WithStateStore(store))
		require.NoError(t, err)
		assert.NotNil(t, conv)
		assert.Equal(t, "conv-123", conv.ID())
	})

	t.Run("resume with option error", func(t *testing.T) {
		store := newMockStore()
		store.conversations["conv-123"] = &statestore.ConversationState{
			ID: "conv-123",
		}

		_, err := Resume("conv-123", packFile, "main",
			WithStateStore(store),
			func(c *config) error { return errors.New("option error") },
		)
		assert.Error(t, err)
	})

	t.Run("resume with invalid pack", func(t *testing.T) {
		store := newMockStore()
		store.conversations["conv-123"] = &statestore.ConversationState{
			ID: "conv-123",
		}

		_, err := Resume("conv-123", "/nonexistent/pack.json", "main", WithStateStore(store))
		assert.Error(t, err)
	})
}
