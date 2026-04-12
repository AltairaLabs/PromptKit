package engine

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/memory"
)

// initMemory sets up the memory subsystem if config.Memory is set.
// Creates an InMemoryStore and registers the memory executor + tools.
// The actual tool execution is real (not mocked) — the InMemoryStore
// persists data across turns within a run.
func (e *Engine) initMemory() error {
	if e.config.Memory == nil {
		return nil
	}

	store := memory.NewInMemoryStore()

	// Register executor with nil scope — per-run scope is set in executeRun
	exec := memory.NewExecutor(store, nil)
	e.toolRegistry.RegisterExecutor(exec)
	memory.RegisterMemoryTools(e.toolRegistry)

	e.memoryStore = store
	logger.Info("Memory subsystem initialized", "store", "in-memory")
	return nil
}

// registerMemoryForRun creates a per-run memory executor with scope isolation.
// Called before each scenario execution to ensure memory is scoped to the run.
func (e *Engine) registerMemoryForRun(scenarioID, runID string) map[string]string {
	if e.memoryStore == nil {
		return nil
	}
	scope := map[string]string{
		"scenario": scenarioID,
		"run":      runID,
	}
	exec := memory.NewExecutor(e.memoryStore, scope)
	e.toolRegistry.RegisterExecutor(exec)
	return scope
}

// seedMemoriesForRun pre-populates the memory store with seed entries from the
// scenario config. Called after registerMemoryForRun and before the first turn.
func (e *Engine) seedMemoriesForRun(scenario *config.Scenario, scope map[string]string) error {
	if e.memoryStore == nil || len(scenario.SeedMemories) == 0 {
		return nil
	}
	ctx := context.Background()
	for i, entry := range scenario.SeedMemories {
		if entry.Content == "" {
			return fmt.Errorf("seed_memories[%d]: empty content", i)
		}
		memType := entry.Type
		if memType == "" {
			memType = "general"
		}
		confidence := entry.Confidence
		if confidence <= 0 {
			confidence = 0.8
		}
		m := &memory.Memory{
			Type:       memType,
			Content:    entry.Content,
			Confidence: confidence,
			Metadata:   entry.Metadata,
			Scope:      scope,
		}
		m.SetProvenance(memory.ProvenanceOperatorCurated)
		if err := e.memoryStore.Save(ctx, m); err != nil {
			return fmt.Errorf("seed_memories[%d]: %w", i, err)
		}
	}
	logger.Info("Seeded memories for scenario", "scenario", scenario.ID, "count", len(scenario.SeedMemories))
	return nil
}
