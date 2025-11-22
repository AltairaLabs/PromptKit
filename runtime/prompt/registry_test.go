package prompt

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPromptRepository is a simple in-memory repository for testing
type mockPromptRepository struct {
	prompts   map[string]*PromptConfig
	fragments map[string]*Fragment
}

func newMockPromptRepository() *mockPromptRepository {
	return &mockPromptRepository{
		prompts:   make(map[string]*PromptConfig),
		fragments: make(map[string]*Fragment),
	}
}

func (m *mockPromptRepository) LoadPrompt(taskType string) (*PromptConfig, error) {
	if prompt, ok := m.prompts[taskType]; ok {
		return prompt, nil
	}
	return nil, fmt.Errorf("prompt not found: %s", taskType)
}

func (m *mockPromptRepository) LoadFragment(name string, relativePath string, baseDir string) (*Fragment, error) {
	if fragment, ok := m.fragments[name]; ok {
		return fragment, nil
	}
	return nil, fmt.Errorf("fragment not found: %s", name)
}

func (m *mockPromptRepository) ListPrompts() ([]string, error) {
	prompts := make([]string, 0, len(m.prompts))
	for taskType := range m.prompts {
		prompts = append(prompts, taskType)
	}
	return prompts, nil
}

func (m *mockPromptRepository) SavePrompt(config *PromptConfig) error {
	m.prompts[config.Spec.TaskType] = config
	return nil
}

// createTestRegistry creates a registry with no repository for testing
// This allows direct manipulation of cache fields for unit tests
func createTestRegistry() *Registry {
	return &Registry{
		promptCache:      make(map[string]*PromptConfig),
		fragmentCache:    make(map[string]*Fragment),
		fragmentResolver: &FragmentResolver{fragmentCache: make(map[string]*Fragment)},
	}
}

// createTestRegistryWithRepo creates a registry with a mock repository for testing
func createTestRegistryWithRepo() *Registry {
	return NewRegistryWithRepository(newMockPromptRepository())
}

func TestRegistry_GetAvailableTaskTypes(t *testing.T) {
	t.Run("returns task types from cache", func(t *testing.T) {
		reg := createTestRegistry()
		reg.promptCache["task1"] = &PromptConfig{
			Spec: PromptSpec{TaskType: "task1"},
		}
		reg.promptCache["task2"] = &PromptConfig{
			Spec: PromptSpec{TaskType: "task2"},
		}

		taskTypes := reg.GetAvailableTaskTypes()
		assert.Len(t, taskTypes, 2)
		assert.Contains(t, taskTypes, "task1")
		assert.Contains(t, taskTypes, "task2")
	})

	// Test removed: configMappings field was deleted in config-first refactoring

	t.Run("handles empty registry", func(t *testing.T) {
		reg := createTestRegistry()
		taskTypes := reg.GetAvailableTaskTypes()
		assert.Empty(t, taskTypes)
	})
}

func TestRegistry_GetAvailableRegions(t *testing.T) {
	t.Run("detects US region", func(t *testing.T) {
		reg := createTestRegistry()
		reg.fragmentCache["persona_support_us"] = &Fragment{Content: "test"}

		regions := reg.GetAvailableRegions()
		assert.Len(t, regions, 1)
		assert.Contains(t, regions, "us")
	})

	t.Run("detects multiple regions", func(t *testing.T) {
		reg := createTestRegistry()
		reg.fragmentCache["persona_support_us"] = &Fragment{Content: "test"}
		reg.fragmentCache["persona_assistant_uk"] = &Fragment{Content: "test"}
		reg.fragmentCache["persona_agent_au"] = &Fragment{Content: "test"}

		regions := reg.GetAvailableRegions()
		assert.Len(t, regions, 3)
		assert.Contains(t, regions, "us")
		assert.Contains(t, regions, "uk")
		assert.Contains(t, regions, "au")
	})

	t.Run("returns empty for no region-specific fragments", func(t *testing.T) {
		reg := createTestRegistry()
		reg.fragmentCache["persona_generic"] = &Fragment{Content: "test"}

		regions := reg.GetAvailableRegions()
		assert.Empty(t, regions)
	})
}

func TestRegistry_GetLoadedPrompts(t *testing.T) {
	t.Run("returns loaded prompts", func(t *testing.T) {
		reg := createTestRegistry()
		reg.promptCache["prompt1"] = &PromptConfig{Spec: PromptSpec{TaskType: "prompt1"}}
		reg.promptCache["prompt2"] = &PromptConfig{Spec: PromptSpec{TaskType: "prompt2"}}

		prompts := reg.GetLoadedPrompts()
		assert.Len(t, prompts, 2)
		assert.Contains(t, prompts, "prompt1")
		assert.Contains(t, prompts, "prompt2")
	})

	t.Run("returns empty for no loaded prompts", func(t *testing.T) {
		reg := createTestRegistry()
		prompts := reg.GetLoadedPrompts()
		assert.Empty(t, prompts)
	})
}

func TestRegistry_GetLoadedFragments(t *testing.T) {
	t.Run("returns loaded fragment paths", func(t *testing.T) {
		reg := createTestRegistry()
		reg.fragmentCache["path/to/fragment1"] = &Fragment{Content: "test"}
		reg.fragmentCache["path/to/fragment2"] = &Fragment{Content: "test"}

		fragments := reg.GetLoadedFragments()
		assert.Len(t, fragments, 2)
		assert.Contains(t, fragments, "path/to/fragment1")
		assert.Contains(t, fragments, "path/to/fragment2")
	})

	t.Run("returns empty for no fragments", func(t *testing.T) {
		reg := createTestRegistry()
		fragments := reg.GetLoadedFragments()
		assert.Empty(t, fragments)
	})
}

func TestRegistry_ClearCache(t *testing.T) {
	reg := createTestRegistry()
	reg.promptCache["task1"] = &PromptConfig{Spec: PromptSpec{TaskType: "task1"}}
	reg.fragmentCache["fragment1"] = &Fragment{Content: "test"}

	reg.ClearCache()

	assert.Empty(t, reg.promptCache)
	assert.Empty(t, reg.fragmentCache)
}

func TestNewRegistryWithRepository(t *testing.T) {
	// Create a mock repository (nil is fine for this test)
	reg := NewRegistryWithRepository(nil)

	require.NotNil(t, reg)
	// Private fields exist but cannot be tested directly
}

// Test removed: NewRegistryWithMappings relied on configMappings field that was deleted in config-first refactoring

// Test removed: SetConfigPath method was deleted in config-first refactoring

func TestRegistry_RegisterConfig(t *testing.T) {
	reg := createTestRegistry()

	config := &PromptConfig{
		Spec: PromptSpec{
			TaskType: "my_task",
		},
	}

	err := reg.RegisterConfig("my_task", config)

	assert.NoError(t, err)
	// Verify it was registered by checking if it shows up in ListTaskTypes
	tasks := reg.ListTaskTypes()
	assert.Contains(t, tasks, "my_task")
}

func TestRegistry_ListTaskTypes(t *testing.T) {
	reg := createTestRegistry()

	t.Run("returns registered configs from cache", func(t *testing.T) {
		// Use RegisterConfig to add to the promptCache
		cfg1 := &PromptConfig{Spec: PromptSpec{TaskType: "task1"}}
		cfg2 := &PromptConfig{Spec: PromptSpec{TaskType: "task2"}}

		_ = reg.RegisterConfig("task1", cfg1)
		_ = reg.RegisterConfig("task2", cfg2)

		tasks := reg.ListTaskTypes()
		assert.Len(t, tasks, 2)
		assert.Contains(t, tasks, "task1")
		assert.Contains(t, tasks, "task2")
	})

	t.Run("returns empty for no registered configs", func(t *testing.T) {
		reg := createTestRegistry()
		tasks := reg.ListTaskTypes()
		assert.Empty(t, tasks)
	})
}

func TestRegistry_GetPromptInfo(t *testing.T) {
	t.Run("returns prompt info for valid task", func(t *testing.T) {
		reg := createTestRegistryWithRepo()

		// Register a config
		cfg := &PromptConfig{
			Spec: PromptSpec{
				TaskType:    "test-task",
				Version:     "1.0.0",
				Description: "Test prompt",
				Fragments: []FragmentRef{
					{Name: "fragment1", Required: true},
					{Name: "fragment2", Required: false},
				},
				RequiredVars: []string{"var1", "var2"},
				OptionalVars: map[string]string{
					"opt1": "default1",
					"opt2": "default2",
				},
				AllowedTools: []string{"tool1", "tool2"},
				ModelOverrides: map[string]ModelOverride{
					"override1": {},
					"override2": {},
				},
			},
		}
		_ = reg.RegisterConfig("test-task", cfg)

		info, err := reg.GetPromptInfo("test-task")

		require.NoError(t, err)
		assert.Equal(t, "test-task", info.TaskType)
		assert.Equal(t, "1.0.0", info.Version)
		assert.Equal(t, "Test prompt", info.Description)
		assert.Equal(t, 2, info.FragmentCount)
		assert.Equal(t, []string{"var1", "var2"}, info.RequiredVars)
		assert.ElementsMatch(t, []string{"opt1", "opt2"}, info.OptionalVars)
		assert.Equal(t, []string{"tool1", "tool2"}, info.ToolAllowlist)
		assert.ElementsMatch(t, []string{"override1", "override2"}, info.ModelOverrides)
	})

	t.Run("returns error for non-existent task", func(t *testing.T) {
		reg := createTestRegistryWithRepo()

		info, err := reg.GetPromptInfo("non-existent-task")

		assert.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "failed to get prompt info")
	})
}

// Tests for refactored helper methods

func TestRegistry_PrepareVariables(t *testing.T) {
	reg := createTestRegistry()

	tests := []struct {
		name        string
		config      *PromptConfig
		vars        map[string]string
		wantErr     bool
		expectedLen int
	}{
		{
			name: "valid required and optional vars",
			config: &PromptConfig{
				Spec: PromptSpec{
					RequiredVars: []string{"var1", "var2"},
					OptionalVars: map[string]string{
						"opt1": "default1",
						"opt2": "default2",
					},
				},
			},
			vars: map[string]string{
				"var1": "value1",
				"var2": "value2",
			},
			wantErr:     false,
			expectedLen: 4, // 2 required + 2 optional
		},
		{
			name: "override optional vars",
			config: &PromptConfig{
				Spec: PromptSpec{
					RequiredVars: []string{"var1"},
					OptionalVars: map[string]string{
						"opt1": "default1",
					},
				},
			},
			vars: map[string]string{
				"var1": "value1",
				"opt1": "override1",
			},
			wantErr:     false,
			expectedLen: 2, // 1 required + 1 optional (overridden)
		},
		{
			name: "missing required var",
			config: &PromptConfig{
				Spec: PromptSpec{
					RequiredVars: []string{"var1", "var2"},
					OptionalVars: map[string]string{},
				},
			},
			vars: map[string]string{
				"var1": "value1",
				// var2 missing
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := reg.prepareVariables(tt.config, tt.vars, "test-activity")

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Len(t, result, tt.expectedLen)
		})
	}
}

func TestRegistry_ApplyModelOverrides(t *testing.T) {
	reg := createTestRegistry()

	tests := []struct {
		name           string
		config         *PromptConfig
		model          string
		expectedResult string
	}{
		{
			name: "no model specified",
			config: &PromptConfig{
				Spec: PromptSpec{
					SystemTemplate: "base template",
				},
			},
			model:          "",
			expectedResult: "base template",
		},
		{
			name: "model with template override",
			config: &PromptConfig{
				Spec: PromptSpec{
					SystemTemplate: "base template",
					ModelOverrides: map[string]ModelOverride{
						"gpt-4": {
							SystemTemplate: "gpt-4 template",
						},
					},
				},
			},
			model:          "gpt-4",
			expectedResult: "gpt-4 template",
		},
		{
			name: "model with suffix override",
			config: &PromptConfig{
				Spec: PromptSpec{
					SystemTemplate: "base template",
					ModelOverrides: map[string]ModelOverride{
						"gpt-4": {
							SystemTemplateSuffix: " additional instructions",
						},
					},
				},
			},
			model:          "gpt-4",
			expectedResult: "base template additional instructions",
		},
		{
			name: "model with both template and suffix",
			config: &PromptConfig{
				Spec: PromptSpec{
					SystemTemplate: "base template",
					ModelOverrides: map[string]ModelOverride{
						"gpt-4": {
							SystemTemplate:       "gpt-4 template",
							SystemTemplateSuffix: " with suffix",
						},
					},
				},
			},
			model:          "gpt-4",
			expectedResult: "gpt-4 template with suffix",
		},
		{
			name: "model without override uses base",
			config: &PromptConfig{
				Spec: PromptSpec{
					SystemTemplate: "base template",
					ModelOverrides: map[string]ModelOverride{
						"gpt-4": {
							SystemTemplate: "gpt-4 template",
						},
					},
				},
			},
			model:          "claude-3",
			expectedResult: "base template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reg.applyModelOverrides(tt.config, tt.model)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestRegistry_MergeVars(t *testing.T) {
	reg := createTestRegistry()

	tests := []struct {
		name     string
		config   *PromptConfig
		vars     map[string]string
		expected map[string]string
	}{
		{
			name: "no optional vars",
			config: &PromptConfig{
				Spec: PromptSpec{
					OptionalVars: map[string]string{},
				},
			},
			vars: map[string]string{
				"var1": "value1",
			},
			expected: map[string]string{
				"var1": "value1",
			},
		},
		{
			name: "optional vars with no overrides",
			config: &PromptConfig{
				Spec: PromptSpec{
					OptionalVars: map[string]string{
						"opt1": "default1",
						"opt2": "default2",
					},
				},
			},
			vars: map[string]string{
				"var1": "value1",
			},
			expected: map[string]string{
				"opt1": "default1",
				"opt2": "default2",
				"var1": "value1",
			},
		},
		{
			name: "override optional vars",
			config: &PromptConfig{
				Spec: PromptSpec{
					OptionalVars: map[string]string{
						"opt1": "default1",
						"opt2": "default2",
					},
				},
			},
			vars: map[string]string{
				"opt1": "override1", // Override default
				"var1": "value1",
			},
			expected: map[string]string{
				"opt1": "override1",
				"opt2": "default2",
				"var1": "value1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reg.mergeVars(tt.config, tt.vars)

			assert.Len(t, result, len(tt.expected))
			for key, expectedVal := range tt.expected {
				assert.Equal(t, expectedVal, result[key], "Variable %s mismatch", key)
			}
		})
	}
}
