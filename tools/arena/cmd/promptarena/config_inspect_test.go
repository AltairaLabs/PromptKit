package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectInspectionData_EmptyConfig(t *testing.T) {
	// Create minimal valid config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "arena.yaml")

	configContent := `
apiVersion: v1
kind: Arena
metadata:
  name: test-arena
prompts:
  test-prompt:
    content: "Hello {{name}}"
providers:
  test-provider:
    type: mock
    model: test-model
scenarios:
  test-scenario:
    prompt: test-prompt
    provider: test-provider
    turns:
      - user: "Hello"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Load and inspect
	cfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)

	data := collectInspectionData(cfg, configPath)

	// Empty config returns empty slices
	assert.NotNil(t, data)
	assert.Empty(t, data.PromptConfigs)
	assert.Empty(t, data.Providers)
	assert.Empty(t, data.Scenarios)
}

func TestOutputJSON_ValidData(t *testing.T) {
	data := &InspectionData{
		ConfigFile: "test.yaml",
		PromptConfigs: []PromptInspectData{
			{ID: "prompt1", File: "prompts/prompt1.yaml"},
			{ID: "prompt2", File: "prompts/prompt2.yaml"},
		},
		Providers: []ProviderInspectData{
			{ID: "openai", File: "providers/openai.yaml", Type: "openai"},
		},
		Scenarios: []ScenarioInspectData{
			{ID: "scenario1", File: "scenarios/scenario1.yaml"},
		},
		AvailableTaskTypes: []string{"task1", "task2"},
		ValidationPassed:   true,
		ValidationWarnings: 0,
	}

	// Capture output to temp file
	tmpFile := filepath.Join(t.TempDir(), "output.json")
	originalStdout := os.Stdout
	defer func() { os.Stdout = originalStdout }()

	f, err := os.Create(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	os.Stdout = f

	err = outputJSON(data)
	require.NoError(t, err)

	// Reset stdout before reading file
	os.Stdout = originalStdout
	f.Close()

	// Verify JSON is valid
	content, err := os.ReadFile(tmpFile)
	require.NoError(t, err)

	var result InspectionData
	err = json.Unmarshal(content, &result)
	require.NoError(t, err)

	assert.Equal(t, len(data.PromptConfigs), len(result.PromptConfigs))
	assert.Equal(t, len(data.Providers), len(result.Providers))
	assert.True(t, result.ValidationPassed)
}

func TestOutputText_ValidData(t *testing.T) {
	data := &InspectionData{
		ConfigFile: "test.yaml",
		PromptConfigs: []PromptInspectData{
			{ID: "prompt1", File: "prompts/prompt1.yaml"},
		},
		Providers: []ProviderInspectData{
			{ID: "openai", File: "providers/openai.yaml", Type: "openai"},
		},
		Scenarios: []ScenarioInspectData{
			{ID: "scenario1", File: "scenarios/scenario1.yaml"},
		},
		AvailableTaskTypes: []string{"task1"},
		ValidationPassed:   true,
	}

	// Just verify it doesn't crash
	err := outputText(data, nil)
	assert.NoError(t, err)
}

func TestOutputText_WithValidationError(t *testing.T) {
	data := &InspectionData{
		ConfigFile: "test.yaml",
		PromptConfigs: []PromptInspectData{
			{ID: "prompt1", File: "prompts/prompt1.yaml"},
		},
		Providers:          []ProviderInspectData{},
		Scenarios:          []ScenarioInspectData{},
		AvailableTaskTypes: []string{},
		ValidationPassed:   false,
		ValidationError:    "Missing required field: provider",
		ValidationWarnings: 2,
	}

	// Just verify it doesn't crash
	err := outputText(data, nil)
	assert.NoError(t, err)
}

func TestOutputJSON_WithCacheStats(t *testing.T) {
	data := &InspectionData{
		ConfigFile: "test.yaml",
		PromptConfigs: []PromptInspectData{
			{ID: "prompt1", File: "prompts/prompt1.yaml"},
		},
		Providers: []ProviderInspectData{
			{ID: "openai", File: "providers/openai.yaml", Type: "openai"},
		},
		Scenarios: []ScenarioInspectData{
			{ID: "scenario1", File: "scenarios/scenario1.yaml"},
		},
		AvailableTaskTypes: []string{"task1"},
		ValidationPassed:   true,
		CacheStats: &CacheStatsData{
			PromptCache: CacheInfo{
				Size:    5,
				Entries: []string{"entry1", "entry2"},
				Hits:    10,
			},
			FragmentCache: CacheInfo{
				Size:    3,
				Entries: []string{"frag1"},
				Hits:    7,
			},
		},
	}

	// Capture output
	tmpFile := filepath.Join(t.TempDir(), "output.json")
	originalStdout := os.Stdout
	defer func() { os.Stdout = originalStdout }()

	f, err := os.Create(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	os.Stdout = f

	err = outputJSON(data)
	require.NoError(t, err)

	// Reset and verify
	os.Stdout = originalStdout
	f.Close()

	content, err := os.ReadFile(tmpFile)
	require.NoError(t, err)

	var result InspectionData
	err = json.Unmarshal(content, &result)
	require.NoError(t, err)

	assert.NotNil(t, result.CacheStats)
	assert.Equal(t, 5, result.CacheStats.PromptCache.Size)
	assert.Equal(t, 10, result.CacheStats.PromptCache.Hits)
}

func TestOutputText_WithSelfPlayRoles(t *testing.T) {
	data := &InspectionData{
		ConfigFile: "test.yaml",
		PromptConfigs: []PromptInspectData{
			{ID: "prompt1", File: "prompts/prompt1.yaml"},
		},
		Providers: []ProviderInspectData{
			{ID: "openai", File: "providers/openai.yaml", Type: "openai"},
		},
		Scenarios: []ScenarioInspectData{
			{ID: "scenario1", File: "scenarios/scenario1.yaml"},
		},
		SelfPlayRoles:      []string{"user", "assistant"},
		AvailableTaskTypes: []string{"task1"},
		ValidationPassed:   true,
	}

	err := outputText(data, nil)
	assert.NoError(t, err)
}

// Helper function to load config (wrapping the actual implementation)
func loadConfigHelper(path string) (*config.Config, error) {
	return config.LoadConfig(path)
}

func TestCollectCacheStats(t *testing.T) {
	tests := []struct {
		name                 string
		cfg                  *config.Config
		expectedPromptSize   int
		expectedFragmentSize int
		expectSelfPlayCache  bool
	}{
		{
			name: "empty config",
			cfg: &config.Config{
				LoadedPromptConfigs: map[string]*config.PromptConfigData{},
				LoadedPersonas:      map[string]*config.UserPersonaPack{},
				SelfPlay:            nil,
			},
			expectedPromptSize:   0,
			expectedFragmentSize: 0,
			expectSelfPlayCache:  false,
		},
		{
			name: "config with prompts",
			cfg: &config.Config{
				LoadedPromptConfigs: map[string]*config.PromptConfigData{
					"task1": {},
					"task2": {},
					"task3": {},
				},
				LoadedPersonas: map[string]*config.UserPersonaPack{},
				SelfPlay:       nil,
			},
			expectedPromptSize:   3,
			expectedFragmentSize: 0,
			expectSelfPlayCache:  false,
		},
		{
			name: "config with self-play",
			cfg: &config.Config{
				LoadedPromptConfigs: map[string]*config.PromptConfigData{
					"task1": {},
				},
				LoadedPersonas: map[string]*config.UserPersonaPack{
					"persona1": {},
					"persona2": {},
				},
				SelfPlay: &config.SelfPlayConfig{
					Roles: []config.SelfPlayRoleGroup{
						{ID: "assistant"},
						{ID: "user"},
					},
				},
			},
			expectedPromptSize:   1,
			expectedFragmentSize: 0,
			expectSelfPlayCache:  true,
		},
		{
			name: "config without self-play roles",
			cfg: &config.Config{
				LoadedPromptConfigs: map[string]*config.PromptConfigData{
					"task1": {},
				},
				LoadedPersonas: map[string]*config.UserPersonaPack{
					"persona1": {},
				},
				SelfPlay: &config.SelfPlayConfig{
					Roles: nil,
				},
			},
			expectedPromptSize:   1,
			expectedFragmentSize: 0,
			expectSelfPlayCache:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collectCacheStats(tt.cfg)

			require.NotNil(t, result)
			assert.Equal(t, tt.expectedPromptSize, result.PromptCache.Size)
			assert.Equal(t, tt.expectedFragmentSize, result.FragmentCache.Size)

			if tt.expectSelfPlayCache {
				assert.NotZero(t, result.SelfPlayCache.Size)
			} else {
				assert.Zero(t, result.SelfPlayCache.Size)
			}
		})
	}
}
