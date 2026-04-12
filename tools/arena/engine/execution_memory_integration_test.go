package engine

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedMemoriesForRun(t *testing.T) {
	store := memory.NewInMemoryStore()

	eng := &Engine{
		memoryStore: store,
	}

	scenario := &config.Scenario{
		ID: "test-scenario",
		SeedMemories: []config.SeedMemoryEntry{
			{
				Content: "Customer prefers dark mode.",
			},
			{
				Content:    "Order #1023 was delayed 5 days.",
				Type:       "episodic",
				Confidence: 0.95,
				Metadata:   map[string]any{"tags": []string{"order-1023"}},
			},
		},
	}

	scope := map[string]string{"scenario": "test-scenario", "run": "run-1"}
	err := eng.seedMemoriesForRun(scenario, scope)
	require.NoError(t, err)

	// Verify memories were saved with correct scope
	memories, err := store.List(t.Context(), scope, memory.ListOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, memories, 2)

	// First memory — defaults applied
	assert.Equal(t, "Customer prefers dark mode.", memories[0].Content)
	assert.Equal(t, "general", memories[0].Type)
	assert.Equal(t, 0.8, memories[0].Confidence)
	assert.Equal(t, scope, memories[0].Scope)

	// Second memory — explicit values preserved
	assert.Equal(t, "Order #1023 was delayed 5 days.", memories[1].Content)
	assert.Equal(t, "episodic", memories[1].Type)
	assert.Equal(t, 0.95, memories[1].Confidence)
	assert.Contains(t, memories[1].Metadata, "tags")
}

func TestSeedMemoriesForRun_NoMemoryStore(t *testing.T) {
	eng := &Engine{memoryStore: nil}
	scenario := &config.Scenario{
		SeedMemories: []config.SeedMemoryEntry{{Content: "test"}},
	}
	// Should be a no-op, not an error
	err := eng.seedMemoriesForRun(scenario, nil)
	assert.NoError(t, err)
}

func TestSeedMemoriesForRun_EmptySeeds(t *testing.T) {
	store := memory.NewInMemoryStore()
	eng := &Engine{memoryStore: store}
	scenario := &config.Scenario{}

	err := eng.seedMemoriesForRun(scenario, map[string]string{"run": "1"})
	assert.NoError(t, err)

	memories, _ := store.List(t.Context(), map[string]string{"run": "1"}, memory.ListOptions{Limit: 10})
	assert.Empty(t, memories)
}

func TestSeedMemoriesForRun_EmptyContent(t *testing.T) {
	store := memory.NewInMemoryStore()
	eng := &Engine{memoryStore: store}
	scenario := &config.Scenario{
		SeedMemories: []config.SeedMemoryEntry{{Content: ""}},
	}

	err := eng.seedMemoriesForRun(scenario, map[string]string{"run": "1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty content")
}
