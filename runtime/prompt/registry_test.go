package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestRegistry creates a registry with no repository for testing
// This allows direct manipulation of cache fields for unit tests
func createTestRegistry() *Registry {
	return &Registry{
		promptCache:      make(map[string]*PromptConfig),
		fragmentCache:    make(map[string]*Fragment),
		fragmentResolver: &FragmentResolver{fragmentCache: make(map[string]*Fragment)},
	}
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
