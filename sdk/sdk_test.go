package sdk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
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

		_, err := Open(packFile, "nonexistent", WithSkipSchemaValidation())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("option error", func(t *testing.T) {
		_, err := Open(packFile, "main", WithSkipSchemaValidation(), func(c *config) error {
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

		_, err := Open(packFile, "main", WithSkipSchemaValidation())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "provider")
	})

	t.Run("success with API key", func(t *testing.T) {
		os.Setenv("OPENAI_API_KEY", "test-key")
		defer os.Unsetenv("OPENAI_API_KEY")

		conv, err := Open(packFile, "main", WithSkipSchemaValidation())
		require.NoError(t, err)
		assert.NotNil(t, conv)
		assert.Equal(t, "main", conv.promptName)
	})

	t.Run("success with provided API key", func(t *testing.T) {
		os.Unsetenv("OPENAI_API_KEY")

		conv, err := Open(packFile, "main", WithSkipSchemaValidation(), WithAPIKey("my-api-key"))
		require.NoError(t, err)
		assert.NotNil(t, conv)
	})

	t.Run("success with model override", func(t *testing.T) {
		os.Setenv("OPENAI_API_KEY", "test-key")
		defer os.Unsetenv("OPENAI_API_KEY")

		conv, err := Open(packFile, "main", WithSkipSchemaValidation(), WithModel("gpt-4o"))
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

	conv, err := Open(packFile, "main", WithSkipSchemaValidation())
	require.NoError(t, err)

	// Check that defaults were applied
	val1, _ := conv.GetVar("name")
	val2, _ := conv.GetVar("greeting")
	assert.Equal(t, "World", val1)
	assert.Equal(t, "Hi", val2)
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

func (m *mockStore) Fork(_ context.Context, sourceID, newID string) error {
	source, exists := m.conversations[sourceID]
	if !exists {
		return statestore.ErrNotFound
	}
	// Create a copy
	forked := &statestore.ConversationState{
		ID:         newID,
		UserID:     source.UserID,
		Messages:   append([]types.Message{}, source.Messages...),
		TokenCount: source.TokenCount,
		Metadata:   source.Metadata,
	}
	m.conversations[newID] = forked
	return nil
}

// mockProvider implements providers.Provider for testing
type mockProvider struct{}

func (m *mockProvider) ID() string { return "mock" }
func (m *mockProvider) Predict(_ context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{}, nil
}
func (m *mockProvider) PredictStream(_ context.Context, _ providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, nil
}
func (m *mockProvider) SupportsStreaming() bool      { return false }
func (m *mockProvider) ShouldIncludeRawOutput() bool { return false }
func (m *mockProvider) Close() error                 { return nil }
func (m *mockProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
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
		_, err := Resume("nonexistent", packFile, "main", WithSkipSchemaValidation(), WithStateStore(store))
		assert.ErrorIs(t, err, ErrConversationNotFound)
	})

	t.Run("state load error", func(t *testing.T) {
		store := newMockStore()
		store.loadErr = errors.New("storage failure")
		_, err := Resume("conv-123", packFile, "main", WithSkipSchemaValidation(), WithStateStore(store))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "storage failure")
	})

	t.Run("successful resume", func(t *testing.T) {
		store := newMockStore()
		// Save a conversation state
		store.conversations["conv-123"] = &statestore.ConversationState{
			ID: "conv-123",
		}

		conv, err := Resume("conv-123", packFile, "main", WithSkipSchemaValidation(), WithStateStore(store))
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
			WithSkipSchemaValidation(),
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

		_, err := Resume("conv-123", "/nonexistent/pack.json", "main", WithSkipSchemaValidation(), WithStateStore(store))
		assert.Error(t, err)
	})
}

func TestApplyOptions(t *testing.T) {
	t.Run("empty options", func(t *testing.T) {
		cfg, err := applyOptions("test-prompt", nil)
		require.NoError(t, err)
		assert.Equal(t, "test-prompt", cfg.promptName)
	})

	t.Run("with valid options", func(t *testing.T) {
		cfg, err := applyOptions("test-prompt", []Option{
			WithModel("gpt-4"),
			WithAPIKey("test-key"),
		})
		require.NoError(t, err)
		assert.Equal(t, "gpt-4", cfg.model)
		assert.Equal(t, "test-key", cfg.apiKey)
	})

	t.Run("with error option", func(t *testing.T) {
		_, err := applyOptions("test-prompt", []Option{
			func(c *config) error { return errors.New("test error") },
		})
		assert.Error(t, err)
	})
}

func TestLoadAndValidatePack(t *testing.T) {
	// Create a valid pack file
	dir := t.TempDir()
	packFile := filepath.Join(dir, "test.pack.json")
	packContent := `{
		"name": "test-pack",
		"version": "v1",
		"prompts": {
			"main": {
				"system_template": "You are helpful."
			}
		}
	}`
	err := os.WriteFile(packFile, []byte(packContent), 0644)
	require.NoError(t, err)

	// Config with schema validation skipped (test fixtures aren't schema-compliant)
	cfg := &config{skipSchemaValidation: true}

	t.Run("valid pack and prompt", func(t *testing.T) {
		p, prompt, err := loadAndValidatePack(packFile, "main", cfg)
		require.NoError(t, err)
		assert.NotNil(t, p)
		assert.NotNil(t, prompt)
	})

	t.Run("nonexistent pack", func(t *testing.T) {
		_, _, err := loadAndValidatePack("/nonexistent.pack.json", "main", cfg)
		assert.Error(t, err)
	})

	t.Run("nonexistent prompt", func(t *testing.T) {
		_, _, err := loadAndValidatePack(packFile, "nonexistent", cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestResolveProviderHelper(t *testing.T) {
	t.Run("with provided provider", func(t *testing.T) {
		mock := &mockProvider{}
		cfg := &config{provider: mock}
		prov, err := resolveProvider(cfg)
		require.NoError(t, err)
		assert.Equal(t, mock, prov)
	})

	t.Run("auto-detect with api key", func(t *testing.T) {
		cfg := &config{apiKey: "test-key"}
		prov, err := resolveProvider(cfg)
		require.NoError(t, err)
		assert.NotNil(t, prov)
	})
}

func TestInitMCPRegistry(t *testing.T) {
	t.Run("no servers configured", func(t *testing.T) {
		conv := &Conversation{}
		cfg := &config{}
		err := initMCPRegistry(conv, cfg)
		require.NoError(t, err)
		assert.Nil(t, conv.mcpRegistry)
	})
}

func TestApplyDefaultVariables(t *testing.T) {
	t.Run("applies defaults", func(t *testing.T) {
		conv := &Conversation{
			config: &config{},
		}
		prompt := &pack.Prompt{
			Variables: []pack.Variable{
				{Name: "var1", Default: "default1"},
				{Name: "var2", Default: ""},
				{Name: "var3", Default: "default3"},
			},
		}
		applyDefaultVariables(conv, prompt)

		// Variables are now stored in config.initialVariables, not directly in conversation
		assert.Equal(t, "default1", conv.config.initialVariables["var1"])
		assert.Empty(t, conv.config.initialVariables["var2"])
		assert.Equal(t, "default3", conv.config.initialVariables["var3"])
	})
}

func TestInitEventBus(t *testing.T) {
	t.Run("creates new bus when not provided", func(t *testing.T) {
		conv := &Conversation{config: &config{}}
		cfg := &config{}
		initEventBus(conv, cfg)
		assert.NotNil(t, cfg.eventBus)
	})

	t.Run("uses provided event bus", func(t *testing.T) {
		conv := &Conversation{config: &config{}}
		bus := events.NewEventBus()
		cfg := &config{eventBus: bus}
		initEventBus(conv, cfg)
		assert.Equal(t, bus, cfg.eventBus)
	})
}

func TestOpenDuplex(t *testing.T) {
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

	t.Run("fails with non-streaming provider", func(t *testing.T) {
		// Use a regular provider that doesn't support streaming input
		mockProv := &mockProvider{}
		_, err := OpenDuplex(packFile, "main",
			WithProvider(mockProv),
			WithSkipSchemaValidation(),
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not support duplex streaming")
	})

	t.Run("succeeds with streaming provider", func(t *testing.T) {
		// Use a provider that supports streaming input
		mockProv := &mockStreamingProvider{}
		conv, err := OpenDuplex(packFile, "main",
			WithProvider(mockProv),
			WithSkipSchemaValidation(),
		)
		require.NoError(t, err)
		assert.NotNil(t, conv)
		assert.Equal(t, DuplexMode, conv.mode)
		assert.NotNil(t, conv.duplexSession)
		assert.Nil(t, conv.unarySession)
		defer conv.Close()
	})

	t.Run("fails with invalid pack", func(t *testing.T) {
		mockProv := &mockStreamingProvider{}
		_, err := OpenDuplex("/nonexistent.pack.json", "main",
			WithProvider(mockProv),
		)
		assert.Error(t, err)
	})

	t.Run("fails with non-existent prompt", func(t *testing.T) {
		mockProv := &mockStreamingProvider{}
		_, err := OpenDuplex(packFile, "nonexistent",
			WithProvider(mockProv),
			WithSkipSchemaValidation(),
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// mockStreamingProvider implements providers.StreamInputSupport for testing
type mockStreamingProvider struct {
	mockProvider
}

func (m *mockStreamingProvider) CreateStreamSession(ctx context.Context, cfg *providers.StreamingInputConfig) (providers.StreamInputSession, error) {
	return &mockStreamSession{}, nil
}

func (m *mockStreamingProvider) SupportsStreamInput() []string {
	return []string{types.ContentTypeAudio}
}

func (m *mockStreamingProvider) GetStreamingCapabilities() providers.StreamingCapabilities {
	return providers.StreamingCapabilities{}
}

// mockStreamSession implements providers.StreamInputSession
type mockStreamSession struct{}

func (m *mockStreamSession) SendChunk(ctx context.Context, chunk *types.MediaChunk) error {
	return nil
}

func (m *mockStreamSession) SendText(ctx context.Context, text string) error {
	return nil
}

func (m *mockStreamSession) Response() <-chan providers.StreamChunk {
	ch := make(chan providers.StreamChunk)
	close(ch)
	return ch
}

func (m *mockStreamSession) Close() error {
	return nil
}

func (m *mockStreamSession) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (m *mockStreamSession) Error() error {
	return nil
}
