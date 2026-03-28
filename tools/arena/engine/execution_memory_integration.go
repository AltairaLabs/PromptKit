package engine

import (
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
func (e *Engine) registerMemoryForRun(scenarioID, runID string) {
	if e.memoryStore == nil {
		return
	}
	scope := map[string]string{
		"scenario": scenarioID,
		"run":      runID,
	}
	exec := memory.NewExecutor(e.memoryStore, scope)
	e.toolRegistry.RegisterExecutor(exec)
}
